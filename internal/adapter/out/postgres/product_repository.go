package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"elvan-catalog-api/internal/adapter/out/postgres/sqlcgen"
	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

const (
	defaultPageSize = 24
	maxPageSize     = 100
)

// ProductRepository adalah implementasi port.ProductRepository di Postgres.
type ProductRepository struct {
	pool *pgxpool.Pool
}

var _ port.ProductRepository = (*ProductRepository)(nil)

// NewProductRepository membuat repository produk di atas pool.
func NewProductRepository(pool *pgxpool.Pool) *ProductRepository {
	return &ProductRepository{pool: pool}
}

// List mengembalikan satu halaman proyeksi katalog dengan keyset pagination.
// Cursor dianggap opaque; cursor milik `sort` lain ditolak.
func (r *ProductRepository) List(ctx context.Context, q port.ProductQuery) (port.ProductPage, error) {
	if q.Sort == "" {
		q.Sort = domain.SortDefault
	}
	if !q.Sort.Valid() {
		return port.ProductPage{}, sortError(q.Sort)
	}

	limit := q.Limit
	if limit <= 0 {
		limit = defaultPageSize
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}

	cursor, err := decodeCursor(q.Cursor)
	if err != nil {
		return port.ProductPage{}, err
	}
	if cursor != nil && cursor.Sort != q.Sort {
		return port.ProductPage{}, cursorError("cursor milik urutan lain; mulai dari halaman pertama")
	}

	// Ambil limit+1 supaya bisa menentukan hasMore tanpa count terpisah.
	products, err := r.listPage(ctx, q, cursor, int32(limit+1))
	if err != nil {
		return port.ProductPage{}, err
	}

	hasMore := len(products) > limit
	if hasMore {
		products = products[:limit]
	}

	page := port.ProductPage{Products: products, HasMore: hasMore}
	if hasMore && len(products) > 0 {
		page.Cursor = encodeCursor(cursorFromProduct(q.Sort, products[len(products)-1]))
	}
	return page, nil
}

// ListAll mengembalikan proyeksi ringan seluruh katalog (GET /catalog).
func (r *ProductRepository) ListAll(ctx context.Context, includeInactive bool) ([]domain.CatalogProduct, error) {
	rows, err := querier(ctx, r.pool).ListAllProducts(ctx, includeInactive)
	if err != nil {
		return nil, MapError(err)
	}
	out := make([]domain.CatalogProduct, 0, len(rows))
	for _, row := range rows {
		out = append(out, newCatalogProduct(
			row.ID, row.Name, row.Slug, row.Price,
			row.RatingRate, row.RatingCount, row.IsActive, row.CreatedAt,
			row.Category, row.Brand, row.Thumbnail,
		))
	}
	return out, nil
}

// GetByID mengambil produk lengkap (termasuk gambar). `id` boleh berupa UUID
// atau id legacy Firestore; yang terakhir menjaga URL lama tetap hidup setelah
// cutover.
func (r *ProductRepository) GetByID(ctx context.Context, id string) (*domain.Product, error) {
	qs := querier(ctx, r.pool)

	if uid, err := uuid.Parse(id); err == nil {
		row, err := qs.GetProductByID(ctx, uid)
		if err == nil {
			p := productFromRow(row)
			return r.hydrate(ctx, p)
		}
		if !errors.Is(MapError(err), domain.ErrNotFound) {
			return nil, MapError(err)
		}
	}

	row, err := qs.GetProductByLegacyID(ctx, &id)
	if err != nil {
		return nil, MapError(err)
	}
	p := productFromLegacyRow(row)
	return r.hydrate(ctx, p)
}

