package port

import "context"

// PasswordHasher adalah port hashing password (implementasi: argon2id).
type PasswordHasher interface {
	Hash(ctx context.Context, password string) (string, error)
	// Verify melaporkan apakah password cocok dengan hash tersimpan.
	Verify(ctx context.Context, password, hash string) (bool, error)
}

// TokenGenerator membuat token sesi acak. Implementasi mengembalikan token
// mentah (dikirim ke klien) dan hash-nya (disimpan di DB).
type TokenGenerator interface {
	Generate() (token string, hash []byte, err error)
}
