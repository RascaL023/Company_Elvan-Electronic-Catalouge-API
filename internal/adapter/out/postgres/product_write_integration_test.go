package postgres

// Test tulis produk di sini menguji **atomisitas** (issue #4). Berbeda dengan
// test lain di package ini, test di sini sengaja TIDAK memakai `inRollbackTx`:
// transaksi dibuka oleh use case (bukan oleh test), dan tujuannya justru
// mengamati apa yang tersisa di database *setelah* transaksi itu dibatalkan.
// Karena itu test di sini memakai data yang di-commit lalu membersihkannya
// sendiri lewat `t.Cleanup`.
//
// Kegagalan insert gambar dipaksa dengan menyisipkan byte NUL ke `image.key`:
// PostgreSQL menolak `0x00` di kolom `text` ("invalid byte sequence for
// encoding UTF8"), dan itu terjadi tepat di tengah penggantian gambar.

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"elvan-catalog-api/internal/application/catalog"
	"elvan-catalog-api/internal/domain"
)

func TestIntegrationProductCreateAtomicOnImageFailure(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cats := NewCategoryRepository(pool)
	products := NewProductRepository(pool, nil)
	svc := catalog.New(products, NewTxManager(pool), nil)

	suffix := uuid.NewString()[:8]
	cat, err := cats.Create(ctx, domain.Category{Name: "Kat Atomic", Slug: "it-atomic-cat-" + suffix})
	if err != nil {
		t.Fatalf("seed kategori: %v", err)
	}
	slug := "it-atomic-prod-" + suffix
	cleanupProductAndCategory(t, pool, ctx, slug, cat.ID)

	_, err = svc.Create(ctx, catalog.CreateProductInput{
		Name:     "Produk Atomic",
		Slug:     slug,
		Price:    1000,
		Category: cat.Slug,
		IsActive: true,
		Images:   []domain.Image{{Key: "assets/images/products/it/atomic-\x00.jpg", FileID: "f1"}},
	})
	if err == nil {
		t.Fatal("Create dengan gambar tidak valid seharusnya gagal")
	}

	if n := countProductsBySlug(t, pool, ctx, slug); n != 0 {
		t.Errorf("produk tertinggal setelah insert gambar gagal (count=%d): Create tidak atomik", n)
	}
}

func TestIntegrationProductUpdateAtomicOnImageFailure(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cats := NewCategoryRepository(pool)
	products := NewProductRepository(pool, nil)
	svc := catalog.New(products, NewTxManager(pool), nil)

	suffix := uuid.NewString()[:8]
	cat, err := cats.Create(ctx, domain.Category{Name: "Kat Atomic", Slug: "it-atomic-cat-" + suffix})
	if err != nil {
		t.Fatalf("seed kategori: %v", err)
	}
	slug := "it-atomic-prod-" + suffix
	cleanupProductAndCategory(t, pool, ctx, slug, cat.ID)

	created, err := svc.Create(ctx, catalog.CreateProductInput{
		Name:     "Produk Atomic",
		Slug:     slug,
		Price:    1000,
		Category: cat.Slug,
		IsActive: true,
		Images: []domain.Image{
			{Key: "assets/images/products/it/keep-0.jpg", FileID: "k0"},
			{Key: "assets/images/products/it/keep-1.jpg", FileID: "k1"},
		},
	})
	if err != nil {
		t.Fatalf("seed produk: %v", err)
	}

	bad := []domain.Image{{Key: "assets/images/products/it/bad-\x00.jpg", FileID: "bad"}}
	if _, err := svc.Update(ctx, created.ID, domain.ProductPatch{Images: &bad}); err == nil {
		t.Fatal("Update dengan gambar tidak valid seharusnya gagal")
	}

	// Gambar lama harus utuh: penggantian gambar tidak boleh "terlanjur"
	// menghapus gambar lama sebelum insert baru sukses.
	got, err := products.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID setelah update gagal: %v", err)
	}
	if len(got.Images) != 2 {
		t.Fatalf("gambar setelah update gagal = %d, ingin 2 (gambar lama utuh)", len(got.Images))
	}
	if got.Images[0].Key != "assets/images/products/it/keep-0.jpg" ||
		got.Images[1].Key != "assets/images/products/it/keep-1.jpg" {
		t.Errorf("gambar lama berubah: %+v", got.Images)
	}
}

// cleanupProductAndCategory menghapus produk lalu kategorinya setelah test.
// Produk dihapus lebih dulu karena FK `products.category_id` memakai
// ON DELETE RESTRICT.
func cleanupProductAndCategory(t *testing.T, pool *pgxpool.Pool, ctx context.Context, productSlug, categoryID string) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DELETE FROM products WHERE slug = $1", productSlug); err != nil {
			t.Logf("cleanup produk %q: %v", productSlug, err)
		}
		if _, err := pool.Exec(ctx, "DELETE FROM categories WHERE id = $1", categoryID); err != nil {
			t.Logf("cleanup kategori %q: %v", categoryID, err)
		}
	})
}

func countProductsBySlug(t *testing.T, pool *pgxpool.Pool, ctx context.Context, slug string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM products WHERE slug = $1", slug).Scan(&n); err != nil {
		t.Fatalf("hitung produk %q: %v", slug, err)
	}
	return n
}
