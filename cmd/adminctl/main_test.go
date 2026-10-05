package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"elvan-catalog-api/internal/application/port"
	"elvan-catalog-api/internal/domain"
)

// --- fake -------------------------------------------------------------------

type fakeAdmins struct {
	byEmail   map[string]domain.Admin
	created   []domain.Admin
	createErr error
	updates   map[string]string
	updateErr error
}

func newFakeAdmins() *fakeAdmins {
	return &fakeAdmins{byEmail: map[string]domain.Admin{}, updates: map[string]string{}}
}

func (f *fakeAdmins) GetByID(_ context.Context, id string) (*domain.Admin, error) {
	for _, a := range f.byEmail {
		if a.ID == id {
			return &a, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeAdmins) GetByEmail(_ context.Context, email string) (*domain.Admin, error) {
	a, ok := f.byEmail[email]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &a, nil
}

func (f *fakeAdmins) Create(_ context.Context, a domain.Admin) (*domain.Admin, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	if _, dup := f.byEmail[a.Email]; dup {
		return nil, domain.ErrConflict
	}
	a.ID = "admin-1"
	f.byEmail[a.Email] = a
	f.created = append(f.created, a)
	return &a, nil
}

func (f *fakeAdmins) UpdatePassword(_ context.Context, id, hash string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updates[id] = hash
	return nil
}

type fakeSessions struct {
	deletedBefore []time.Time
	expiredCount  int64
	err           error
}

func (f *fakeSessions) Create(context.Context, port.Session) error { return nil }

func (f *fakeSessions) GetByTokenHash(context.Context, []byte) (*port.Session, error) {
	return nil, domain.ErrNotFound
}

func (f *fakeSessions) Delete(context.Context, []byte) error { return nil }

func (f *fakeSessions) Touch(context.Context, []byte, time.Time) error { return nil }

func (f *fakeSessions) DeleteExpired(_ context.Context, before time.Time) (int64, error) {
	f.deletedBefore = append(f.deletedBefore, before)
	return f.expiredCount, f.err
}

// fakeHasher memakai penanda sederhana supaya test bisa memastikan password
// mentah tidak pernah diteruskan ke repositori.
type fakeHasher struct{ err error }

func (f *fakeHasher) Hash(_ context.Context, password string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return "hashed:" + password, nil
}

func (f *fakeHasher) Verify(context.Context, string, string) (bool, error) { return false, nil }

func newTestApp(stdin string) (*app, *fakeAdmins, *fakeSessions, *bytes.Buffer) {
	admins := newFakeAdmins()
	sessions := &fakeSessions{}
	out := &bytes.Buffer{}
	return &app{
		admins:   admins,
		sessions: sessions,
		hasher:   &fakeHasher{},
		stdin:    strings.NewReader(stdin),
		stdout:   out,
	}, admins, sessions, out
}

// --- create -----------------------------------------------------------------

func TestCreateStoresNormalizedEmailAndHash(t *testing.T) {
	a, admins, _, out := newTestApp("")

	if err := a.create(context.Background(), "  Admin@Example.COM ", "rahasia"); err != nil {
		t.Fatalf("create: %v", err)
	}

	if len(admins.created) != 1 {
		t.Fatalf("jumlah admin dibuat = %d, ingin 1", len(admins.created))
	}
	got := admins.created[0]
	if got.Email != "admin@example.com" {
		t.Errorf("email tersimpan = %q, ingin dinormalkan", got.Email)
	}
	if got.PasswordHash != "hashed:rahasia" {
		t.Errorf("password tersimpan = %q, ingin hash (bukan mentah)", got.PasswordHash)
	}
	if strings.Contains(got.PasswordHash, "rahasia") && got.PasswordHash == "rahasia" {
		t.Error("password mentah tersimpan")
	}
	if !strings.Contains(out.String(), "admin@example.com") {
		t.Errorf("stdout = %q, ingin menyebut email admin", out.String())
	}
}

func TestCreateRequiresEmail(t *testing.T) {
	a, _, _, _ := newTestApp("")
	if err := a.create(context.Background(), "   ", "rahasia"); err == nil {
		t.Fatal("create tanpa email seharusnya gagal")
	}
}

func TestCreateRequiresPassword(t *testing.T) {
	a, _, _, _ := newTestApp("")
	if err := a.create(context.Background(), "admin@example.com", ""); err == nil {
		t.Fatal("create tanpa password seharusnya gagal")
	}
}

func TestCreatePropagatesConflict(t *testing.T) {
	a, admins, _, _ := newTestApp("")
	admins.createErr = domain.ErrConflict

	if err := a.create(context.Background(), "admin@example.com", "rahasia"); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("create = %v, ingin ErrConflict diteruskan", err)
	}
}

// --- reset password ---------------------------------------------------------

func TestResetPasswordUpdatesHash(t *testing.T) {
	a, admins, _, _ := newTestApp("")
	admins.byEmail["admin@example.com"] = domain.Admin{ID: "admin-1", Email: "admin@example.com"}

	if err := a.resetPassword(context.Background(), "Admin@Example.com", "baru"); err != nil {
		t.Fatalf("resetPassword: %v", err)
	}
	if got := admins.updates["admin-1"]; got != "hashed:baru" {
		t.Errorf("hash diperbarui = %q, ingin hash password baru", got)
	}
}

func TestResetPasswordUnknownEmail(t *testing.T) {
	a, _, _, _ := newTestApp("")
	if err := a.resetPassword(context.Background(), "tidak-ada@example.com", "baru"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("resetPassword = %v, ingin ErrNotFound", err)
	}
}

// --- prune sessions ---------------------------------------------------------

func TestPruneSessionsPassesNow(t *testing.T) {
	a, _, sessions, out := newTestApp("")
	sessions.expiredCount = 7
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	if err := a.pruneSessions(context.Background(), now); err != nil {
		t.Fatalf("pruneSessions: %v", err)
	}
	if len(sessions.deletedBefore) != 1 || !sessions.deletedBefore[0].Equal(now) {
		t.Errorf("before = %v, ingin %v", sessions.deletedBefore, now)
	}
	if !strings.Contains(out.String(), "7") {
		t.Errorf("stdout = %q, ingin menyebut jumlah sesi terhapus", out.String())
	}
}

// --- membaca password dari stdin --------------------------------------------

func TestReadPasswordFromStdin(t *testing.T) {
	a, _, _, _ := newTestApp("rahasia-dari-stdin\n")
	got, err := a.readPassword("")
	if err != nil {
		t.Fatalf("readPassword: %v", err)
	}
	if got != "rahasia-dari-stdin" {
		t.Errorf("password = %q, ingin tanpa newline", got)
	}
}

func TestReadPasswordPrefersFlag(t *testing.T) {
	a, _, _, _ := newTestApp("dari-stdin\n")
	got, err := a.readPassword("dari-flag")
	if err != nil {
		t.Fatalf("readPassword: %v", err)
	}
	if got != "dari-flag" {
		t.Errorf("password = %q, ingin memakai nilai --password", got)
	}
}

func TestReadPasswordRejectsEmpty(t *testing.T) {
	a, _, _, _ := newTestApp("\n")
	if _, err := a.readPassword(""); err == nil {
		t.Fatal("password kosong seharusnya ditolak")
	}
}

// --- dispatch ---------------------------------------------------------------

func TestRunRejectsUnknownCommandBeforeWiring(t *testing.T) {
	var out bytes.Buffer
	err := run([]string{"hapus-semua"}, strings.NewReader(""), &out)
	if err == nil {
		t.Fatal("perintah tidak dikenal seharusnya gagal")
	}
	if !strings.Contains(err.Error(), "tidak dikenal") {
		t.Errorf("error = %v, ingin menyebut perintah tidak dikenal", err)
	}
}

func TestRunWithoutArgsShowsUsage(t *testing.T) {
	var out bytes.Buffer
	err := run(nil, strings.NewReader(""), &out)
	if err == nil || !strings.Contains(err.Error(), "Perintah") {
		t.Errorf("error = %v, ingin menampilkan usage", err)
	}
}
