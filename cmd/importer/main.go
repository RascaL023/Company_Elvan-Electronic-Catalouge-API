// Command importer memindahkan data Firestore (hasil ekspor JSON) ke Postgres.
//
// Satu kali jalan = satu transaksi (ARCHITECTURE §16). Idempoten: jalankan
// ulang aman karena baris dicocokkan lewat legacy_id (id Firestore).
//
// Perintah:
//
//	importer import --input export.json [--dry-run]
//	importer verify
//
// Sumber JSON adalah hasil ekspor koleksi products/categories/brands dari
// scripts/migrate-firestore.cjs (repo FE); dokumen catalog/snapshot dilewati
// karena hanya proyeksi (sumber kebenaran tetap products/*).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"elvan-catalog-api/internal/adapter/out/postgres"
	"elvan-catalog-api/internal/application/importer"
	"elvan-catalog-api/internal/platform/config"
	"elvan-catalog-api/internal/platform/dotenv"
	"elvan-catalog-api/internal/platform/logger"
)

const (
	cmdImport = "import"
	cmdVerify = "verify"

	// importTimeout membatasi total waktu impor (satu transaksi besar).
	importTimeout = 10 * time.Minute
	// verifyTimeout membatasi query hitung verifikasi.
	verifyTimeout = 30 * time.Second
)

const usage = `importer — pindahkan data Firestore (JSON) ke Postgres

Perintah:
  import   Impor file ekspor JSON ke Postgres (satu transaksi, idempoten)
  verify   Tampilkan jumlah baris per tabel (untuk verifikasi pasca-impor)

Opsi import:
  --input    path file JSON hasil ekspor (wajib)
  --dry-run  validasi saja tanpa menulis ke database

Contoh:
  importer import --input export.json --dry-run
  importer import --input export.json
  importer verify`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "importer:", err)
		os.Exit(1)
	}
}

// app menampung dependensi supaya perintah bisa diuji tanpa database.
type app struct {
	svc    *importer.Service
	log    *slog.Logger
	stdout io.Writer
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	cmd, rest := args[0], args[1:]

	// Validasi nama perintah sebelum menyentuh konfigurasi/database.
	switch cmd {
	case cmdImport, cmdVerify:
	default:
		return fmt.Errorf("perintah tidak dikenal: %q\n\n%s", cmd, usage)
	}

	fs := flag.NewFlagSet("importer "+cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	input := fs.String("input", "", "path file JSON hasil ekspor")
	dryRun := fs.Bool("dry-run", false, "validasi saja, tanpa menulis")
	if err := fs.Parse(rest); err != nil {
		return fmt.Errorf("%w\n\n%s", err, usage)
	}
	if cmd == cmdImport && *input == "" {
		return fmt.Errorf("--input wajib diisi\n\n%s", usage)
	}

	if _, err := dotenv.LoadDev(); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(cfg.LogLevel)

	ctx, cancel := context.WithTimeout(context.Background(), timeoutFor(cmd))
	defer cancel()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()

	a := &app{
		svc: importer.New(
			postgres.NewImporterRepository(pool),
			postgres.NewCategoryRepository(pool),
			postgres.NewBrandRepository(pool),
			postgres.NewTxManager(pool),
			log,
		),
		log:    log,
		stdout: stdout,
	}

	switch cmd {
	case cmdImport:
		return a.importFrom(ctx, *input, *dryRun)
	default: // cmdVerify
		return a.verify(ctx)
	}
}

// importFrom membaca file JSON lalu mengimpornya (atau hanya memvalidasi pada
// mode dry-run). Kategori/brand/produk ditulis dalam satu transaksi.
func (a *app) importFrom(ctx context.Context, path string, dryRun bool) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("buka file: %w", err)
	}
	defer func() { _ = f.Close() }()

	ds, err := importer.Decode(f)
	if err != nil {
		return err
	}
	cats, brands, products := ds.Counts()

	if dryRun {
		if err := a.svc.Validate(ds); err != nil {
			return fmt.Errorf("validasi gagal: %w", err)
		}
		_, _ = fmt.Fprintf(a.stdout,
			"dry-run OK: %d kategori, %d brand, %d produk (tidak ada yang ditulis)\n",
			cats, brands, products)
		return nil
	}

	res, err := a.svc.Import(ctx, ds)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout,
		"impor selesai: %d kategori, %d brand, %d produk (%d gambar)\n",
		res.Categories, res.Brands, res.Products, res.Images)
	_, _ = fmt.Fprintf(a.stdout, "sumber: %d kategori, %d brand, %d produk\n",
		cats, brands, products)
	return nil
}

// verify menampilkan jumlah baris per tabel untuk dibandingkan dengan jumlah
// dokumen di Firestore (ARCHITECTURE §16 langkah 4).
func (a *app) verify(ctx context.Context) error {
	counts, err := a.svc.Counts(ctx)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout,
		"baris di database: categories=%d brands=%d products=%d\n",
		counts.Categories, counts.Brands, counts.Products)
	return nil
}

// timeoutFor membatasi waktu perintah sesuai bobot kerjanya.
func timeoutFor(cmd string) time.Duration {
	if cmd == cmdImport {
		return importTimeout
	}
	return verifyTimeout
}
