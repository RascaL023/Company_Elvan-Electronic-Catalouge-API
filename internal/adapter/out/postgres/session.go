package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"elvan-catalog-api/internal/adapter/out/postgres/sqlcgen"
	"elvan-catalog-api/internal/application/port"
)

// SessionRepository adalah implementasi port.SessionStore di Postgres.
//
// Yang disimpan hanya hash SHA-256 dari token (`sessions.token_hash`); token
// mentah tidak pernah masuk database.
type SessionRepository struct {
	pool *pgxpool.Pool
}

var _ port.SessionStore = (*SessionRepository)(nil)

// NewSessionRepository membuat store sesi di atas pool.
func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

// Create menyimpan sesi baru.
func (r *SessionRepository) Create(ctx context.Context, s port.Session) error {
	adminID, err := parseID(s.AdminID)
	if err != nil {
		return err
	}
	_, err = querier(ctx, r.pool).CreateSession(ctx, sqlcgen.CreateSessionParams{
		AdminID:   adminID,
		TokenHash: s.TokenHash,
		ExpiresAt: timestamptzFrom(s.ExpiresAt),
	})
	return MapError(err)
}

// GetByTokenHash mencari sesi berdasarkan hash token; tidak ada → ErrNotFound.
func (r *SessionRepository) GetByTokenHash(ctx context.Context, hash []byte) (*port.Session, error) {
	row, err := querier(ctx, r.pool).GetSessionByTokenHash(ctx, hash)
	if err != nil {
		return nil, MapError(err)
	}
	return &port.Session{
		ID:         row.ID.String(),
		AdminID:    row.AdminID.String(),
		TokenHash:  row.TokenHash,
		ExpiresAt:  timeFrom(row.ExpiresAt),
		CreatedAt:  timeFrom(row.CreatedAt),
		LastSeenAt: timeFrom(row.LastSeenAt),
	}, nil
}

// Delete mencabut sesi. Sesi yang sudah tidak ada bukan error (idempoten).
func (r *SessionRepository) Delete(ctx context.Context, hash []byte) error {
	return MapError(querier(ctx, r.pool).DeleteSession(ctx, hash))
}

// Touch memperbarui last_seen_at pada waktu eksplisit (dipanggil berkala).
func (r *SessionRepository) Touch(ctx context.Context, hash []byte, at time.Time) error {
	return MapError(querier(ctx, r.pool).TouchSession(ctx, sqlcgen.TouchSessionParams{
		At:        timestamptzFrom(at),
		TokenHash: hash,
	}))
}

// DeleteExpired menghapus sesi kedaluwarsa sebelum `before` dan mengembalikan
// jumlahnya (dipakai job pembersihan).
func (r *SessionRepository) DeleteExpired(ctx context.Context, before time.Time) (int64, error) {
	n, err := querier(ctx, r.pool).DeleteExpiredSessions(ctx, timestamptzFrom(before))
	if err != nil {
		return 0, MapError(err)
	}
	return n, nil
}