// Create menyimpan produk baru beserta gambarnya. Pemanggil sebaiknya
// membungkusnya dalam TxManager agar atomik.
func (r *ProductRepository) Create(ctx context.Context, p domain.Product) (*domain.Product, error) {
	row, err := querier(ctx, r.pool).CreateProduct(ctx, sqlcgen.CreateProductParams{
		Name:        p.Name,
		Slug:        p.Slug,
		Price:       p.Price,
		Description: p.Description,
		Category:    p.Category,
		Brand:       p.Brand,
		RatingRate:  p.Rating.Rate,
		RatingCount: int32(p.Rating.Count),
		IsActive:    p.IsActive,
	})
	if err != nil {
		return nil, MapError(err)
	}

	if err := r.replaceImages(ctx, row.ID, p.Images); err != nil {
		return nil, err
	}

	p.ID = row.ID.String()
	p.CreatedAt = timeFrom(row.CreatedAt)
	p.UpdatedAt = timeFrom(row.UpdatedAt)
	return &p, nil
}

// Update mengubah sebagian produk. Bila patch.Images tidak nil, seluruh gambar
// diganti. Bila patch.Rating tidak nil, rating_rate dan rating_count ikut
// di-update; jika nil, nilai existing tidak berubah. Use case bertanggung
// jawab menghitung fileId yang terlepas (dari gambar sebelum update) untuk
// dibersihkan di provider.
func (r *ProductRepository) Update(ctx context.Context, id string, patch domain.ProductPatch) (*domain.Product, error) {
	uid, err := parseID(id)
	if err != nil {
		return nil, err
	}

	params := sqlcgen.UpdateProductParams{
		Name:        patch.Name,
		Slug:        patch.Slug,
		Price:       patch.Price,
		Description: patch.Description,
		Category:    patch.Category,
		Brand:       patch.Brand,
		IsActive:    patch.IsActive,
		ID:          uid,
	}
	if patch.Rating != nil {
		params.RatingRate = numericFromFloat(patch.Rating.Rate)
		count := int32(patch.Rating.Count)
		params.RatingCount = &count
	}

	row, err := querier(ctx, r.pool).UpdateProduct(ctx, params)
	if err != nil {
		return nil, MapError(err)
	}

	if patch.Images != nil {
		if err := r.replaceImages(ctx, uid, *patch.Images); err != nil {
			return nil, err
		}
	}

	return r.GetByID(ctx, row.ID.String())
}

// Delete menghapus produk dan mengembalikan gambarnya supaya pemanggil dapat
// membersihkan file di provider setelah commit.
func (r *ProductRepository) Delete(ctx context.Context, id string) ([]domain.Image, error) {
	product, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	uid, err := uuid.Parse(product.ID)
	if err != nil {
		return nil, domain.ErrNotFound
	}

	affected, err := querier(ctx, r.pool).DeleteProduct(ctx, uid)
	if err != nil {
		return nil, MapError(err)
	}
	if affected == 0 {
		return nil, domain.ErrNotFound
	}
	return product.Images, nil
}

