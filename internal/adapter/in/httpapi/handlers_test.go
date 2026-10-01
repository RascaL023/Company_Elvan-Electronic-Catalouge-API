package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"elvan-catalog-api/internal/application"
	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// --- fake service -----------------------------------------------------------

type fakeCatalog struct {
	products       []domain.CatalogProduct
	page           port.ProductPage
	product        *domain.Product
	err            error
	panicOnListAll bool
	lastQuery      port.ProductQuery
	lastActor      application.Actor
}

func (f *fakeCatalog) ListAll(_ context.Context, actor application.Actor) ([]domain.CatalogProduct, error) {
	if f.panicOnListAll {
		panic("boom")
	}
	f.lastActor = actor
	return f.products, f.err
}

func (f *fakeCatalog) List(_ context.Context, q port.ProductQuery, actor application.Actor) (port.ProductPage, error) {
	f.lastQuery = q
	f.lastActor = actor
	return f.page, f.err
}

func (f *fakeCatalog) Get(_ context.Context, _ string) (*domain.Product, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.product, nil
}

type fakeTaxonomy struct {
	categories []domain.Category
	brands     []domain.Brand
	err        error
}

func (f *fakeTaxonomy) ListCategories(context.Context) ([]domain.Category, error) {
	return f.categories, f.err
}

func (f *fakeTaxonomy) GetCategory(context.Context, string) (*domain.Category, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.categories) == 0 {
		return nil, domain.ErrNotFound
	}
	return &f.categories[0], nil
}

func (f *fakeTaxonomy) ListBrands(context.Context) ([]domain.Brand, error) {
	return f.brands, f.err
}

func (f *fakeTaxonomy) GetBrand(context.Context, string) (*domain.Brand, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.brands) == 0 {
		return nil, domain.ErrNotFound
	}
	return &f.brands[0], nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestRouter(cat CatalogReader, tax TaxonomyReader, cfg CORSConfig) http.Handler {
	return NewRouter(Deps{Catalog: cat, Taxonomy: tax, Log: discardLogger(), CORS: cfg})
}

func do(t *testing.T, router http.Handler, method, target string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// --- test -------------------------------------------------------------------

func TestCatalogReturnsArrayWithETagAnd304(t *testing.T) {
	created := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	cat := &fakeCatalog{products: []domain.CatalogProduct{{
		ID: "p1", Name: "TV", Slug: "tv", Price: 100, Category: "television",
		Brand: "sharp", Thumbnail: "a.jpg", Rating: domain.Rating{Rate: 4.5, Count: 2},
		IsActive: true, CreatedAt: created,
	}}}
	router := newTestRouter(cat, &fakeTaxonomy{}, CORSConfig{})

	rec := do(t, router, http.MethodGet, "/api/v1/catalog", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, ingin 200; body=%s", rec.Code, rec.Body.String())
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Error("ETag kosong")
	}
	if cc := rec.Header().Get("Cache-Control"); cc != cacheControlPublic {
		t.Errorf("Cache-Control = %q, ingin %q", cc, cacheControlPublic)
	}
	if cat.lastActor.IsAdmin {
		t.Error("actor untuk endpoint publik seharusnya anonim")
	}

	var products []catalogProductDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &products); err != nil {
		t.Fatalf("body bukan array produk: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("jumlah produk = %d, ingin 1", len(products))
	}
	if products[0].CreatedAt != "2026-10-01T03:00:00.000Z" {
		t.Errorf("createdAt = %q, ingin format ISO-8601 UTC milidetik", products[0].CreatedAt)
	}
	if products[0].Rating.Rate != 4.5 || products[0].Rating.Count != 2 {
		t.Errorf("rating tidak sesuai: %+v", products[0].Rating)
	}

	// Permintaan kedua dengan If-None-Match harus 304.
	rec2 := do(t, router, http.MethodGet, "/api/v1/catalog", func(r *http.Request) {
		r.Header.Set("If-None-Match", etag)
	})
	if rec2.Code != http.StatusNotModified {
		t.Errorf("dengan If-None-Match status = %d, ingin 304", rec2.Code)
	}
}

func TestProductListShape(t *testing.T) {
	t.Run("tanpa halaman berikutnya cursor null", func(t *testing.T) {
		cat := &fakeCatalog{page: port.ProductPage{
			Products: []domain.CatalogProduct{{ID: "p1", Name: "TV", Category: "television", IsActive: true}},
			HasMore:  false,
		}}
		rec := do(t, newTestRouter(cat, &fakeTaxonomy{}, CORSConfig{}), http.MethodGet, "/api/v1/products", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
		}
		var body productListDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body.Cursor != nil {
			t.Errorf("cursor = %q, ingin null", *body.Cursor)
		}
		if body.HasMore {
			t.Error("hasMore seharusnya false")
		}
		if len(body.Products) != 1 {
			t.Errorf("jumlah produk = %d, ingin 1", len(body.Products))
		}
	})

	t.Run("dengan halaman berikutnya cursor terisi", func(t *testing.T) {
		cat := &fakeCatalog{page: port.ProductPage{
			Products: []domain.CatalogProduct{{ID: "p1"}},
			HasMore:  true,
			Cursor:   "next-token",
		}}
		rec := do(t, newTestRouter(cat, &fakeTaxonomy{}, CORSConfig{}), http.MethodGet, "/api/v1/products", nil)
		var body productListDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if body.Cursor == nil || *body.Cursor != "next-token" {
			t.Errorf("cursor = %v, ingin \"next-token\"", body.Cursor)
		}
		if !body.HasMore {
			t.Error("hasMore seharusnya true")
		}
	})
}

func TestErrorMapping(t *testing.T) {
	validationErr := domain.NewValidationError()
	validationErr.Add("price", "harus >= 0")

	cursorErr := domain.NewValidationError()
	cursorErr.Add("cursor", "cursor tidak valid")

	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"validation", validationErr, http.StatusBadRequest, codeValidationFailed},
		{"cursor", cursorErr, http.StatusBadRequest, codeInvalidCursor},
		{"unauthorized", domain.ErrUnauthorized, http.StatusUnauthorized, codeUnauthorized},
		{"forbidden", domain.ErrForbidden, http.StatusForbidden, codeForbidden},
		{"not found", domain.ErrNotFound, http.StatusNotFound, codeNotFound},
		{"conflict", domain.ErrConflict, http.StatusConflict, codeConflict},
		{"internal", errors.New("detail rahasia jangan bocor"), http.StatusInternalServerError, codeInternal},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cat := &fakeCatalog{err: tt.err}
			rec := do(t, newTestRouter(cat, &fakeTaxonomy{}, CORSConfig{}), http.MethodGet, "/api/v1/catalog", nil)

			if rec.Code != tt.status {
				t.Fatalf("status = %d, ingin %d; body=%s", rec.Code, tt.status, rec.Body.String())
			}
			var body errorBody
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal error body: %v", err)
			}
			if body.Error.Code != tt.code {
				t.Errorf("code = %q, ingin %q", body.Error.Code, tt.code)
			}
			if tt.status == http.StatusInternalServerError && strings.Contains(rec.Body.String(), "rahasia") {
				t.Error("detail error internal bocor ke klien")
			}
			if tt.code == codeInvalidCursor {
				if len(body.Error.Details) == 0 || body.Error.Details[0].Field != "cursor" {
					t.Errorf("detail cursor tidak ada: %+v", body.Error.Details)
				}
			}
		})
	}
}

