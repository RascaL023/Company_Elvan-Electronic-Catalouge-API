package port

import (
	"context"

	"elvan-catalog-api/internal/domain"
)

// ImporCounts adalah ringkasan jumlah baris untuk verifikasi pasca-impor.
type ImporCounts struct {
	Categories int64
	Brands     int64
	Products   int64
}

// ImporRepository adalah port tulis khusus importer (Fase 6).
//
// Berbeda dengan repository biasa, method di sini menerima `legacyID` (id
// dokumen Firestore) dan bersifat **idempoten**: baris yang sudah punya
// `legacy_id` sama diperbarui, bukan digandakan. Implementasinya adalah
// INSERT ... ON CONFLICT (legacy_id) di adapter Postgres.
//
// Semua method menghormati transaksi dari context (sama seperti repository
// lain), jadi satu impor = satu transaksi lewat TxManager.WithinTx.
type ImporRepository interface {
	// UpsertCategory menulis kategori berdasarkan legacy_id.
	UpsertCategory(ctx context.Context, c domain.Category, legacyID string) (*domain.Category, error)
	// UpsertBrand menulis brand berdasarkan legacy_id.
	UpsertBrand(ctx context.Context, b domain.Brand, legacyID string) (*domain.Brand, error)
	// UpsertProduct menulis produk berdasarkan legacy_id; category/brand
	// berupa slug yang di-resolve adapter ke foreign key.
	UpsertProduct(ctx context.Context, p domain.Product, legacyID string) (*domain.Product, error)
	// Counts menghitung baris ketiga tabel (di luar transaksi impor; untuk
	// verifikasi sebelum/sesudah impor).
	Counts(ctx context.Context) (ImporCounts, error)
}
