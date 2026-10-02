// Package logger menyediakan logger aplikasi berbasis log/slog.
//
// Output selalu JSON ke stdout supaya mudah dikumpulkan agen log. Nilai
// sensitif (password, token, private key) tidak boleh di-log oleh pemanggil.
package logger

import (
	"log/slog"
	"os"
	"strings"
)

// New mengembalikan logger JSON dengan level yang diminta. Level tak dikenal
// diperlakukan sebagai "info".
func New(level string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLevel(level),
	}))
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
