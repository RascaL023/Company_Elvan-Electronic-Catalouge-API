package postgres

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"elvan-catalog-api/internal/domain"
)

// timeFrom mengubah timestamptz sqlc ke time.Time. Nil menjadi zero time.
func timeFrom(ts pgtype.Timestamptz) time.Time {
	if !ts.Valid {
		return time.Time{}
	}
	return ts.Time
}

// timestamptzFrom mengubah time.Time menjadi pgtype.Timestamptz. Zero time
// dianggap NULL (Valid=false).
func timestamptzFrom(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// parseID mengubah string id menjadi uuid. Id yang tidak dapat diurai
// diperlakukan sebagai tidak ditemukan, sehingga use case tidak perlu tahu
// soal format uuid.
func parseID(id string) (uuid.UUID, error) {
	u, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, domain.ErrNotFound
	}
	return u, nil
}
