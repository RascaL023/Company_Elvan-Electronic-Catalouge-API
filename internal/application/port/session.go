package port

import (
	"context"
	"time"
)

// Session adalah sesi login admin yang disimpan server-side. Yang disimpan
// hanya hash token; token mentah hanya ada di klien.
type Session struct {
	ID         string
	AdminID    string
	TokenHash  []byte
	ExpiresAt  time.Time
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// SessionStore adalah port penyimpanan sesi.
type SessionStore interface {
	Create(ctx context.Context, s Session) error
	GetByTokenHash(ctx context.Context, hash []byte) (*Session, error)
	Delete(ctx context.Context, hash []byte) error
	// Touch memperbarui last_seen_at (dipanggil berkala, bukan tiap request).
	Touch(ctx context.Context, hash []byte, at time.Time) error
	// DeleteExpired menghapus sesi kedaluwarsa dan mengembalikan jumlahnya.
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}
