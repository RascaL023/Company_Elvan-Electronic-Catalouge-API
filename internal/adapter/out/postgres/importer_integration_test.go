package postgres

// Test integrasi adapter importer (Fase 6): upsert idempoten berbasis
// legacy_id, resolusi category/brand slug -> foreign key, penggantian gambar,
// dan pemertahanan created_at dari dokumen sumber.
//
// Seperti test tulis lainnya, transaksi dibuka use case sehingga datanya
// di-commit; dibersihkan lewat t.Cleanup berdasarkan legacy_id (lihat catatan
// di product_write_integration_test.go).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"elvan-catalog-api/internal/application/importer"
	"elvan-catalog-api/internal/domain"
)

func newImportService(pool *pgxpool.Pool) *importer.Service {
	return importer.New(
		NewImporterRepository(pool),
		NewCategoryRepository(pool),
		NewBrandRepository(pool),
		NewTxManager(pool),
		nil,
	)
}

// cleanupImport menghapus baris berdasar legacy_id (urut produk dulu karena
// ON DELETE RESTRICT pada FK kategori/brand).
func cleanupImport(t *testing.T, pool *pgxpool.Pool, ctx context.Context, productLegacy, categoryLegacy, brandLegacy string) {
	t.Helper()
	t.Cleanup(func() {
		if productLegacy != "" {
			if _, err := pool.Exec(ctx,
				"DELETE FROM products WHERE legacy_id = $1", productLegacy); err != nil {
				t.Logf("cleanup produk legacy=%q: %v", productLegacy, err)
			}
		}
		if brandLegacy != "" {
			if _, err := pool.Exec(ctx,
				"DELETE FROM brands WHERE legacy_id = $1", brandLegacy); err != nil {
				t.Logf("cleanup brand legacy=%q: %v", brandLegacy, err)
			}
		}
		if categoryLegacy != "" {
			if _, err := pool.Exec(ctx,
				"DELETE FROM categories WHERE legacy_id = $1", categoryLegacy); err != nil {
				t.Logf("cleanup kategori legacy=%q: %v", categoryLegacy, err)
			}
		}
	})
}

func importDataset(suffix string) *importer.Dataset {
	createdAt := time.Date(2024, 5, 1, 10, 30, 0, 0, time.UTC)
	active := true
	return &importer.Dataset{
		Categories: []importer.CategoryDoc{{
			LegacyID: "cat-it-" + suffix, Name: "Television IT", Slug: "it-tv-" + suffix,
			Description: "dari impor", CreatedAt: createdAt.Format(time.RFC3339),
		}},
		Brands: []importer.BrandDoc{{
			LegacyID: "brand-it-" + suffix, Name: "Polytron", Slug: "it-brand-" + suffix,
		}},
		Products: []importer.ProductDoc{{
			LegacyID:     "prod-it-" + suffix,
			Name:         "TV 24 IT",
			Slug:         "it-prod-" + suffix,
			Price:        1350000,
			Description:  "desc",
			Category:     "it-tv-" + suffix,
			Brand:        "it-brand-" + suffix,
			Images:       []string{"assets/images/products/it/a.jpg", "assets/images/products/it/b.jpg"},
			ImageFileIds: []string{"file-a", "file-b"},
			Rating:       importer.RatingDoc{Rate: 4.4, Count: 85},
			IsActive:     &active,
			CreatedAt:    createdAt.Format(time.RFC3339),
		}},
	}
}

