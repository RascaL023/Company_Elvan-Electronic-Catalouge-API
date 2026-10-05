package domain

import (
	"strings"
	"time"
)

// Admin adalah akun pengelola. Password disimpan sebagai hash (argon2id);
// entity tidak pernah menyimpan password mentah.
type Admin struct {
	ID           string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// NormalizeEmail menormalkan email untuk lookup dan penyimpanan: trim dan
// huruf kecil. Dipakai adminctl dan use case auth supaya "Admin@X" dan
// "admin@x" selalu menunjuk akun yang sama.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Validate memeriksa invarian Admin. `PasswordHash` wajib terisi untuk admin
// yang disimpan; verifikasi password dilakukan lewat port.PasswordHasher.
func (a Admin) Validate() error {
	v := NewValidationError()

	email := strings.TrimSpace(a.Email)
	if email == "" {
		v.Add("email", "tidak boleh kosong")
	} else if !strings.Contains(email, "@") || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") {
		v.Add("email", "format tidak valid")
	}
	if strings.TrimSpace(a.PasswordHash) == "" {
		v.Add("passwordHash", "tidak boleh kosong")
	}

	if v.HasErrors() {
		return v
	}
	return nil
}
