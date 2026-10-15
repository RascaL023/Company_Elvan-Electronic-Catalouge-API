package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// --- fake -------------------------------------------------------------------

type fakeAdmins struct {
	byEmail map[string]domain.Admin
	byID    map[string]domain.Admin
	err     error
}

func (f *fakeAdmins) GetByID(_ context.Context, id string) (*domain.Admin, error) {
	if f.err != nil {
		return nil, f.err
	}
	a, ok := f.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &a, nil
}

func (f *fakeAdmins) GetByEmail(_ context.Context, email string) (*domain.Admin, error) {
	if f.err != nil {
		return nil, f.err
	}
	a, ok := f.byEmail[email]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &a, nil
}

func (f *fakeAdmins) Create(context.Context, domain.Admin) (*domain.Admin, error) {
	return nil, nil
}

func (f *fakeAdmins) UpdatePassword(context.Context, string, string) error { return nil }

type fakeSessions struct {
	byHash   map[string]port.Session
	created  []port.Session
	deleted  [][]byte
	touched  [][]byte
	touchErr error
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{byHash: map[string]port.Session{}}
}

func (f *fakeSessions) Create(_ context.Context, s port.Session) error {
	f.created = append(f.created, s)
	f.byHash[string(s.TokenHash)] = s
	return nil
}

func (f *fakeSessions) GetByTokenHash(_ context.Context, hash []byte) (*port.Session, error) {
	s, ok := f.byHash[string(hash)]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &s, nil
}

func (f *fakeSessions) Delete(_ context.Context, hash []byte) error {
	f.deleted = append(f.deleted, hash)
	delete(f.byHash, string(hash))
	return nil
}

func (f *fakeSessions) Touch(_ context.Context, hash []byte, at time.Time) error {
	if f.touchErr != nil {
		return f.touchErr
	}
	f.touched = append(f.touched, hash)
	if s, ok := f.byHash[string(hash)]; ok {
		s.LastSeenAt = at
		f.byHash[string(hash)] = s
	}
	return nil
}

func (f *fakeSessions) DeleteExpired(context.Context, time.Time) (int64, error) { return 0, nil }

// fakeHasher mencocokkan password dengan hash yang disimpan sebagai "hash:<pw>".
type fakeHasher struct{ err error }

func (f *fakeHasher) Hash(_ context.Context, password string) (string, error) {
	return "hash:" + password, nil
}

func (f *fakeHasher) Verify(_ context.Context, password, hash string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return hash == "hash:"+password, nil
}

type fakeTokens struct{}

func (fakeTokens) Generate() (string, []byte, error) {
	return "token-mentah", []byte("hash-token"), nil
}

func (fakeTokens) Hash(token string) []byte { return []byte("hash:" + token) }

type fakeTx struct{ calls int }

func (f *fakeTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	f.calls++
	return fn(ctx)
}

type fakeClock struct{ now time.Time }

func (c fakeClock) Now() time.Time { return c.now }

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// --- helper -----------------------------------------------------------------

type harness struct {
	svc      *Service
	admins   *fakeAdmins
	sessions *fakeSessions
	hasher   *fakeHasher
	tx       *fakeTx
	now      time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	now := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	admin := domain.Admin{ID: "admin-1", Email: "admin@example.com", PasswordHash: "hash:rahasia"}

	admins := &fakeAdmins{
		byEmail: map[string]domain.Admin{"admin@example.com": admin},
		byID:    map[string]domain.Admin{"admin-1": admin},
	}
	sessions := newFakeSessions()
	hasher := &fakeHasher{}
	tx := &fakeTx{}

	svc := New(admins, sessions, hasher, fakeTokens{}, tx, fakeClock{now: now},
		Config{SessionTTL: time.Hour, FailureDelay: 0, TouchInterval: 5 * time.Minute}, discardLogger())

	return &harness{svc: svc, admins: admins, sessions: sessions, hasher: hasher, tx: tx, now: now}
}

