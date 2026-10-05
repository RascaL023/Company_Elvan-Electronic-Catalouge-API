// Command adminctl mengelola akun admin dari CLI.
//
// Tidak ada endpoint registrasi publik: admin pertama (dan reset password)
// dibuat lewat perintah ini. Command ini juga menyediakan job pembersihan sesi
// kedaluwarsa. Ia memakai port yang sama dengan server, jadi aturan hashing dan
// penyimpanan tidak terduplikasi.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"elvan-catalog-api/internal/adapter/out/postgres"
	"elvan-catalog-api/internal/adapter/out/security"
	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
	"elvan-catalog-api/internal/platform/config"
	"elvan-catalog-api/internal/platform/dotenv"
)

const (
	cmdCreate        = "create"
	cmdResetPassword = "reset-password"
	cmdPruneSessions = "prune-sessions"

	// commandTimeout membatasi total waktu eksekusi satu perintah.
	commandTimeout = 30 * time.Second
)

const usage = `adminctl — kelola akun admin Elvan Catalog API

Perintah:
  create           Buat admin baru
  reset-password   Ganti password admin
  prune-sessions   Hapus sesi yang sudah kedaluwarsa

Opsi:
  --email     email admin (wajib untuk create/reset-password)
  --password  password; bila kosong dibaca dari stdin (tidak masuk argv)

Contoh:
  adminctl create --email admin@example.com
  echo 'rahasia' | adminctl create --email admin@example.com
  adminctl reset-password --email admin@example.com
  adminctl prune-sessions`

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "adminctl:", err)
		os.Exit(1)
	}
}

// app menampung dependensi supaya perintah bisa diuji tanpa database.
type app struct {
	admins   port.AdminRepository
	sessions port.SessionStore
	hasher   port.PasswordHasher
	stdin    io.Reader
	stdout   io.Writer
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	cmd, rest := args[0], args[1:]

	// Validasi nama perintah sebelum menyentuh konfigurasi/database, supaya
	// salah ketik tidak menuntut DATABASE_URL.
	switch cmd {
	case cmdCreate, cmdResetPassword, cmdPruneSessions:
	default:
		return fmt.Errorf("perintah tidak dikenal: %q\n\n%s", cmd, usage)
	}

	fs := flag.NewFlagSet("adminctl "+cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	email := fs.String("email", "", "email admin")
	password := fs.String("password", "", "password (kosong = dibaca dari stdin)")
	if err := fs.Parse(rest); err != nil {
		return fmt.Errorf("%w\n\n%s", err, usage)
	}

	if _, err := dotenv.LoadDev(); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pool.Close()

	a := &app{
		admins:   postgres.NewAdminRepository(pool),
		sessions: postgres.NewSessionRepository(pool),
		hasher:   security.NewPasswordHasher(security.DefaultArgon2Params()),
		stdin:    stdin,
		stdout:   stdout,
	}

	switch cmd {
	case cmdCreate:
		pw, err := a.readPassword(*password)
		if err != nil {
			return err
		}
		return a.create(ctx, *email, pw)
	case cmdResetPassword:
		pw, err := a.readPassword(*password)
		if err != nil {
			return err
		}
		return a.resetPassword(ctx, *email, pw)
	default: // cmdPruneSessions
		return a.pruneSessions(ctx, time.Now())
	}
}

// create membuat admin baru. Email dinormalkan; password di-hash argon2id
// sebelum disimpan sehingga password mentah tidak pernah menyentuh database.
func (a *app) create(ctx context.Context, email, password string) error {
	email = domain.NormalizeEmail(email)
	if email == "" {
		return errors.New("--email wajib diisi")
	}
	if password == "" {
		return errors.New("password tidak boleh kosong")
	}

	hash, err := a.hasher.Hash(ctx, password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	admin, err := a.admins.Create(ctx, domain.Admin{Email: email, PasswordHash: hash})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "admin dibuat: %s\n", admin.Email)
	return nil
}

// resetPassword mengganti password admin yang sudah ada (dicari lewat email).
func (a *app) resetPassword(ctx context.Context, email, password string) error {
	email = domain.NormalizeEmail(email)
	if email == "" {
		return errors.New("--email wajib diisi")
	}

	if password == "" {
		return errors.New("password tidak boleh kosong")
	}

	admin, err := a.admins.GetByEmail(ctx, email)
	if err != nil {
		return err
	}

	hash, err := a.hasher.Hash(ctx, password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := a.admins.UpdatePassword(ctx, admin.ID, hash); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "password diperbarui: %s\n", admin.Email)
	return nil
}

// pruneSessions menghapus sesi yang sudah kedaluwarsa `now` (job berkala).
func (a *app) pruneSessions(ctx context.Context, now time.Time) error {
	n, err := a.sessions.DeleteExpired(ctx, now)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "sesi kedaluwarsa dihapus: %d\n", n)
	return nil
}

// readPassword memakai --password bila diisi; selain itu membaca satu baris dari
// stdin supaya password tidak muncul di argv (terlihat lewat `ps`).
func (a *app) readPassword(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}

	_, _ = fmt.Fprint(a.stdout, "Password: ")
	line, err := bufio.NewReader(a.stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("baca password: %w", err)
	}

	password := strings.TrimRight(line, "\r\n")
	if password == "" {
		return "", errors.New("password tidak boleh kosong")
	}
	return password, nil
}
