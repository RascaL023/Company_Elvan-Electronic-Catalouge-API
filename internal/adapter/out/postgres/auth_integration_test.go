package postgres

// Test integrasi Fase 4a: repository admin + penyimpanan sesi + alur login/me/
// logout di atas PostgreSQL nyata. Sesi disimpan sebagai hash SHA-256 (token
// mentah tidak pernah masuk DB). Seperti test tulis lain, data di-commit lalu
// dibersihkan sendiri (transaksi dibuka use case).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"elvan-catalog-api/internal/adapter/out/security"
	"elvan-catalog-api/internal/application/auth"
	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
	"elvan-catalog-api/internal/platform/clock"
)

// fastArgon2 memakai parameter murah supaya test cepat; kekuatannya sudah diuji
// terpisah di package security.
func fastArgon2() security.Argon2Params {
	return security.Argon2Params{MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
}

func TestIntegrationAdminAuthFlow(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	admins := NewAdminRepository(pool)
	sessions := NewSessionRepository(pool)
	hasher := security.NewPasswordHasher(fastArgon2())

	svc := auth.New(admins, sessions, hasher, security.NewTokenGenerator(), NewTxManager(pool),
		clock.Real{}, auth.Config{SessionTTL: time.Hour, FailureDelay: 0}, nil)

	email := "it-admin-" + uuid.NewString()[:8] + "@example.com"
	hash, err := hasher.Hash(ctx, "rahasia-1")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	admin, err := admins.Create(ctx, domain.Admin{Email: email, PasswordHash: hash})
	if err != nil {
		t.Fatalf("buat admin: %v", err)
	}
	// Sesi ikut terhapus lewat ON DELETE CASCADE saat admin dihapus.
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DELETE FROM admins WHERE id = $1", admin.ID); err != nil {
			t.Logf("cleanup admin %q: %v", admin.ID, err)
		}
	})

	// Email duplikat → konflik.
	if _, err := admins.Create(ctx, domain.Admin{Email: email, PasswordHash: hash}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("email duplikat = %v, ingin ErrConflict", err)
	}

	// Login dengan kredensial benar.
	session, err := svc.Login(ctx, email, "rahasia-1")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if session.Token == "" || len(session.Token) < 32 {
		t.Errorf("token = %q, ingin acak yang cukup panjang", session.Token)
	}

	// Token mentah tidak boleh tersimpan: yang ada di DB adalah hash SHA-256.
	got, err := sessions.GetByTokenHash(ctx, security.NewTokenGenerator().Hash(session.Token))
	if err != nil {
		t.Fatalf("GetByTokenHash: %v", err)
	}
	if string(got.TokenHash) == session.Token {
		t.Error("token mentah tersimpan di database")
	}
	if got.AdminID != admin.ID {
		t.Errorf("adminID = %q, ingin %q", got.AdminID, admin.ID)
	}

	// Me mengembalikan admin pemilik sesi.
	me, err := svc.Me(ctx, session.Token)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if me.ID != admin.ID || me.Email != email {
		t.Errorf("Me = %+v, ingin admin %q", me, admin.ID)
	}

	// Kredensial salah → unauthorized (dan tidak membuat sesi baru).
	if _, err := svc.Login(ctx, email, "salah"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("login password salah = %v, ingin ErrUnauthorized", err)
	}
	if _, err := svc.Login(ctx, "tidak-ada@example.com", "rahasia-1"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("login email asing = %v, ingin ErrUnauthorized", err)
	}

	// Reset password lewat repository.
	newHash, err := hasher.Hash(ctx, "rahasia-2")
	if err != nil {
		t.Fatalf("hash baru: %v", err)
	}
	if err := admins.UpdatePassword(ctx, admin.ID, newHash); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	if _, err := svc.Login(ctx, email, "rahasia-1"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("login dengan password lama = %v, ingin ErrUnauthorized", err)
	}
	if _, err := svc.Login(ctx, email, "rahasia-2"); err != nil {
		t.Errorf("login dengan password baru: %v", err)
	}

	// Update password untuk id yang tidak ada → ErrNotFound.
	if err := admins.UpdatePassword(ctx, uuid.NewString(), newHash); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("UpdatePassword id asing = %v, ingin ErrNotFound", err)
	}

	// Logout mencabut sesi.
	if err := svc.Logout(ctx, session.Token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := svc.Me(ctx, session.Token); !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("Me setelah logout = %v, ingin ErrUnauthorized", err)
	}
}

func TestIntegrationDeleteExpiredSessions(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	admins := NewAdminRepository(pool)
	sessions := NewSessionRepository(pool)

	email := "it-admin-" + uuid.NewString()[:8] + "@example.com"
	admin, err := admins.Create(ctx, domain.Admin{Email: email, PasswordHash: "hash"})
	if err != nil {
		t.Fatalf("buat admin: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "DELETE FROM admins WHERE id = $1", admin.ID); err != nil {
			t.Logf("cleanup admin %q: %v", admin.ID, err)
		}
	})

	now := time.Now().UTC()
	expiredHash := []byte("expired-" + uuid.NewString())
	liveHash := []byte("live-" + uuid.NewString())

	if err := sessions.Create(ctx, portSession(admin.ID, expiredHash, now.Add(-time.Hour))); err != nil {
		t.Fatalf("Create sesi kedaluwarsa: %v", err)
	}
	if err := sessions.Create(ctx, portSession(admin.ID, liveHash, now.Add(time.Hour))); err != nil {
		t.Fatalf("Create sesi aktif: %v", err)
	}

	n, err := sessions.DeleteExpired(ctx, now)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n < 1 {
		t.Errorf("sesi terhapus = %d, ingin minimal 1", n)
	}

	if _, err := sessions.GetByTokenHash(ctx, expiredHash); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("sesi kedaluwarsa masih ada: %v", err)
	}
	if _, err := sessions.GetByTokenHash(ctx, liveHash); err != nil {
		t.Errorf("sesi aktif ikut terhapus: %v", err)
	}

	// Touch memperbarui last_seen_at.
	if err := sessions.Touch(ctx, liveHash, now); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got, err := sessions.GetByTokenHash(ctx, liveHash)
	if err != nil {
		t.Fatalf("GetByTokenHash: %v", err)
	}
	if got.LastSeenAt.Before(now.Add(-time.Minute)) {
		t.Errorf("last_seen_at = %v, ingin mendekati %v", got.LastSeenAt, now)
	}
}

func portSession(adminID string, tokenHash []byte, expiresAt time.Time) port.Session {
	return port.Session{AdminID: adminID, TokenHash: tokenHash, ExpiresAt: expiresAt}
}
