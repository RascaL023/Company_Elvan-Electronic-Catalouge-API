// Package security berisi adapter kriptografi: hashing password (argon2id) dan
// generator token sesi. Parameter dikumpulkan di sini agar mudah diubah dan
// diuji; composition root yang memilih nilainya.
package security

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"

	"elvan-catalog-api/internal/application/port"
)

// ErrInvalidHash dikembalikan Verify bila hash tersimpan bukan PHC string
// argon2id yang dikenali (rusak, versi lain, atau bukan keluaran kita).
var ErrInvalidHash = errors.New("format hash argon2id tidak dikenal")

// Batas kewarasan parameter yang dibaca dari hash tersimpan: mencegah hash
// yang rusak/dirusak memicu alokasi memori raksasa saat verifikasi.
const (
	minMemoryKiB  = 8
	maxMemoryKiB  = 1 << 20 // 1 GiB
	maxIterations = 10
	maxKeyLength  = 64
)

// Argon2Params adalah parameter argon2id.
type Argon2Params struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultArgon2Params mengembalikan parameter yang dipakai produksi.
// Mengikuti rekomendasi OWASP untuk argon2id (19 MiB, 2 iterasi, 1 lane).
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{
		MemoryKiB:   19 * 1024,
		Iterations:  2,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// PasswordHasher meng-hash dan memverifikasi password dengan argon2id.
//
// Hash yang dihasilkan berbentuk PHC string
// (`$argon2id$v=19$m=...,t=...,p=...$salt$hash`), sehingga parameter ikut
// tersimpan di dalam hash dan verifikasi tetap benar walau konfigurasi berubah.
type PasswordHasher struct {
	params Argon2Params
}

var _ port.PasswordHasher = (*PasswordHasher)(nil)

// NewPasswordHasher membuat hasher dengan parameter tertentu.
func NewPasswordHasher(params Argon2Params) *PasswordHasher {
	return &PasswordHasher{params: params}
}

// Hash menghasilkan PHC string argon2id dari password.
func (h *PasswordHasher) Hash(ctx context.Context, password string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("buat salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt,
		h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, h.params.KeyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		h.params.MemoryKiB, h.params.Iterations, h.params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify melaporkan apakah password cocok dengan hash tersimpan. Perbandingan
// dilakukan constant-time. Hash yang tidak dikenali → ErrInvalidHash (bukan
// panic), sehingga pemanggil bisa memperlakukannya sebagai kredensial gagal.
func (h *PasswordHasher) Verify(ctx context.Context, password, hash string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	params, salt, want, err := parsePHC(hash)
	if err != nil {
		return false, err
	}

	got := argon2.IDKey([]byte(password), salt,
		params.Iterations, params.MemoryKiB, params.Parallelism, uint32(len(want)))

	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// parsePHC mengurai PHC string argon2id menjadi parameter, salt, dan hash.
func parsePHC(hash string) (Argon2Params, []byte, []byte, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}

	var mem, iter, par uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &iter, &par); err != nil {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	if mem < minMemoryKiB || mem > maxMemoryKiB || iter == 0 || iter > maxIterations || par == 0 || par > 255 {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 || len(key) > maxKeyLength {
		return Argon2Params{}, nil, nil, ErrInvalidHash
	}

	return Argon2Params{
		MemoryKiB:   mem,
		Iterations:  iter,
		Parallelism: uint8(par),
		SaltLength:  uint32(len(salt)),
		KeyLength:   uint32(len(key)),
	}, salt, key, nil
}
