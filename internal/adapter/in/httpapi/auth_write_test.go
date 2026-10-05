package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"elvan-catalog-api/internal/application/auth"
	"elvan-catalog-api/internal/application/catalog"
	"elvan-catalog-api/internal/application/taxonomy"
	"elvan-catalog-api/internal/domain"
)

// --- fake -------------------------------------------------------------------

type fakeAuth struct {
	loginResult *auth.Session
	loginErr    error
	meAdmin     *domain.Admin
	meErr       error
	logoutErr   error
	loggedOut   []string
}

func (f *fakeAuth) Login(context.Context, string, string) (*auth.Session, error) {
	if f.loginErr != nil {
		return nil, f.loginErr
	}
	return f.loginResult, nil
}

func (f *fakeAuth) Logout(_ context.Context, token string) error {
	f.loggedOut = append(f.loggedOut, token)
	return f.logoutErr
}

func (f *fakeAuth) Me(context.Context, string) (*domain.Admin, error) {
	if f.meErr != nil {
		return nil, f.meErr
	}
	return f.meAdmin, nil
}

type fakeCatalogWriter struct {
	created   *catalog.CreateProductInput
	updatedID string
	patch     domain.ProductPatch
	deletedID string
	err       error
}

func (f *fakeCatalogWriter) Create(_ context.Context, in catalog.CreateProductInput) (*domain.Product, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.created = &in
	return &domain.Product{ID: "p1", Name: in.Name, Slug: in.Slug, Category: in.Category}, nil
}

func (f *fakeCatalogWriter) Update(_ context.Context, id string, patch domain.ProductPatch) (*domain.Product, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.updatedID = id
	f.patch = patch
	return &domain.Product{ID: id}, nil
}

func (f *fakeCatalogWriter) Delete(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.deletedID = id
	return nil
}

type fakeTaxonomyWriter struct {
	createdCategory *taxonomy.CreateCategoryInput
	deletedCategory string
	createdBrand    *taxonomy.CreateBrandInput
	err             error
}

func (f *fakeTaxonomyWriter) CreateCategory(_ context.Context, in taxonomy.CreateCategoryInput) (*domain.Category, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.createdCategory = &in
	return &domain.Category{ID: "c1", Name: in.Name, Slug: in.Slug}, nil
}

func (f *fakeTaxonomyWriter) UpdateCategory(_ context.Context, id string, _ domain.CategoryPatch) (*domain.Category, error) {
	return &domain.Category{ID: id}, f.err
}

func (f *fakeTaxonomyWriter) DeleteCategory(_ context.Context, id string) error {
	f.deletedCategory = id
	return f.err
}

func (f *fakeTaxonomyWriter) CreateBrand(_ context.Context, in taxonomy.CreateBrandInput) (*domain.Brand, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.createdBrand = &in
	return &domain.Brand{ID: "b1", Name: in.Name, Slug: in.Slug}, nil
}

func (f *fakeTaxonomyWriter) UpdateBrand(_ context.Context, id string, _ domain.BrandPatch) (*domain.Brand, error) {
	return &domain.Brand{ID: id}, f.err
}

func (f *fakeTaxonomyWriter) DeleteBrand(_ context.Context, _ string) error { return f.err }

func newAuthRouter(au Authenticator, cw CatalogWriter, tw TaxonomyWriter, limit RateLimit, cors CORSConfig) http.Handler {
	return NewRouter(Deps{
		Catalog:        &fakeCatalog{},
		CatalogWriter:  cw,
		Taxonomy:       &fakeTaxonomy{},
		TaxonomyWriter: tw,
		Auth:           au,
		Transport:      CookieTransport{},
		LoginLimit:     limit,
		Log:            discardLogger(),
		CORS:           cors,
	})
}