func TestInvalidQueryParams(t *testing.T) {
	targets := []string{
		"/api/v1/products?sort=bogus",
		"/api/v1/products?limit=abc",
		"/api/v1/products?limit=0",
		"/api/v1/products?includeInactive=maybe",
	}
	for _, target := range targets {
		rec := do(t, newTestRouter(&fakeCatalog{}, &fakeTaxonomy{}, CORSConfig{}), http.MethodGet, target, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s -> status %d, ingin 400", target, rec.Code)
		}
		var body errorBody
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Error.Code != codeValidationFailed {
			t.Errorf("%s -> code %q, ingin %q", target, body.Error.Code, codeValidationFailed)
		}
	}
}

func TestLimitCappedAt100(t *testing.T) {
	cat := &fakeCatalog{}
	rec := do(t, newTestRouter(cat, &fakeTaxonomy{}, CORSConfig{}), http.MethodGet, "/api/v1/products?limit=5000", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if cat.lastQuery.Limit != 100 {
		t.Errorf("limit diteruskan = %d, ingin dibatasi 100", cat.lastQuery.Limit)
	}
}

func TestAnonymousActorUsedForReads(t *testing.T) {
	cat := &fakeCatalog{}
	rec := do(t, newTestRouter(cat, &fakeTaxonomy{}, CORSConfig{}), http.MethodGet, "/api/v1/products?includeInactive=true", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if cat.lastActor.IsAdmin {
		t.Error("handler publik tidak boleh menganggap actor admin")
	}
	// includeInactive diteruskan apa adanya; use case yang membuangnya untuk
	// non-admin (diuji di package catalog).
	if !cat.lastQuery.IncludeInactive {
		t.Error("query mentah seharusnya diteruskan ke use case")
	}
}

func TestTaxonomyEndpoints(t *testing.T) {
	tax := &fakeTaxonomy{
		categories: []domain.Category{{ID: "c1", Name: "TV", Slug: "television"}},
		brands:     []domain.Brand{{ID: "b1", Name: "Sharp", Slug: "sharp"}},
	}
	router := newTestRouter(&fakeCatalog{}, tax, CORSConfig{})

	if rec := do(t, router, http.MethodGet, "/api/v1/categories", nil); rec.Code != http.StatusOK {
		t.Errorf("categories status = %d", rec.Code)
	}
	if rec := do(t, router, http.MethodGet, "/api/v1/categories/c1", nil); rec.Code != http.StatusOK {
		t.Errorf("category detail status = %d", rec.Code)
	}
	if rec := do(t, router, http.MethodGet, "/api/v1/brands", nil); rec.Code != http.StatusOK {
		t.Errorf("brands status = %d", rec.Code)
	}
	if rec := do(t, router, http.MethodGet, "/api/v1/brands/b1", nil); rec.Code != http.StatusOK {
		t.Errorf("brand detail status = %d", rec.Code)
	}
}

func TestProductNotFoundAndPathID(t *testing.T) {
	cat := &fakeCatalog{err: domain.ErrNotFound}
	rec := do(t, newTestRouter(cat, &fakeTaxonomy{}, CORSConfig{}), http.MethodGet, "/api/v1/products/tidak-ada", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, ingin 404", rec.Code)
	}
}

func TestCORS(t *testing.T) {
	t.Run("mati bila origin kosong", func(t *testing.T) {
		rec := do(t, newTestRouter(&fakeCatalog{}, &fakeTaxonomy{}, CORSConfig{}), http.MethodGet, "/api/v1/catalog", func(r *http.Request) {
			r.Header.Set("Origin", "https://app.example.com")
		})
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Error("CORS seharusnya mati saat AllowedOrigins kosong")
		}
	})

	cfg := CORSConfig{AllowedOrigins: []string{"https://app.example.com"}}
	router := newTestRouter(&fakeCatalog{}, &fakeTaxonomy{}, cfg)

	t.Run("preflight origin diizinkan", func(t *testing.T) {
		rec := do(t, router, http.MethodOptions, "/api/v1/products", func(r *http.Request) {
			r.Header.Set("Origin", "https://app.example.com")
			r.Header.Set("Access-Control-Request-Method", "GET")
		})
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, ingin 204", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
			t.Errorf("Allow-Origin = %q", got)
		}
		if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
			t.Error("Allow-Credentials tidak diset")
		}
		if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
			t.Error("Vary: Origin tidak diset")
		}
	})

	t.Run("preflight origin asing ditolak", func(t *testing.T) {
		rec := do(t, router, http.MethodOptions, "/api/v1/products", func(r *http.Request) {
			r.Header.Set("Origin", "https://jahat.example.com")
			r.Header.Set("Access-Control-Request-Method", "GET")
		})
		if rec.Code != http.StatusForbidden {
			t.Errorf("status = %d, ingin 403", rec.Code)
		}
	})
}

