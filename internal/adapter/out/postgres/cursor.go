package postgres

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"elvan-catalog-api/internal/domain"
)

// cursorPayload adalah isi cursor keyset. Bentuknya detail internal adapter;
// klien hanya melihat base64url-nya dan tidak boleh menafsirkannya.
//
// Hanya field yang relevan dengan `Sort` yang diisi.
type cursorPayload struct {
	Sort        domain.SortOption `json:"s"`
	CreatedAt   *time.Time        `json:"c,omitempty"`
	Price       *int64            `json:"p,omitempty"`
	RatingRate  *float64          `json:"r,omitempty"`
	RatingCount *int32            `json:"rc,omitempty"`
	ID          string            `json:"i"`
}

// encodeCursor mengubah payload menjadi token base64url tanpa padding.
func encodeCursor(c cursorPayload) string {
	b, err := json.Marshal(c)
	if err != nil {
		// Tidak mungkin terjadi untuk tipe ini; biarkan kosong agar halaman
		// terakhir (hasMore=false) tidak membawa cursor.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeCursor mengurai token opaque. String kosong berarti halaman pertama
// (mengembalikan nil, nil). Cursor rusak mengembalikan error validasi domain
// supaya adapter HTTP memetakannya ke 400 `invalid_cursor`.
func decodeCursor(token string) (*cursorPayload, error) {
	if strings.TrimSpace(token) == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, cursorError("cursor tidak dapat dibaca")
	}
	var c cursorPayload
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, cursorError("cursor tidak valid")
	}
	if c.ID == "" || !c.Sort.Valid() {
		return nil, cursorError("cursor tidak valid")
	}
	return &c, nil
}

// cursorError membuat error validasi pada field "cursor".
func cursorError(message string) error {
	v := domain.NewValidationError()
	v.Add("cursor", message)
	return v
}
