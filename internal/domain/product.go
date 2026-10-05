package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SortOption adalah pilihan urutan untuk daftar produk.
type SortOption string

const (
	SortDefault    SortOption = "default"
	SortPriceAsc   SortOption = "price-asc"
	SortPriceDesc  SortOption = "price-desc"
	SortRatingDesc SortOption = "rating-desc"
)

// Valid melaporkan apakah SortOption termasuk yang didukung (whitelist).
func (s SortOption) Valid() bool {
	switch s {
	case SortDefault, SortPriceAsc, SortPriceDesc, SortRatingDesc:
		return true
	default:
		return false
	}
}

var (
	slugRE    = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	nonSlugRE = regexp.MustCompile(`[^a-z0-9]+`)
)

// ValidSlug melaporkan apakah s berupa slug yang sah (huruf kecil, angka,
// tanda hubung). Dipakai validasi patch dan pembuatan kategori/brand.
func ValidSlug(s string) bool { return slugRE.MatchString(s) }

// Slugify mengubah teks menjadi slug: huruf kecil, non-alfanumerik menjadi "-",
// tanpa tanda hubung di ujung. Dipakai server saat slug tidak dikirim klien.
func Slugify(s string) string {
	return strings.Trim(nonSlugRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-")
}

// Product adalah entity inti katalog.
//
// `Category` dan `Brand` di sini adalah **slug** (bukan foreign key); mapper
// Postgres yang menerjemahkan ke/dari kolom category_id/brand_id. `Images`
// terurut dan index 0 adalah thumbnail.
type Product struct {
	ID          string
	Name        string
	Slug        string
	Price       int64 // satuan terkecil mata uang (Rupiah, bilangan bulat)
	Description string
	Category    string
	Brand       string // kosong jika tidak ada
	Images      []Image
	Rating      Rating
	IsActive    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Image adalah satu gambar produk: relative key (tanpa slash di depan) plus
// fileId provider untuk penghapusan. FileID boleh kosong untuk gambar lama.
type Image struct {
	Key    string
	FileID string
}

// Rating adalah ringkasan rating produk.
type Rating struct {
	Rate  float64
	Count int
}

// CatalogProduct adalah proyeksi ringan untuk listing publik.
// `Thumbnail` adalah relative key gambar pertama (posisi 0).
type CatalogProduct struct {
	ID        string
	Name      string
	Slug      string
	Price     int64
	Category  string
	Brand     string
	Thumbnail string
	Rating    Rating
	IsActive  bool
	CreatedAt time.Time
}

// ToCatalog memproyeksikan Product ke CatalogProduct.
func (p Product) ToCatalog() CatalogProduct {
	var thumbnail string
	if len(p.Images) > 0 {
		thumbnail = p.Images[0].Key
	}
	return CatalogProduct{
		ID:        p.ID,
		Name:      p.Name,
		Slug:      p.Slug,
		Price:     p.Price,
		Category:  p.Category,
		Brand:     p.Brand,
		Thumbnail: thumbnail,
		Rating:    p.Rating,
		IsActive:  p.IsActive,
		CreatedAt: p.CreatedAt,
	}
}

// Validate memeriksa invarian Product. Mengembalikan *ValidationError (yang
// membungkus ErrValidation) atau nil.
func (p Product) Validate() error {
	v := NewValidationError()

	if strings.TrimSpace(p.Name) == "" {
		v.Add("name", "tidak boleh kosong")
	}
	if p.Slug == "" {
		v.Add("slug", "tidak boleh kosong")
	} else if !slugRE.MatchString(p.Slug) {
		v.Add("slug", "harus huruf kecil, angka, dan tanda hubung")
	}
	if p.Price < 0 {
		v.Add("price", "harus >= 0")
	}
	if strings.TrimSpace(p.Category) == "" {
		v.Add("category", "tidak boleh kosong")
	}
	if p.Rating.Rate < 0 || p.Rating.Rate > 5 {
		v.Add("rating.rate", "harus di antara 0 dan 5")
	}
	if p.Rating.Count < 0 {
		v.Add("rating.count", "harus >= 0")
	}
	for i, img := range p.Images {
		if strings.TrimSpace(img.Key) == "" {
			v.Add(fmt.Sprintf("images[%d].key", i), "tidak boleh kosong")
		}
	}

	if v.HasErrors() {
		return v
	}
	return nil
}

// ProductPatch adalah perubahan parsial; field nil berarti "jangan diubah".
// Untuk menghapus brand, kirim Brand menunjuk ke string kosong.
type ProductPatch struct {
	Name        *string
	Slug        *string
	Price       *int64
	Description *string
	Category    *string
	Brand       *string
	Images      *[]Image
	Rating      *Rating
	IsActive    *bool
}
