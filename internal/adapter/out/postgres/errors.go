package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elvan-catalog-api/internal/domain"
)

// Kode error PostgreSQL yang relevan (SQLSTATE).
//
// Catatan: `ON DELETE RESTRICT` menghasilkan 23001 (restrict_violation), bukan
// 23503 (foreign_key_violation). Keduanya diperlakukan sebagai konflik.
const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
	pgRestrictViolation   = "23001"
	pgNotNullViolation    = "23502"
	pgCheckViolation      = "23514"
)

// MapError menerjemahkan error driver/database ke error domain.
//
// Ini satu-satunya tempat yang mengenal SQLSTATE; use case cukup memakai
// errors.Is(err, domain.ErrConflict) dsb. Error tak dikenal diteruskan apa
// adanya agar tetap tercatat sebagai 500 di adapter HTTP.
func MapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgUniqueViolation, pgForeignKeyViolation, pgRestrictViolation:
			return fmt.Errorf("%w: %s", domain.ErrConflict, pgErr.Message)
		case pgNotNullViolation, pgCheckViolation:
			return fmt.Errorf("%w: %s", domain.ErrValidation, pgErr.Message)
		}
	}
	return err
}
