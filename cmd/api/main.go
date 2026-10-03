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
	"elvan-catalog-api/internal/adapter/out/imagekit"
	"elvan-catalog-api/internal/adapter/out/postgres"
	"elvan-catalog-api/internal/adapter/out/security"
	"elvan-catalog-api/internal/application/auth"
	"elvan-catalog-api/internal/application/catalog"
	"elvan-catalog-api/internal/application/media"
	"elvan-catalog-api/internal/application/taxonomy"
	"elvan-catalog-api/internal/platform/clock"
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

	// Wiring adapter → use case (satu-satunya tempat DI).
	productRepo := postgres.NewProductRepository(pool, log)
	categoryRepo := postgres.NewCategoryRepository(pool)
	brandRepo := postgres.NewBrandRepository(pool)
	adminRepo := postgres.NewAdminRepository(pool)
	sessionRepo := postgres.NewSessionRepository(pool)
	txManager := postgres.NewTxManager(pool)

	// Media hanya aktif bila kredensial ImageKit tersedia. Tanpa itu, catalog
	// tetap berjalan dan gambar terlepas hanya dicatat di log. Interface dibiarkan
	// nil (bukan typed-nil) agar pemeriksaan `== nil` di use case dan router benar.
	var (
		cleaner     catalog.ImageCleaner
		mediaIssuer httpapi.MediaIssuer
	)
	if cfg.ImageKitPrivateKey != "" {
		mediaService := media.New(imagekit.New(cfg.ImageKitPrivateKey), log)
		cleaner = mediaService
		mediaIssuer = mediaService
	} else {
		log.Warn("IMAGEKIT_PRIVATE_KEY kosong; endpoint media dinonaktifkan")
	}

	catalogService := catalog.New(productRepo, categoryRepo, brandRepo, txManager, cleaner, log)
	taxonomyService := taxonomy.New(categoryRepo, brandRepo, log)

	authService := auth.New(
		adminRepo,
		sessionRepo,
		security.NewPasswordHasher(security.DefaultArgon2Params()),
		security.NewTokenGenerator(),
		txManager,
		clock.Real{},
		auth.Config{SessionTTL: cfg.SessionTTL},
		log,
	)

	loginLimit, err := httpapi.ParseRateLimit(cfg.RateLimitLogin)
	if err != nil {
		return err
	}

	router := httpapi.NewRouter(httpapi.Deps{
		Catalog:        catalogService,
		CatalogWriter:  catalogService,
		Taxonomy:       taxonomyService,
		TaxonomyWriter: taxonomyService,
		Auth:           authService,
		Media:          mediaIssuer,
		Transport: httpapi.CookieTransport{
			Domain:   cfg.CookieDomain,
			SameSite: cookieSameSite(cfg.CookieSameSite),
			Secure:   cfg.CookieSecure,
		},
		LoginLimit: loginLimit,
		Ready:      func(ctx context.Context) error { return postgres.Healthcheck(ctx, pool) },
		Log:        log,
		CORS:       httpapi.CORSConfig{AllowedOrigins: cfg.CORSAllowedOrigins},
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

// cookieSameSite memetakan nilai konfigurasi (lax/none/strict) ke http.SameSite.
func cookieSameSite(v string) http.SameSite {
	switch v {
	case "none":
		return http.SameSiteNoneMode
	case "strict":
		return http.SameSiteStrictMode
	default:
		return http.SameSiteLaxMode
	}
}
