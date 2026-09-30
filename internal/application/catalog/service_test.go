package catalog

import (
	"context"
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

func (f *fakeRepo) Create(context.Context, domain.Product) (*domain.Product, error) {
	return nil, nil
}

func (f *fakeRepo) Update(context.Context, string, domain.ProductPatch) (*domain.Product, error) {
	return nil, nil
}

func (f *fakeRepo) Delete(context.Context, string) ([]domain.Image, error) { return nil, nil }

func TestIncludeInactiveOnlyForAdmin(t *testing.T) {
	repo := &fakeRepo{}
	svc := New(repo, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
