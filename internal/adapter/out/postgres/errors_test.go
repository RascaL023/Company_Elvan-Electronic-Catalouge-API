package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"elvan-catalog-api/internal/domain"
)

func TestMapError(t *testing.T) {
	tests := []struct {
		name string
		in   error
		want error
	}{
		{"no rows menjadi not found", pgx.ErrNoRows, domain.ErrNotFound},
		{"unique violation menjadi conflict", &pgconn.PgError{Code: pgUniqueViolation, Message: "dup"}, domain.ErrConflict},
		{"foreign key menjadi conflict", &pgconn.PgError{Code: pgForeignKeyViolation, Message: "fk"}, domain.ErrConflict},
		{"restrict (ON DELETE RESTRICT) menjadi conflict", &pgconn.PgError{Code: pgRestrictViolation, Message: "restrict"}, domain.ErrConflict},
		{"check violation menjadi validation", &pgconn.PgError{Code: pgCheckViolation, Message: "chk"}, domain.ErrValidation},
		{"not null menjadi validation", &pgconn.PgError{Code: pgNotNullViolation, Message: "nn"}, domain.ErrValidation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := MapError(tt.in); !errors.Is(err, tt.want) {
				t.Fatalf("MapError(%v) = %v, ingin errors.Is %v", tt.in, err, tt.want)
			}
		})
	}

	if MapError(nil) != nil {
		t.Error("MapError(nil) seharusnya nil")
	}

	plain := errors.New("boom")
	if got := MapError(plain); !errors.Is(got, plain) {
		t.Errorf("error tak dikenal harus diteruskan apa adanya, dapat %v", got)
	}
}
