package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"elvan-catalog-api/internal/adapter/out/postgres/sqlcgen"
	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// CategoryRepository adalah implementasi port.CategoryRepository di Postgres.
type CategoryRepository struct {
	pool *pgxpool.Pool
}

var _ port.CategoryRepository = (*CategoryRepository)(nil)

// NewCategoryRepository membuat repository kategori di atas pool.
func NewCategoryRepository(pool *pgxpool.Pool) *CategoryRepository {
	return &CategoryRepository{pool: pool}
}

func (r *CategoryRepository) List(ctx context.Context) ([]domain.Category, error) {
	rows, err := querier(ctx, r.pool).ListCategories(ctx)
	if err != nil {
		return nil, MapError(err)
	}
	out := make([]domain.Category, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Category{
			ID:          row.ID.String(),
			Name:        row.Name,
			Slug:        row.Slug,
			Description: row.Description,
			CreatedAt:   timeFrom(row.CreatedAt),
			UpdatedAt:   timeFrom(row.UpdatedAt),
		})
	}
	return out, nil
}

func (r *CategoryRepository) GetByID(ctx context.Context, id string) (*domain.Category, error) {
	uid, err := parseID(id)
	if err != nil {
		return nil, err
	}
	row, err := querier(ctx, r.pool).GetCategoryByID(ctx, uid)
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

func (r *CategoryRepository) Create(ctx context.Context, c domain.Category) (*domain.Category, error) {
	row, err := querier(ctx, r.pool).CreateCategory(ctx, sqlcgen.CreateCategoryParams{
		Name:        c.Name,
		Slug:        c.Slug,
		Description: c.Description,
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

func (r *CategoryRepository) Update(ctx context.Context, id string, patch domain.CategoryPatch) (*domain.Category, error) {
	uid, err := parseID(id)
	if err != nil {
		return nil, err
	}
	row, err := querier(ctx, r.pool).UpdateCategory(ctx, sqlcgen.UpdateCategoryParams{
		Name:        patch.Name,
		Slug:        patch.Slug,
		Description: patch.Description,
		ID:          uid,
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

func (r *CategoryRepository) Delete(ctx context.Context, id string) error {
	uid, err := parseID(id)
	if err != nil {
		return err
	}
	affected, err := querier(ctx, r.pool).DeleteCategory(ctx, uid)
	if err != nil {
		return MapError(err)
	}
	if affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}
