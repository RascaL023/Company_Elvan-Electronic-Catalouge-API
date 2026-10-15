package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"elvan-catalog-api/internal/adapter/out/postgres/sqlcgen"
	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// AdminRepository adalah implementasi port.AdminRepository di Postgres.
type AdminRepository struct {
	pool *pgxpool.Pool
}

var _ port.AdminRepository = (*AdminRepository)(nil)

// NewAdminRepository membuat repository admin di atas pool.
func NewAdminRepository(pool *pgxpool.Pool) *AdminRepository {
	return &AdminRepository{pool: pool}
}

// GetByID mengambil admin berdasarkan id UUID; id non-UUID → ErrNotFound.
func (r *AdminRepository) GetByID(ctx context.Context, id string) (*domain.Admin, error) {
	uid, err := parseID(id)
	if err != nil {
		return nil, err
	}
	row, err := querier(ctx, r.pool).GetAdminByID(ctx, uid)
	if err != nil {
		return nil, MapError(err)
	}
	admin := newAdmin(row.ID.String(), row.Email, row.PasswordHash, row.CreatedAt)
	return &admin, nil
}

// GetByEmail mengambil admin berdasarkan email yang sudah dinormalkan pemanggil
// (lihat domain.NormalizeEmail); tidak ketemu → ErrNotFound.
func (r *AdminRepository) GetByEmail(ctx context.Context, email string) (*domain.Admin, error) {
	row, err := querier(ctx, r.pool).GetAdminByEmail(ctx, email)
	if err != nil {
		return nil, MapError(err)
	}
	admin := newAdmin(row.ID.String(), row.Email, row.PasswordHash, row.CreatedAt)
	return &admin, nil
}

// Create menyimpan admin baru; email duplikat → ErrConflict (UNIQUE 23505).
func (r *AdminRepository) Create(ctx context.Context, a domain.Admin) (*domain.Admin, error) {
	row, err := querier(ctx, r.pool).CreateAdmin(ctx, sqlcgen.CreateAdminParams{
		Email:        a.Email,
		PasswordHash: a.PasswordHash,
	})
	if err != nil {
		return nil, MapError(err)
	}
	admin := newAdmin(row.ID.String(), row.Email, row.PasswordHash, row.CreatedAt)
	return &admin, nil
}

// UpdatePassword mengganti hash password admin; id tidak ada → ErrNotFound.
func (r *AdminRepository) UpdatePassword(ctx context.Context, id, passwordHash string) error {
	uid, err := parseID(id)
	if err != nil {
		return err
	}
	affected, err := querier(ctx, r.pool).UpdateAdminPassword(ctx, sqlcgen.UpdateAdminPasswordParams{
		PasswordHash: passwordHash,
		ID:           uid,
	})
	if err != nil {
		return MapError(err)
	}
	if affected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func newAdmin(id, email, passwordHash string, createdAt pgtype.Timestamptz) domain.Admin {
	return domain.Admin{
		ID:           id,
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    timeFrom(createdAt),
	}
}