func TestRequestIDHeader(t *testing.T) {
	router := newTestRouter(&fakeCatalog{}, &fakeTaxonomy{}, CORSConfig{})

	rec := do(t, router, http.MethodGet, "/healthz", nil)
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id tidak diset")
	}

	rec = do(t, router, http.MethodGet, "/healthz", func(r *http.Request) {
		r.Header.Set("X-Request-Id", "abc-123")
	})
	if got := rec.Header().Get("X-Request-Id"); got != "abc-123" {
		t.Errorf("X-Request-Id = %q, ingin diteruskan dari klien", got)
	}
}

func TestHealthAndReady(t *testing.T) {
	ok := NewRouter(Deps{
		Catalog: &fakeCatalog{}, Taxonomy: &fakeTaxonomy{}, Log: discardLogger(),
		Ready: func(context.Context) error { return nil },
	})
	if rec := do(t, ok, http.MethodGet, "/healthz", nil); rec.Code != http.StatusOK {
		t.Errorf("healthz status = %d", rec.Code)
	}
	if rec := do(t, ok, http.MethodGet, "/readyz", nil); rec.Code != http.StatusOK {
		t.Errorf("readyz status = %d", rec.Code)
	}

	down := NewRouter(Deps{
		Catalog: &fakeCatalog{}, Taxonomy: &fakeTaxonomy{}, Log: discardLogger(),
		Ready: func(context.Context) error { return errors.New("db down") },
	})
	if rec := do(t, down, http.MethodGet, "/readyz", nil); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("readyz gagal status = %d, ingin 503", rec.Code)
	}
}

func TestPanicReturnsJSON500(t *testing.T) {
	cat := &fakeCatalog{panicOnListAll: true}
	rec := do(t, newTestRouter(cat, &fakeTaxonomy{}, CORSConfig{}), http.MethodGet, "/api/v1/catalog", nil)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, ingin 500", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, ingin application/json", ct)
	}

	var body errorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("body bukan JSON valid: %v\nraw = %q", err, rec.Body.String())
	}
	if body.Error.Code != codeInternal {
		t.Errorf("error.code = %q, ingin %q", body.Error.Code, codeInternal)
	}
	if body.Error.Message == "" {
		t.Error("error.message kosong")
	}

	// Bandingkan dengan envelope error normal (500 lewat writeError).
	wantRec := httptest.NewRecorder()
	writeError(wantRec, discardLogger(), httptest.NewRequest(http.MethodGet, "/", nil), errors.New("internal-for-compare"))
	var want errorBody
	if err := json.NewDecoder(wantRec.Body).Decode(&want); err != nil {
		t.Fatalf("decode envelope normal: %v", err)
	}
	if body.Error.Code != want.Error.Code || body.Error.Message != want.Error.Message {
		t.Errorf("envelope panic = %+v, ingin sama dengan writeError = %+v", body.Error, want.Error)
	}
}
