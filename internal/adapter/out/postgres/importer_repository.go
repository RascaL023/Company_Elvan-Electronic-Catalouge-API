package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"elvan-catalog-api/internal/adapter/out/postgres/sqlcgen"
	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// ImporterRepository mengimplementasikan port.ImporRepository di Postgres.
//
// Setiap method adalah satu statement INSERT ... ON CONFLICT (legacy_id), jadi
// impor idempoten: baris yang sudah punya legacy_id sama diperbarui, bukan
// digandakan. Semua method menghormati transaksi dari context, sehingga satu
// kali impor = satu transaksi (ARCHITECTURE §7).
type ImporterRepository struct {
	pool *pgxpool.Pool
}

var _ port.ImporRepository = (*ImporterRepository)(nil)

// NewImporterRepository membuat repository importer di atas pool.
func NewImporterRepository(pool *pgxpool.Pool) *ImporterRepository {
	return &ImporterRepository{pool: pool}
}

// UpsertCategory menulis kategori berdasarkan legacy_id (idempoten).
func (r *ImporterRepository) UpsertCategory(
	ctx context.Context, c domain.Category, legacyID string,
) (*domain.Category, error) {
	row, err := querier(ctx, r.pool).UpsertCategoryImport(ctx, sqlcgen.UpsertCategoryImportParams{
		Name:        c.Name,
		Slug:        c.Slug,
		Description: c.Description,
		LegacyID:    &legacyID,
		CreatedAt:   timestamptzFrom(c.CreatedAt),
	})
	if err != nil {
		return nil, MapError(err)
	}
	return &domain.Category{
		ID:          row.ID.String(),
		Name:        row.Name,
		Slug:        row.Slug,
		Description: row.Description,
		CreatedAt:   timeFrom(row.CreatedAt),
		UpdatedAt:   timeFrom(row.UpdatedAt),
	}, nil
}

// UpsertBrand menulis brand berdasarkan legacy_id (idempoten).
func (r *ImporterRepository) UpsertBrand(
	ctx context.Context, b domain.Brand, legacyID string,
) (*domain.Brand, error) {
	row, err := querier(ctx, r.pool).UpsertBrandImport(ctx, sqlcgen.UpsertBrandImportParams{
		Name:      b.Name,
		Slug:      b.Slug,
		LegacyID:  &legacyID,
		CreatedAt: timestamptzFrom(b.CreatedAt),
	})
	if err != nil {
		return nil, MapError(err)
	}
	return &domain.Brand{
		ID:        row.ID.String(),
		Name:      row.Name,
		Slug:      row.Slug,
		CreatedAt: timeFrom(row.CreatedAt),
		UpdatedAt: timeFrom(row.UpdatedAt),
	}, nil
}

// UpsertProduct menulis produk berdasar legacy_id (idempoten) beserta
// seluruh gambarnya. `category`/`brand` di domain berupa slug; subquery di
// query me-resolve-nya ke foreign key, dan brand kosong menjadi NULL.
//
// Gambar selalu diganti (delete + insert berurutan) sehingga impor ulang
// menyelaraskan gambar dengan file sumber, bukan menggandakannya.
func (r *ImporterRepository) UpsertProduct(
	ctx context.Context, p domain.Product, legacyID string,
) (*domain.Product, error) {
	qs := querier(ctx, r.pool)
	row, err := qs.UpsertProductImport(ctx, sqlcgen.UpsertProductImportParams{
		Name:        p.Name,
		Slug:        p.Slug,
		Price:       p.Price,
		Description: p.Description,
		Category:    p.Category,
		Brand:       p.Brand,
		RatingRate:  p.Rating.Rate,
		RatingCount: int32(p.Rating.Count),
		IsActive:    p.IsActive,
		LegacyID:    &legacyID,
		CreatedAt:   timestamptzFrom(p.CreatedAt),
	})
	if err != nil {
		return nil, MapError(err)
	}

	if err := replaceImages(ctx, qs, row.ID, p.Images); err != nil {
		return nil, err
	}

	p.ID = row.ID.String()
	p.CreatedAt = timeFrom(row.CreatedAt)
	p.UpdatedAt = timeFrom(row.UpdatedAt)
	return &p, nil
}

// Counts menghitung baris ketiga tabel untuk verifikasi pasca-impor.
func (r *ImporterRepository) Counts(ctx context.Context) (port.ImporCounts, error) {
	qs := querier(ctx, r.pool)

	categories, err := qs.CountCategories(ctx)
	if err != nil {
		return port.ImporCounts{}, MapError(err)
	}
	brands, err := qs.CountBrands(ctx)
	if err != nil {
		return port.ImporCounts{}, MapError(err)
	}
	products, err := qs.CountProducts(ctx)
	if err != nil {
		return port.ImporCounts{}, MapError(err)
	}
	return port.ImporCounts{
		Categories: categories,
		Brands:     brands,
		Products:   products,
	}, nil
}
