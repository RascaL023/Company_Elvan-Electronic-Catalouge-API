// Package httpapi adalah adapter inbound HTTP: router, middleware, DTO, dan
// pemetaan error domain ke status HTTP. Handler bersifat tipis — decode,
// panggil use case, encode — tanpa logika bisnis.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"elvan-catalog-api/internal/application"
	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

const readinessTimeout = 2 * time.Second

// CatalogReader adalah bagian use case katalog yang dipakai adapter HTTP.
// Didefinisikan di sisi konsumen agar handler mudah diuji dengan fake.
type CatalogReader interface {
	ListAll(ctx context.Context, actor application.Actor) ([]domain.CatalogProduct, error)
	List(ctx context.Context, q port.ProductQuery, actor application.Actor) (port.ProductPage, error)
	Get(ctx context.Context, id string) (*domain.Product, error)
}

// TaxonomyReader adalah bagian use case kategori/brand yang dipakai adapter HTTP.
type TaxonomyReader interface {
	ListCategories(ctx context.Context) ([]domain.Category, error)
	GetCategory(ctx context.Context, id string) (*domain.Category, error)
	ListBrands(ctx context.Context) ([]domain.Brand, error)
	GetBrand(ctx context.Context, id string) (*domain.Brand, error)
}

// Readiness memeriksa kesiapan dependensi (mis. ping database).
type Readiness func(ctx context.Context) error

// Deps adalah dependensi router.
//
// Katalog dan taxonomy diisi dua kali (reader dan writer) meski implementasi
// konkretnya satu service; pemisahan interface membuat handler baca dan tulis
// bisa diuji terpisah.
type Deps struct {
	Catalog        CatalogReader
	CatalogWriter  CatalogWriter
	Taxonomy       TaxonomyReader
	TaxonomyWriter TaxonomyWriter
	Auth           Authenticator
	Transport      SessionTransport
	LoginLimit     RateLimit
	Ready          Readiness
	Log            *slog.Logger
	CORS           CORSConfig
}

type handlers struct {
	deps Deps
}

// NewRouter menyusun rute dan middleware. Urutan dari luar:
// recover → request id → access log → CORS → origin/CSRF guard → (rate limit
// login) → auth → handler. Rate limit hanya dipasang pada rute login.
func NewRouter(d Deps) http.Handler {
	h := &handlers{deps: d}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)

	// Baca publik.
	mux.HandleFunc("GET /api/v1/catalog", h.catalog)
	mux.HandleFunc("GET /api/v1/products", h.listProducts)
	mux.HandleFunc("GET /api/v1/products/{id}", h.getProduct)
	mux.HandleFunc("GET /api/v1/categories", h.listCategories)
	mux.HandleFunc("GET /api/v1/categories/{id}", h.getCategory)
	mux.HandleFunc("GET /api/v1/brands", h.listBrands)
	mux.HandleFunc("GET /api/v1/brands/{id}", h.getBrand)

	// Auth. Login dibatasi rate limit; logout/me butuh sesi admin.
	login := newLoginRateLimiter(d.LoginLimit, d.Log).middleware()
	mux.Handle("POST /api/v1/auth/login", login(http.HandlerFunc(h.login)))
	mux.Handle("POST /api/v1/auth/logout", h.requireAdmin(http.HandlerFunc(h.logout)))
	mux.Handle("GET /api/v1/auth/me", h.requireAdmin(http.HandlerFunc(h.me)))

	// Tulis (admin). Id di path wajib UUID canonical: id legacy hanya untuk
	// baca (issue #6).
	mux.Handle("POST /api/v1/products", h.requireAdmin(http.HandlerFunc(h.createProduct)))
	mux.Handle("PATCH /api/v1/products/{id}", h.requireAdmin(http.HandlerFunc(h.updateProduct)))
	mux.Handle("DELETE /api/v1/products/{id}", h.requireAdmin(http.HandlerFunc(h.deleteProduct)))

	mux.Handle("POST /api/v1/categories", h.requireAdmin(http.HandlerFunc(h.createCategory)))
	mux.Handle("PATCH /api/v1/categories/{id}", h.requireAdmin(http.HandlerFunc(h.updateCategory)))
	mux.Handle("DELETE /api/v1/categories/{id}", h.requireAdmin(http.HandlerFunc(h.deleteCategory)))

	mux.Handle("POST /api/v1/brands", h.requireAdmin(http.HandlerFunc(h.createBrand)))
	mux.Handle("PATCH /api/v1/brands/{id}", h.requireAdmin(http.HandlerFunc(h.updateBrand)))
	mux.Handle("DELETE /api/v1/brands/{id}", h.requireAdmin(http.HandlerFunc(h.deleteBrand)))

	var handler http.Handler = mux
	handler = originGuard(d.CORS.AllowedOrigins)(handler)
	handler = cors(d.CORS)(handler)
	handler = accessLog(d.Log)(handler)
	handler = requestID(handler)
	handler = recoverer(d.Log)(handler)
	return handler
}