// --- login ------------------------------------------------------------------

func TestLoginSuccess(t *testing.T) {
	h := newHarness(t)

	got, err := h.svc.Login(context.Background(), "Admin@Example.com ", "rahasia")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got.Token != "token-mentah" {
		t.Errorf("token = %q", got.Token)
	}
	if want := h.now.Add(time.Hour); !got.ExpiresAt.Equal(want) {
		t.Errorf("expiresAt = %v, ingin %v", got.ExpiresAt, want)
	}
	if len(h.sessions.created) != 1 {
		t.Fatalf("sesi dibuat = %d, ingin 1", len(h.sessions.created))
	}
	if string(h.sessions.created[0].TokenHash) != "hash-token" {
		t.Errorf("hash token tersimpan = %q", h.sessions.created[0].TokenHash)
	}
	if h.sessions.created[0].AdminID != "admin-1" {
		t.Errorf("adminID = %q", h.sessions.created[0].AdminID)
	}
	if h.tx.calls != 1 {
		t.Errorf("pembuatan sesi harus dalam transaksi, calls = %d", h.tx.calls)
	}
}

func TestLoginNormalizesEmail(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.Login(context.Background(), "ADMIN@example.com", "rahasia"); err != nil {
		t.Fatalf("Login dengan email huruf besar: %v", err)
	}
}

func TestLoginRejectsUnknownEmail(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.Login(context.Background(), "tidak-ada@example.com", "rahasia"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Login = %v, ingin ErrUnauthorized", err)
	}
	if len(h.sessions.created) != 0 {
		t.Error("sesi tidak boleh dibuat saat login gagal")
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.Login(context.Background(), "admin@example.com", "salah"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Login = %v, ingin ErrUnauthorized", err)
	}
	if len(h.sessions.created) != 0 {
		t.Error("sesi tidak boleh dibuat saat password salah")
	}
}

// Error infrastruktur (mis. database mati) TIDAK disamarkan sebagai 401: ia
// diteruskan apa adanya supaya klien/monitoring menerima 500, bukan salah
// kredensial yang menyesatkan. Kegagalan kredensial tetap 401 seragam (lihat
// TestLoginRejectsUnknownEmail / TestLoginRejectsWrongPassword).
func TestLoginPropagatesInternalError(t *testing.T) {
	h := newHarness(t)
	cause := errors.New("database mati")
	h.admins.err = cause

	_, err := h.svc.Login(context.Background(), "admin@example.com", "rahasia")
	if !errors.Is(err, cause) {
		t.Fatalf("Login = %v, ingin error internal diteruskan apa adanya", err)
	}
	if errors.Is(err, domain.ErrUnauthorized) {
		t.Error("error internal tidak boleh dipetakan menjadi ErrUnauthorized")
	}
	if len(h.sessions.created) != 0 {
		t.Error("sesi tidak boleh dibuat saat login gagal")
	}
}

// Error dari hasher (mis. hash rusak) adalah error internal, bukan kredensial
// salah; ia juga harus diteruskan, bukan disamarkan jadi 401.
func TestLoginPropagatesHasherError(t *testing.T) {
	h := newHarness(t)
	cause := errors.New("hash rusak")
	h.hasher.err = cause

	_, err := h.svc.Login(context.Background(), "admin@example.com", "rahasia")
	if !errors.Is(err, cause) {
		t.Fatalf("Login = %v, ingin error hasher diteruskan apa adanya", err)
	}
	if errors.Is(err, domain.ErrUnauthorized) {
		t.Error("error hasher tidak boleh dipetakan menjadi ErrUnauthorized")
	}
}

// --- me ---------------------------------------------------------------------

func TestMeReturnsAdmin(t *testing.T) {
	h := newHarness(t)
	h.sessions.byHash["hash:token-mentah"] = port.Session{
		AdminID:    "admin-1",
		TokenHash:  []byte("hash:token-mentah"),
		ExpiresAt:  h.now.Add(time.Hour),
		LastSeenAt: h.now,
	}

	admin, err := h.svc.Me(context.Background(), "token-mentah")
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if admin.ID != "admin-1" || admin.Email != "admin@example.com" {
		t.Errorf("admin = %+v", admin)
	}
}

func TestMeRejectsEmptyAndUnknownToken(t *testing.T) {
	h := newHarness(t)

	if _, err := h.svc.Me(context.Background(), ""); !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("Me(\"\") = %v, ingin ErrUnauthorized", err)
	}
	if _, err := h.svc.Me(context.Background(), "tidak-dikenal"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Errorf("Me(token asing) = %v, ingin ErrUnauthorized", err)
	}
}

