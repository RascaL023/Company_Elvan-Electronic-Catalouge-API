package httpapi

import (
	"net/http"
	"time"
)

// SessionTransport membawa token sesi antara klien dan server. Use case auth
// tidak tahu transport-nya; adapter HTTP yang memilih (cookie hari ini, bearer
// nanti — ARCHITECTURE §10).
type SessionTransport interface {
	Read(r *http.Request) (token string, ok bool)
	Write(w http.ResponseWriter, token string, expires time.Time)
	Clear(w http.ResponseWriter)
}

// CookieTransport menyimpan token di cookie HttpOnly (default).
type CookieTransport struct {
	Name     string
	Path     string
	Domain   string
	SameSite http.SameSite
	Secure   bool
}

var _ SessionTransport = CookieTransport{}

// DefaultSessionCookie adalah nama cookie sesi bila tidak dikonfigurasi.
const DefaultSessionCookie = "elvan_session"

func (c CookieTransport) name() string {
	if c.Name != "" {
		return c.Name
	}
	return DefaultSessionCookie
}

func (c CookieTransport) path() string {
	if c.Path != "" {
		return c.Path
	}
	return "/"
}

// Read mengambil token dari cookie.
func (c CookieTransport) Read(r *http.Request) (string, bool) {
	ck, err := r.Cookie(c.name())
	if err != nil || ck.Value == "" {
		return "", false
	}
	return ck.Value, true
}

// Write mengirim token sebagai cookie HttpOnly.
func (c CookieTransport) Write(w http.ResponseWriter, token string, expires time.Time) {
	maxAge := int(time.Until(expires).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name:     c.name(),
		Value:    token,
		Path:     c.path(),
		Domain:   c.Domain,
		Expires:  expires.UTC(),
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: c.SameSite,
	})
}

// Clear menghapus cookie sesi (nilai kosong, kedaluwarsa seketika).
func (c CookieTransport) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     c.name(),
		Value:    "",
		Path:     c.path(),
		Domain:   c.Domain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.Secure,
		SameSite: c.SameSite,
	})
}
