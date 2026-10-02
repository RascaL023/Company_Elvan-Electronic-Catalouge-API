package domain

import (
	"strings"
	"time"
)

// Category adalah kategori produk. `Slug` adalah referensi yang dipakai
// Product.Category.
type Category struct {
	ID          string
	Name        string
	Slug        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CategoryPatch adalah perubahan parsial; field nil berarti "jangan diubah".
type CategoryPatch struct {
	Name        *string
	Slug        *string
	Description *string
}

// Validate memeriksa invarian Category.
func (c Category) Validate() error {
	v := NewValidationError()

	if strings.TrimSpace(c.Name) == "" {
		v.Add("name", "tidak boleh kosong")
	}
	if c.Slug == "" {
		v.Add("slug", "tidak boleh kosong")
	} else if !slugRE.MatchString(c.Slug) {
		v.Add("slug", "harus huruf kecil, angka, dan tanda hubung")
	}

	if v.HasErrors() {
		return v
	}
	return nil
}
