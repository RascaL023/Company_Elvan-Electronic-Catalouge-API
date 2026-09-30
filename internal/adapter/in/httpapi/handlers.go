package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"elvan-catalog-api/internal/application"
	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// cacheControlPublic dipakai untuk endpoint baca publik (ARCHITECTURE §13).
const cacheControlPublic = "public, max-age=60"

func (h *handlers) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (h *handlers) readyz(w http.ResponseWriter, r *http.Request) {
	if h.deps.Ready == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()
	if err := h.deps.Ready(ctx); err != nil {
		if h.deps.Log != nil {
			h.deps.Log.WarnContext(r.Context(), "readiness gagal", "error", err.Error())
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unavailable"))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// catalog: GET /api/v1/catalog — array proyeksi katalog (FE `getAll()`).
func (h *handlers) catalog(w http.ResponseWriter, r *http.Request) {
	products, err := h.deps.Catalog.ListAll(r.Context(), application.Anonymous)
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	writeJSONCached(w, r, http.StatusOK, toCatalogProductDTOs(products))
}

// listProducts: GET /api/v1/products.
func (h *handlers) listProducts(w http.ResponseWriter, r *http.Request) {
	q, err := parseProductQuery(r)
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	page, err := h.deps.Catalog.List(r.Context(), q, application.Anonymous)
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}

	var cursor *string
	if page.Cursor != "" {
		c := page.Cursor
		cursor = &c
	}
	writeJSONCached(w, r, http.StatusOK, productListDTO{
		Products: toCatalogProductDTOs(page.Products),
		HasMore:  page.HasMore,
		Cursor:   cursor,
	})
}

// getProduct: GET /api/v1/products/{id} — id UUID atau legacy (URL lama).
func (h *handlers) getProduct(w http.ResponseWriter, r *http.Request) {
	product, err := h.deps.Catalog.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	writeJSONCached(w, r, http.StatusOK, toProductDTO(product))
}

// listCategories: GET /api/v1/categories.
func (h *handlers) listCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := h.deps.Taxonomy.ListCategories(r.Context())
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	out := make([]categoryDTO, 0, len(categories))
	for _, c := range categories {
		out = append(out, toCategoryDTO(c))
	}
	writeJSONCached(w, r, http.StatusOK, out)
}

// getCategory: GET /api/v1/categories/{id}.
func (h *handlers) getCategory(w http.ResponseWriter, r *http.Request) {
	category, err := h.deps.Taxonomy.GetCategory(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	writeJSONCached(w, r, http.StatusOK, toCategoryDTO(*category))
}

// listBrands: GET /api/v1/brands.
func (h *handlers) listBrands(w http.ResponseWriter, r *http.Request) {
	brands, err := h.deps.Taxonomy.ListBrands(r.Context())
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	out := make([]brandDTO, 0, len(brands))
	for _, b := range brands {
		out = append(out, toBrandDTO(b))
	}
	writeJSONCached(w, r, http.StatusOK, out)
}

// getBrand: GET /api/v1/brands/{id}.
func (h *handlers) getBrand(w http.ResponseWriter, r *http.Request) {
	brand, err := h.deps.Taxonomy.GetBrand(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}
	writeJSONCached(w, r, http.StatusOK, toBrandDTO(*brand))
}

// parseProductQuery membaca dan memvalidasi query string /products.
func parseProductQuery(r *http.Request) (port.ProductQuery, error) {
	values := r.URL.Query()
	q := port.ProductQuery{
		Category: strings.TrimSpace(values.Get("category")),
		Brand:    strings.TrimSpace(values.Get("brand")),
		Search:   strings.TrimSpace(values.Get("search")),
		Cursor:   values.Get("cursor"),
	}

	sort := strings.TrimSpace(values.Get("sort"))
	if sort == "" {
		q.Sort = domain.SortDefault
	} else {
		q.Sort = domain.SortOption(sort)
	}
	if !q.Sort.Valid() {
		return q, validationError("sort", "nilai tidak didukung: "+sort)
	}

	if raw := values.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return q, validationError("limit", "harus bilangan bulat > 0")
		}
		if n > 100 {
			n = 100
		}
		q.Limit = n
	}

	if raw := values.Get("includeInactive"); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return q, validationError("includeInactive", "harus boolean")
		}
		q.IncludeInactive = b
	}

	return q, nil
}

func validationError(field, message string) error {
	v := domain.NewValidationError()
	v.Add(field, message)
	return v
}

// writeJSONCached menulis JSON dengan ETag dan Cache-Control, serta menjawab
// 304 bila klien sudah punya versi yang sama.
func writeJSONCached(w http.ResponseWriter, r *http.Request, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, errorBody{Error: errorDetail{
			Code:    codeInternal,
			Message: "Terjadi kesalahan internal",
		}})
		return
	}

	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", cacheControlPublic)

	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// etagMatches menangani daftar ETag dan prefix weak validator "W/".
func etagMatches(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "W/")
		if part == "*" || part == etag {
			return true
		}
	}
	return false
}
