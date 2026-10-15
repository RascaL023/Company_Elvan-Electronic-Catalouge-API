// Package taxonomy berisi use case kategori dan brand. Operasi baca tersedia
// sejak Fase 2; operasi tulis ditambahkan pada Fase 4. Kategori/brand hanya
// menyentuh satu tabel, jadi tidak butuh transaksi eksplisit.
package taxonomy

import (
	"context"
	"log/slog"
	"strings"

	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// Service adalah use case kategori & brand.
type Service struct {
	categories port.CategoryRepository
	brands     port.BrandRepository
	log        *slog.Logger
}

// New membuat Service taxonomy.
func New(categories port.CategoryRepository, brands port.BrandRepository, log *slog.Logger) *Service {
	return &Service{categories: categories, brands: brands, log: log}
}

// ListCategories mengembalikan seluruh kategori.
func (s *Service) ListCategories(ctx context.Context) ([]domain.Category, error) {
	return s.categories.List(ctx)
}

// GetCategory mengambil satu kategori.
func (s *Service) GetCategory(ctx context.Context, id string) (*domain.Category, error) {
	return s.categories.GetByID(ctx, id)
}

// ListBrands mengembalikan seluruh brand.
func (s *Service) ListBrands(ctx context.Context) ([]domain.Brand, error) {
	return s.brands.List(ctx)
}

// GetBrand mengambil satu brand.
func (s *Service) GetBrand(ctx context.Context, id string) (*domain.Brand, error) {
	return s.brands.GetByID(ctx, id)
}

// CreateCategoryInput adalah input use case CreateCategory. Slug boleh kosong;
// server menurunkannya dari nama (ARCHITECTURE §18 V3).
type CreateCategoryInput struct {
	Name        string
	Slug        string
	Description string
}

// CreateCategory memvalidasi lalu menyimpan kategori baru. Slug duplikat
// dikembalikan adapter sebagai `ErrConflict` (409).
func (s *Service) CreateCategory(ctx context.Context, in CreateCategoryInput) (*domain.Category, error) {
	c := domain.Category{
		Name:        strings.TrimSpace(in.Name),
		Slug:        slugOrDerive(in.Slug, in.Name),
		Description: in.Description,
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return s.categories.Create(ctx, c)
}

// UpdateCategory mengubah sebagian kategori. Field yang diisi divalidasi lebih
// dulu supaya nama/slug tidak bisa menjadi tidak sah.
func (s *Service) UpdateCategory(ctx context.Context, id string, patch domain.CategoryPatch) (*domain.Category, error) {
	if err := validateTaxonomyPatch(patch.Name, patch.Slug); err != nil {
		return nil, err
	}
	return s.categories.Update(ctx, id, patch)
}

// DeleteCategory menghapus kategori. Kategori yang masih dipakai produk
// menghasilkan `ErrConflict` (RESTRICT di database).
func (s *Service) DeleteCategory(ctx context.Context, id string) error {
	return s.categories.Delete(ctx, id)
}

// CreateBrandInput adalah input use case CreateBrand. Slug boleh kosong.
type CreateBrandInput struct {
	Name string
	Slug string
}

// CreateBrand memvalidasi lalu menyimpan brand baru.
func (s *Service) CreateBrand(ctx context.Context, in CreateBrandInput) (*domain.Brand, error) {
	b := domain.Brand{
		Name: strings.TrimSpace(in.Name),
		Slug: slugOrDerive(in.Slug, in.Name),
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return s.brands.Create(ctx, b)
}

// UpdateBrand mengubah sebagian brand.
func (s *Service) UpdateBrand(ctx context.Context, id string, patch domain.BrandPatch) (*domain.Brand, error) {
	if err := validateTaxonomyPatch(patch.Name, patch.Slug); err != nil {
		return nil, err
	}
	return s.brands.Update(ctx, id, patch)
}

// DeleteBrand menghapus brand. Brand yang masih dipakai produk → `ErrConflict`.
func (s *Service) DeleteBrand(ctx context.Context, id string) error {
	return s.brands.Delete(ctx, id)
}

// slugOrDerive memakai slug dari klien bila diisi; selain itu menurunkannya dari
// nama sehingga klien tidak wajib menghitung slug sendiri.
func slugOrDerive(slug, name string) string {
	if s := strings.TrimSpace(slug); s != "" {
		return s
	}
	return domain.Slugify(name)
}

// validateTaxonomyPatch memvalidasi field yang memang disertakan pada patch
// (nil = tidak diubah). Ini menjaga slug tetap sah walau tidak ada constraint
// format di database.
func validateTaxonomyPatch(name, slug *string) error {
	v := domain.NewValidationError()

	if name != nil && strings.TrimSpace(*name) == "" {
		v.Add("name", "tidak boleh kosong")
	}
	if slug != nil {
		switch {
		case strings.TrimSpace(*slug) == "":
			v.Add("slug", "tidak boleh kosong")
		case !domain.ValidSlug(*slug):
			v.Add("slug", "harus huruf kecil, angka, dan tanda hubung")
		}
	}

	if v.HasErrors() {
		return v
	}
	return nil
}
