package security

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// testParams memakai biaya jauh lebih murah dari produksi supaya test cepat;
// kekuatan parameter tidak perlu diuji di sini.
func testParams() Argon2Params {
	return Argon2Params{
		MemoryKiB:   8 * 8, // cukup untuk memenuhi syarat argon2: memory >= 8*parallelism
		Iterations:  1,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}
}

func TestHashVerifyRoundTrip(t *testing.T) {
	h := NewPasswordHasher(testParams())
	ctx := context.Background()

	hash, err := h.Hash(ctx, "s3cret-p@ssword")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Errorf("hash bukan PHC argon2id: %q", hash)
	}
	if strings.Contains(hash, "s3cret-p@ssword") {
		t.Fatalf("password mentah bocor ke dalam hash: %q", hash)
	}

	ok, err := h.Verify(ctx, "s3cret-p@ssword", hash)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Error("password benar ditolak")
	}

	ok, err = h.Verify(ctx, "password-lain", hash)
	if err != nil {
		t.Fatalf("Verify password salah: %v", err)
	}
	if ok {
		t.Error("password salah diterima")
	}
}

func TestHashUsesRandomSalt(t *testing.T) {
	h := NewPasswordHasher(testParams())
	ctx := context.Background()

	first, err := h.Hash(ctx, "sama")
	if err != nil {
		t.Fatalf("Hash pertama: %v", err)
	}
	second, err := h.Hash(ctx, "sama")
	if err != nil {
		t.Fatalf("Hash kedua: %v", err)
	}
	if first == second {
		t.Error("dua hash password yang sama identik; salt tidak acak")
	}
}

// TestVerifyUsesParamsFromHash membuktikan verifikasi tidak bergantung pada
// konfigurasi hasher saat ini: parameter dibaca dari PHC string tersimpan.
func TestVerifyUsesParamsFromHash(t *testing.T) {
	ctx := context.Background()
	strong := NewPasswordHasher(Argon2Params{
		MemoryKiB: 32 * 1024, Iterations: 3, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
	weak := NewPasswordHasher(testParams())

	hash, err := strong.Hash(ctx, "rahasia")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	ok, err := weak.Verify(ctx, "rahasia", hash)
	if err != nil {
		t.Fatalf("Verify oleh hasher berparameter berbeda: %v", err)
	}
	if !ok {
		t.Error("hash seharusnya tetap terverifikasi walau parameter hasher berbeda")
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	h := NewPasswordHasher(testParams())
	ctx := context.Background()

	cases := map[string]string{
		"kosong":                "",
		"bukan phc":             "bukan-hash",
		"bagian kurang":         "$argon2id$v=19$m=65536,t=1,p=1$c2FsdA",
		"algoritma lain":        "$argon2i$v=19$m=65536,t=1,p=1$c2FsdA$aGFzaA",
		"versi lain":            "$argon2id$v=18$m=65536,t=1,p=1$c2FsdA$aGFzaA",
		"parameter tidak angka": "$argon2id$v=19$m=x,t=1,p=1$c2FsdA$aGFzaA",
		"memori di luar batas":  "$argon2id$v=19$m=99999999,t=1,p=1$c2FsdA$aGFzaA",
		"iterasi nol":           "$argon2id$v=19$m=65536,t=0,p=1$c2FsdA$aGFzaA",
		"parallelism nol":       "$argon2id$v=19$m=65536,t=1,p=0$c2FsdA$aGFzaA",
		"salt bukan base64":     "$argon2id$v=19$m=65536,t=1,p=1$!!!$aGFzaA",
		"hash bukan base64":     "$argon2id$v=19$m=65536,t=1,p=1$c2FsdA$!!!",
		"salt kosong":           "$argon2id$v=19$m=65536,t=1,p=1$$aGFzaA",
	}

	for name, hash := range cases {
		t.Run(name, func(t *testing.T) {
			ok, err := h.Verify(ctx, "apa saja", hash)
			if ok {
				t.Error("hash rusak tidak boleh lolos verifikasi")
			}
			if !errors.Is(err, ErrInvalidHash) {
				t.Errorf("error = %v, ingin ErrInvalidHash", err)
			}
		})
	}
}

func TestHashRespectsContext(t *testing.T) {
	h := NewPasswordHasher(testParams())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := h.Hash(ctx, "apa saja"); !errors.Is(err, context.Canceled) {
		t.Errorf("Hash dengan context batal = %v, ingin context.Canceled", err)
	}
	if _, err := h.Verify(ctx, "apa saja", "$argon2id$v=19$m=65536,t=1,p=1$c2FsdA$aGFzaA"); !errors.Is(err, context.Canceled) {
		t.Errorf("Verify dengan context batal = %v, ingin context.Canceled", err)
	}
}

func TestDefaultParamsAreSane(t *testing.T) {
	p := DefaultArgon2Params()
	if p.MemoryKiB < 19*1024 {
		t.Errorf("memory default = %d KiB, ingin minimal 19 MiB", p.MemoryKiB)
	}
	if p.Iterations < 2 {
		t.Errorf("iterasi default = %d, ingin minimal 2", p.Iterations)
	}
	if p.SaltLength < 16 {
		t.Errorf("panjang salt default = %d, ingin minimal 16", p.SaltLength)
	}
	if p.KeyLength < 32 {
		t.Errorf("panjang key default = %d, ingin minimal 32", p.KeyLength)
	}
}
