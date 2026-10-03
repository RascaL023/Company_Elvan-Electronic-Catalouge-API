package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"elvan-catalog-api/internal/domain"
)

// Kode error yang dikirim ke klien (lihat ARCHITECTURE §8).
const (
	codeValidationFailed = "validation_failed"
	codeInvalidCursor    = "invalid_cursor"
	codeUnauthorized     = "unauthorized"
	codeForbidden        = "forbidden"
	codeNotFound         = "not_found"
	codeConflict         = "conflict"
	codeRateLimited      = "rate_limited"
	codeTimeout          = "timeout"
	codeInternal         = "internal"
)

// Sentinel lokal adapter (bukan error domain) supaya middleware bisa memakai
// error mapper yang sama tanpa membocorkan konsepnya ke domain.
var (
	errForbiddenRequest   = errors.New("forbidden request")
	errRateLimitedRequest = errors.New("rate limited")
)

type fieldDetail struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type errorDetail struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Details []fieldDetail `json:"details,omitempty"`
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

// writeError adalah satu-satunya tempat yang menerjemahkan error domain ke
// status HTTP. Detail error 500 hanya masuk log, tidak pernah ke klien.
func writeError(w http.ResponseWriter, log *slog.Logger, r *http.Request, err error) {
	status := http.StatusInternalServerError
	code := codeInternal
	message := "Terjadi kesalahan internal"
	var details []fieldDetail

	switch {
	case errors.Is(err, domain.ErrValidation):
		status = http.StatusBadRequest
		code = codeValidationFailed
		message = "Data tidak valid"

		var ve *domain.ValidationError
		if errors.As(err, &ve) {
			details = make([]fieldDetail, 0, len(ve.Fields))
			for _, f := range ve.Fields {
				details = append(details, fieldDetail{Field: f.Field, Message: f.Message})
				if f.Field == "cursor" {
					code = codeInvalidCursor
					message = "Cursor tidak valid"
				}
			}
		}

	case errors.Is(err, domain.ErrUnauthorized):
		status = http.StatusUnauthorized
		code = codeUnauthorized
		message = "Belum terautentikasi"

	case errors.Is(err, domain.ErrForbidden):
		status = http.StatusForbidden
		code = codeForbidden
		message = "Tidak diizinkan"

	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
		code = codeNotFound
		message = "Data tidak ditemukan"

	case errors.Is(err, domain.ErrConflict):
		status = http.StatusConflict
		code = codeConflict
		message = "Konflik data"

	case errors.Is(err, errRateLimitedRequest):
		status = http.StatusTooManyRequests
		code = codeRateLimited
		message = "Terlalu banyak percobaan; coba lagi nanti"

	case errors.Is(err, context.DeadlineExceeded):
		// Request/query melewati REQUEST_TIMEOUT / DB_STATEMENT_TIMEOUT.
		// Bukan bug server, jadi tidak dicatat sebagai error internal.
		status = http.StatusGatewayTimeout
		code = codeTimeout
		message = "Permintaan memakan waktu terlalu lama; coba lagi"

	case errors.Is(err, errForbiddenRequest):
		status = http.StatusForbidden
		code = codeForbidden
		message = "Tidak diizinkan"

	default:
		if log != nil {
			log.ErrorContext(r.Context(), "error internal",
				"error", err.Error(),
				"request_id", requestIDFrom(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
			)
		}
	}

	writeJSONStatus(w, status, errorBody{Error: errorDetail{
		Code:    code,
		Message: message,
		Details: details,
	}})
}

// writeJSONStatus menulis JSON tanpa ETag (dipakai untuk error).
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