func doJSON(t *testing.T, router http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(rec *httptest.ResponseRecorder) string {
	for _, c := range rec.Result().Cookies() {
		if c.Name == DefaultSessionCookie {
			return c.Value
		}
	}
	return ""
}

// --- login / me / logout ----------------------------------------------------

func TestLoginSetsCookieAndReturnsAdmin(t *testing.T) {
	expires := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	au := &fakeAuth{loginResult: &auth.Session{
		Token: "tok", ExpiresAt: expires,
		Admin: domain.Admin{ID: "a1", Email: "admin@example.com"},
	}}
	router := newAuthRouter(au, &fakeCatalogWriter{}, &fakeTaxonomyWriter{}, RateLimit{Limit: 5, Period: time.Minute}, CORSConfig{})

	rec := doJSON(t, router, http.MethodPost, "/api/v1/auth/login", `{"email":"admin@example.com","password":"rahasia"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, ingin 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := sessionCookie(rec); got != "tok" {
		t.Errorf("cookie sesi = %q, ingin token dari use case", got)
	}
	var body sessionDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Admin.Email != "admin@example.com" || body.ExpiresAt != "2026-10-09T10:00:00.000Z" {
		t.Errorf("body = %+v", body)
	}
}

func TestLoginMapsUnauthorized(t *testing.T) {
	au := &fakeAuth{loginErr: domain.ErrUnauthorized}
	router := newAuthRouter(au, &fakeCatalogWriter{}, &fakeTaxonomyWriter{}, RateLimit{Limit: 5, Period: time.Minute}, CORSConfig{})

	rec := doJSON(t, router, http.MethodPost, "/api/v1/auth/login", `{"email":"x@y.z","password":"salah"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, ingin 401", rec.Code)
	}
	var body errorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error.Code != codeUnauthorized {
		t.Errorf("code = %q, ingin %q", body.Error.Code, codeUnauthorized)
	}
}

// DB bermasalah saat login harus muncul sebagai 500, bukan 401 yang menyesatkan
// (use case sudah berhenti menyamarkannya; ini mengunci pemetaan HTTP-nya).
func TestLoginMapsInternalErrorTo500(t *testing.T) {
	au := &fakeAuth{loginErr: errors.New("database mati")}
	router := newAuthRouter(au, &fakeCatalogWriter{}, &fakeTaxonomyWriter{}, RateLimit{Limit: 5, Period: time.Minute}, CORSConfig{})

	rec := doJSON(t, router, http.MethodPost, "/api/v1/auth/login", `{"email":"x@y.z","password":"rahasia"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, ingin 500", rec.Code)
	}
	var body errorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error.Code != codeInternal {
		t.Errorf("code = %q, ingin %q", body.Error.Code, codeInternal)
	}
	if strings.Contains(rec.Body.String(), "database mati") {
		t.Error("detail error internal tidak boleh bocor ke klien")
	}
}

func TestLoginMapsDBTimeoutTo504(t *testing.T) {
	au := &fakeAuth{loginErr: context.DeadlineExceeded}
	router := newAuthRouter(au, &fakeCatalogWriter{}, &fakeTaxonomyWriter{}, RateLimit{Limit: 5, Period: time.Minute}, CORSConfig{})

	rec := doJSON(t, router, http.MethodPost, "/api/v1/auth/login", `{"email":"x@y.z","password":"rahasia"}`)
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, ingin 504", rec.Code)
	}
	var body errorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error.Code != codeTimeout {
		t.Errorf("code = %q, ingin %q", body.Error.Code, codeTimeout)
	}
}

func TestLoginRejectsMalformedJSON(t *testing.T) {
	router := newAuthRouter(&fakeAuth{}, &fakeCatalogWriter{}, &fakeTaxonomyWriter{}, RateLimit{Limit: 5, Period: time.Minute}, CORSConfig{})
	rec := doJSON(t, router, http.MethodPost, "/api/v1/auth/login", `{bukan json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, ingin 400", rec.Code)
	}
}

func TestLoginRateLimited(t *testing.T) {
	au := &fakeAuth{loginErr: domain.ErrUnauthorized}
	router := newAuthRouter(au, &fakeCatalogWriter{}, &fakeTaxonomyWriter{}, RateLimit{Limit: 2, Period: time.Minute}, CORSConfig{})

	for i := 0; i < 2; i++ {
		if rec := doJSON(t, router, http.MethodPost, "/api/v1/auth/login", `{}`); rec.Code != http.StatusUnauthorized {
			t.Fatalf("percobaan %d: status = %d, ingin 401", i+1, rec.Code)
		}
	}

	rec := doJSON(t, router, http.MethodPost, "/api/v1/auth/login", `{}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("percobaan ke-3: status = %d, ingin 429", rec.Code)
	}
	var body errorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error.Code != codeRateLimited {
		t.Errorf("code = %q, ingin %q", body.Error.Code, codeRateLimited)
	}
}

func TestMeRequiresSession(t *testing.T) {
	router := newAuthRouter(&fakeAuth{meErr: domain.ErrUnauthorized}, &fakeCatalogWriter{}, &fakeTaxonomyWriter{}, RateLimit{Limit: 5, Period: time.Minute}, CORSConfig{})

	rec := do(t, router, http.MethodGet, "/api/v1/auth/me", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, ingin 401 (tanpa cookie)", rec.Code)
	}
}

func TestMeReturnsAdminWithSession(t *testing.T) {
	au := &fakeAuth{meAdmin: &domain.Admin{ID: "a1", Email: "admin@example.com"}}
	router := newAuthRouter(au, &fakeCatalogWriter{}, &fakeTaxonomyWriter{}, RateLimit{Limit: 5, Period: time.Minute}, CORSConfig{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: DefaultSessionCookie, Value: "tok"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, ingin 200; body=%s", rec.Code, rec.Body.String())
	}
	var body adminDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Email != "admin@example.com" {
		t.Errorf("email = %q", body.Email)
	}
}

func TestMeInvalidSessionClearsCookie(t *testing.T) {
	router := newAuthRouter(&fakeAuth{meErr: domain.ErrUnauthorized}, &fakeCatalogWriter{}, &fakeTaxonomyWriter{}, RateLimit{Limit: 5, Period: time.Minute}, CORSConfig{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: DefaultSessionCookie, Value: "basi"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, ingin 401", rec.Code)
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == DefaultSessionCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("cookie sesi basi tidak dibersihkan")
	}
}

func TestLogoutRequiresSessionAndClearsCookie(t *testing.T) {
	router := newAuthRouter(&fakeAuth{meErr: domain.ErrUnauthorized}, &fakeCatalogWriter{}, &fakeTaxonomyWriter{}, RateLimit{Limit: 5, Period: time.Minute}, CORSConfig{})

	rec := doJSON(t, router, http.MethodPost, "/api/v1/auth/logout", `{}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, ingin 401", rec.Code)
	}

	au := &fakeAuth{meAdmin: &domain.Admin{ID: "a1"}}
	router = newAuthRouter(au, &fakeCatalogWriter{}, &fakeTaxonomyWriter{}, RateLimit{Limit: 5, Period: time.Minute}, CORSConfig{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: DefaultSessionCookie, Value: "tok"})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, ingin 204", rec.Code)
	}
	if len(au.loggedOut) != 1 || au.loggedOut[0] != "tok" {
		t.Errorf("logout dipanggil dengan %v, ingin token dari cookie", au.loggedOut)
	}
}

// --- guard ------------------------------------------------------------------

func TestWriteEndpointsRequireAdmin(t *testing.T) {
	router := newAuthRouter(&fakeAuth{meErr: domain.ErrUnauthorized}, &fakeCatalogWriter{}, &fakeTaxonomyWriter{}, RateLimit{Limit: 5, Period: time.Minute}, CORSConfig{})

	for _, tc := range []struct{ method, target string }{
		{http.MethodPost, "/api/v1/products"},
		{http.MethodPatch, "/api/v1/products/" + uuid.NewString()},
		{http.MethodDelete, "/api/v1/products/" + uuid.NewString()},
		{http.MethodPost, "/api/v1/categories"},
		{http.MethodPost, "/api/v1/brands"},
	} {
		rec := doJSON(t, router, tc.method, tc.target, `{}`)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s -> %d, ingin 401", tc.method, tc.target, rec.Code)
		}
	}
}

func TestOriginGuardRejectsForeignOrigin(t *testing.T) {
	cors := CORSConfig{AllowedOrigins: []string{"https://app.example.com"}}
	router := newAuthRouter(&fakeAuth{loginErr: domain.ErrUnauthorized}, &fakeCatalogWriter{}, &fakeTaxonomyWriter{}, RateLimit{Limit: 5, Period: time.Minute}, cors)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://jahat.example.com")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, ingin 403", rec.Code)
	}
}

