package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"elvan-catalog-api/internal/adapter/out/postgres/sqlcgen"
	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// BrandRepository adalah implementasi port.BrandRepository di Postgres.
type BrandRepository struct {
	pool *pgxpool.Pool
}

var _ port.BrandRepository = (*BrandRepository)(nil)

// NewBrandRepository membuat repository brand di atas pool.
func NewBrandRepository(pool *pgxpool.Pool) *BrandRepository {
	return &BrandRepository{pool: pool}
}

func (r *BrandRepository) List(ctx context.Context) ([]domain.Brand, error) {
	rows, err := querier(ctx, r.pool).ListBrands(ctx)
	if err != nil {
		return nil, MapError(err)
	}
	out := make([]domain.Brand, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Brand{
			ID:        row.ID.String(),
			Name:      row.Name,
			Slug:      row.Slug,
			CreatedAt: timeFrom(row.CreatedAt),
			UpdatedAt: timeFrom(row.UpdatedAt),
		})
	}
	return out, nil
}

func (r *BrandRepository) GetByID(ctx context.Context, id string) (*domain.Brand, error) {
	uid, err := parseID(id)
	if err != nil {
		return nil, err
	}
	row, err := querier(ctx, r.pool).GetBrandByID(ctx, uid)
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

// GetBySlug mencari brand berdasarkan slug; tidak ketemu → domain.ErrNotFound.
func (r *BrandRepository) GetBySlug(ctx context.Context, slug string) (*domain.Brand, error) {
	row, err := querier(ctx, r.pool).GetBrandBySlug(ctx, slug)
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

func (r *BrandRepository) Create(ctx context.Context, b domain.Brand) (*domain.Brand, error) {
	row, err := querier(ctx, r.pool).CreateBrand(ctx, sqlcgen.CreateBrandParams{
		Name: b.Name,
		Slug: b.Slug,
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

func (r *BrandRepository) Update(ctx context.Context, id string, patch domain.BrandPatch) (*domain.Brand, error) {
	uid, err := parseID(id)
	if err != nil {
		return nil, err
	}
	row, err := querier(ctx, r.pool).UpdateBrand(ctx, sqlcgen.UpdateBrandParams{
		Name: patch.Name,
		Slug: patch.Slug,
		ID:   uid,
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

func (r *BrandRepository) Delete(ctx context.Context, id string) error {
	uid, err := parseID(id)
	if err != nil {
		return err
	}
	affected, err := querier(ctx, r.pool).DeleteBrand(ctx, uid)
	if err != nil {
		return MapError(err)
	}
	if affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}
