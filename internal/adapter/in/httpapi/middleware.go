package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type ctxKey int

const requestIDKey ctxKey = iota

// requestIDFrom mengambil request id dari context (kosong bila tidak ada).
func requestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// requestID memastikan setiap request punya id (dari header atau baru) dan
// mengembalikannya di response header.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-Id"))
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// recoverer menangkap panic agar server tidak mati dan klien mendapat 500 JSON
// dengan envelope yang sama seperti writeError (code=internal).
func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if log != nil {
						log.ErrorContext(r.Context(), "panic tertangkap",
							"panic", rec,
							"request_id", requestIDFrom(r.Context()),
							"method", r.Method,
							"path", r.URL.Path,
						)
					}
					writeJSONStatus(w, http.StatusInternalServerError, errorBody{Error: errorDetail{
						Code:    codeInternal,
						Message: "Terjadi kesalahan internal",
					}})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder mencatat status dan ukuran respons untuk access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// RequestObservation adalah satu event request yang diamati middleware access
// log. Ini adalah **titik sambung** metrics/tracing (Fase 7): implementasi
// (Prometheus, OTel, dsb.) dipasang lewat Deps.Observer tanpa menyentuh logika
// handler. Field-nya sengaja berupa tipe dasar agar adapter bebas memilih
// backend.
type RequestObservation struct {
	RequestID  string
	Method     string
	Path       string
	Status     int
	Bytes      int
	Duration   time.Duration
	RemoteAddr string
}

// Observer menerima satu event per request. Boleh nil (mati). Implementasi
// harus ringan dan tidak pernah memblokir request lama (mis. goroutine/channel
// sendiri bila backend-nya lambat).
type Observer interface {
	ObserveRequest(o RequestObservation)
}

// ObserverFunc memudahkan memasang fungsi biasa sebagai Observer.
type ObserverFunc func(RequestObservation)

func (f ObserverFunc) ObserveRequest(o RequestObservation) { f(o) }

// accessLog mencatat satu baris log per request (JSON via slog) dan
// meneruskan observasi ke Deps.Observer bila dipasang.
func accessLog(log *slog.Logger, observer Observer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if log == nil && observer == nil {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			dur := time.Since(start)
			if log != nil {
				log.LogAttrs(r.Context(), slog.LevelInfo, "http request",
					slog.String("request_id", requestIDFrom(r.Context())),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", rec.status),
					slog.Int("bytes", rec.bytes),
					slog.Int64("duration_ms", dur.Milliseconds()),
				)
			}
			if observer != nil {
				observer.ObserveRequest(RequestObservation{
					RequestID:  requestIDFrom(r.Context()),
					Method:     r.Method,
					Path:       r.URL.Path,
					Status:     rec.status,
					Bytes:      rec.bytes,
					Duration:   dur,
					RemoteAddr: r.RemoteAddr,
				})
			}
		})
	}
}

// timeout membatasi waktu proses satu request lewat context (Fase 7).
//
// Semua work di dalam handler (query database, dsb) menerima context yang sama,
// jadi ketika batas tercapai query ikut ter-batalkan dan kembali sebagai error
// context deadline yang dipetakan ke 504. Nilai <= 0 membuat middleware ini
// lewat-tangan tanpa memasang deadline (lihat Deps.RequestTimeout).
//
// Ini pelengkap statement_timeout di sisi database (config
// DB_STATEMENT_TIMEOUT): context membatalkan eksekusi di klien, statement_timeout
// membatalkan di server — keduanya mencegah query menumpuk.
func timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if d <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CORSConfig adalah konfigurasi CORS. AllowedOrigins kosong berarti CORS mati
// (skenario satu domain, lihat ARCHITECTURE §10).
type CORSConfig struct {
	AllowedOrigins []string
}

// cors menjawab preflight dan menambahkan header CORS untuk origin yang eksak
// (tidak pernah wildcard). Bila tidak ada origin yang dikonfigurasi, middleware
// ini tidak melakukan apa-apa.
func cors(cfg CORSConfig) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		if o = strings.TrimSpace(o); o != "" {
			allowed[o] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		if len(allowed) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			_, originAllowed := allowed[origin]

			if origin != "" {
				w.Header().Add("Vary", "Origin")
			}
			if originAllowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				if !originAllowed {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id, If-None-Match")
				w.Header().Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
