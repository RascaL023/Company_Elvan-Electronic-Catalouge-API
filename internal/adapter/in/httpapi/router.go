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
type Deps struct {
	Catalog  CatalogReader
	Taxonomy TaxonomyReader
	Ready    Readiness
	Log      *slog.Logger
	CORS     CORSConfig
}

type handlers struct {
	deps Deps
}

// NewRouter menyusun rute dan middleware. Urutan middleware dari luar:
// recover → request id → access log → CORS → handler.
// Rate limit, CSRF/Origin check, dan auth ditambahkan pada Fase 4.
func NewRouter(d Deps) http.Handler {
	h := &handlers{deps: d}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)

	mux.HandleFunc("GET /api/v1/catalog", h.catalog)
	mux.HandleFunc("GET /api/v1/products", h.listProducts)
	mux.HandleFunc("GET /api/v1/products/{id}", h.getProduct)
	mux.HandleFunc("GET /api/v1/categories", h.listCategories)
	mux.HandleFunc("GET /api/v1/categories/{id}", h.getCategory)
	mux.HandleFunc("GET /api/v1/brands", h.listBrands)
	mux.HandleFunc("GET /api/v1/brands/{id}", h.getBrand)

	var handler http.Handler = mux
	handler = cors(d.CORS)(handler)
	handler = accessLog(d.Log)(handler)
	handler = requestID(handler)
	handler = recoverer(d.Log)(handler)
	return handler
}
