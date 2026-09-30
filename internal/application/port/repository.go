// Package port mendefinisikan interface yang dibutuhkan use case.
//
// Port berbicara dalam tipe domain dan tipe Go biasa; tidak ada tipe library
// (pgx, HTTP, dsb) yang boleh muncul di sini. Implementasinya hidup di
// internal/adapter/out/*.
package port

import (
	"context"

	"elvan-catalog-api/internal/domain"
)

// ProductQuery adalah parameter daftar produk (filter, sort, pagination).
type ProductQuery struct {
	Category        string
	Brand           string
	Search          string
	Sort            domain.SortOption
	Limit           int
	Cursor          string // opaque; diurai adapter
	IncludeInactive bool
}

// ProductPage adalah satu halaman hasil daftar produk.
type ProductPage struct {
	Products []domain.CatalogProduct
	HasMore  bool
	Cursor   string // token halaman berikutnya; kosong bila tidak ada
}

// ProductRepository adalah port penyimpanan produk.
type ProductRepository interface {
	List(ctx context.Context, q ProductQuery) (ProductPage, error)
	ListAll(ctx context.Context, includeInactive bool) ([]domain.CatalogProduct, error)
	GetByID(ctx context.Context, id string) (*domain.Product, error)
	Create(ctx context.Context, p domain.Product) (*domain.Product, error)
	Update(ctx context.Context, id string, patch domain.ProductPatch) (*domain.Product, error)
	// Delete menghapus produk dan mengembalikan gambar yang terlepas supaya
	// pemanggil bisa membersihkan file di provider setelah commit.
	Delete(ctx context.Context, id string) (removed []domain.Image, err error)
}

// CategoryRepository adalah port penyimpanan kategori.
type CategoryRepository interface {
	List(ctx context.Context) ([]domain.Category, error)
	GetByID(ctx context.Context, id string) (*domain.Category, error)
	Create(ctx context.Context, c domain.Category) (*domain.Category, error)
	Update(ctx context.Context, id string, patch domain.CategoryPatch) (*domain.Category, error)
	Delete(ctx context.Context, id string) error
}

// BrandRepository adalah port penyimpanan brand.
type BrandRepository interface {
	List(ctx context.Context) ([]domain.Brand, error)
	GetByID(ctx context.Context, id string) (*domain.Brand, error)
	Create(ctx context.Context, b domain.Brand) (*domain.Brand, error)
	Update(ctx context.Context, id string, patch domain.BrandPatch) (*domain.Brand, error)
	Delete(ctx context.Context, id string) error
}

// AdminRepository adalah port penyimpanan akun admin.
type AdminRepository interface {
	GetByID(ctx context.Context, id string) (*domain.Admin, error)
	GetByEmail(ctx context.Context, email string) (*domain.Admin, error)
	Create(ctx context.Context, a domain.Admin) (*domain.Admin, error)
	UpdatePassword(ctx context.Context, id, passwordHash string) error
}
