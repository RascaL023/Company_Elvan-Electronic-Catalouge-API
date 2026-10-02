package dotenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	data := []byte(`
# komentar
APP_ENV=development

DATABASE_URL="postgres://u:p@localhost:5432/db?sslmode=disable"
SINGLE_QUOTED='hello world'
INLINE=value # komentar inline
WITH_HASH="pass#word"
export EXPORTED=also-works
EMPTY=
`)
	entries, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := map[string]string{
		"APP_ENV":       "development",
		"DATABASE_URL":  "postgres://u:p@localhost:5432/db?sslmode=disable",
		"SINGLE_QUOTED": "hello world",
		"INLINE":        "value",
		"WITH_HASH":     "pass#word",
		"EXPORTED":      "also-works",
		"EMPTY":         "",
	}
	if len(entries) != len(want) {
		t.Fatalf("jumlah entri = %d, ingin %d (%+v)", len(entries), len(want), entries)
	}
	for _, e := range entries {
		if want[e.Key] != e.Value {
			t.Errorf("%s = %q, ingin %q", e.Key, e.Value, want[e.Key])
		}
	}
}

func TestParseInvalid(t *testing.T) {
	for _, in := range []string{"BUKAN_ASSIGNMENT", "1INVALID=value", "HAS SPACE=value"} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Errorf("Parse(%q) seharusnya error", in)
		}
	}
}

func TestLoadMissingFileIsOK(t *testing.T) {
	n, err := Load(filepath.Join(t.TempDir(), "tidak-ada.env"))
	if err != nil {
		t.Fatalf("berkas tidak ada seharusnya bukan error: %v", err)
	}
	if n != 0 {
		t.Errorf("applied = %d, ingin 0", n)
	}
}

func TestLoadDoesNotOverrideExisting(t *testing.T) {
	var (
		keepKey = "ELVAN_TEST_KEEP"
		newKey  = "ELVAN_TEST_NEW"
	)
	t.Setenv(keepKey, "existing")
	t.Cleanup(func() { _ = os.Unsetenv(newKey) })

	path := filepath.Join(t.TempDir(), ".env")
	content := keepKey + "=from-file\n" + newKey + "=from-file\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("tulis .env: %v", err)
	}

	applied, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if applied != 1 {
		t.Errorf("applied = %d, ingin 1 (hanya variabel baru)", applied)
	}
	if got := os.Getenv(keepKey); got != "existing" {
		t.Errorf("%s = %q, ingin tidak ditimpa", keepKey, got)
	}
	if got := os.Getenv(newKey); got != "from-file" {
		t.Errorf("%s = %q, ingin \"from-file\"", newKey, got)
	}
}

func TestLoadDevSkipsProduction(t *testing.T) {
	var (
		key  = "ELVAN_TEST_PROD"
		path = filepath.Join(t.TempDir(), ".env")
	)
	t.Setenv("APP_ENV", "production")
	t.Setenv("DOTENV_PATH", path)
	t.Cleanup(func() { _ = os.Unsetenv(key) })

	if err := os.WriteFile(path, []byte(key+"=should-not-load\n"), 0o600); err != nil {
		t.Fatalf("tulis .env: %v", err)
	}

	applied, err := LoadDev()
	if err != nil {
		t.Fatalf("LoadDev: %v", err)
	}
	if applied != 0 {
		t.Errorf("applied = %d, ingin 0 di production", applied)
	}
	if _, exists := os.LookupEnv(key); exists {
		t.Error("production tidak boleh memuat .env")
	}
}

func TestLoadDevLoadsWhenNotProduction(t *testing.T) {
	var (
		key  = "ELVAN_TEST_DEV"
		path = filepath.Join(t.TempDir(), ".env")
	)
	t.Setenv("APP_ENV", "development")
	t.Setenv("DOTENV_PATH", path)
	t.Cleanup(func() { _ = os.Unsetenv(key) })

	if err := os.WriteFile(path, []byte(key+"=loaded\n"), 0o600); err != nil {
		t.Fatalf("tulis .env: %v", err)
	}

	applied, err := LoadDev()
	if err != nil {
		t.Fatalf("LoadDev: %v", err)
	}
	if applied != 1 {
		t.Errorf("applied = %d, ingin 1", applied)
	}
	if got := os.Getenv(key); got != "loaded" {
		t.Errorf("%s = %q, ingin \"loaded\"", key, got)
	}
}