func TestOriginGuardRejectsNonJSONBody(t *testing.T) {
	router := newAuthRouter(&fakeAuth{}, &fakeCatalogWriter{}, &fakeTaxonomyWriter{}, RateLimit{Limit: 5, Period: time.Minute}, CORSConfig{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader("email=x&password=y"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, ingin 400", rec.Code)
	}
}

func TestOriginGuardAllowsGet(t *testing.T) {
	router := newAuthRouter(&fakeAuth{}, &fakeCatalogWriter{}, &fakeTaxonomyWriter{}, RateLimit{Limit: 5, Period: time.Minute}, CORSConfig{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
	req.Header.Set("Origin", "https://jahat.example.com")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, ingin 200 (GET tidak diblokir guard)", rec.Code)
	}
}

// --- write endpoints --------------------------------------------------------

func adminRouter(cw CatalogWriter, tw TaxonomyWriter) http.Handler {
	au := &fakeAuth{meAdmin: &domain.Admin{ID: "a1", Email: "admin@example.com"}}
	return newAuthRouter(au, cw, tw, RateLimit{Limit: 5, Period: time.Minute}, CORSConfig{})
}

func doAdmin(t *testing.T, router http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: DefaultSessionCookie, Value: "tok"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestCreateProduct(t *testing.T) {
	cw := &fakeCatalogWriter{}
	rec := doAdmin(t, adminRouter(cw, &fakeTaxonomyWriter{}), http.MethodPost, "/api/v1/products",
		`{"name":"TV","slug":"tv","price":1500,"category":"television","brand":"sharp","images":[{"key":"a.jpg","fileId":"f1"}],"rating":{"rate":4.5,"count":2}}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, ingin 201; body=%s", rec.Code, rec.Body.String())
	}
	if cw.created == nil {
		t.Fatal("use case tidak dipanggil")
	}
	if cw.created.Price != 1500 || cw.created.Category != "television" || !cw.created.IsActive {
		t.Errorf("input diteruskan = %+v", cw.created)
	}
	if len(cw.created.Images) != 1 || cw.created.Images[0].FileID != "f1" {
		t.Errorf("gambar diteruskan = %+v", cw.created.Images)
	}
	if cw.created.Rating.Rate != 4.5 || cw.created.Rating.Count != 2 {
		t.Errorf("rating diteruskan = %+v", cw.created.Rating)
	}
}

func TestCreateProductRequiresPrice(t *testing.T) {
	rec := doAdmin(t, adminRouter(&fakeCatalogWriter{}, &fakeTaxonomyWriter{}), http.MethodPost, "/api/v1/products",
		`{"name":"TV","slug":"tv","category":"television"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, ingin 400", rec.Code)
	}
}

// Id legacy Firestore hanya untuk baca; PATCH/DELETE harus menolaknya sebagai
// 400 validation_failed (issue #6).
func TestProductWriteRejectsLegacyID(t *testing.T) {
	cw := &fakeCatalogWriter{}
	router := adminRouter(cw, &fakeTaxonomyWriter{})

	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		rec := doAdmin(t, router, method, "/api/v1/products/firestore-doc-abc", `{}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s id legacy -> %d, ingin 400", method, rec.Code)
		}
		var body errorBody
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body.Error.Code != codeValidationFailed {
			t.Errorf("%s code = %q, ingin %q", method, body.Error.Code, codeValidationFailed)
		}
		if len(body.Error.Details) == 0 || body.Error.Details[0].Field != "id" {
			t.Errorf("%s details = %+v, ingin field id", method, body.Error.Details)
		}
	}
	if cw.updatedID != "" || cw.deletedID != "" {
		t.Error("use case tidak boleh dipanggil untuk id legacy")
	}
}

func TestUpdateProductPassesPatch(t *testing.T) {
	cw := &fakeCatalogWriter{}
	id := uuid.NewString()
	rec := doAdmin(t, adminRouter(cw, &fakeTaxonomyWriter{}), http.MethodPatch, "/api/v1/products/"+id,
		`{"name":"TV Baru","isActive":false,"images":[]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, ingin 200; body=%s", rec.Code, rec.Body.String())
	}
	if cw.updatedID != id {
		t.Errorf("id = %q, ingin %q", cw.updatedID, id)
	}
	if cw.patch.Name == nil || *cw.patch.Name != "TV Baru" {
		t.Errorf("patch.Name = %v", cw.patch.Name)
	}
	if cw.patch.IsActive == nil || *cw.patch.IsActive {
		t.Errorf("patch.IsActive = %v, ingin false", cw.patch.IsActive)
	}
	if cw.patch.Images == nil || len(*cw.patch.Images) != 0 {
		t.Errorf("patch.Images = %v, ingin slice kosong (bukan nil)", cw.patch.Images)
	}
	if cw.patch.Price != nil {
		t.Error("field yang tidak dikirim seharusnya tetap nil")
	}
}

func TestDeleteProduct(t *testing.T) {
	cw := &fakeCatalogWriter{}
	id := uuid.NewString()
	rec := doAdmin(t, adminRouter(cw, &fakeTaxonomyWriter{}), http.MethodDelete, "/api/v1/products/"+id, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, ingin 204", rec.Code)
	}
	if cw.deletedID != id {
		t.Errorf("id dihapus = %q, ingin %q", cw.deletedID, id)
	}
}

func TestCreateCategoryDerivesSlugAndMapsConflict(t *testing.T) {
	tw := &fakeTaxonomyWriter{}
	rec := doAdmin(t, adminRouter(&fakeCatalogWriter{}, tw), http.MethodPost, "/api/v1/categories", `{"name":"Televisi LED","description":"d"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, ingin 201; body=%s", rec.Code, rec.Body.String())
	}
	if tw.createdCategory == nil || tw.createdCategory.Name != "Televisi LED" {
		t.Errorf("input = %+v", tw.createdCategory)
	}

	twErr := &fakeTaxonomyWriter{err: domain.ErrConflict}
	rec = doAdmin(t, adminRouter(&fakeCatalogWriter{}, twErr), http.MethodPost, "/api/v1/categories", `{"name":"X","slug":"x"}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, ingin 409", rec.Code)
	}
}

func TestDeleteCategoryMapsConflict(t *testing.T) {
	id := uuid.NewString()
	twErr := &fakeTaxonomyWriter{err: domain.ErrConflict}
	rec := doAdmin(t, adminRouter(&fakeCatalogWriter{}, twErr), http.MethodDelete, "/api/v1/categories/"+id, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, ingin 409", rec.Code)
	}
}

func TestWriterErrorBubblesTo500(t *testing.T) {
	cw := &fakeCatalogWriter{err: errors.New("rahasia-db")}
	rec := doAdmin(t, adminRouter(cw, &fakeTaxonomyWriter{}), http.MethodPost, "/api/v1/products",
		`{"name":"TV","slug":"tv","price":1,"category":"television"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, ingin 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "rahasia-db") {
		t.Error("detail error internal bocor ke klien")
	}
}

func TestResponseBodyIsValidJSONForErrors(t *testing.T) {
	rec := doAdmin(t, adminRouter(&fakeCatalogWriter{}, &fakeTaxonomyWriter{}), http.MethodPost, "/api/v1/products", `{"name":"TV","slug":"tv","category":"television"}`)
	var body errorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("body bukan JSON valid: %v", err)
	}
}
