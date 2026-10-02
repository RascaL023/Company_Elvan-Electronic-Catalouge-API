package domain

import (
	"strings"
	"time"
)

// Brand adalah merek produk. `Slug` adalah referensi yang dipakai
// Product.Brand.
type Brand struct {
	ID        string
	Name      string
	Slug      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// BrandPatch adalah perubahan parsial; field nil berarti "jangan diubah".
type BrandPatch struct {
	Name *string
	Slug *string
}

// Validate memeriksa invarian Brand.
func (b Brand) Validate() error {
	v := NewValidationError()

	if strings.TrimSpace(b.Name) == "" {
		v.Add("name", "tidak boleh kosong")
	}
	if b.Slug == "" {
		v.Add("slug", "tidak boleh kosong")
	} else if !slugRE.MatchString(b.Slug) {
		v.Add("slug", "harus huruf kecil, angka, dan tanda hubung")
	}

	if v.HasErrors() {
		return v
	}
	return nil
}
