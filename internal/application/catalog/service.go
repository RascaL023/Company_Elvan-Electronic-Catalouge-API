// Package catalog berisi use case katalog produk.
package catalog

import (
	"context"
	"errors"
	"log/slog"

	"elvan-catalog-api/internal/application"
	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// ImageCleaner membersihkan berkas gambar di provider setelah transaksi commit
// (ARCHITECTURE §11). Implementasi konkretnya use case media; interface ini
// didefinisikan di sisi konsumen supaya paket catalog tidak bergantung pada
// paket media.
type ImageCleaner interface {
	DeleteImages(ctx context.Context, fileIDs []string) (port.DeleteResult, error)
}

// Service adalah use case katalog. Operasi baca tersedia sejak Fase 2; operasi
// tulis (Create/Update/Delete) mulai dibangun pada Fase 4. Pembersihan berkas
// gambar server-side (Fase 5) memakai `cleaner` bila diisi.
type Service struct {
	products   port.ProductRepository
	categories port.CategoryRepository
	brands     port.BrandRepository
	tx         port.TxManager
	cleaner    ImageCleaner
	log        *slog.Logger
}

// New membuat Service katalog. `categories`/`brands` dipakai memvalidasi
// referensi sebelum menulis produk (issue #3); `tx` adalah unit of work untuk
// operasi tulis (issue #4); `cleaner` menghapus berkas gambar yang terlepas
// **setelah** commit (boleh nil, mis. saat media tidak dikonfigurasi).
func New(
	products port.ProductRepository,
	categories port.CategoryRepository,
	brands port.BrandRepository,
	tx port.TxManager,
	cleaner ImageCleaner,
	log *slog.Logger,
) *Service {
	return &Service{products: products, categories: categories, brands: brands, tx: tx, cleaner: cleaner, log: log}
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

// validateRefs memastikan slug category/brand yang akan ditulis benar-benar ada
// (issue #3). Tanpa ini, category yang tidak ada gagal lewat `NOT NULL` (pesan
// tidak informatif) sedangkan brand yang tidak ada **diam-diam dikosongkan**
// atau perubahannya diabaikan.
//
// `nil` berarti field tidak diubah (patch parsial) sehingga tidak divalidasi;
// brand bernilai "" berarti produk memang tanpa brand. Dijalankan **di dalam**
// transaksi penulisan supaya tidak ada TOCTOU: kategori tidak bisa dihapus di
// antara validasi dan insert.
func (s *Service) validateRefs(ctx context.Context, category, brand *string) error {
	if category != nil {
		if _, err := s.categories.GetBySlug(ctx, *category); err != nil {
			return refNotFound("category", *category, err)
		}
	}
	if brand != nil && *brand != "" {
		if _, err := s.brands.GetBySlug(ctx, *brand); err != nil {
			return refNotFound("brand", *brand, err)
		}
	}
	return nil
}

// refNotFound mengubah `ErrNotFound` dari lookup referensi menjadi error
// validasi per-field; error lain (mis. database bermasalah) diteruskan apa
// adanya agar tetap menjadi 500, bukan 400.
func refNotFound(field, slug string, err error) error {
	if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	v := domain.NewValidationError()
	v.Add(field, "tidak ditemukan: "+slug)
	return v
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
// Referensi category/brand divalidasi lebih dulu di dalam transaksi yang sama
// (issue #3), jadi slug yang tidak dikenal berhenti sebagai `400` sebelum ada
// baris apa pun yang ditulis.
//
// Kalau salah satu insert gambar gagal, insert produk ikut dibatalkan sehingga
// tidak ada produk setengah jadi. Invarian ini milik use case (ARCHITECTURE §7),
// bukan tanggungan pemanggil — jadi tidak mungkin lupa membungkusnya.
func (s *Service) Create(ctx context.Context, in CreateProductInput) (*domain.Product, error) {
	var created *domain.Product
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.validateRefs(ctx, &in.Category, &in.Brand); err != nil {
			return err
		}
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
// gagal. Field `Category`/`Brand` yang diubah divalidasi lebih dulu
// (issue #3) — brand yang tidak ada tidak lagi mengosongkan brand secara diam-diam. `patch` memakai tipe domain karena bentuknya memang sudah patch.
func (s *Service) Update(ctx context.Context, id string, patch domain.ProductPatch) (*domain.Product, error) {
	// Bila gambar diganti, catat fileId lama **sebelum** update supaya berkas
	// yang dilepas bisa dibersihkan setelah commit. Dibaca di luar transaksi
	// karena hanya untuk keperluan pembersihan best-effort.
	var removed []string
	if patch.Images != nil {
		existing, err := s.products.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		removed = removedFileIDs(fileIDsOf(existing.Images), *patch.Images)
	}

	var updated *domain.Product
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.validateRefs(ctx, patch.Category, patch.Brand); err != nil {
			return err
		}
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
	s.cleanupImages(ctx, removed)
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
	s.cleanupImages(ctx, fileIDsOf(removed))
	return nil
}

// cleanupImages menghapus berkas gambar di provider **setelah commit**
// (ARCHITECTURE §11). Best-effort: kegagalan tidak membatalkan operasi DB dan
// sudah dicatat di log oleh use case media. Gambar tanpa fileId dilewati oleh
// adapter.
func (s *Service) cleanupImages(ctx context.Context, fileIDs []string) {
	if len(fileIDs) == 0 {
		return
	}
	if s.cleaner == nil {
		if s.log != nil {
			s.log.InfoContext(ctx, "gambar terlepas menunggu pembersihan provider (media belum dikonfigurasi)",
				"file_ids", fileIDs,
			)
		}
		return
	}
	if _, err := s.cleaner.DeleteImages(ctx, fileIDs); err != nil {
		if s.log != nil {
			s.log.ErrorContext(ctx, "pembersihan gambar setelah commit gagal",
				"file_ids", fileIDs,
				"error", err.Error(),
			)
		}
	}
}

// fileIDsOf mengambil fileId dari daftar gambar, membuang yang kosong.
func fileIDsOf(images []domain.Image) []string {
	out := make([]string, 0, len(images))
	for _, img := range images {
		if img.FileID != "" {
			out = append(out, img.FileID)
		}
	}
	return out
}

// removedFileIDs mengembalikan fileId lama yang tidak lagi dipakai gambar
// baru — inilah berkas yang perlu dihapus di provider.
func removedFileIDs(old []string, next []domain.Image) []string {
	keep := make(map[string]struct{}, len(next))
	for _, img := range next {
		if img.FileID != "" {
			keep[img.FileID] = struct{}{}
		}
	}
	out := make([]string, 0, len(old))
	for _, id := range old {
		if _, ok := keep[id]; !ok {
			out = append(out, id)
		}
	}
	return out
}
