package postgres

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolOption menyesuaikan konfigurasi pool tanpa mengubah signature lama.
type PoolOption func(*pgxpool.Config)

// WithStatementTimeout membatasi waktu eksekusi satu statement di sisi server
// PostgreSQL (runtime parameter `statement_timeout`, dalam milidetik).
// Query yang lewat batas dibatalkan server dan kembali sebagai error context
// deadline — dipakai sebagai jaring pengaman per-query (Fase 7).
func WithStatementTimeout(d time.Duration) PoolOption {
	return func(cfg *pgxpool.Config) {
		if d <= 0 {
			return
		}
		if cfg.ConnConfig == nil {
			return
		}
		if cfg.ConnConfig.RuntimeParams == nil {
			cfg.ConnConfig.RuntimeParams = make(map[string]string)
		}
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(d.Milliseconds(), 10)
	}
}

// NewPool membuat pgxpool dari DSN dan memverifikasi koneksi bisa dibuka.
// Gagal cepat bila DSN tidak valid atau database tidak dapat dihubungi.
func NewPool(ctx context.Context, dsn string, maxConns int32, opts ...PoolOption) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute
	for _, opt := range opts {
		opt(cfg)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("buat pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// Healthcheck memastikan database masih dapat dihubungi (dipakai /readyz).
func Healthcheck(ctx context.Context, pool *pgxpool.Pool) error {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return pool.Ping(pingCtx)
}
