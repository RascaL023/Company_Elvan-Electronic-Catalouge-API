package postgres

// Test validasi referensi (issue #3): slug category/brand yang tidak ada harus
// berhenti sebagai error validasi dan **tidak mengubah database**. Seperti test
// atomisitas, transaksi di sini dibuka use case, jadi datanya di-commit lalu
// dibersihkan lewat `t.Cleanup` (lihat catatan di product_write_integration_test.go).
//
// Empat kasus sesuai kriteria issue: create/update × category/brand.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"elvan-catalog-api/internal/application/catalog"
	"elvan-catalog-api/internal/domain"
)

// newWriteService merangkai use case katalog dengan adapter Postgres nyata.
func newWriteService(pool *pgxpool.Pool) *catalog.Service {
	return catalog.New(
		NewProductRepository(pool, nil),
		NewCategoryRepository(pool),
		NewBrandRepository(pool),
		NewTxManager(pool),
		nil,
		nil,
	)
}

func TestIntegrationCreateRejectsUnknownCategory(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	svc := newWriteService(pool)

	slug := "it-ref-prod-" + uuid.NewString()[:8]
	cleanupFixtures(t, pool, ctx, slug, "", "")

	_, err := svc.Create(ctx, catalog.CreateProductInput{
		Name: "Produk Ref", Slug: slug, Price: 1000, Category: "kategori-tidak-ada", IsActive: true,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Create dengan category tidak ada = %v, ingin ErrValidation", err)
	}
	if n := countProductsBySlug(t, pool, ctx, slug); n != 0 {
		t.Errorf("produk tertinggal setelah validasi gagal (count=%d)", n)
	}
}

func TestIntegrationCreateRejectsUnknownBrand(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cats := NewCategoryRepository(pool)
	svc := newWriteService(pool)

	suffix := uuid.NewString()[:8]
	cat, err := cats.Create(ctx, domain.Category{Name: "Kat Ref", Slug: "it-ref-cat-" + suffix})
	if err != nil {
		t.Fatalf("seed kategori: %v", err)
	}
	slug := "it-ref-prod-" + suffix
	cleanupFixtures(t, pool, ctx, slug, cat.ID, "")

	_, err = svc.Create(ctx, catalog.CreateProductInput{
		Name: "Produk Ref", Slug: slug, Price: 1000,
		Category: cat.Slug, Brand: "brand-tidak-ada", IsActive: true,
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Create dengan brand tidak ada = %v, ingin ErrValidation", err)
	}
	if n := countProductsBySlug(t, pool, ctx, slug); n != 0 {
		t.Errorf("produk tersimpan walau brand tidak ada (count=%d)", n)
	}
}

func TestIntegrationUpdateRejectsUnknownCategory(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cats := NewCategoryRepository(pool)
	products := NewProductRepository(pool, nil)
	svc := newWriteService(pool)

	suffix := uuid.NewString()[:8]
	cat, err := cats.Create(ctx, domain.Category{Name: "Kat Ref", Slug: "it-ref-cat-" + suffix})
	if err != nil {
		t.Fatalf("seed kategori: %v", err)
	}
	slug := "it-ref-prod-" + suffix
	cleanupFixtures(t, pool, ctx, slug, cat.ID, "")

	created, err := svc.Create(ctx, catalog.CreateProductInput{
		Name: "Produk Ref", Slug: slug, Price: 1000, Category: cat.Slug, IsActive: true,
	})
	if err != nil {
		t.Fatalf("seed produk: %v", err)
	}

	bogus := "kategori-tidak-ada"
	if _, err := svc.Update(ctx, created.ID, domain.ProductPatch{Category: &bogus}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Update dengan category tidak ada = %v, ingin ErrValidation", err)
	}

	got, err := products.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID setelah update gagal: %v", err)
	}
	if got.Category != cat.Slug {
		t.Errorf("category berubah jadi %q, ingin tetap %q", got.Category, cat.Slug)
	}
}

// Kasus paling berbahaya sebelum issue #3: brand yang tidak ada membuat
// `brand_id` di-NULL-kan, sehingga brand produk terhapus tanpa error.
func TestIntegrationUpdateRejectsUnknownBrand(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cats := NewCategoryRepository(pool)
	brands := NewBrandRepository(pool)
	products := NewProductRepository(pool, nil)
	svc := newWriteService(pool)

	suffix := uuid.NewString()[:8]
	cat, err := cats.Create(ctx, domain.Category{Name: "Kat Ref", Slug: "it-ref-cat-" + suffix})
	if err != nil {
		t.Fatalf("seed kategori: %v", err)
	}
	brand, err := brands.Create(ctx, domain.Brand{Name: "Brand Ref", Slug: "it-ref-brand-" + suffix})
	if err != nil {
		t.Fatalf("seed brand: %v", err)
	}
	slug := "it-ref-prod-" + suffix
	cleanupFixtures(t, pool, ctx, slug, cat.ID, brand.ID)

	created, err := svc.Create(ctx, catalog.CreateProductInput{
		Name: "Produk Ref", Slug: slug, Price: 1000,
		Category: cat.Slug, Brand: brand.Slug, IsActive: true,
	})
	if err != nil {
		t.Fatalf("seed produk: %v", err)
	}

	bogus := "brand-tidak-ada"
	if _, err := svc.Update(ctx, created.ID, domain.ProductPatch{Brand: &bogus}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("Update dengan brand tidak ada = %v, ingin ErrValidation", err)
	}

	got, err := products.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID setelah update gagal: %v", err)
	}
	if got.Brand != brand.Slug {
		t.Errorf("brand berubah jadi %q, ingin tetap %q (brand tidak boleh terhapus diam-diam)", got.Brand, brand.Slug)
	}
}