// listPage menjalankan query sesuai sort dan memetakan hasilnya.
func (r *ProductRepository) listPage(
	ctx context.Context, q port.ProductQuery, cur *cursorPayload, rowLimit int32,
) ([]domain.CatalogProduct, error) {
	qs := querier(ctx, r.pool)
	category := nullIfEmpty(q.Category)
	brand := nullIfEmpty(q.Brand)
	search := nullIfEmpty(q.Search)

	switch q.Sort {
	case domain.SortPriceAsc:
		rows, err := qs.ListProductsPriceAsc(ctx, sqlcgen.ListProductsPriceAscParams{
			IncludeInactive: q.IncludeInactive,
			Category:        category, Brand: brand, Search: search,
			AfterPrice: cursorPrice(cur), AfterID: cursorUUID(cur), RowLimit: rowLimit,
		})
		if err != nil {
			return nil, MapError(err)
		}
		out := make([]domain.CatalogProduct, 0, len(rows))
		for _, row := range rows {
			out = append(out, newCatalogProduct(row.ID, row.Name, row.Slug, row.Price,
				row.RatingRate, row.RatingCount, row.IsActive, row.CreatedAt,
				row.Category, row.Brand, row.Thumbnail))
		}
		return out, nil

	case domain.SortPriceDesc:
		rows, err := qs.ListProductsPriceDesc(ctx, sqlcgen.ListProductsPriceDescParams{
			IncludeInactive: q.IncludeInactive,
			Category:        category, Brand: brand, Search: search,
			AfterPrice: cursorPrice(cur), AfterID: cursorUUID(cur), RowLimit: rowLimit,
		})
		if err != nil {
			return nil, MapError(err)
		}
		out := make([]domain.CatalogProduct, 0, len(rows))
		for _, row := range rows {
			out = append(out, newCatalogProduct(row.ID, row.Name, row.Slug, row.Price,
				row.RatingRate, row.RatingCount, row.IsActive, row.CreatedAt,
				row.Category, row.Brand, row.Thumbnail))
		}
		return out, nil

	case domain.SortRatingDesc:
		rate, count := cursorRating(cur)
		rows, err := qs.ListProductsRatingDesc(ctx, sqlcgen.ListProductsRatingDescParams{
			IncludeInactive: q.IncludeInactive,
			Category:        category, Brand: brand, Search: search,
			AfterRatingRate: rate, AfterRatingCount: count,
			AfterID: cursorUUID(cur), RowLimit: rowLimit,
		})
		if err != nil {
			return nil, MapError(err)
		}
		out := make([]domain.CatalogProduct, 0, len(rows))
		for _, row := range rows {
			out = append(out, newCatalogProduct(row.ID, row.Name, row.Slug, row.Price,
				row.RatingRate, row.RatingCount, row.IsActive, row.CreatedAt,
				row.Category, row.Brand, row.Thumbnail))
		}
		return out, nil

	default: // domain.SortDefault
		rows, err := qs.ListProductsDefault(ctx, sqlcgen.ListProductsDefaultParams{
			IncludeInactive: q.IncludeInactive,
			Category:        category, Brand: brand, Search: search,
			AfterCreatedAt: cursorCreatedAt(cur), AfterID: cursorUUID(cur), RowLimit: rowLimit,
		})
		if err != nil {
			return nil, MapError(err)
		}
		out := make([]domain.CatalogProduct, 0, len(rows))
		for _, row := range rows {
			out = append(out, newCatalogProduct(row.ID, row.Name, row.Slug, row.Price,
				row.RatingRate, row.RatingCount, row.IsActive, row.CreatedAt,
				row.Category, row.Brand, row.Thumbnail))
		}
		return out, nil
	}
}

// hydrate memuat gambar untuk sebuah produk.
func (r *ProductRepository) hydrate(ctx context.Context, p domain.Product) (*domain.Product, error) {
	uid, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, domain.ErrNotFound
	}
	images, err := r.loadImages(ctx, uid)
	if err != nil {
		return nil, err
	}
	p.Images = images
	return &p, nil
}

func (r *ProductRepository) loadImages(ctx context.Context, productID uuid.UUID) ([]domain.Image, error) {
	rows, err := querier(ctx, r.pool).ListProductImages(ctx, productID)
	if err != nil {
		return nil, MapError(err)
	}
	out := make([]domain.Image, 0, len(rows))
	for _, row := range rows {
		img := domain.Image{Key: row.Key}
		if row.FileID != nil {
			img.FileID = *row.FileID
		}
		out = append(out, img)
	}
	return out, nil
}

// replaceImages mengganti seluruh gambar produk dengan daftar baru (urut sesuai
// index = position).
func (r *ProductRepository) replaceImages(ctx context.Context, productID uuid.UUID, images []domain.Image) error {
	qs := querier(ctx, r.pool)
	if err := qs.DeleteProductImages(ctx, productID); err != nil {
		return MapError(err)
	}
	for i, img := range images {
		params := sqlcgen.InsertProductImageParams{
			ProductID: productID,
			Position:  int16(i),
			Key:       img.Key,
		}
		if img.FileID != "" {
			fileID := img.FileID
			params.FileID = &fileID
		}
		if err := qs.InsertProductImage(ctx, params); err != nil {
			return MapError(err)
		}
	}
	return nil
}

