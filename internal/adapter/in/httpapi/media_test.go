package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// fakeMedia adalah MediaIssuer palsu untuk handler.
type fakeMedia struct {
	sig      port.UploadSignature
	sigErr   error
	result   port.DeleteResult
	deleteID []string
	delErr   error
}

func (f *fakeMedia) IssueUploadSignature(context.Context) (port.UploadSignature, error) {
	if f.sigErr != nil {
		return port.UploadSignature{}, f.sigErr
	}
	return f.sig, nil
}

func (f *fakeMedia) DeleteImages(_ context.Context, fileIDs []string) (port.DeleteResult, error) {
	f.deleteID = fileIDs
	if f.delErr != nil {
		return port.DeleteResult{}, f.delErr
	}
	return f.result, nil
}

func newMediaRouter(au Authenticator, media MediaIssuer) http.Handler {
	return NewRouter(Deps{
		Catalog:    &fakeCatalog{},
		Taxonomy:   &fakeTaxonomy{},
		Auth:       au,
		Transport:  CookieTransport{},
		Media:      media,
		LoginLimit: RateLimit{Limit: 5, Period: time.Minute},
		Log:        discardLogger(),
	})
}

func authedRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: DefaultSessionCookie, Value: "tok"})
	return req
}

func TestMediaSignatureRequiresSession(t *testing.T) {
	router := newMediaRouter(&fakeAuth{meErr: domain.ErrUnauthorized}, &fakeMedia{})

	rec := do(t, router, http.MethodGet, "/api/v1/media/signature", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, ingin 401 tanpa sesi", rec.Code)
	}
}

func TestMediaSignatureReturnsTokenExpireSignature(t *testing.T) {
	media := &fakeMedia{sig: port.UploadSignature{Token: "tok-1", Expire: 1234567890, Signature: "abc"}}
	router := newMediaRouter(&fakeAuth{meAdmin: &domain.Admin{ID: "a1"}}, media)

	req := authedRequest(http.MethodGet, "/api/v1/media/signature", "")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, ingin 200; body=%s", rec.Code, rec.Body.String())
	}
	var body signatureResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Token != "tok-1" || body.Expire != 1234567890 || body.Signature != "abc" {
		t.Errorf("body = %+v", body)
	}
}

func TestMediaDeleteFilesRequiresSession(t *testing.T) {
	router := newMediaRouter(&fakeAuth{meErr: domain.ErrUnauthorized}, &fakeMedia{})

	rec := doJSON(t, router, http.MethodDelete, "/api/v1/media/files", `{"fileIds":["f1"]}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, ingin 401 tanpa sesi", rec.Code)
	}
}

func TestMediaDeleteFilesRejectsEmptyIDs(t *testing.T) {
	router := newMediaRouter(&fakeAuth{meAdmin: &domain.Admin{ID: "a1"}}, &fakeMedia{})

	req := authedRequest(http.MethodDelete, "/api/v1/media/files", `{"fileIds":[]}`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, ingin 400", rec.Code)
	}
	var body errorBody
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Error.Code != codeValidationFailed {
		t.Errorf("code = %q, ingin %q", body.Error.Code, codeValidationFailed)
	}
}

func TestMediaDeleteFilesCallsUseCase(t *testing.T) {
	media := &fakeMedia{result: port.DeleteResult{Deleted: 2}}
	router := newMediaRouter(&fakeAuth{meAdmin: &domain.Admin{ID: "a1"}}, media)

	req := authedRequest(http.MethodDelete, "/api/v1/media/files", `{"fileIds":["f1","f2"]}`)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, ingin 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(media.deleteID) != 2 || media.deleteID[0] != "f1" || media.deleteID[1] != "f2" {
		t.Errorf("fileId diteruskan = %v", media.deleteID)
	}
}
