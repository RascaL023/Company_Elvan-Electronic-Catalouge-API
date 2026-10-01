package catalog

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"elvan-catalog-api/internal/application"
	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// fakeRepo mencatat nilai includeInactive yang diteruskan ke repository.
type fakeRepo struct {
	listAllIncludeInactive bool
	listIncludeInactive    bool
	writeErr               error
}

func (f *fakeRepo) ListAll(_ context.Context, includeInactive bool) ([]domain.CatalogProduct, error) {
	f.listAllIncludeInactive = includeInactive
	return nil, nil
}

func (f *fakeRepo) List(_ context.Context, q port.ProductQuery) (port.ProductPage, error) {
	f.listIncludeInactive = q.IncludeInactive
	return port.ProductPage{}, nil
}

func (f *fakeRepo) GetByID(context.Context, string) (*domain.Product, error) {
	return nil, domain.ErrNotFound
}

func (f *fakeRepo) Create(_ context.Context, p domain.Product) (*domain.Product, error) {
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return &p, nil
}

func (f *fakeRepo) Update(_ context.Context, _ string, _ domain.ProductPatch) (*domain.Product, error) {
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return &domain.Product{}, nil
}

func (f *fakeRepo) Delete(context.Context, string) ([]domain.Image, error) {
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return nil, nil
}

// fakeTx menjalankan fn apa adanya dan mencatat berapa kali dipakai.
type fakeTx struct{ calls int }

func (f *fakeTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	f.calls++
	return fn(ctx)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestIncludeInactiveOnlyForAdmin(t *testing.T) {
	repo := &fakeRepo{}
	svc := New(repo, &fakeTx{}, discardLogger())
	ctx := context.Background()

	// Anonim: includeInactive harus dibuang.
	if _, err := svc.ListAll(ctx, application.Anonymous); err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if repo.listAllIncludeInactive {
		t.Error("anonim tidak boleh mendapatkan produk nonaktif di ListAll")
	}
	if _, err := svc.List(ctx, port.ProductQuery{IncludeInactive: true}, application.Anonymous); err != nil {
		t.Fatalf("List: %v", err)
	}
	if repo.listIncludeInactive {
		t.Error("anonim tidak boleh memaksa includeInactive di List")
	}

	// Admin: includeInactive dihormati.
	if _, err := svc.ListAll(ctx, application.Actor{IsAdmin: true}); err != nil {
		t.Fatalf("ListAll admin: %v", err)
	}
	if !repo.listAllIncludeInactive {
		t.Error("admin seharusnya mendapatkan produk nonaktif di ListAll")
	}
	if _, err := svc.List(ctx, port.ProductQuery{IncludeInactive: true}, application.Actor{IsAdmin: true}); err != nil {
		t.Fatalf("List admin: %v", err)
	}
	if !repo.listIncludeInactive {
		t.Error("admin seharusnya boleh includeInactive")
	}
}

// TestWriteUseCasesRunInTransaction mengunci kepemilikan transaksi (issue #4):
// use case tulis yang membungkus operasi repo, bukan pemanggil.
func TestWriteUseCasesRunInTransaction(t *testing.T) {
	repo := &fakeRepo{}
	tx := &fakeTx{}
	svc := New(repo, tx, discardLogger())
	ctx := context.Background()

	if _, err := svc.Create(ctx, CreateProductInput{Name: "TV", Slug: "tv", Category: "television"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Update(ctx, "some-id", domain.ProductPatch{}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := svc.Delete(ctx, "some-id"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if tx.calls != 3 {
		t.Errorf("jumlah transaksi = %d, ingin 3 (satu per operasi tulis)", tx.calls)
	}
}

func TestWriteUseCasePropagatesRepoError(t *testing.T) {
	repo := &fakeRepo{writeErr: domain.ErrNotFound}
	tx := &fakeTx{}
	svc := New(repo, tx, discardLogger())

	if _, err := svc.Create(context.Background(), CreateProductInput{Name: "TV", Slug: "tv", Category: "television"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Create = %v, ingin meneruskan ErrNotFound", err)
	}
	if tx.calls != 1 {
		t.Errorf("transaksi tetap harus dibuka walau repo gagal, calls = %d", tx.calls)
	}
}