func TestMeRejectsExpiredSessionAndDeletesIt(t *testing.T) {
	h := newHarness(t)
	h.sessions.byHash["hash:token-mentah"] = port.Session{
		AdminID:   "admin-1",
		TokenHash: []byte("hash:token-mentah"),
		ExpiresAt: h.now.Add(-time.Minute),
	}

	if _, err := h.svc.Me(context.Background(), "token-mentah"); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("Me sesi kedaluwarsa = %v, ingin ErrUnauthorized", err)
	}
	if len(h.sessions.deleted) != 1 {
		t.Errorf("sesi kedaluwarsa = %d dihapus, ingin 1", len(h.sessions.deleted))
	}
}

func TestMeTouchesOnlyWhenStale(t *testing.T) {
	h := newHarness(t)

	fresh := h.now.Add(-time.Minute)
	h.sessions.byHash["hash:fresh"] = port.Session{
		AdminID: "admin-1", TokenHash: []byte("hash:fresh"),
		ExpiresAt: h.now.Add(time.Hour), LastSeenAt: fresh,
	}
	if _, err := h.svc.Me(context.Background(), "fresh"); err != nil {
		t.Fatalf("Me: %v", err)
	}
	if len(h.sessions.touched) != 0 {
		t.Error("last_seen_at tidak boleh diperbarui bila belum lewat TouchInterval")
	}

	stale := h.now.Add(-time.Hour)
	h.sessions.byHash["hash:stale"] = port.Session{
		AdminID: "admin-1", TokenHash: []byte("hash:stale"),
		ExpiresAt: h.now.Add(time.Hour), LastSeenAt: stale,
	}
	if _, err := h.svc.Me(context.Background(), "stale"); err != nil {
		t.Fatalf("Me: %v", err)
	}
	if len(h.sessions.touched) != 1 {
		t.Fatalf("last_seen_at diperbarui %d kali, ingin 1", len(h.sessions.touched))
	}
}

// --- logout -----------------------------------------------------------------

func TestLogoutDeletesSession(t *testing.T) {
	h := newHarness(t)
	h.sessions.byHash["hash:token-mentah"] = port.Session{AdminID: "admin-1"}

	if err := h.svc.Logout(context.Background(), "token-mentah"); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if len(h.sessions.deleted) != 1 {
		t.Errorf("sesi dihapus = %d, ingin 1", len(h.sessions.deleted))
	}
}

func TestLogoutWithEmptyTokenIsNoop(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.Logout(context.Background(), ""); err != nil {
		t.Fatalf("Logout(\"\") = %v, ingin nil", err)
	}
	if len(h.sessions.deleted) != 0 {
		t.Error("token kosong tidak boleh menyentuh store")
	}
}

// --- config -----------------------------------------------------------------

func TestConfigNormalizeFillsDefaults(t *testing.T) {
	got := Config{}.Normalize()
	def := DefaultConfig()
	if got.SessionTTL != def.SessionTTL || got.TouchInterval != def.TouchInterval {
		t.Errorf("Normalize = %+v, ingin default %+v", got, def)
	}
}
