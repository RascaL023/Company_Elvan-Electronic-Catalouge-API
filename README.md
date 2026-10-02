# Elvan Catalog API

Backend Go + PostgreSQL untuk katalog elektronik Elvan Electronic. Repo ini
terpisah dari frontend (`Company_Elvan-Electronic-Catalouge`).

Desain lengkap ada di [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), dan
langkah implementasi per fase ada di
[`docs/IMPLEMENTATION_PLAN.md`](docs/IMPLEMENTATION_PLAN.md).

Arsitektur: **modular monolith** dengan **hexagonal / ports & adapters**.
`domain` tidak bergantung pada apa pun; `application` mendefinisikan port;
`adapter` mengimplementasikannya; `cmd/*` melakukan wiring.

## Kebutuhan

| Tool | Versi |
|---|---|
| Go | 1.26 |
| PostgreSQL | 18 |
| sqlc | v1.31+ |
| goose | v3.28+ |
| golangci-lint | v2.14+ |
| make | 4.x |

## Mulai

```bash
cp .env.example .env      # isi DATABASE_URL
make migrate-up           # terapkan migrasi (butuh DATABASE_URL)
make run                  # jalankan server di HTTP_ADDR
```

Test integrasi Postgres memakai `TEST_DATABASE_URL` (di CI diisi otomatis oleh
service container); bila kosong, test di-skip sehingga `make test` tetap aman
tanpa database:

```bash
export TEST_DATABASE_URL="postgres://user:pass@localhost:5432/elvan_catalog_test?sslmode=disable"
make test-integration
```

Cek liveness:

```bash
curl -fsS http://localhost:8080/healthz   # -> ok
```

## Perintah

```bash
make help            # daftar semua target
make run             # jalankan server
make build           # build ke bin/api
make test            # go test ./...
make test-integration # test integrasi Postgres (butuh TEST_DATABASE_URL)
make vet             # go vet ./...
make lint            # golangci-lint (termasuk aturan layer depguard)
make fmt             # gofmt
make sqlc            # generate kode sqlc
make migrate-up      # terapkan migrasi
make migrate-status  # status migrasi
make migrate-down    # mundur 1 migrasi
make migrate-create NAME=add_xyz
```

## Struktur

```
cmd/                 composition root (api, adminctl, importer)
internal/domain/     entity + aturan bisnis (tanpa dependensi)
internal/application/ use case + port
internal/adapter/    in/httpapi, out/postgres|imagekit|security
internal/platform/   config, logger
db/migrations/       goose SQL
db/queries/          query untuk sqlc
api/openapi.yaml     kontrak API
```
