// Package catalog berisi use case katalog produk.
package catalog

import (
	"context"
	"log/slog"

	"elvan-catalog-api/internal/application"
	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// Service adalah use case katalog. Pada Fase 2 baru operasi baca; operasi tulis
// ditambahkan pada Fase 4.
type Service struct {
	products port.ProductRepository
	log      *slog.Logger
}

// New membuat Service katalog.
func New(products port.ProductRepository, log *slog.Logger) *Service {
	return &Service{products: products, log: log}
}

// ListAll mengembalikan proyeksi ringan seluruh katalog (GET /catalog).
// Produk nonaktif hanya disertakan bila actor adalah admin.
func (s *Service) ListAll(ctx context.Context, actor application.Actor) ([]domain.CatalogProduct, error) {
	return s.products.ListAll(ctx, actor.IsAdmin)
}

// List mengembalikan satu halaman katalog. `includeInactive` diabaikan untuk
// actor non-admin.
func (s *Service) List(ctx context.Context, q port.ProductQuery, actor application.Actor) (port.ProductPage, error) {
	if !actor.IsAdmin {
		q.IncludeInactive = false
	}
	return s.products.List(ctx, q)
}

// Get mengambil produk lengkap berdasarkan id (UUID atau id legacy).
func (s *Service) Get(ctx context.Context, id string) (*domain.Product, error) {
	return s.products.GetByID(ctx, id)
}
