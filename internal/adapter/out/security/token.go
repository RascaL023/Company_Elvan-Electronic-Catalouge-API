package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"elvan-catalog-api/internal/application/port"
)

// TokenBytes adalah panjang token sesi mentah dalam byte (ARCHITECTURE §9.4).
const TokenBytes = 32

// TokenGenerator membuat token sesi acak dari `crypto/rand`.
//
// Token mentah hanya dikirim ke klien; yang disimpan di database adalah hash
// SHA-256-nya (`sessions.token_hash`), jadi bocornya isi tabel tidak langsung
// memberi penyerang token yang bisa dipakai.
type TokenGenerator struct{}

var _ port.TokenGenerator = (*TokenGenerator)(nil)

// NewTokenGenerator membuat generator token.
func NewTokenGenerator() *TokenGenerator { return &TokenGenerator{} }

// Generate mengembalikan token mentah (base64url tanpa padding, aman dipakai
// di cookie/header) beserta hash SHA-256 yang harus disimpan.
func (g *TokenGenerator) Generate() (string, []byte, error) {
	raw := make([]byte, TokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("baca entropi: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	return token, g.Hash(token), nil
}

// Hash menghitung hash SHA-256 dari token mentah. Dipakai saat lookup sesi
// dari permintaan masuk sehingga token mentah tidak pernah disimpan atau
// dicari di database.
func (g *TokenGenerator) Hash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
