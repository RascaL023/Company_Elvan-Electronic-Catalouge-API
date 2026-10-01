// Package catalog berisi use case katalog produk.
package catalog

import (
	"context"
	"log/slog"

	"elvan-catalog-api/internal/application"
	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// Service adalah use case katalog. Operasi baca tersedia sejak Fase 2; operasi
// tulis (Create/Update/Delete) mulai dibangun pada Fase 4. Pada tahap ini belum
// ada handler HTTP — use case tulis dipakai lewat test dan pemanggil lain dulu.
type Service struct {
	products port.ProductRepository
	tx       port.TxManager
	log      *slog.Logger
}

// New membuat Service katalog. `tx` adalah unit of work yang dipakai operasi
// tulis; lihat catatan kepemilikan transaksi di method Create/Update/Delete.
func New(products port.ProductRepository, tx port.TxManager, log *slog.Logger) *Service {
	return &Service{products: products, tx: tx, log: log}
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

// CreateProductInput adalah input use case Create. `Category`/`Brand` berupa
// slug; `Brand` kosong berarti produk tanpa brand.
type CreateProductInput struct {
	Name        string
	Slug        string
	Price       int64
	Description string
	Category    string
	Brand       string
	Images      []domain.Image
	Rating      domain.Rating
	IsActive    bool
}

func (in CreateProductInput) toDomain() domain.Product {
	return domain.Product{
		Name:        in.Name,
		Slug:        in.Slug,
		Price:       in.Price,
		Description: in.Description,
		Category:    in.Category,
		Brand:       in.Brand,
		Images:      in.Images,
		Rating:      in.Rating,
		IsActive:    in.IsActive,
	}
}

// Create menambah produk beserta gambarnya dalam **satu transaksi** (issue #4).
//
// Kalau salah satu insert gambar gagal, insert produk ikut dibatalkan sehingga
// tidak ada produk setengah jadi. Invarian ini milik use case (ARCHITECTURE §7),
// bukan tanggungan pemanggil — jadi tidak mungkin lupa membungkusnya.
func (s *Service) Create(ctx context.Context, in CreateProductInput) (*domain.Product, error) {
	var created *domain.Product
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		p, err := s.products.Create(ctx, in.toDomain())
		if err != nil {
			return err
		}
		created = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// Update mengubah sebagian produk. Bila `patch.Images` diisi, seluruh gambar
// diganti; perubahan produk dan penggantian gambar berjalan dalam **satu
// transaksi** (issue #4) sehingga gambar lama tidak hilang bila insert baru
// gagal. `patch` memakai tipe domain karena bentuknya memang sudah patch.
func (s *Service) Update(ctx context.Context, id string, patch domain.ProductPatch) (*domain.Product, error) {
	var updated *domain.Product
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		p, err := s.products.Update(ctx, id, patch)
		if err != nil {
			return err
		}
		updated = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// Delete menghapus produk beserta metadata gambarnya dalam **satu transaksi**
// (issue #4).
//
// Repositori mengembalikan gambar yang terlepas supaya bisa dibersihkan di
// provider **setelah commit** (ARCHITECTURE §7). Pembersihan itu menunggu modul
// media, jadi untuk sekarang kelepasan gambar hanya dicatat di log.
func (s *Service) Delete(ctx context.Context, id string) error {
	var removed []domain.Image
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		images, err := s.products.Delete(ctx, id)
		if err != nil {
			return err
		}
		removed = images
		return nil
	})
	if err != nil {
		return err
	}
	if len(removed) > 0 && s.log != nil {
		s.log.InfoContext(ctx, "produk dihapus; gambar menunggu pembersihan provider",
			"product_id", id,
			"images", len(removed),
		)
	}
	return nil
}
