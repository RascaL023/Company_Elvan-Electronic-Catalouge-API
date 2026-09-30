// Package taxonomy berisi use case kategori dan brand. Pada Fase 2 baru operasi
// baca; operasi tulis ditambahkan pada Fase 4.
package taxonomy

import (
	"context"
	"log/slog"

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
