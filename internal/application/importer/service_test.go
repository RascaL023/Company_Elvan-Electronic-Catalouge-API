package importer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// --- fakes -----------------------------------------------------------------

// fakeImpor meniru perilaku upsert idempoten berbasis legacy_id: dokumen
// kedua dengan legacy_id yang sama MENIMPA yang pertama (tidak menggandakan).
type fakeImpor struct {
	categories map[string]domain.Category // legacy_id -> entity
	brands     map[string]domain.Brand
	products   map[string]domain.Product

	order    []string // urutan operasi tulis, mis. "category:television"
	images   map[string][]domain.Image
	errOn    string // legacy_id yang memicu error (opsional)
	countErr error
}

func newFakeImpor() *fakeImpor {
	return &fakeImpor{
		categories: map[string]domain.Category{},
		brands:     map[string]domain.Brand{},
		products:   map[string]domain.Product{},
		images:     map[string][]domain.Image{},
	}
}

func (f *fakeImpor) UpsertCategory(_ context.Context, c domain.Category, legacyID string) (*domain.Category, error) {
	if f.errOn == legacyID {
		return nil, errors.New("gagal tulis kategori")
	}
	f.categories[legacyID] = c
	f.order = append(f.order, "category:"+c.Slug)
	return &c, nil
}

func (f *fakeImpor) UpsertBrand(_ context.Context, b domain.Brand, legacyID string) (*domain.Brand, error) {
	if f.errOn == legacyID {
		return nil, errors.New("gagal tulis brand")
	}
	f.brands[legacyID] = b
	f.order = append(f.order, "brand:"+b.Slug)
	return &b, nil
}

func (f *fakeImpor) UpsertProduct(_ context.Context, p domain.Product, legacyID string) (*domain.Product, error) {
	if f.errOn == legacyID {
		return nil, errors.New("gagal tulis produk")
	}
	f.products[legacyID] = p
	f.images[legacyID] = p.Images
	f.order = append(f.order, "product:"+p.Slug)
	return &p, nil
}

func (f *fakeImpor) Counts(context.Context) (port.ImporCounts, error) {
	if f.countErr != nil {
		return port.ImporCounts{}, f.countErr
	}
	return port.ImporCounts{
		Categories: int64(len(f.categories)),
		Brands:     int64(len(f.brands)),
		Products:   int64(len(f.products)),
	}, nil
}

// fakeCategoryRepo/fakeBrandRepo meniru GetBySlug terhadap entitas yang sudah
// diimpor: entitas ditulis ke fakeImpor lebih dulu (urutan kategori/brand
// sebelum produk), sehingga resolusi referensi di dalam transaksi terlihat
// seperti pada adapter nyata.
type fakeCategoryRepo struct{ impor *fakeImpor }

