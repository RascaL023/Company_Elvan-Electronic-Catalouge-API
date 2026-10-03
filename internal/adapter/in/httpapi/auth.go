package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"elvan-catalog-api/internal/application/auth"
	"elvan-catalog-api/internal/domain"
)

// Authenticator adalah use case auth yang dipakai adapter HTTP.
type Authenticator interface {
	Login(ctx context.Context, email, password string) (*auth.Session, error)
	Logout(ctx context.Context, token string) error
	Me(ctx context.Context, token string) (*domain.Admin, error)
}

// adminKey adalah kunci context untuk admin terautentikasi.
type adminKey struct{}

func adminFromContext(ctx context.Context) (*domain.Admin, bool) {
	admin, ok := ctx.Value(adminKey{}).(*domain.Admin)
	return admin, ok
}

// requireAdmin menolak request yang tidak membawa sesi admin yang sah.
//
// Sesi yang tidak valid/kedaluwarsa sekaligus menghapus cookie-nya supaya klien
// tidak terjebak mengirim token basi.
func (h *handlers) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.deps.Auth == nil || h.deps.Transport == nil {
			writeError(w, h.deps.Log, r, domain.ErrUnauthorized)
			return
		}

		token, ok := h.deps.Transport.Read(r)
		if !ok {
			writeError(w, h.deps.Log, r, domain.ErrUnauthorized)
			return
		}

		admin, err := h.deps.Auth.Me(r.Context(), token)
		if err != nil {
			if errors.Is(err, domain.ErrUnauthorized) {
				h.deps.Transport.Clear(w)
			}
			writeError(w, h.deps.Log, r, err)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminKey{}, admin)))
	})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// login: POST /api/v1/auth/login — verifikasi kredensial, buat sesi, kirim
// token lewat transport sesi.
func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}

	session, err := h.deps.Auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		writeError(w, h.deps.Log, r, err)
		return
	}

	h.deps.Transport.Write(w, session.Token, session.ExpiresAt)
	writeJSONStatus(w, http.StatusOK, sessionDTO{
		Admin:     toAdminDTO(session.Admin),
		ExpiresAt: formatTime(session.ExpiresAt),
	})
}

// logout: POST /api/v1/auth/logout — cabut sesi dan hapus cookie.
func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	if token, ok := h.deps.Transport.Read(r); ok {
		if err := h.deps.Auth.Logout(r.Context(), token); err != nil {
			writeError(w, h.deps.Log, r, err)
			return
		}
	}
	h.deps.Transport.Clear(w)
	w.WriteHeader(http.StatusNoContent)
}

// me: GET /api/v1/auth/me — admin dari sesi saat ini (dijaga requireAdmin).
func (h *handlers) me(w http.ResponseWriter, r *http.Request) {
	admin, ok := adminFromContext(r.Context())
	if !ok {
		writeError(w, h.deps.Log, r, domain.ErrUnauthorized)
		return
	}
	writeJSONStatus(w, http.StatusOK, toAdminDTO(*admin))
}

// decodeJSON membaca body JSON dengan batas ukuran dan menolak field yang tidak
// dikenal, supaya salah ketik di payload ketahuan lebih awal (400).
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return validationError("body", "JSON tidak valid: "+err.Error())
	}
	return nil
}
