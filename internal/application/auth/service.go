// Package auth berisi use case autentikasi admin: login, logout, dan validasi
// sesi. Use case tidak tahu bagaimana token dibawa klien (cookie atau bearer);
// itu urusan adapter HTTP.
package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// Config mengatur perilaku use case auth. Dirakit composition root dari
// konfigurasi (SESSION_TTL) dan nilai default yang aman.
type Config struct {
	// SessionTTL adalah masa berlaku sesi baru.
	SessionTTL time.Duration
	// FailureDelay adalah penundaan konstan saat login gagal supaya waktu
	// respons tidak membocorkan apakah email terdaftar.
	FailureDelay time.Duration
	// TouchInterval adalah jarak minimum pembaruan `last_seen_at`.
	TouchInterval time.Duration
}

// DefaultConfig mengembalikan nilai default yang dipakai bila pemanggil tidak
// mengisi field tertentu.
func DefaultConfig() Config {
	return Config{
		SessionTTL:    7 * 24 * time.Hour,
		FailureDelay:  500 * time.Millisecond,
		TouchInterval: 5 * time.Minute,
	}
}

// Normalize melengkapi nilai nol dengan default supaya TTL/interval tidak
// pernah nol karena lupa konfigurasi.
func (c Config) Normalize() Config {
	d := DefaultConfig()
	if c.SessionTTL <= 0 {
		c.SessionTTL = d.SessionTTL
	}
	if c.TouchInterval <= 0 {
		c.TouchInterval = d.TouchInterval
	}
	return c
}

// Sesi adalah hasil login: token mentah (dikirim ke klien) plus masa berlakunya.
type Session struct {
	Token     string
	ExpiresAt time.Time
	Admin     domain.Admin
}

// Service adalah use case auth.
type Service struct {
	admins   port.AdminRepository
	sessions port.SessionStore
	hasher   port.PasswordHasher
	tokens   port.TokenGenerator
	tx       port.TxManager
	clock    port.Clock
	cfg      Config
	log      *slog.Logger
}

// New membuat Service auth.
func New(
	admins port.AdminRepository,
	sessions port.SessionStore,
	hasher port.PasswordHasher,
	tokens port.TokenGenerator,
	tx port.TxManager,
	clock port.Clock,
	cfg Config,
	log *slog.Logger,
) *Service {
	return &Service{
		admins:   admins,
		sessions: sessions,
		hasher:   hasher,
		tokens:   tokens,
		tx:       tx,
		clock:    clock,
		cfg:      cfg.Normalize(),
		log:      log,
	}
}

// Login memverifikasi email + password lalu membuat sesi baru.
//
// Semua kegagalan kredensial menjadi `ErrUnauthorized` yang sama (email tidak
// terdaftar vs password salah tidak dibedakan) dan selalu melewati penundaan
// konstan, agar tidak bisa dipakai memetakan email admin. Error infrastruktur
// (mis. database mati atau timeout) **tidak** disamarkan sebagai 401; ia
// diteruskan apa adanya supaya klien menerima 500/504 yang jujur dan mudah
// diamati.
func (s *Service) Login(ctx context.Context, email, password string) (*Session, error) {
	email = domain.NormalizeEmail(email)

	admin, err := s.admins.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, s.rejectCredentials(ctx)
		}
		return nil, s.rejectInternal(ctx, err)
	}

	ok, err := s.hasher.Verify(ctx, password, admin.PasswordHash)
	if err != nil {
		return nil, s.rejectInternal(ctx, err)
	}
	if !ok {
		return nil, s.rejectCredentials(ctx)
	}

	token, hash, err := s.tokens.Generate()
	if err != nil {
		return nil, err
	}
	expiresAt := s.clock.Now().Add(s.cfg.SessionTTL)

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		return s.sessions.Create(ctx, port.Session{
			AdminID:   admin.ID,
			TokenHash: hash,
			ExpiresAt: expiresAt,
		})
	})
	if err != nil {
		return nil, err
	}

	return &Session{Token: token, ExpiresAt: expiresAt, Admin: *admin}, nil
}

// Logout mencabut sesi. Idempoten: token kosong atau tidak dikenal bukan error,
// sehingga klien selalu bisa "logout" tanpa tersandung keadaan.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.sessions.Delete(ctx, s.tokens.Hash(token))
}

// Me mengembalikan admin pemilik sesi. Token kosong, tidak dikenal, atau
// kedaluwarsa → `ErrUnauthorized`.
//
// `last_seen_at` diperbarui hanya bila sudah lewat `TouchInterval`, supaya tidak
// ada tulis DB di setiap request.
func (s *Service) Me(ctx context.Context, token string) (*domain.Admin, error) {
	if token == "" {
		return nil, domain.ErrUnauthorized
	}

	hash := s.tokens.Hash(token)
	session, err := s.sessions.GetByTokenHash(ctx, hash)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrUnauthorized
		}
		return nil, err
	}

	now := s.clock.Now()
	if !session.ExpiresAt.After(now) {
		// Bersihkan sesi kedaluwarsa yang masih tersimpan (best-effort).
		if err := s.sessions.Delete(ctx, hash); err != nil && s.log != nil {
			s.log.WarnContext(ctx, "gagal menghapus sesi kedaluwarsa", "error", err.Error())
		}
		return nil, domain.ErrUnauthorized
	}

	admin, err := s.admins.GetByID(ctx, session.AdminID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrUnauthorized
		}
		return nil, err
	}

	if now.Sub(session.LastSeenAt) >= s.cfg.TouchInterval {
		if err := s.sessions.Touch(ctx, hash, now); err != nil && s.log != nil {
			s.log.WarnContext(ctx, "gagal memperbarui last_seen_at", "error", err.Error())
		}
	}

	return admin, nil
}

// rejectCredentials menyeragamkan kegagalan kredensial (email tidak terdaftar
// atau password salah) menjadi ErrUnauthorized, selalu lewat penundaan konstan
// agar waktu respons tidak bisa dipakai memetakan email admin.
func (s *Service) rejectCredentials(ctx context.Context) error {
	s.delayFailure(ctx)
	return domain.ErrUnauthorized
}

// rejectInternal meneruskan error infrastruktur (mis. database mati atau
// timeout) apa adanya supaya klien menerima 500/504, bukan 401 yang menyesatkan
// dan menyulitkan observability. Error tetap dicatat, dan penundaan konstan
// tetap diterapkan agar waktu respons tidak membocorkan apakah email terdaftar.
func (s *Service) rejectInternal(ctx context.Context, cause error) error {
	if s.log != nil {
		s.log.ErrorContext(ctx, "login gagal karena error internal", "error", cause.Error())
	}
	s.delayFailure(ctx)
	return cause
}

// delayFailure menerapkan penundaan konstan saat login gagal; dibatalkan bila
// context selesai lebih dulu.
func (s *Service) delayFailure(ctx context.Context) {
	if s.cfg.FailureDelay <= 0 {
		return
	}
	select {
	case <-time.After(s.cfg.FailureDelay):
	case <-ctx.Done():
	}
}
