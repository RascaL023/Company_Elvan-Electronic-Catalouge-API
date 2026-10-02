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
	writeCalls             int
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
	f.writeCalls++
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return &p, nil
}

func (f *fakeRepo) Update(_ context.Context, _ string, _ domain.ProductPatch) (*domain.Product, error) {
	f.writeCalls++
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return &domain.Product{}, nil
}

func (f *fakeRepo) Delete(context.Context, string) ([]domain.Image, error) {
	f.writeCalls++
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return nil, nil
}

// fakeCategoryRepo/fakeBrandRepo hanya mengimplementasikan GetBySlug secara
// bermakna; sisanya stub karena use case katalog tidak memakainya.
type fakeCategoryRepo struct {
	bySlug map[string]domain.Category
	err    error
}

func (f *fakeCategoryRepo) List(context.Context) ([]domain.Category, error) { return nil, f.err }

func (f *fakeCategoryRepo) GetByID(context.Context, string) (*domain.Category, error) {
	return nil, domain.ErrNotFound
}

func (f *fakeCategoryRepo) GetBySlug(_ context.Context, slug string) (*domain.Category, error) {
	if f.err != nil {
		return nil, f.err
	}
	c, ok := f.bySlug[slug]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &c, nil
}

func (f *fakeCategoryRepo) Create(context.Context, domain.Category) (*domain.Category, error) {
	return nil, nil
}

func (f *fakeCategoryRepo) Update(context.Context, string, domain.CategoryPatch) (*domain.Category, error) {
	return nil, nil
}

func (f *fakeCategoryRepo) Delete(context.Context, string) error { return nil }

type fakeBrandRepo struct {
	bySlug map[string]domain.Brand
	err    error
}

func (f *fakeBrandRepo) List(context.Context) ([]domain.Brand, error) { return nil, f.err }

func (f *fakeBrandRepo) GetByID(context.Context, string) (*domain.Brand, error) {
	return nil, domain.ErrNotFound
}

func (f *fakeBrandRepo) GetBySlug(_ context.Context, slug string) (*domain.Brand, error) {
	if f.err != nil {
		return nil, f.err
	}
	b, ok := f.bySlug[slug]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &b, nil
}

func (f *fakeBrandRepo) Create(context.Context, domain.Brand) (*domain.Brand, error) {
	return nil, nil
}

func (f *fakeBrandRepo) Update(context.Context, string, domain.BrandPatch) (*domain.Brand, error) {
	return nil, nil
}

func (f *fakeBrandRepo) Delete(context.Context, string) error { return nil }

// newTestService merangkai Service dengan taxonomy fake: hanya "television" dan
// "sharp" yang dianggap ada.
func newTestService(repo *fakeRepo, tx *fakeTx) *Service {
	return New(repo,
		&fakeCategoryRepo{bySlug: map[string]domain.Category{"television": {Slug: "television"}}},
		&fakeBrandRepo{bySlug: map[string]domain.Brand{"sharp": {Slug: "sharp"}}},
		tx, discardLogger())
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
	svc := newTestService(repo, &fakeTx{})
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
	svc := newTestService(repo, tx)
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
	svc := newTestService(repo, tx)

	if _, err := svc.Create(context.Background(), CreateProductInput{Name: "TV", Slug: "tv", Category: "television"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Create = %v, ingin meneruskan ErrNotFound", err)
	}
	if tx.calls != 1 {
		t.Errorf("transaksi tetap harus dibuka walau repo gagal, calls = %d", tx.calls)
	}
}

// TestValidateRefsRejectsUnknownSlug mengunci keempat kasus issue #3 di level
// use case: slug category/brand yang tidak ada harus jadi error validasi dan
// repository TIDAK boleh tersentuh (tidak ada perubahan database).
func TestValidateRefsRejectsUnknownSlug(t *testing.T) {
	cases := []struct {
		name  string
		field string
		run   func(svc *Service) error
	}{
		{
			name:  "create category tidak ada",
			field: "category",
			run: func(svc *Service) error {
				_, err := svc.Create(context.Background(), CreateProductInput{
					Name: "TV", Slug: "tv-1", Category: "tidak-ada",
				})
				return err
			},
		},
		{
			name:  "create brand tidak ada",
			field: "brand",
			run: func(svc *Service) error {
				_, err := svc.Create(context.Background(), CreateProductInput{
					Name: "TV", Slug: "tv-2", Category: "television", Brand: "tidak-ada",
				})
				return err
			},
		},
		{
			name:  "update category tidak ada",
			field: "category",
			run: func(svc *Service) error {
				bogus := "tidak-ada"
				_, err := svc.Update(context.Background(), "some-id", domain.ProductPatch{Category: &bogus})
				return err
			},
		},
		{
			name:  "update brand tidak ada",
			field: "brand",
			run: func(svc *Service) error {
				bogus := "tidak-ada"
				_, err := svc.Update(context.Background(), "some-id", domain.ProductPatch{Brand: &bogus})
				return err
			},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := newTestService(repo, &fakeTx{})

			err := tt.run(svc)
			if !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("error = %v, ingin ErrValidation", err)
			}

			var ve *domain.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("error = %v, ingin *domain.ValidationError", err)
			}
			found := false
			for _, f := range ve.Fields {
				if f.Field == tt.field {
					found = true
				}
			}
			if !found {
				t.Errorf("detail field %q tidak ada di %+v", tt.field, ve.Fields)
			}

			if repo.writeCalls != 0 {
				t.Errorf("repository tersentuh %d kali; validasi gagal tidak boleh menulis apa pun", repo.writeCalls)
			}
		})
	}
}

// TestValidateRefsAllowsEmptyBrandAndKnownSlugs memastikan validasi tidak
// menolak kasus sah: brand kosong (tanpa brand) dan field yang tidak diubah.
func TestValidateRefsAllowsEmptyBrandAndKnownSlugs(t *testing.T) {
	repo := &fakeRepo{}
	svc := newTestService(repo, &fakeTx{})
	ctx := context.Background()

	if _, err := svc.Create(ctx, CreateProductInput{Name: "TV", Slug: "tv", Category: "television"}); err != nil {
		t.Errorf("Create tanpa brand seharusnya lolos, dapat %v", err)
	}
	if _, err := svc.Create(ctx, CreateProductInput{Name: "TV", Slug: "tv2", Category: "television", Brand: "sharp"}); err != nil {
		t.Errorf("Create dengan brand yang ada seharusnya lolos, dapat %v", err)
	}
	// Patch yang tidak menyentuh category/brand tidak boleh memicu lookup.
	if _, err := svc.Update(ctx, "some-id", domain.ProductPatch{}); err != nil {
		t.Errorf("Update tanpa category/brand seharusnya lolos, dapat %v", err)
	}
}
