// Package importer berisi use case impor data Firestore ke Postgres
// (ARCHITECTURE §16). Ekspor JSON Firestore di-decode menjadi Dataset,
// divalidasi, lalu ditulis dalam **satu transaksi** secara idempoten lewat
// `legacy_id`.
package importer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// Service adalah use case importer.
type Service struct {
	impor      port.ImporRepository
	categories port.CategoryRepository
	brands     port.BrandRepository
	tx         port.TxManager
	log        *slog.Logger
}

// New membuat Service importer. `tx` adalah unit of work: seluruh impor
// berjalan dalam satu transaksi supaya tidak pernah ada data setengah jadi.
func New(
	impor port.ImporRepository,
	categories port.CategoryRepository,
	brands port.BrandRepository,
	tx port.TxManager,
	log *slog.Logger,
) *Service {
	return &Service{impor: impor, categories: categories, brands: brands, tx: tx, log: log}
}

// Result adalah ringkasan satu kali impor.
type Result struct {
	Categories int
	Brands     int
	Products   int
	Images     int
}

// Validate memeriksa seluruh dokumen secara struktural **tanpa** menyentuh
// database. Dipakai mode dry-run supaya masalah data ketahuan sebelum transaksi.
//
// Error yang dikembalikan selalu *domain.ValidationError berisi semua
// temuan sekaligus (bukan gagal di temuan pertama) supaya satu kali perbaikan
// cukup untuk seluruh file.
func (s *Service) Validate(ds *Dataset) error {
	v := domain.NewValidationError()

	for i, c := range ds.Categories {
		ref := fmt.Sprintf("categories[%d]", i)
		if strings.TrimSpace(c.LegacyID) == "" {
			v.Add(ref+".legacy_id", "tidak boleh kosong")
		}
		if err := (domain.Category{Name: c.Name, Slug: c.Slug, Description: c.Description}).Validate(); err != nil {
			mergeFields(v, ref, err)
		}
	}

	for i, b := range ds.Brands {
		ref := fmt.Sprintf("brands[%d]", i)
		if strings.TrimSpace(b.LegacyID) == "" {
			v.Add(ref+".legacy_id", "tidak boleh kosong")
		}
		if err := (domain.Brand{Name: b.Name, Slug: b.Slug}).Validate(); err != nil {
			mergeFields(v, ref, err)
		}
	}

	for i, p := range ds.Products {
		ref := fmt.Sprintf("products[%d]", i)
		if strings.TrimSpace(p.LegacyID) == "" {
			v.Add(ref+".legacy_id", "tidak boleh kosong")
		}
		// Invarian lain (name/slug/price/category/rating/gambar) sudah dipegang
		// domain.Product.Validate; cukup panggil sekali lalu beri prefix posisi.
		if err := (domain.Product{
			Name: p.Name, Slug: p.Slug, Price: p.Price, Category: p.Category,
			Images: pairImages(p), Rating: domain.Rating{Rate: p.Rating.Rate, Count: p.Rating.Count},
		}).Validate(); err != nil {
			mergeFields(v, ref, err)
		}
	}

	// Duplikat legacy_id di file = dua dokumen berebut satu baris; tertangkap
	// oleh UNIQUE di database, tapi lebih ramah dilaporkan di sini.
	checkDuplicates(v, "categories", categoryLegacyIDs(ds.Categories))
	checkDuplicates(v, "brands", brandLegacyIDs(ds.Brands))
	checkDuplicates(v, "products", productLegacyIDs(ds.Products))

	if v.HasErrors() {
		return v
	}
	return nil
}

