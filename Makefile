# Makefile — Elvan Catalog API
# Semua target dijalankan dari root repo. Shell: POSIX sh (bash/fish kompatibel).

GO             ?= go
BINARY         ?= bin/api
MIGRATIONS_DIR ?= db/migrations
# DSN test integrasi; test yang di-skip bila kosong memakai fallback DATABASE_URL.
TEST_DATABASE_URL ?=

.PHONY: help run build test test-integration vet lint fmt tidy sqlc admin-create sessions-prune migrate-status migrate-up migrate-down migrate-create

help: ## Tampilkan daftar target
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

run: ## Jalankan server API
	$(GO) run ./cmd/api

build: ## Build binary ke bin/api
	$(GO) build -o $(BINARY) ./cmd/api

test: ## Jalankan semua test
	$(GO) test ./...

test-integration: ## Test integrasi Postgres (butuh TEST_DATABASE_URL; fallback DATABASE_URL)
	@test -n "$(TEST_DATABASE_URL)" || { echo "TEST_DATABASE_URL belum diisi"; exit 1; }
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" $(GO) test ./internal/adapter/out/postgres/ -run Integration -v

vet: ## go vet
	$(GO) vet ./...

lint: ## golangci-lint
	golangci-lint run

fmt: ## Format kode
	gofmt -w .

tidy: ## Sinkronkan go.mod/go.sum
	$(GO) mod tidy

sqlc: ## Generate kode sqlc
	sqlc generate

admin-create: ## Buat admin: make admin-create EMAIL=admin@example.com
	@test -n "$(EMAIL)" || { echo "EMAIL belum diisi, contoh: make admin-create EMAIL=admin@example.com"; exit 1; }
	$(GO) run ./cmd/adminctl create --email "$(EMAIL)"

sessions-prune: ## Hapus sesi yang sudah kedaluwarsa
	$(GO) run ./cmd/adminctl prune-sessions

migrate-status: ## Status migrasi (butuh DATABASE_URL)
	@test -n "$(DATABASE_URL)" || { echo "DATABASE_URL belum diisi"; exit 1; }
	goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" status

migrate-up: ## Terapkan migrasi (butuh DATABASE_URL)
	@test -n "$(DATABASE_URL)" || { echo "DATABASE_URL belum diisi"; exit 1; }
	goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" up

migrate-down: ## Mundur 1 migrasi (butuh DATABASE_URL)
	@test -n "$(DATABASE_URL)" || { echo "DATABASE_URL belum diisi"; exit 1; }
	goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" down

migrate-create: ## Buat migrasi baru: make migrate-create NAME=add_xyz
	@test -n "$(NAME)" || { echo "NAME belum diisi, contoh: make migrate-create NAME=add_variants"; exit 1; }
	goose -dir $(MIGRATIONS_DIR) create $(NAME) sql