func TestIntegrationImportIdempotent(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	svc := newImportService(pool)
	suffix := "imp" + uuid.NewString()[:8]

	ds := importDataset(suffix)
	cleanupImport(t, pool, ctx, ds.Products[0].LegacyID, ds.Categories[0].LegacyID, ds.Brands[0].LegacyID)

	// 1. Impor pertama.
	res, err := svc.Import(ctx, ds)
	if err != nil {
		t.Fatalf("impor pertama: %v", err)
	}
	if res.Categories != 1 || res.Brands != 1 || res.Products != 1 || res.Images != 2 {
		t.Fatalf("result = %+v", res)
	}

	// 2. Impor ulang: idempoten, tidak menggandakan baris.
	before, err := svc.Counts(ctx)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if _, err := svc.Import(ctx, ds); err != nil {
		t.Fatalf("impor kedua: %v", err)
	}
	after, err := svc.Counts(ctx)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if before.Categories != after.Categories || before.Brands != after.Brands || before.Products != after.Products {
		t.Errorf("impor ulang mengubah jumlah baris: %+v -> %+v", before, after)
	}

	// 3. Produk terbaca lengkap: resolusi slug + gambar berpasangan.
	repo := NewProductRepository(pool, nil)
	p, err := repo.GetByID(ctx, ds.Products[0].LegacyID)
	if err != nil {
		t.Fatalf("get by legacy id: %v", err)
	}
	if p.Category != ds.Products[0].Category || p.Brand != ds.Products[0].Brand {
		t.Errorf("kategori/brand = %q/%q, ingin %q/%q",
			p.Category, p.Brand, ds.Products[0].Category, ds.Products[0].Brand)
	}
	if len(p.Images) != 2 {
		t.Fatalf("jumlah gambar = %d, ingin 2", len(p.Images))
	}
	if p.Images[0].FileID != "file-a" || p.Images[1].FileID != "file-b" {
		t.Errorf("fileId tidak terpasang per index: %+v", p.Images)
	}

	// 4. created_at dipertahankan dari dokumen sumber (urutan /catalog).
	wantCreated := time.Date(2024, 5, 1, 10, 30, 0, 0, time.UTC)
	if !p.CreatedAt.Equal(wantCreated) {
		t.Errorf("createdAt = %s, ingin %s", p.CreatedAt.Format(time.RFC3339), wantCreated.Format(time.RFC3339))
	}
}

func TestIntegrationImportUpdatesExistingRow(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	svc := newImportService(pool)
	suffix := "upd" + uuid.NewString()[:8]

	ds := importDataset(suffix)
	cleanupImport(t, pool, ctx, ds.Products[0].LegacyID, ds.Categories[0].LegacyID, ds.Brands[0].LegacyID)

	if _, err := svc.Import(ctx, ds); err != nil {
		t.Fatalf("impor pertama: %v", err)
	}

	// Ubah data sumber lalu impor lagi: baris sama harus diperbarui.
	ds.Products[0].Name = "TV 24 Diperbarui"
	ds.Products[0].Price = 999000
	ds.Products[0].Images = []string{"assets/images/products/it/satu.jpg"} // gambar diganti
	ds.Products[0].ImageFileIds = []string{"file-satu"}
	if _, err := svc.Import(ctx, ds); err != nil {
		t.Fatalf("impor kedua: %v", err)
	}

	repo := NewProductRepository(pool, nil)
	p, err := repo.GetByID(ctx, ds.Products[0].LegacyID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p.Name != "TV 24 Diperbarui" || p.Price != 999000 {
		t.Errorf("produk = %+v, ingin nama & harga terbaru", p)
	}
	if len(p.Images) != 1 || p.Images[0].FileID != "file-satu" {
		t.Errorf("gambar = %+v, ingin diganti dengan versi sumber", p.Images)
	}
	if n := countProductsBySlug(t, pool, ctx, ds.Products[0].Slug); n != 1 {
		t.Errorf("produk %q berjumlah %d, ingin 1 (idempoten)", ds.Products[0].Slug, n)
	}
}

func TestIntegrationImportRejectsUnknownReference(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	svc := newImportService(pool)
	suffix := "ref" + uuid.NewString()[:8]

	ds := importDataset(suffix)
	cleanupImport(t, pool, ctx, ds.Products[0].LegacyID, ds.Categories[0].LegacyID, ds.Brands[0].LegacyID)

	// Kategori sengaja tidak ikut diimpor: produk merujuk slug yang tidak ada.
	ds.Categories = nil
	ds.Brands = nil

	_, err := svc.Import(ctx, ds)
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("impor dengan rujukan hilang = %v, ingin ErrValidation", err)
	}

	// Tidak ada baris yang tertinggal (satu transaksi).
	var n int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM products WHERE legacy_id = $1",
		ds.Products[0].LegacyID).Scan(&n); err != nil {
		t.Fatalf("hitung: %v", err)
	}
	if n != 0 {
		t.Errorf("produk tertinggal setelah impor gagal (count=%d)", n)
	}
}
