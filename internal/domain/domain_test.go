package domain

import (
	"errors"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Sharp 24 Inch LED TV": "sharp-24-inch-led-tv",
		"  Kulkas 2 Pintu  ":   "kulkas-2-pintu",
		"Mesin Cuci!!!":        "mesin-cuci",
		"---":                  "",
		"":                     "",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, ingin %q", in, got, want)
		}
	}
}

func TestSortOptionValid(t *testing.T) {
	valid := []SortOption{SortDefault, SortPriceAsc, SortPriceDesc, SortRatingDesc}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("SortOption(%q) seharusnya valid", s)
		}
	}
	if SortOption("bogus").Valid() {
		t.Error("SortOption(\"bogus\") seharusnya tidak valid")
	}
}

func TestProductValidate(t *testing.T) {
	valid := Product{
		Name:     "TV LED",
		Slug:     "tv-led",
		Price:    1500000,
		Category: "television",
		Rating:   Rating{Rate: 4.5, Count: 3},
		Images:   []Image{{Key: "assets/images/products/television/tv.jpg", FileID: "abc"}},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("produk valid ditolak: %v", err)
	}

	tests := []struct {
		name  string
		field string
		mut   func(p Product) Product
	}{
		{"nama kosong", "name", func(p Product) Product { p.Name = "  "; return p }},
		{"slug kosong", "slug", func(p Product) Product { p.Slug = ""; return p }},
		{"slug tidak valid", "slug", func(p Product) Product { p.Slug = "TV LED!"; return p }},
		{"harga negatif", "price", func(p Product) Product { p.Price = -1; return p }},
		{"kategori kosong", "category", func(p Product) Product { p.Category = ""; return p }},
		{"rating di atas 5", "rating.rate", func(p Product) Product { p.Rating.Rate = 5.1; return p }},
		{"rating negatif", "rating.rate", func(p Product) Product { p.Rating.Rate = -0.1; return p }},
		{"rating count negatif", "rating.count", func(p Product) Product { p.Rating.Count = -1; return p }},
		{"gambar tanpa key", "images[0].key", func(p Product) Product { p.Images = []Image{{}}; return p }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mut(valid).Validate()
			if err == nil {
				t.Fatal("ingin error validasi, dapat nil")
			}
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("errors.Is(err, ErrValidation) = false, err = %v", err)
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("ingin *ValidationError, dapat %T", err)
			}
			found := false
			for _, f := range ve.Fields {
				if f.Field == tt.field {
					found = true
				}
			}
			if !found {
				t.Errorf("field %q tidak ada di error: %+v", tt.field, ve.Fields)
			}
		})
	}
}

func TestProductToCatalog(t *testing.T) {
	p := Product{
		ID:       "p1",
		Name:     "TV",
		Slug:     "tv",
		Price:    100,
		Category: "television",
		Brand:    "sharp",
		Images:   []Image{{Key: "a.jpg"}, {Key: "b.jpg"}},
		Rating:   Rating{Rate: 4, Count: 2},
		IsActive: true,
	}
	c := p.ToCatalog()

	if c.Thumbnail != "a.jpg" {
		t.Errorf("Thumbnail = %q, ingin %q", c.Thumbnail, "a.jpg")
	}
	if c.Brand != "sharp" || c.Category != "television" {
		t.Errorf("slug brand/kategori salah: %+v", c)
	}

	// Produk tanpa gambar: thumbnail kosong, bukan panic.
	empty := Product{ID: "p2", Category: "x"}.ToCatalog()
	if empty.Thumbnail != "" {
		t.Errorf("Thumbnail tanpa gambar = %q, ingin kosong", empty.Thumbnail)
	}
}

func TestCategoryAndBrandValidate(t *testing.T) {
	if err := (Category{Name: "TV", Slug: "television"}).Validate(); err != nil {
		t.Errorf("kategori valid ditolak: %v", err)
	}
	if err := (Category{Name: "", Slug: "television"}).Validate(); !errors.Is(err, ErrValidation) {
		t.Errorf("kategori tanpa nama seharusnya ErrValidation, dapat %v", err)
	}
	if err := (Brand{Name: "Sharp", Slug: "sharp"}).Validate(); err != nil {
		t.Errorf("brand valid ditolak: %v", err)
	}
	if err := (Brand{Name: "Sharp", Slug: "Sharp!"}).Validate(); !errors.Is(err, ErrValidation) {
		t.Errorf("brand slug tidak valid seharusnya ErrValidation, dapat %v", err)
	}
}

func TestAdminValidate(t *testing.T) {
	if err := (Admin{Email: "admin@example.com", PasswordHash: "$argon2id$..."}).Validate(); err != nil {
		t.Errorf("admin valid ditolak: %v", err)
	}
	for _, a := range []Admin{
		{Email: "", PasswordHash: "x"},
		{Email: "bukan-email", PasswordHash: "x"},
		{Email: "admin@example.com", PasswordHash: ""},
	} {
		if err := a.Validate(); !errors.Is(err, ErrValidation) {
			t.Errorf("admin %+v seharusnya ErrValidation, dapat %v", a, err)
		}
	}
}
