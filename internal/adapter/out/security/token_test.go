package security

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestGenerateProducesTokenAndHash(t *testing.T) {
	g := NewTokenGenerator()

	token, hash, err := g.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if token == "" {
		t.Fatal("token kosong")
	}
	if len(hash) != sha256.Size {
		t.Fatalf("panjang hash = %d, ingin %d (SHA-256)", len(hash), sha256.Size)
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("token bukan base64url tanpa padding: %v", err)
	}
	if len(raw) != TokenBytes {
		t.Errorf("token berisi %d byte acak, ingin %d", len(raw), TokenBytes)
	}

	want := sha256.Sum256([]byte(token))
	if string(hash) != string(want[:]) {
		t.Error("hash bukan SHA-256 dari token mentah")
	}
}

func TestGenerateIsUnique(t *testing.T) {
	g := NewTokenGenerator()
	seen := make(map[string]struct{}, 32)

	for i := 0; i < 32; i++ {
		token, hash, err := g.Generate()
		if err != nil {
			t.Fatalf("Generate #%d: %v", i, err)
		}
		if _, dup := seen[token]; dup {
			t.Fatalf("token berulang pada iterasi %d", i)
		}
		seen[token] = struct{}{}

		// Hash harus konsisten dengan helper Hash untuk token yang sama.
		if string(g.Hash(token)) != string(hash) {
			t.Errorf("Hash(token) tidak sama dengan hash hasil Generate pada iterasi %d", i)
		}
	}
}

func TestHashIsStable(t *testing.T) {
	g := NewTokenGenerator()

	first := g.Hash("token-contoh")
	second := g.Hash("token-contoh")
	if string(first) != string(second) {
		t.Error("hash untuk token yang sama seharusnya deterministik")
	}

	other := g.Hash("token-lain")
	if string(first) == string(other) {
		t.Error("token berbeda menghasilkan hash yang sama")
	}
}