func (f *fakeCategoryRepo) GetBySlug(_ context.Context, slug string) (*domain.Category, error) {
	if f.impor == nil {
		return nil, domain.ErrNotFound
	}
	for _, c := range f.impor.categories {
		if c.Slug == slug {
			return &c, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (f *fakeCategoryRepo) List(context.Context) ([]domain.Category, error) { return nil, nil }
func (f *fakeCategoryRepo) GetByID(context.Context, string) (*domain.Category, error) {
	return nil, domain.ErrNotFound
}
func (f *fakeCategoryRepo) Create(context.Context, domain.Category) (*domain.Category, error) {
	return nil, nil
}
func (f *fakeCategoryRepo) Update(context.Context, string, domain.CategoryPatch) (*domain.Category, error) {
	return nil, nil
}
func (f *fakeCategoryRepo) Delete(context.Context, string) error { return nil }

type fakeBrandRepo struct{ impor *fakeImpor }

func (f *fakeBrandRepo) GetBySlug(_ context.Context, slug string) (*domain.Brand, error) {
	if f.impor == nil {
		return nil, domain.ErrNotFound
	}
	for _, b := range f.impor.brands {
		if b.Slug == slug {
			return &b, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (f *fakeBrandRepo) List(context.Context) ([]domain.Brand, error) { return nil, nil }
func (f *fakeBrandRepo) GetByID(context.Context, string) (*domain.Brand, error) {
	return nil, domain.ErrNotFound
}
func (f *fakeBrandRepo) Create(context.Context, domain.Brand) (*domain.Brand, error) {
	return nil, nil
}
func (f *fakeBrandRepo) Update(context.Context, string, domain.BrandPatch) (*domain.Brand, error) {
	return nil, nil
}
func (f *fakeBrandRepo) Delete(context.Context, string) error { return nil }

// fakeTx menjalankan fn apa adanya dan mencatat pemakaian + error.
type fakeTx struct {
	calls int
	err   error
}

func (f *fakeTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	return fn(ctx)
}

func newTestService(
	impor *fakeImpor, cats *fakeCategoryRepo, brands *fakeBrandRepo, tx *fakeTx,
) *Service {
	return New(impor, cats, brands, tx, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// newHarness menyiapkan service + fakes yang saling terhubung.
func newHarness() (*fakeImpor, *fakeCategoryRepo, *fakeBrandRepo, *fakeTx, *Service) {
	impor := newFakeImpor()
	cats := &fakeCategoryRepo{impor: impor}
	brands := &fakeBrandRepo{impor: impor}
	tx := &fakeTx{}
	return impor, cats, brands, tx, newTestService(impor, cats, brands, tx)
}

func testDataset() *Dataset {
	return &Dataset{
		Categories: []CategoryDoc{
			{LegacyID: "cat-tv", Name: "Television", Slug: "television", Description: "TV"},
		},
		Brands: []BrandDoc{
			{LegacyID: "brand-polytron", Name: "Polytron", Slug: "polytron"},
		},
		Products: []ProductDoc{
			{
				LegacyID:     "prod-1",
				Name:         "Polytron PLD-24",
				Slug:         "pld-24",
				Price:        1350000,
				Description:  "desc",
				Category:     "television",
				Brand:        "polytron",
				Images:       []string{"assets/images/products/tv.jpg"},
				ImageFileIds: []string{"file-123"},
				Rating:       RatingDoc{Rate: 4.4, Count: 85},
			},
		},
	}
}

// --- decode ----------------------------------------------------------------

func TestDecodeMembacaEksporJSON(t *testing.T) {
	const raw = `{
	  "categories": [{"legacy_id":"cat-tv","name":"Television","slug":"television","description":"TV"}],
	  "brands": [{"legacy_id":"b1","name":"Polytron","slug":"polytron"}],
	  "products": [{
	    "legacy_id":"p1","name":"TV 24","slug":"tv-24","price":1350000,
	    "category":"television","brand":"polytron",
	    "images":["a.jpg","b.jpg"],"imageFileIds":["f1",""],
	    "rating":{"rate":4.4,"count":85}
	  }],
	  "catalog_snapshot": {"version": 12}
	}`

	ds, err := Decode(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	cats, brands, prods := ds.Counts()
	if cats != 1 || brands != 1 || prods != 1 {
		t.Fatalf("counts = %d/%d/%d, ingin 1/1/1", cats, brands, prods)
	}
	if len(ds.CatalogSnapshot) == 0 {
		t.Error("catalog_snapshot seharusnya ter-decode (walau tidak dipakai)")
	}
	if ds.Products[0].ImageFileIds[1] != "" {
		t.Errorf("fileId[1] = %q, ingin kosong", ds.Products[0].ImageFileIds[1])
	}
}

func TestDecodeJSONRusak(t *testing.T) {
	if _, err := Decode(strings.NewReader("{tidak valid")); err == nil {
		t.Fatal("JSON rusak seharusnya menghasilkan error")
	}
}

// --- validasi --------------------------------------------------------------

func TestValidateKumpulkanSemuaTemuan(t *testing.T) {
	ds := &Dataset{
		Categories: []CategoryDoc{
			{LegacyID: "", Name: "", Slug: "!!"}, // legacy_id kosong + nama + slug
			{LegacyID: "dup", Name: "A", Slug: "a"},
		},
		Products: []ProductDoc{
			{LegacyID: "p1", Name: "", Slug: "", Price: -1, Category: ""},
			{LegacyID: "p1", Name: "B", Slug: "b", Price: 1, Category: "x"},
		},
	}

	err := (&Service{}).Validate(ds)
	if err == nil {
		t.Fatal("validasi seharusnya gagal")
	}
	ve, ok := err.(*domain.ValidationError)
	if !ok {
		t.Fatalf("error = %T, ingin *domain.ValidationError", err)
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("error tidak membungkus ErrValidation: %v", err)
	}

	wantFields := []string{
		"categories[0].legacy_id",
		"categories[0].name",
		"categories[0].slug",
		"products[0].category",
		"products[0].price",
		"products[0].name",
		"products[0].slug",
		"products[1].legacy_id", // duplikat
	}
	for _, want := range wantFields {
		if !hasField(ve, want) {
			t.Errorf("field %q tidak dilaporkan; dapat: %v", want, fieldNames(ve))
		}
	}
}

func TestValidateDatasetValid(t *testing.T) {
	if err := (&Service{}).Validate(testDataset()); err != nil {
		t.Fatalf("dataset valid seharusnya lolos: %v", err)
	}
}

func hasField(ve *domain.ValidationError, name string) bool {
	for _, f := range ve.Fields {
		if f.Field == name {
			return true
		}
	}
	return false
}

func fieldNames(ve *domain.ValidationError) []string {
	out := make([]string, len(ve.Fields))
	for i, f := range ve.Fields {
		out[i] = f.Field
	}
	return out
}

// --- import ---------------------------------------------------------------

func TestImportUrutanKategoriBrandProduk(t *testing.T) {
	impor, _, _, tx, svc := newHarness()

	res, err := svc.Import(context.Background(), testDataset())
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if tx.calls != 1 {
		t.Errorf("WithinTx dipanggil %d kali, ingin 1 (satu transaksi)", tx.calls)
	}
	if res.Categories != 1 || res.Brands != 1 || res.Products != 1 || res.Images != 1 {
		t.Errorf("result = %+v", res)
	}
	want := []string{"category:television", "brand:polytron", "product:pld-24"}
	if strings.Join(impor.order, ",") != strings.Join(want, ",") {
		t.Errorf("urutan = %v, ingin %v", impor.order, want)
	}
}

func TestImportPasangkanGambarBerdasarkanIndex(t *testing.T) {
	impor, _, _, _, svc := newHarness()

	ds := testDataset()
	ds.Products[0].Images = []string{"a.jpg", "b.jpg", "", "d.jpg"}
	ds.Products[0].ImageFileIds = []string{"fa", "fb"} // lebih pendek

	if _, err := svc.Import(context.Background(), ds); err != nil {
		t.Fatalf("Import: %v", err)
	}

	got := impor.images["prod-1"]
	want := []domain.Image{
		{Key: "a.jpg", FileID: "fa"},
		{Key: "b.jpg", FileID: "fb"},
		{Key: "d.jpg", FileID: ""}, // key kosong dilewati, fileId tanpa pasangan = ""
	}
	if len(got) != len(want) {
		t.Fatalf("gambar = %v, ingin %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("gambar[%d] = %+v, ingin %+v", i, got[i], want[i])
		}
	}
}

func TestImportIdempoten(t *testing.T) {
	impor, _, _, _, svc := newHarness()

	ds := testDataset()
	if _, err := svc.Import(context.Background(), ds); err != nil {
		t.Fatalf("impor pertama: %v", err)
	}
	if _, err := svc.Import(context.Background(), ds); err != nil {
		t.Fatalf("impor kedua: %v", err)
	}

	if len(impor.categories) != 1 || len(impor.brands) != 1 || len(impor.products) != 1 {
		t.Errorf("impor ulang menggandakan baris: %d/%d/%d",
			len(impor.categories), len(impor.brands), len(impor.products))
	}
}

func TestImportGagalBilaReferensiTidakAda(t *testing.T) {
	_, cats, brands, tx, svc := newHarness()
	// Dataset punya kategori "television", tapi repo referensi kosong karena
	// kita pakai repo terpisah yang tidak melihat tulisan fakeImpor — simulasi
	// kategori hilang di database.
	cats.impor = nil
	brands.impor = nil

	_, err := svc.Import(context.Background(), testDataset())
	if err == nil {
		t.Fatal("referensi hilang seharusnya menggagalkan impor")
	}
	if !errors.Is(err, domain.ErrValidation) {
		t.Errorf("error = %v, ingin ErrValidation", err)
	}
	ve := &domain.ValidationError{}
	if !asValidationError(err, &ve) || !hasField(ve, "products[0].category") {
		t.Errorf("detail field tidak sesuai: %v", err)
	}
	if tx.calls != 1 {
		t.Errorf("transaksi dipanggil %d kali; referensi divalidasi di dalam transaksi", tx.calls)
	}
}

func TestImportGagalValidasiSebelumTransaksi(t *testing.T) {
	impor, _, _, tx, svc := newHarness()

	ds := testDataset()
	ds.Products[0].Price = -1

	if _, err := svc.Import(context.Background(), ds); err == nil {
		t.Fatal("data invalid seharusnya ditolak")
	}
	if tx.calls != 0 {
		t.Errorf("transaksi dibuka %d kali, ingin 0 (validasi dulu)", tx.calls)
	}
	if len(impor.order) != 0 {
		t.Errorf("tidak boleh ada tulis, dapat %v", impor.order)
	}
}

func TestImportGagalTulisMemicuError(t *testing.T) {
	impor, _, _, _, svc := newHarness()
	impor.errOn = "brand-polytron"

	_, err := svc.Import(context.Background(), testDataset())
	if err == nil {
		t.Fatal("gagal tulis seharusnya menggagalkan impor")
	}
	if !strings.Contains(err.Error(), "brands[0]") {
		t.Errorf("error = %v, ingin menyebut posisi dokumen", err)
	}
}

func TestImportDefaultIsActiveDanWaktu(t *testing.T) {
	impor, _, _, _, svc := newHarness()

	ds := testDataset()
	ds.Products[0].IsActive = nil // hilang -> aktif
	ds.Products[0].CreatedAt = "2024-05-01T00:00:00Z"

	if _, err := svc.Import(context.Background(), ds); err != nil {
		t.Fatalf("Import: %v", err)
	}
	p := impor.products["prod-1"]
	if !p.IsActive {
		t.Error("isActive default harus true")
	}
	if p.CreatedAt.IsZero() {
		t.Error("createdAt seharusnya diisi dari dokumen sumber")
	}
}

func TestCountsMeneruskanError(t *testing.T) {
	impor, _, _, _, svc := newHarness()
	impor.countErr = errors.New("db mati")

	if _, err := svc.Counts(context.Background()); err == nil {
		t.Fatal("error counts seharusnya diteruskan")
	}
}