// Import menulis seluruh Dataset dalam **satu transaksi** dengan urutan
// kategori → brand → produk (+ gambar) (ARCHITECTURE §16).
//
// Idempoten: baris yang sudah punya `legacy_id` sama diperbarui, bukan
// digandakan, sehingga impor boleh dijalankan ulang tanpa risiko.
//
// Referensi `category`/`brand` di-resolve ke foreign key **di dalam
// transaksi** (setelah kategori/brand diimpor), jadi tidak ada TOCTOU dan
// kategori yang baru diimpor langsung dipakai produk.
func (s *Service) Import(ctx context.Context, ds *Dataset) (Result, error) {
	if err := s.Validate(ds); err != nil {
		return Result{}, err
	}

	var res Result
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		// 1. Kategori.
		for i, doc := range ds.Categories {
			if _, err := s.impor.UpsertCategory(ctx, domain.Category{
				Name:        doc.Name,
				Slug:        doc.Slug,
				Description: doc.Description,
				CreatedAt:   parseTime(doc.CreatedAt),
				UpdatedAt:   parseTime(doc.UpdatedAt),
			}, doc.LegacyID); err != nil {
				return importErr("categories", i, err)
			}
			res.Categories++
		}

		// 2. Brand.
		for i, doc := range ds.Brands {
			if _, err := s.impor.UpsertBrand(ctx, domain.Brand{
				Name:      doc.Name,
				Slug:      doc.Slug,
				CreatedAt: parseTime(doc.CreatedAt),
				UpdatedAt: parseTime(doc.UpdatedAt),
			}, doc.LegacyID); err != nil {
				return importErr("brands", i, err)
			}
			res.Brands++
		}

		// 3. Produk (+ gambar). Referensi di-resolve setelah kategori/brand
		//    ada di dalam transaksi yang sama.
		for i, doc := range ds.Products {
			p := domain.Product{
				Name:        doc.Name,
				Slug:        doc.Slug,
				Price:       doc.Price,
				Description: doc.Description,
				Category:    doc.Category,
				Brand:       doc.Brand,
				Images:      pairImages(doc),
				Rating:      domain.Rating{Rate: doc.Rating.Rate, Count: doc.Rating.Count},
				IsActive:    activeByDefault(doc.IsActive),
				CreatedAt:   parseTime(doc.CreatedAt),
				UpdatedAt:   parseTime(doc.UpdatedAt),
			}
			if err := s.validateRefs(ctx, p.Category, p.Brand); err != nil {
				return importErr("products", i, err)
			}
			if _, err := s.impor.UpsertProduct(ctx, p, doc.LegacyID); err != nil {
				return importErr("products", i, err)
			}
			res.Products++
			res.Images += len(p.Images)
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	if s.log != nil {
		s.log.InfoContext(ctx, "impor selesai",
			"categories", res.Categories,
			"brands", res.Brands,
			"products", res.Products,
			"images", res.Images,
		)
	}
	return res, nil
}

// Counts mengambil jumlah baris per tabel (untuk verifikasi sebelum/sesudah
// impor; di luar transaksi impor).
func (s *Service) Counts(ctx context.Context) (port.ImporCounts, error) {
	return s.impor.Counts(ctx)
}

// validateRefs memastikan slug category/brand benar-benar ada. Dijalankan di
// dalam transaksi penulisan sehingga kategori yang baru diimpor sudah terlihat
// dan referensi tidak bisa berubah di antara validasi dan insert.
//
// Category yang tidak ada adalah error validasi (bukan senyap); brand kosong
// berarti produk tanpa brand dan tidak divalidasi.
func (s *Service) validateRefs(ctx context.Context, category, brand string) error {
	if _, err := s.categories.GetBySlug(ctx, category); err != nil {
		if err == domain.ErrNotFound {
			v := domain.NewValidationError()
			v.Add("category", "tidak ditemukan: "+category)
			return v
		}
		return err
	}
	if brand == "" {
		return nil
	}
	if _, err := s.brands.GetBySlug(ctx, brand); err != nil {
		if err == domain.ErrNotFound {
			v := domain.NewValidationError()
			v.Add("brand", "tidak ditemukan: "+brand)
			return v
		}
		return err
	}
	return nil
}

// pairImages memasangkan images[] dan imageFileIds[] berdasarkan index
// (kontrak FE, ARCHITECTURE §4). Key yang kosong dilewati; fileId tanpa
// pasangan menjadi "" (gambar lama tanpa file id provider).
func pairImages(doc ProductDoc) []domain.Image {
	images := make([]domain.Image, 0, len(doc.Images))
	for i, key := range doc.Images {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		img := domain.Image{Key: key}
		if i < len(doc.ImageFileIds) {
			img.FileID = strings.TrimSpace(doc.ImageFileIds[i])
		}
		images = append(images, img)
	}
	return images
}

// activeByDefault mengembalikan isActive; dokumen tanpa field ini dianggap
// aktif (perilaku asli FE/Firestore).
func activeByDefault(flag *bool) bool {
	if flag == nil {
		return true
	}
	return *flag
}

// importErr membungkus error adapter dengan konteks dokumen sumber. Rujukan
// hilang (ValidationError) di-prefix posisi dokumen supaya pesannya menyebut
// slug sekaligus letaknya di file ekspor.
func importErr(collection string, index int, err error) error {
	var ve *domain.ValidationError
	if asValidationError(err, &ve) {
		prefixed := domain.NewValidationError()
		for _, f := range ve.Fields {
			prefixed.Add(fmt.Sprintf("%s[%d].%s", collection, index, f.Field), f.Message)
		}
		return prefixed
	}
	return fmt.Errorf("%s[%d]: %w", collection, index, err)
}

// mergeFields menyalin field error dari error validasi entity ke validator
// induk dengan prefix referensi dokumen.
func mergeFields(dst *domain.ValidationError, prefix string, err error) {
	var ve *domain.ValidationError
	if !asValidationError(err, &ve) {
		dst.Add(prefix, err.Error())
		return
	}
	for _, f := range ve.Fields {
		dst.Add(prefix+"."+f.Field, f.Message)
	}
}

// asValidationError menyalin err ke target bila err adalah *domain.ValidationError.
func asValidationError(err error, target **domain.ValidationError) bool {
	if ve, ok := err.(*domain.ValidationError); ok {
		*target = ve
		return true
	}
	return false
}

// checkDuplicates menambahkan error bila ada legacy_id ganda dalam satu
// koleksi (dua dokumen berebut satu baris).
func checkDuplicates(v *domain.ValidationError, collection string, ids []string) {
	seen := make(map[string]int, len(ids))
	for i, id := range ids {
		if prev, ok := seen[id]; ok {
			v.Add(fmt.Sprintf("%s[%d].legacy_id", collection, i),
				fmt.Sprintf("duplikat dengan %s[%d]", collection, prev))
			continue
		}
		seen[id] = i
	}
}

func categoryLegacyIDs(docs []CategoryDoc) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.LegacyID
	}
	return out
}

func brandLegacyIDs(docs []BrandDoc) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.LegacyID
	}
	return out
}

func productLegacyIDs(docs []ProductDoc) []string {
	out := make([]string, len(docs))
	for i, d := range docs {
		out[i] = d.LegacyID
	}
	return out
}
