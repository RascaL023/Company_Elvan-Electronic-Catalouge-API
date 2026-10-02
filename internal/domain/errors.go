// Package domain berisi entity dan aturan bisnis murni.
//
// Paket ini tidak boleh meng-import paket internal lain maupun library pihak
// ketiga (hanya stdlib). Aturan itu ditegakkan depguard di .golangci.yml.
package domain

import (
	"errors"
	"strings"
)

// Error sentinel domain. Adapter HTTP memetakan ini ke status HTTP; use case
// hanya perlu mengembalikannya.
var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrValidation   = errors.New("validation")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)

// FieldError adalah satu kesalahan validasi pada satu field.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError mengumpulkan FieldError dan membungkus ErrValidation,
// sehingga pemanggil tetap bisa memakai errors.Is(err, ErrValidation) sekaligus
// membaca detail per field (dipakai adapter HTTP untuk respons 400/422).
type ValidationError struct {
	Fields []FieldError
}

// NewValidationError membuat ValidationError kosong.
func NewValidationError() *ValidationError { return &ValidationError{} }

// Add menambahkan satu kesalahan field.
func (e *ValidationError) Add(field, message string) {
	e.Fields = append(e.Fields, FieldError{Field: field, Message: message})
}

// HasErrors melaporkan apakah ada minimal satu field bermasalah.
func (e *ValidationError) HasErrors() bool { return len(e.Fields) > 0 }

func (e *ValidationError) Error() string {
	if len(e.Fields) == 0 {
		return ErrValidation.Error()
	}
	var b strings.Builder
	b.WriteString("validasi gagal: ")
	for i, f := range e.Fields {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(f.Field)
		b.WriteString(": ")
		b.WriteString(f.Message)
	}
	return b.String()
}

// Unwrap membuat errors.Is(err, ErrValidation) bernilai true.
func (e *ValidationError) Unwrap() error { return ErrValidation }
