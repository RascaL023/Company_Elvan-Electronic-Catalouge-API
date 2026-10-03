// Package importer berisi use case impor data Firestore ke Postgres
// (ARCHITECTURE §16). Ekspor JSON Firestore di-decode menjadi Dataset,
// divalidasi, lalu ditulis dalam **satu transaksi** secara idempoten lewat
// `legacy_id`.
package importer

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// Dataset adalah isi file ekspor JSON. Hanya tiga koleksi yang diimpor;
// koleksi/proyeksi lain (mis. `catalog/snapshot`) sengaja tidak punya field
// di sini sehingga otomatis dilewati (lihat CatalogSnapshot di bawah).
type Dataset struct {
	Categories []CategoryDoc `json:"categories"`
	Brands     []BrandDoc    `json:"brands"`
	Products   []ProductDoc  `json:"products"`

	// CatalogSnapshot menampung dokumen `catalog/snapshot` bila ikut ter-ekspor.
	// Field ini hanya ada supaya decode tidak gagal; isinya tidak pernah
	// dipakai (hanya proyeksi, sumber kebenaran tetap `products/*`).
	CatalogSnapshot json.RawMessage `json:"catalog_snapshot,omitempty"`
}

// CategoryDoc adalah satu dokumen kategori dari ekspor Firestore.
type CategoryDoc struct {
	LegacyID    string `json:"legacy_id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// BrandDoc adalah satu dokumen brand dari ekspor Firestore.
type BrandDoc struct {
	LegacyID  string `json:"legacy_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// ProductDoc adalah satu dokumen produk dari ekspor Firestore.
//
// `Images` dan `ImageFileIds` adalah array paralel: keduanya dipasangkan
// berdasarkan index (kontrak FE, ARCHITECTURE §4). Panjang boleh beda; entri
// yang tidak punya pasangan dianggap kosong.
type ProductDoc struct {
	LegacyID     string    `json:"legacy_id"`
	Name         string    `json:"name"`
	Slug         string    `json:"slug"`
	Price        int64     `json:"price"`
	Description  string    `json:"description"`
	Category     string    `json:"category"` // slug kategori
	Brand        string    `json:"brand"`    // slug brand; kosong = tanpa brand
	Images       []string  `json:"images"`
	ImageFileIds []string  `json:"imageFileIds"`
	Rating       RatingDoc `json:"rating"`
	IsActive     *bool     `json:"isActive"`
	CreatedAt    string    `json:"createdAt"`
	UpdatedAt    string    `json:"updatedAt"`
}

// RatingDoc adalah rating dokumen produk.
type RatingDoc struct {
	Rate  float64 `json:"rate"`
	Count int     `json:"count"`
}

// Decode membaca satu file ekspor JSON (bisa stdin) menjadi Dataset.
// Field yang tidak dikenal diabaikan — ekspor Firestore bisa saja membawa
// metadata tambahan yang tidak relevan bagi importer.
func Decode(r io.Reader) (*Dataset, error) {
	dec := json.NewDecoder(r)
	var ds Dataset
	if err := dec.Decode(&ds); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	return &ds, nil
}

// Counts mengembalikan jumlah dokumen per koleksi (untuk verifikasi).
func (d *Dataset) Counts() (categories, brands, products int) {
	return len(d.Categories), len(d.Brands), len(d.Products)
}

// parseTime mengurai timestamp ekspor (ISO-8601) ke time.Time.
// String kosong menghasilkan time.Time{} (nol) sehingga adapter memakai
// default database (now()).
func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	// Firestore kadang mengekspor "YYYY-MM-DD" saja.
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}
