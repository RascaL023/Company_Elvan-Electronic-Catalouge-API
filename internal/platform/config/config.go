// Package config memuat seluruh konfigurasi aplikasi dari environment.
//
// Semua nilai dibaca sekali saat startup dan divalidasi di sini (fail fast).
// Tidak ada nilai default untuk secret: nilai yang hilang atau tidak valid
// membuat proses berhenti dengan pesan yang jelas. Tidak ada secret yang
// boleh hidup di dalam kode atau repo.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config menampung konfigurasi runtime. Field-nya mengikuti tabel env di
// docs/ARCHITECTURE.md §12.
type Config struct {
	Env      string // development | production
	HTTPAddr string // alamat listen, mis. ":8080"
	LogLevel string // debug | info | warn | error

	DatabaseURL string // DSN PostgreSQL (pgx)
	DBMaxConns  int32  // ukuran pool
	// DBStatementTimeout adalah batas waktu eksekusi per statement (server-side,
	// lewat runtime parameter `statement_timeout`). Query lambat dipotong oleh
	// PostgreSQL sehingga koneksi tidak tersangkut (Fase 7).
	DBStatementTimeout time.Duration

	ImageKitPrivateKey  string // secret; hanya untuk modul media
	ImageKitURLEndpoint string

	SessionTTL    time.Duration
	AuthTransport string // cookie | bearer

	CookieDomain   string // host-only bila kosong
	CookieSameSite string // lax | none | strict
	CookieSecure   bool

	CORSAllowedOrigins []string // origin eksak, tanpa wildcard
	RateLimitLogin     string   // mis. "5/min"; diurai di modul auth

	// RequestTimeout membatasi waktu proses satu request lewat context
	// (Fase 7): handler dan seluruh query di dalamnya ikut ter-batalkan.
	RequestTimeout time.Duration
}

// Load membaca environment dan mengembalikan Config yang sudah tervalidasi.
// Semua error validasi dikumpulkan agar operator melihat semuanya sekaligus.
func Load() (*Config, error) {
	l := &loader{}

	cfg := &Config{
		Env:      l.str("APP_ENV", "development"),
		HTTPAddr: l.str("HTTP_ADDR", ":8080"),
		LogLevel: l.str("LOG_LEVEL", "info"),

		DatabaseURL:        l.str("DATABASE_URL", ""),
		DBMaxConns:         int32(l.integer("DB_MAX_CONNS", 10)),
		DBStatementTimeout: l.duration("DB_STATEMENT_TIMEOUT", 10*time.Second),

		ImageKitPrivateKey:  l.str("IMAGEKIT_PRIVATE_KEY", ""),
		ImageKitURLEndpoint: l.str("IMAGEKIT_URL_ENDPOINT", ""),

		SessionTTL:    l.duration("SESSION_TTL", 7*24*time.Hour),
		AuthTransport: l.str("AUTH_TRANSPORT", "cookie"),

		CookieDomain:   l.str("COOKIE_DOMAIN", ""),
		CookieSameSite: l.str("COOKIE_SAMESITE", "lax"),
		CookieSecure:   l.boolean("COOKIE_SECURE", false),

		CORSAllowedOrigins: l.csv("CORS_ALLOWED_ORIGINS"),
		RateLimitLogin:     l.str("RATE_LIMIT_LOGIN", "5/min"),
		RequestTimeout:     l.duration("REQUEST_TIMEOUT", 30*time.Second),
	}

	cfg.validate(l)

	if len(l.errs) > 0 {
		return nil, fmt.Errorf("konfigurasi tidak valid: %w", errors.Join(l.errs...))
	}
	return cfg, nil
}

func (c *Config) validate(l *loader) {
	switch c.Env {
	case "development", "production":
	default:
		l.errf("APP_ENV harus \"development\" atau \"production\", dapat %q", c.Env)
	}

	if c.HTTPAddr == "" {
		l.errf("HTTP_ADDR tidak boleh kosong")
	}
	if c.DatabaseURL == "" {
		l.errf("DATABASE_URL wajib diisi")
	}
	if c.DBMaxConns <= 0 {
		l.errf("DB_MAX_CONNS harus > 0, dapat %d", c.DBMaxConns)
	}
	if c.DBStatementTimeout <= 0 {
		l.errf("DB_STATEMENT_TIMEOUT harus > 0, dapat %s", c.DBStatementTimeout)
	}
	if c.RequestTimeout <= 0 {
		l.errf("REQUEST_TIMEOUT harus > 0, dapat %s", c.RequestTimeout)
	}
	if c.SessionTTL <= 0 {
		l.errf("SESSION_TTL harus > 0, dapat %s", c.SessionTTL)
	}

	switch c.AuthTransport {
	case "cookie", "bearer":
	default:
		l.errf("AUTH_TRANSPORT harus \"cookie\" atau \"bearer\", dapat %q", c.AuthTransport)
	}

	switch c.CookieSameSite {
	case "lax", "none", "strict":
	default:
		l.errf("COOKIE_SAMESITE harus \"lax\", \"none\", atau \"strict\", dapat %q", c.CookieSameSite)
	}
	if c.CookieSameSite == "none" && !c.CookieSecure {
		l.errf("COOKIE_SECURE wajib true bila COOKIE_SAMESITE=none")
	}
	// Di production cookie sesi wajib Secure: tanpa itu cookie ikut terkirim
	// lewat HTTP dan bisa dicuri. Gagal cepat di sini lebih baik daripada
	// mengandalkan default compose (yang bisa ditimpa atau dilewati bila
	// binary dijalankan langsung).
	if c.Env == "production" && !c.CookieSecure {
		l.errf("COOKIE_SECURE wajib true saat APP_ENV=production")
	}
}

// loader mengumpulkan error parsing supaya semuanya bisa dilaporkan sekaligus.
type loader struct {
	errs []error
}

func (l *loader) errf(format string, args ...any) {
	l.errs = append(l.errs, fmt.Errorf(format, args...))
}

func (l *loader) str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func (l *loader) integer(key string, def int) int {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		l.errf("%s: %q bukan integer", key, raw)
		return def
	}
	return v
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		l.errf("%s: %q bukan durasi yang valid (mis. 168h)", key, raw)
		return def
	}
	return v
}

func (l *loader) boolean(key string, def bool) bool {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		l.errf("%s: %q bukan boolean yang valid", key, raw)
		return def
	}
	return v
}

func (l *loader) csv(key string) []string {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
