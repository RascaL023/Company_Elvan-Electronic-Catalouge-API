package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"elvan-catalog-api/internal/adapter/out/postgres/sqlcgen"
	"elvan-catalog-api/internal/application/port"
)

type txKey struct{}

// TxManager menjalankan fungsi dalam satu transaksi database.
//
// Transaksi disimpan di dalam context sehingga repository memakainya kembali
// tanpa perlu tahu soal pgx; use case pun tidak perlu mengoper handle apapun.
type TxManager struct {
	pool *pgxpool.Pool
}

var _ port.TxManager = (*TxManager)(nil)

// NewTxManager membuat TxManager di atas pool.
func NewTxManager(pool *pgxpool.Pool) *TxManager { return &TxManager{pool: pool} }

// WithinTx menjalankan fn dalam transaksi. Error dari fn membatalkan transaksi;
// hanya commit yang sukses yang disimpan.
func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("mulai transaksi: %w", err)
	}
	// Rollback setelah commit adalah no-op; aman dipanggil selalu.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaksi: %w", err)
	}
	return nil
}

// querier memakai transaksi dari context bila ada, selain itu pool langsung.
func querier(ctx context.Context, pool *pgxpool.Pool) *sqlcgen.Queries {
	if tx, ok := txFromContext(ctx); ok {
		return sqlcgen.New(tx)
	}
	return sqlcgen.New(pool)
}

// txFromContext mengambil transaksi aktif dari context, bila ada.
func txFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	return tx, ok
}
