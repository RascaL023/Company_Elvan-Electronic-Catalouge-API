package config

import (
	"strings"
	"testing"
)

// setBaseEnv mengisi environment minimum yang valid dan menetralkan variabel
// yang diuji supaya hasil tidak bergantung pada environment proses.
func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db?sslmode=disable")
	t.Setenv("APP_ENV", "")
	t.Setenv("COOKIE_SECURE", "")
}

func TestLoadDevelopmentDefaults(t *testing.T) {
	setBaseEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Env != "development" {
		t.Errorf("Env = %q, ingin development", cfg.Env)
	}
	if cfg.CookieSecure {
		t.Error("development tidak boleh memaksa CookieSecure true")
	}
}

func TestLoadDevelopmentAllowsInsecureCookie(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("COOKIE_SECURE", "false")

	if _, err := Load(); err != nil {
		t.Fatalf("Load development: %v", err)
	}
}

// Production wajib Secure: tanpa itu cookie sesi bisa terkirim lewat HTTP.
func TestLoadProductionRequiresCookieSecure(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ENV", "production")

	_, err := Load()
	if err == nil {
		t.Fatal("Load production tanpa COOKIE_SECURE = nil, ingin error")
	}
	if !strings.Contains(err.Error(), "COOKIE_SECURE") {
		t.Errorf("error = %v, ingin menyebut COOKIE_SECURE", err)
	}
}

func TestLoadProductionWithCookieSecure(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("COOKIE_SECURE", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load production: %v", err)
	}
	if !cfg.CookieSecure {
		t.Error("CookieSecure = false, ingin true")
	}
}

// Guard SameSite=none tetap berlaku di luar production.
func TestLoadSameSiteNoneRequiresSecure(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("COOKIE_SAMESITE", "none")
	t.Setenv("COOKIE_SECURE", "false")

	if _, err := Load(); err == nil {
		t.Fatal("SameSite=none tanpa Secure = nil, ingin error")
	}
}
