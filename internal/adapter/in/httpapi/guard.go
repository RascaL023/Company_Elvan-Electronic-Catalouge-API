package httpapi

import (
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// --- CSRF / Origin ----------------------------------------------------------

// originGuard menolak permintaan tidak aman (POST/PATCH/PUT/DELETE) yang datang
// dari origin lain — mitigasi CSRF karena sesi dibawa cookie (ARCHITECTURE §10).
//
// Aturan:
//   - permintaan yang membawa body harus `Content-Type: application/json`,
//     sehingga permintaan lintas-origin memicu preflight;
//   - `Origin` (bila ada) harus termasuk daftar yang diizinkan; bila daftar
//     kosong (skenario satu domain) origin harus sama dengan host permintaan;
//   - `Sec-Fetch-Site: cross-site` selalu ditolak (pertahanan berlapis).
//
// Permintaan tanpa `Origin`/`Sec-Fetch-Site` (curl, klien non-browser) lolos:
// CSRF hanya mengancam permintaan yang otomatis membawa cookie browser.
func originGuard(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o = strings.TrimSpace(o); o != "" {
			allowed[o] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			if hasBody(r) && !hasJSONContentType(r.Header.Get("Content-Type")) {
				writeError(w, nil, r, validationError("contentType", "harus application/json"))
				return
			}
			if !originAllowed(r, allowed) {
				writeError(w, nil, r, errForbiddenRequest)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func hasBody(r *http.Request) bool {
	return r.ContentLength > 0 || len(r.TransferEncoding) > 0
}

func hasJSONContentType(v string) bool {
	if v == "" {
		return false
	}
	mt, _, err := mime.ParseMediaType(v)
	if err != nil {
		return false
	}
	return mt == "application/json"
}

func originAllowed(r *http.Request, allowed map[string]struct{}) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if len(allowed) > 0 {
		_, ok := allowed[origin]
		return ok
	}

	// Tanpa CORS (skenario satu domain): origin harus sama dengan host.
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// --- rate limit login -------------------------------------------------------

// RateLimit adalah batas laju sederhana: `Limit` permintaan per `Period`.
type RateLimit struct {
	Limit  int
	Period time.Duration
}

// ParseRateLimit mengurai bentuk "5/min", "10/s", atau "100/hour".
func ParseRateLimit(s string) (RateLimit, error) {
	parts := strings.SplitN(strings.TrimSpace(s), "/", 2)
	if len(parts) != 2 {
		return RateLimit{}, fmt.Errorf("rate limit %q: format harus N/unit (mis. 5/min)", s)
	}

	n, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || n <= 0 {
		return RateLimit{}, fmt.Errorf("rate limit %q: jumlah harus bilangan > 0", s)
	}

	var period time.Duration
	switch strings.ToLower(strings.TrimSpace(parts[1])) {
	case "s", "sec", "second":
		period = time.Second
	case "m", "min", "minute":
		period = time.Minute
	case "h", "hour":
		period = time.Hour
	default:
		return RateLimit{}, fmt.Errorf("rate limit %q: unit harus s, min, atau hour", s)
	}

	return RateLimit{Limit: n, Period: period}, nil
}

// keyedLimiter membatasi laju per kunci (mis. per IP) memakai token bucket.
type keyedLimiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	limit   rate.Limit
	burst   int
}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// limiterPruneAfter menentukan kapan entri idle dibersihkan (saat map membesar).
const limiterPruneAfter = 10 * time.Minute

func newKeyedLimiter(rl RateLimit) *keyedLimiter {
	period := rl.Period
	if period <= 0 {
		period = time.Minute
	}
	limit := rl.Limit
	if limit <= 0 {
		limit = 5
	}
	return &keyedLimiter{
		entries: make(map[string]*limiterEntry),
		limit:   rate.Limit(float64(limit) / period.Seconds()),
		burst:   limit,
	}
}

func (k *keyedLimiter) allow(key string, now time.Time) bool {
	k.mu.Lock()
	defer k.mu.Unlock()

	if len(k.entries) > 1000 {
		for key, e := range k.entries {
			if now.Sub(e.lastSeen) > limiterPruneAfter {
				delete(k.entries, key)
			}
		}
	}

	e, ok := k.entries[key]
	if !ok {
		e = &limiterEntry{limiter: rate.NewLimiter(k.limit, k.burst)}
		k.entries[key] = e
	}
	e.lastSeen = now

	return e.limiter.AllowN(now, 1)
}

// loginRateLimiter adalah middleware pembatas percobaan login per IP.
type loginRateLimiter struct {
	limiter *keyedLimiter
	log     *slog.Logger
	now     func() time.Time
}

func newLoginRateLimiter(rl RateLimit, log *slog.Logger) *loginRateLimiter {
	return &loginRateLimiter{limiter: newKeyedLimiter(rl), log: log, now: time.Now}
}

func (l *loginRateLimiter) middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !l.limiter.allow(clientIP(r), l.now()) {
				if l.log != nil {
					l.log.WarnContext(r.Context(), "login dibatasi rate limit", "ip", clientIP(r))
				}
				writeError(w, l.log, r, errRateLimitedRequest)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP mengambil IP klien. Di belakang reverse proxy (Caddy di rencana
// deploy) `X-Forwarded-For` diisi proxy; entri pertama adalah klien asli.
// Proxy wajib menimpa header ini, karena API tidak boleh diekspos langsung.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, ok := strings.Cut(xff, ","); ok || first != "" {
			if ip := strings.TrimSpace(first); ip != "" {
				return ip
			}
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
