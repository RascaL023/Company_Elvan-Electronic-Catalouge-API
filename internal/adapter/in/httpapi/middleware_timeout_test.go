package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestTimeoutMembatalkanContext memastikan middleware timeout memasang deadline
// pada context request sehingga handler (dan query di dalamnya) ter-batalkan
// ketika batas tercapai, lalu error-nya dipetakan ke 504 timeout.
func TestTimeoutMembatalkanContext(t *testing.T) {
	var observedDeadline bool

	h := timeout(30 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok := r.Context().Deadline()
		observedDeadline = ok
		if !ok {
			w.WriteHeader(http.StatusOK)
			return
		}
		// Tunggu sampai context dibatalkan (meniru query yang lambat).
		<-r.Context().Done()
		writeError(w, nil, r, r.Context().Err())
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !observedDeadline {
		t.Error("handler tidak menerima context dengan deadline")
	}
	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, ingin 504", rec.Code)
	}

	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body bukan envelope error: %v", err)
	}
	if body.Error.Code != codeTimeout {
		t.Errorf("code = %q, ingin %q", body.Error.Code, codeTimeout)
	}
}

// TestTimeoutNolTanpaBatas memastikan timeout <= 0 tidak mengubah handler
// (dipakai test yang tidak menyetel RequestTimeout).
func TestTimeoutNolTanpaBatas(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Deadline(); ok {
			t.Error("seharusnya tidak ada deadline saat d=0")
		}
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	timeout(0)(inner).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, ingin 204", rec.Code)
	}
}

// TestErrorMapperTimeout memastikan context.DeadlineExceeded dipetakan ke 504
// timeout (bukan 500 internal), sehingga kegagalan batas waktu tidak dianggap
// bug server.
func TestErrorMapperTimeout(t *testing.T) {
	rec := httptest.NewRecorder()
	// Error query biasanya membungkus deadline (fmt.Errorf %w), jadi uji dengan
	// pembungkus, bukan sentinel telanjang.
	writeError(rec, discardLogger(), httptest.NewRequest(http.MethodGet, "/x", nil),
		errors.Join(errors.New("query gagal"), context.DeadlineExceeded))

	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, ingin 504; body=%s", rec.Code, rec.Body.String())
	}
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Error.Code != codeTimeout {
		t.Errorf("code = %q, ingin %q", body.Error.Code, codeTimeout)
	}
}

// recordingObserver menangkap observasi yang dikirim middleware access log.
type recordingObserver struct{ got []RequestObservation }

func (o *recordingObserver) ObserveRequest(obs RequestObservation) {
	o.got = append(o.got, obs)
}

// TestObserverMenerimaEvent memastikan hook metrics dipanggil per request
// dengan field yang benar (titik sambung metrics/tracing, Fase 7).
func TestObserverMenerimaEvent(t *testing.T) {
	obs := &recordingObserver{}
	cat := &fakeCatalog{}
	router := NewRouter(Deps{
		Catalog:  cat,
		Taxonomy: &fakeTaxonomy{},
		Log:      discardLogger(),
		Observer: obs,
	})

	rec := do(t, router, http.MethodGet, "/api/v1/catalog", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(obs.got) != 1 {
		t.Fatalf("observasi = %d, ingin 1", len(obs.got))
	}
	got := obs.got[0]
	if got.Method != http.MethodGet || got.Path != "/api/v1/catalog" {
		t.Errorf("method/path = %q %q", got.Method, got.Path)
	}
	if got.Status != http.StatusOK {
		t.Errorf("status = %d", got.Status)
	}
	if got.RequestID == "" {
		t.Error("request_id kosong")
	}
	if got.Duration <= 0 {
		t.Errorf("duration = %v, harus > 0", got.Duration)
	}
}

// TestObserverBisaDipasangLewatFunc memastikan ObserverFunc memudahkan
// pemasangan fungsi biasa tanpa struct.
func TestObserverBisaDipasangLewatFunc(t *testing.T) {
	called := false
	fn := ObserverFunc(func(RequestObservation) { called = true })
	fn.ObserveRequest(RequestObservation{})
	if !called {
		t.Fatal("ObserverFunc tidak memanggil fungsi bawaan")
	}
}
