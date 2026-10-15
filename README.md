# Elvan Catalog API

Backend Go + PostgreSQL untuk katalog elektronik Elvan Electronic. Repo ini
terpisah dari frontend (`Company_Elvan-Electronic-Catalouge`).

Baru bergabung? Mulai dari **[`docs/DEVELOPER_GUIDE.md`](docs/DEVELOPER_GUIDE.md)**
(onboarding, konvensi, resep tugas, troubleshooting).

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
make import INPUT=export.json          # impor data Firestore (idempoten)
make import-dry INPUT=export.json      # validasi file ekspor saja
make verify                             # hitung baris per tabel (verifikasi)
```

## Impor data Firestore (Fase 6)

Format file ekspor: JSON berisi `categories`, `brands`, `products` (dokumen
Firestore + field `legacy_id` = id dokumen). Dokumen `catalog/snapshot` ikut
boleh ada di file — hanya proyeksi, otomatis tidak dipakai.

```bash
make import-dry INPUT=export.json   # validasi semua dokumen tanpa menulis
make import INPUT=export.json       # tulis dalam SATU transaksi (idempoten)
make verify                         # bandingkan jumlah baris
```

Impor **aman dijalankan ulang**: baris dicocokkan lewat `legacy_id`, jadi tidak
pernah menggandakan data. Urutan: kategori → brand → produk (+ gambar); slug
`category`/`brand` di-resolve ke foreign key di dalam transaksi yang sama.

## Deploy (Fase 6–7)

Runbook produksi (Docker multi-stage, compose `postgres:18` + `api` + `caddy`,
migrasi terpisah, cutover, backup terjadwal) ada di
[`deploy/README.md`](deploy/README.md).

## Struktur

```
cmd/                 composition root (api, adminctl, importer)
internal/domain/     entity + aturan bisnis (tanpa dependensi)
internal/application/ use case + port (catalog, taxonomy, auth, media, importer)
internal/adapter/    in/httpapi, out/postgres|imagekit|security
internal/platform/   config, logger
db/migrations/       goose SQL
db/queries/          query untuk sqlc
api/openapi.yaml     kontrak API
deploy/              Dockerfile, compose, Caddyfile, backup, runbook
```
