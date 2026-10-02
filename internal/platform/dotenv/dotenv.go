// Package dotenv memuat berkas .env untuk pengembangan.
//
// Ini hanya kemudahan lokal: konfigurasi tetap dibaca dari environment proses
// (lihat internal/platform/config). Aturannya:
//
//   - Tidak aktif saat APP_ENV=production.
//   - Variabel yang sudah ada di environment TIDAK ditimpa.
//   - Berkas .env yang tidak ada bukan error.
//   - Tidak ada dependensi eksternal.
//
// Nilai boleh dikutip ("..." atau '...') dan komentar inline diizinkan pada
// nilai tanpa kutip. Baris komentar (diawali #) dan baris kosong dilewati.
package dotenv

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// DefaultPath adalah lokasi .env yang dipakai bila DOTENV_PATH tidak diset.
const DefaultPath = ".env"

// Entry adalah satu pasangan key/value hasil parsing.
type Entry struct {
	Key   string
	Value string
}

// Load membaca berkas .env di path lalu menerapkan entri yang belum ada di
// environment, dan mengembalikan jumlah variabel yang diterapkan. Berkas yang
// tidak ada mengembalikan (0, nil).
func Load(path string) (int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("baca %s: %w", path, err)
	}

	entries, err := Parse(data)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}

	applied := 0
	for _, e := range entries {
		if _, exists := os.LookupEnv(e.Key); exists {
			continue
		}
		if err := os.Setenv(e.Key, e.Value); err != nil {
			return applied, fmt.Errorf("set %s: %w", e.Key, err)
		}
		applied++
	}
	return applied, nil
}

// LoadDev memuat .env hanya bila tidak sedang di production. Path dapat
// ditimpa lewat DOTENV_PATH.
func LoadDev() (int, error) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return 0, nil
	}
	path := strings.TrimSpace(os.Getenv("DOTENV_PATH"))
	if path == "" {
		path = DefaultPath
	}
	return Load(path)
}

// Parse mengurai isi berkas .env. Baris yang terlihat seperti assignment tetapi
// key-nya tidak valid dianggap error agar salah tulis cepat terlihat.
func Parse(data []byte) ([]Entry, error) {
	lines := strings.Split(string(data), "\n")
	entries := make([]Entry, 0, len(lines))

	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))

		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			return nil, fmt.Errorf("baris %d: bukan assignment", i+1)
		}
		key := strings.TrimSpace(line[:eq])
		if !validKey(key) {
			return nil, fmt.Errorf("baris %d: nama variabel tidak valid %q", i+1, key)
		}
		entries = append(entries, Entry{Key: key, Value: parseValue(line[eq+1:])})
	}
	return entries, nil
}

// parseValue mengambil nilai, mendukung kutip dan komentar inline.
func parseValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if raw[0] == '"' || raw[0] == '\'' {
		quote := raw[0]
		if end := strings.IndexByte(raw[1:], quote); end >= 0 {
			inner := raw[1 : 1+end]
			if quote == '"' {
				inner = strings.ReplaceAll(inner, `\n`, "\n")
				inner = strings.ReplaceAll(inner, `\"`, `"`)
			}
			return inner
		}
		// Kutip tidak ditutup: ambil sisanya apa adanya.
		return raw[1:]
	}

	// Nilai tanpa kutip: buang komentar inline " #...".
	if idx := strings.Index(raw, " #"); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw)
}

// validKey membatasi nama variabel ke [A-Za-z_][A-Za-z0-9_]*.
func validKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}
