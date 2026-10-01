// Command api adalah composition root server HTTP.
//
// Ini satu-satunya tempat yang melakukan wiring: config → logger → pool →
// adapter → use case → router. DI dilakukan manual, tanpa framework.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"elvan-catalog-api/internal/adapter/in/httpapi"
	"elvan-catalog-api/internal/adapter/out/postgres"
	"elvan-catalog-api/internal/application/catalog"
	"elvan-catalog-api/internal/application/taxonomy"
	"elvan-catalog-api/internal/platform/config"
	"elvan-catalog-api/internal/platform/dotenv"
	"elvan-catalog-api/internal/platform/logger"
)

const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		logger.New("info").Error("server berhenti", "error", err.Error())
		os.Exit(1)
	}
}

func run() error {
	// Muat .env untuk pengembangan (tidak aktif di production dan tidak
	// menimpa variabel yang sudah ada di environment).
	loadedFromDotenv, err := dotenv.LoadDev()
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := logger.New(cfg.LogLevel)
	slog.SetDefault(log)
	if loadedFromDotenv > 0 {
		log.Info("konfigurasi dimuat dari .env", "jumlah", loadedFromDotenv)
	}

	// SIGINT/SIGTERM membatalkan context dan memicu graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Wiring adapter → use case.
	catalogService := catalog.New(
		postgres.NewProductRepository(pool, log),
		postgres.NewTxManager(pool),
		log,
	)
	taxonomyService := taxonomy.New(
		postgres.NewCategoryRepository(pool),
		postgres.NewBrandRepository(pool),
		log,
	)

	router := httpapi.NewRouter(httpapi.Deps{
		Catalog:  catalogService,
		Taxonomy: taxonomyService,
		Ready:    func(ctx context.Context) error { return postgres.Healthcheck(ctx, pool) },
		Log:      log,
		CORS:     httpapi.CORSConfig{AllowedOrigins: cfg.CORSAllowedOrigins},
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http server mendengarkan", "addr", cfg.HTTPAddr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("sinyal shutdown diterima")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	log.Info("shutdown selesai")
	return nil
}