// --- pemetaan row -> domain -------------------------------------------------

func newCatalogProduct(
	id uuid.UUID, name, slug string, price int64,
	rate float64, count int32, isActive bool, createdAt pgtype.Timestamptz,
	category, brand, thumbnail string,
) domain.CatalogProduct {
	return domain.CatalogProduct{
		ID:        id.String(),
		Name:      name,
		Slug:      slug,
		Price:     price,
		Category:  category,
		Brand:     brand,
		Thumbnail: thumbnail,
		Rating:    domain.Rating{Rate: rate, Count: int(count)},
		IsActive:  isActive,
		CreatedAt: timeFrom(createdAt),
	}
}

func productFromRow(row sqlcgen.GetProductByIDRow) domain.Product {
	return domain.Product{
		ID:          row.ID.String(),
		Name:        row.Name,
		Slug:        row.Slug,
		Price:       row.Price,
		Description: row.Description,
		Category:    row.Category,
		Brand:       row.Brand,
		Rating:      domain.Rating{Rate: row.RatingRate, Count: int(row.RatingCount)},
		IsActive:    row.IsActive,
		CreatedAt:   timeFrom(row.CreatedAt),
		UpdatedAt:   timeFrom(row.UpdatedAt),
	}
}

func productFromLegacyRow(row sqlcgen.GetProductByLegacyIDRow) domain.Product {
	return domain.Product{
		ID:          row.ID.String(),
		Name:        row.Name,
		Slug:        row.Slug,
		Price:       row.Price,
		Description: row.Description,
		Category:    row.Category,
		Brand:       row.Brand,
		Rating:      domain.Rating{Rate: row.RatingRate, Count: int(row.RatingCount)},
		IsActive:    row.IsActive,
		CreatedAt:   timeFrom(row.CreatedAt),
		UpdatedAt:   timeFrom(row.UpdatedAt),
	}
}

// --- bantuan cursor ---------------------------------------------------------

func cursorFromProduct(sort domain.SortOption, p domain.CatalogProduct) cursorPayload {
	c := cursorPayload{Sort: sort, ID: p.ID}
	switch sort {
	case domain.SortPriceAsc, domain.SortPriceDesc:
		price := p.Price
		c.Price = &price
	case domain.SortRatingDesc:
		rate := p.Rating.Rate
		count := int32(p.Rating.Count)
		c.RatingRate = &rate
		c.RatingCount = &count
	default:
		created := p.CreatedAt
		c.CreatedAt = &created
	}
	return c
}

func cursorUUID(cur *cursorPayload) pgtype.UUID {
	if cur == nil {
		return pgtype.UUID{}
	}
	u, err := uuid.Parse(cur.ID)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: u, Valid: true}
}

func cursorPrice(cur *cursorPayload) *int64 {
	if cur == nil {
		return nil
	}
	return cur.Price
}

func cursorCreatedAt(cur *cursorPayload) pgtype.Timestamptz {
	if cur == nil || cur.CreatedAt == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *cur.CreatedAt, Valid: true}
}

func cursorRating(cur *cursorPayload) (pgtype.Numeric, *int32) {
	if cur == nil || cur.RatingRate == nil {
		return pgtype.Numeric{}, nil
	}
	return numericFromFloat(*cur.RatingRate), cur.RatingCount
}

// numericFromFloat memetakan float ke pgtype.Numeric dengan satu desimal
// (skema rating_rate numeric(2,1)).
func numericFromFloat(v float64) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(strconv.FormatFloat(v, 'f', 1, 64))
	return n
}

// --- bantuan umum -----------------------------------------------------------

func nullIfEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func sortError(s domain.SortOption) error {
	v := domain.NewValidationError()
	v.Add("sort", "nilai tidak didukung: "+string(s))
	return v
}
