package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// Test integrasi ini berjalan terhadap PostgreSQL nyata memakai DATABASE_URL.
// Setiap test berjalan di dalam satu transaksi yang SELALU di-rollback, jadi
// data di database tidak pernah berubah. Bila DATABASE_URL tidak diset,
// test dilewati (t.Skip) sehingga `go test ./...` tetap aman tanpa DB.
//
// Jalankan:  set -a; . ./.env; set +a; go test ./internal/adapter/out/postgres/ -run Integration -v

// errRollback menandai bahwa transaksi test sengaja dibatalkan.
var errRollback = errors.New("rollback test")

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL tidak diset; lewati test integrasi")
	}
	pool, err := NewPool(context.Background(), dsn, 4)
	if err != nil {
		t.Fatalf("tidak dapat terhubung ke database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// inRollbackTx menjalankan fn di dalam satu transaksi yang pasti di-rollback.
func inRollbackTx(t *testing.T, pool *pgxpool.Pool, fn func(ctx context.Context)) {
	t.Helper()
	txm := NewTxManager(pool)
	err := txm.WithinTx(context.Background(), func(ctx context.Context) error {
		fn(ctx)
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("transaksi test tidak ter-rollback dengan benar: %v", err)
	}
}

// setLegacyID mengisi legacy_id sebuah produk (tidak tersedia lewat API create).
func setLegacyID(t *testing.T, ctx context.Context, productID, legacyID string) {
	t.Helper()
	tx, ok := txFromContext(ctx)
	if !ok {
		t.Fatal("tidak ada transaksi di context")
	}
	uid, err := uuid.Parse(productID)
	if err != nil {
		t.Fatalf("parse productID: %v", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE products SET legacy_id = $1 WHERE id = $2", legacyID, uid); err != nil {
		t.Fatalf("set legacy_id: %v", err)
	}
}

func TestIntegrationCategoryCRUD(t *testing.T) {
	pool := testPool(t)
	repo := NewCategoryRepository(pool)
	suffix := uuid.NewString()[:8]
	slug := "it-kat-" + suffix

	inRollbackTx(t, pool, func(ctx context.Context) {
		created, err := repo.Create(ctx, domain.Category{Name: "Kategori IT", Slug: slug, Description: "desc"})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if created.ID == "" {
			t.Fatal("ID hasil create kosong")
		}

		got, err := repo.GetByID(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.Slug != slug || got.Description != "desc" {
			t.Errorf("hasil tidak sesuai: %+v", got)
		}

		newName := "Kategori IT 2"
		updated, err := repo.Update(ctx, created.ID, domain.CategoryPatch{Name: &newName})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if updated.Name != newName {
			t.Errorf("name = %q, ingin %q", updated.Name, newName)
		}
		if updated.Slug != slug {
			t.Errorf("slug berubah tanpa diminta: %q", updated.Slug)
		}

		if _, err := repo.GetByID(ctx, "bukan-uuid"); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("id non-uuid seharusnya ErrNotFound, dapat %v", err)
		}

		if err := repo.Delete(ctx, created.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := repo.GetByID(ctx, created.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("setelah delete seharusnya ErrNotFound, dapat %v", err)
		}
		if err := repo.Delete(ctx, created.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("delete kedua seharusnya ErrNotFound, dapat %v", err)
		}
	})

	// Kasus error sengaja diuji di transaksi terpisah: satu error database
	// membatalkan seluruh transaksi, jadi ujinya harus jadi statement terakhir
	// di transaksinya sendiri.
	inRollbackTx(t, pool, func(ctx context.Context) {
		if _, err := repo.Create(ctx, domain.Category{Name: "Kategori IT", Slug: slug}); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := repo.Create(ctx, domain.Category{Name: "Duplikat", Slug: slug}); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("slug duplikat seharusnya ErrConflict, dapat %v", err)
		}
	})
}

func TestIntegrationBrandCRUD(t *testing.T) {
	pool := testPool(t)
	repo := NewBrandRepository(pool)
	suffix := uuid.NewString()[:8]
	slug := "it-brand-" + suffix

	inRollbackTx(t, pool, func(ctx context.Context) {
		created, err := repo.Create(ctx, domain.Brand{Name: "Brand IT", Slug: slug})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		got, err := repo.GetByID(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.Slug != slug {
			t.Errorf("slug = %q, ingin %q", got.Slug, slug)
		}

		newName := "Brand IT 2"
		if _, err := repo.Update(ctx, created.ID, domain.BrandPatch{Name: &newName}); err != nil {
			t.Fatalf("Update: %v", err)
		}

		if err := repo.Delete(ctx, created.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := repo.GetByID(ctx, created.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("setelah delete seharusnya ErrNotFound, dapat %v", err)
		}
	})

	inRollbackTx(t, pool, func(ctx context.Context) {
		if _, err := repo.Create(ctx, domain.Brand{Name: "Brand IT", Slug: slug}); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := repo.Create(ctx, domain.Brand{Name: "Dup", Slug: slug}); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("slug duplikat seharusnya ErrConflict, dapat %v", err)
		}
	})
}

func TestIntegrationProductLifecycle(t *testing.T) {
	pool := testPool(t)
	cats := NewCategoryRepository(pool)
	brands := NewBrandRepository(pool)
	products := NewProductRepository(pool)
	suffix := uuid.NewString()[:8]

	inRollbackTx(t, pool, func(ctx context.Context) {
		cat, err := cats.Create(ctx, domain.Category{Name: "Kat IT", Slug: "it-cat-" + suffix})
		if err != nil {
			t.Fatalf("seed kategori: %v", err)
		}
		brand, err := brands.Create(ctx, domain.Brand{Name: "Brand IT", Slug: "it-brand-" + suffix})
		if err != nil {
			t.Fatalf("seed brand: %v", err)
		}

		created, err := products.Create(ctx, domain.Product{
			Name: "Produk IT", Slug: "it-prod-" + suffix, Price: 1500000,
			Description: "deskripsi", Category: cat.Slug, Brand: brand.Slug,
			Rating:   domain.Rating{Rate: 4.5, Count: 12},
			IsActive: true,
			Images: []domain.Image{
				{Key: "assets/images/products/it/a.jpg", FileID: "file-a"},
				{Key: "assets/images/products/it/b.jpg", FileID: "file-b"},
			},
		})
		if err != nil {
			t.Fatalf("Create produk: %v", err)
		}

		got, err := products.GetByID(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.Category != cat.Slug || got.Brand != brand.Slug {
			t.Errorf("slug tidak ter-resolve: category=%q brand=%q", got.Category, got.Brand)
		}
		if len(got.Images) != 2 || got.Images[0].Key != "assets/images/products/it/a.jpg" || got.Images[0].FileID != "file-a" {
			t.Errorf("gambar tidak sesuai urutan/fileId: %+v", got.Images)
		}
		if got.Rating.Rate != 4.5 || got.Rating.Count != 12 {
			t.Errorf("rating tidak sesuai: %+v", got.Rating)
		}

		// Fallback legacy_id (URL lama).
		legacy := "legacy-" + suffix
		setLegacyID(t, ctx, created.ID, legacy)
		byLegacy, err := products.GetByID(ctx, legacy)
		if err != nil {
			t.Fatalf("GetByID(legacy): %v", err)
		}
		if byLegacy.ID != created.ID {
			t.Errorf("lookup legacy id = %q, ingin %q", byLegacy.ID, created.ID)
		}

		// Update harga + ganti gambar.
		newPrice := int64(2000000)
		newImages := []domain.Image{{Key: "assets/images/products/it/c.jpg", FileID: "file-c"}}
		updated, err := products.Update(ctx, created.ID, domain.ProductPatch{Price: &newPrice, Images: &newImages})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if updated.Price != newPrice {
			t.Errorf("price = %d, ingin %d", updated.Price, newPrice)
		}
		if len(updated.Images) != 1 || updated.Images[0].FileID != "file-c" {
			t.Errorf("gambar setelah update tidak sesuai: %+v", updated.Images)
		}

		// Delete harus mengembalikan gambar yang terlepas.
		removed, err := products.Delete(ctx, created.ID)
		if err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if len(removed) != 1 || removed[0].FileID != "file-c" {
			t.Errorf("gambar terlepas tidak sesuai: %+v", removed)
		}
		if _, err := products.GetByID(ctx, created.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("setelah delete seharusnya ErrNotFound, dapat %v", err)
		}
	})
}

func TestIntegrationProductDeleteRestrictedByCategory(t *testing.T) {
	pool := testPool(t)
	cats := NewCategoryRepository(pool)
	products := NewProductRepository(pool)
	suffix := uuid.NewString()[:8]

	inRollbackTx(t, pool, func(ctx context.Context) {
		cat, err := cats.Create(ctx, domain.Category{Name: "Kat IT", Slug: "it-restrict-" + suffix})
		if err != nil {
			t.Fatalf("seed kategori: %v", err)
		}
		if _, err := products.Create(ctx, domain.Product{
			Name: "Produk IT", Slug: "it-restrict-prod-" + suffix, Price: 1000,
			Category: cat.Slug, IsActive: true,
		}); err != nil {
			t.Fatalf("seed produk: %v", err)
		}

		// Kategori masih dipakai -> RESTRICT -> conflict.
		if err := cats.Delete(ctx, cat.ID); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("hapus kategori terpakai seharusnya ErrConflict, dapat %v", err)
		}
	})
}

func TestIntegrationProductKeysetPagination(t *testing.T) {
	pool := testPool(t)
	cats := NewCategoryRepository(pool)
	products := NewProductRepository(pool)
	suffix := uuid.NewString()[:8]

	inRollbackTx(t, pool, func(ctx context.Context) {
		cat, err := cats.Create(ctx, domain.Category{Name: "Kat IT", Slug: "it-list-" + suffix})
		if err != nil {
			t.Fatalf("seed kategori: %v", err)
		}

		prices := []int64{100, 200, 300, 400, 500}
		var createdIDs []string
		for i, price := range prices {
			p, err := products.Create(ctx, domain.Product{
				Name:     fmt.Sprintf("Produk IT %d", i),
				Slug:     fmt.Sprintf("it-list-%s-%d", suffix, i),
				Price:    price,
				Category: cat.Slug,
				IsActive: true,
				Rating:   domain.Rating{Rate: float64(i), Count: i},
			})
			if err != nil {
				t.Fatalf("seed produk %d: %v", i, err)
			}
			createdIDs = append(createdIDs, p.ID)
		}

		// Produk tidak aktif tidak boleh muncul di list publik.
		if _, err := products.Create(ctx, domain.Product{
			Name: "Produk IT nonaktif", Slug: "it-list-" + suffix + "-off",
			Price: 999, Category: cat.Slug, IsActive: false,
		}); err != nil {
			t.Fatalf("seed produk nonaktif: %v", err)
		}

		base := port.ProductQuery{Category: cat.Slug, Limit: 2}

		// 1. Semua produk aktif terkumpul tepat sekali lewat semua halaman.
		q := base
		q.Sort = domain.SortDefault
		all := collectCatalog(t, ctx, products, q)
		if len(all) != len(createdIDs) {
			t.Fatalf("jumlah produk = %d, ingin %d", len(all), len(createdIDs))
		}
		seen := map[string]int{}
		for _, p := range all {
			seen[p.ID]++
		}
		for _, id := range createdIDs {
			if seen[id] != 1 {
				t.Errorf("id %s muncul %d kali, ingin tepat 1", id, seen[id])
			}
		}

		// 2. Urutan harga naik / turun.
		assertPrices(t, collectCatalog(t, ctx, products, port.ProductQuery{Category: cat.Slug, Limit: 3, Sort: domain.SortPriceAsc}), prices)
		assertPrices(t, collectCatalog(t, ctx, products, port.ProductQuery{Category: cat.Slug, Limit: 3, Sort: domain.SortPriceDesc}), []int64{500, 400, 300, 200, 100})

		// 3. Urutan rating menurun.
		rating := collectCatalog(t, ctx, products, port.ProductQuery{Category: cat.Slug, Limit: 3, Sort: domain.SortRatingDesc})
		for i := 1; i < len(rating); i++ {
			if rating[i-1].Rating.Rate < rating[i].Rating.Rate {
				t.Errorf("rating tidak menurun di index %d: %.1f < %.1f", i, rating[i-1].Rating.Rate, rating[i].Rating.Rate)
			}
		}

		// 4. includeInactive benar-benar menyertakan produk nonaktif.
		withInactive := collectCatalog(t, ctx, products, port.ProductQuery{Category: cat.Slug, Limit: 100, Sort: domain.SortDefault, IncludeInactive: true})
		if len(withInactive) != len(createdIDs)+1 {
			t.Errorf("dengan includeInactive jumlah = %d, ingin %d", len(withInactive), len(createdIDs)+1)
		}

		// 5. Cursor milik sort lain ditolak.
		first, err := products.List(ctx, port.ProductQuery{Category: cat.Slug, Limit: 2, Sort: domain.SortDefault})
		if err != nil {
			t.Fatalf("List halaman pertama: %v", err)
		}
		if !first.HasMore || first.Cursor == "" {
			t.Fatalf("halaman pertama seharusnya hasMore dengan cursor, dapat hasMore=%v cursor=%q", first.HasMore, first.Cursor)
		}
		if _, err := products.List(ctx, port.ProductQuery{Category: cat.Slug, Limit: 2, Sort: domain.SortPriceAsc, Cursor: first.Cursor}); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("cursor sort mismatch seharusnya ErrValidation, dapat %v", err)
		}
		if _, err := products.List(ctx, port.ProductQuery{Category: cat.Slug, Limit: 2, Sort: domain.SortDefault, Cursor: "!!bukan-cursor!!"}); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("cursor rusak seharusnya ErrValidation, dapat %v", err)
		}
		if _, err := products.List(ctx, port.ProductQuery{Category: cat.Slug, Limit: 2, Sort: "bogus"}); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("sort tidak dikenal seharusnya ErrValidation, dapat %v", err)
		}
	})
}

func TestIntegrationListAllProjectsThumbnail(t *testing.T) {
	pool := testPool(t)
	cats := NewCategoryRepository(pool)
	products := NewProductRepository(pool)
	suffix := uuid.NewString()[:8]

	inRollbackTx(t, pool, func(ctx context.Context) {
		cat, err := cats.Create(ctx, domain.Category{Name: "Kat IT", Slug: "it-catalog-" + suffix})
		if err != nil {
			t.Fatalf("seed kategori: %v", err)
		}
		created, err := products.Create(ctx, domain.Product{
			Name: "Produk Katalog", Slug: "it-catalog-prod-" + suffix, Price: 12345,
			Category: cat.Slug, IsActive: true,
			Images: []domain.Image{
				{Key: "assets/images/products/it/thumb.jpg", FileID: "t1"},
				{Key: "assets/images/products/it/second.jpg", FileID: "t2"},
			},
		})
		if err != nil {
			t.Fatalf("seed produk: %v", err)
		}

		// ListAll mengembalikan katalog; cari produk kita.
		all, err := products.ListAll(ctx, false)
		if err != nil {
			t.Fatalf("ListAll: %v", err)
		}
		var found *domain.CatalogProduct
		for i := range all {
			if all[i].ID == created.ID {
				found = &all[i]
			}
		}
		if found == nil {
			t.Fatalf("produk %s tidak ditemukan di ListAll", created.ID)
		}
		if found.Thumbnail != "assets/images/products/it/thumb.jpg" {
			t.Errorf("thumbnail = %q, ingin gambar posisi 0", found.Thumbnail)
		}
		if found.Category != cat.Slug {
			t.Errorf("category = %q, ingin %q", found.Category, cat.Slug)
		}
	})
}

// --- bantuan test -----------------------------------------------------------

// collectCatalog mengumpulkan seluruh halaman sampai hasMore=false.
func collectCatalog(t *testing.T, ctx context.Context, repo *ProductRepository, q port.ProductQuery) []domain.CatalogProduct {
	t.Helper()
	var out []domain.CatalogProduct
	for i := 0; i < 30; i++ {
		page, err := repo.List(ctx, q)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		out = append(out, page.Products...)
		if !page.HasMore {
			return out
		}
		if page.Cursor == "" {
			t.Fatal("hasMore=true tetapi cursor kosong")
		}
		q.Cursor = page.Cursor
	}
	t.Fatal("pagination tidak selesai setelah 30 halaman")
	return nil
}

func assertPrices(t *testing.T, products []domain.CatalogProduct, want []int64) {
	t.Helper()
	if len(products) != len(want) {
		t.Fatalf("jumlah produk = %d, ingin %d", len(products), len(want))
	}
	for i, p := range products {
		if p.Price != want[i] {
			t.Errorf("harga di index %d = %d, ingin %d", i, p.Price, want[i])
		}
	}
}
