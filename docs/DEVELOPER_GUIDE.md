# Panduan Developer — Elvan Catalog API

Dokumen ini untuk **programmer yang baru bergabung**. Tujuannya: dalam 15–30
menit kamu paham bentuk proyek, bisa menjalankannya, tahu di mana harus
menambah kode, dan mengikuti konvensinya. Untuk desain mendalam lihat
[`ARCHITECTURE.md`](ARCHITECTURE.md); untuk status per fase lihat
[`IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md).

---

## 1. Apa ini

Backend Go + PostgreSQL untuk katalog elektronik Elvan Electronic. Berdiri
sendiri dari frontend (repo FE terpisah). Arsitekturnya **modular monolith**
dengan **hexagonal / ports & adapters**:

```
        HTTP (adapter/in)  ──▶  application (use case + port)  ──▶  adapter/out (Postgres, ImageKit, security)
                                        │
                                        └──▶  domain (entity + aturan bisnis)
```

Aturan emas: **`domain` tidak bergantung pada apa pun**, `application` hanya
tahu `domain` + port-nya sendiri, `adapter` mengimplementasikan port, dan
`cmd/*` adalah satu-satunya tempat wiring (DI manual, tanpa framework).

### Tiga binari

| Binari | Fungsi | Entry |
|---|---|---|
| `api` | server HTTP | `cmd/api/main.go` |
| `adminctl` | CLI admin (create/reset password, prune sesi) | `cmd/adminctl/main.go` |
| `importer` | impor JSON Firestore (idempoten) | `cmd/importer/main.go` |

---

## 2. Mulai cepat (TL;DR)

Prasyarat: **Go 1.26**, **PostgreSQL 18**, **sqlc v1.31+**, **goose v3.28+**,
**golangci-lint v2.14+**, `make`.

```bash
git clone <repo> && cd ElectronicWebBE

# 1. Konfigurasi
cp .env.example .env
#   isi DATABASE_URL (dan opsional IMAGEKIT_PRIVATE_KEY)

# 2. Migrasi + jalankan
make migrate-up          # butuh DATABASE_URL
make run                 # server di HTTP_ADDR (default :8080)

# 3. Cek hidup
curl -fsS http://localhost:8080/healthz   # -> ok
curl -fsS http://localhost:8080/readyz    # -> ok (ping DB)
```

`.env` dimuat otomatis **hanya saat `APP_ENV != production`** oleh
`internal/platform/dotenv`. Nilai yang sudah ada di environment tidak ditimpa.

---

## 3. Perintah sehari-hari

```bash
make help             # daftar semua target
make run              # jalankan server
make build            # build ke bin/api
make test             # go test ./...  (integrasi di-skip bila DSN kosong)
make test-integration # test integrasi Postgres (WAJIB set TEST_DATABASE_URL)
make vet              # go vet ./...
make lint             # golangci-lint (termasuk aturan layer depguard)
make fmt              # gofmt -w .
make sqlc             # regenerate kode sqlc
make migrate-up       # terapkan migrasi
make migrate-status   # status migrasi
make migrate-down     # mundur 1 migrasi
make migrate-create NAME=add_xyz   # buat migrasi baru
```

Test integrasi:

```bash
set -a; . ./.env; set +a
TEST_DATABASE_URL="$DATABASE_URL" go test ./internal/adapter/out/postgres/ -run Integration -v -count=1
# atau: make test-integration
```

---

## 4. Peta direktori

```
cmd/                     composition root (api, adminctl, importer)
internal/domain/         entity + aturan bisnis; HANYA stdlib
internal/application/    use case + port (catalog, taxonomy, auth, media, importer)
  └── port/              interface (repository, session, media, security, platform)
internal/adapter/
  ├── in/httpapi/        router, middleware, handler, DTO, error mapper
  └── out/
      ├── postgres/      repository sqlc + sqlcgen/ (GENERATED)
      ├── imagekit/      signature upload + hapus berkas
      └── security/      argon2id + token generator
internal/platform/       config, logger, dotenv, clock
db/migrations/           goose SQL
db/queries/              query untuk sqlc
api/openapi.yaml         kontrak API (OpenAPI 3.1)
deploy/                  Dockerfile, compose, Caddyfile, backup, runbook
```

**File kunci untuk dibaca lebih dulu (urutan yang disarankan):**

1. `internal/domain/product.go` — bentuk data + invarian.
2. `internal/application/port/repository.go` — kontrak yang harus dipenuhi adapter.
3. `internal/application/catalog/service.go` — contoh use case (baca + tulis).
4. `internal/adapter/in/httpapi/router.go` — semua rute dalam satu tempat.
5. `internal/adapter/in/httpapi/errors.go` — **satu-satunya** pemetaan error → HTTP.
6. `cmd/api/main.go` — wiring DI dari config sampai router.

---

## 5. Trace satu request (contoh `GET /api/v1/catalog`)

```
router.go          GET /api/v1/catalog → handlers.catalog
  ↓ (middleware: recover → request id → access log → CORS → originGuard → timeout)
handlers.catalog   panggil deps.Catalog.ListAll(ctx, actor)
  ↓
catalog.Service    baca via port.ProductRepository
  ↓
postgres adapter   jalankan query sqlc (sqlcgen) → mapper row → domain
  ↓
handler            encode DTO camelCase + ETag/Cache-Control → JSON
```

Error tidak pernah "ditebak" di tengah: use case mengembalikan error domain
(`ErrNotFound`, `ErrConflict`, `ErrValidation`, `ErrUnauthorized`, `ErrForbidden`),
lalu `writeError` di `errors.go` yang memutuskan status HTTP + envelope JSON.

---

## 6. Aturan & konvensi (penting)

- **Jangan edit `internal/adapter/out/postgres/sqlcgen/`** — itu hasil generate.
  Ubah `db/queries/*.sql` lalu `make sqlc`; CI/cek `sqlc diff` harus bersih.
- **Aturan layer ditegakkan `depguard`** (`.golangci.yml`):
  - `domain` → hanya stdlib.
  - `application` → stdlib + `domain` + sesama `application`.
  - `adapter/in` tidak boleh import `adapter/out`, dan sebaliknya.
- **Error → HTTP hanya di `errors.go`.** Jangan menulis status langsung di handler.
- **DTO camelCase**, waktu ISO-8601 UTC (`.000Z`). Baca memakai `ETag` +
  `Cache-Control` dan mendukung `If-None-Match` → `304`.
- **`sqlcgen` mapper** (`mapper.go`) adalah satu-satunya tempat yang mengenal
  `category_id`/`brand_id` ↔ slug.
- **Secret tidak pernah di repo**; semua dari env, divalidasi saat startup
  (`internal/platform/config`, gagal cepat).
- **Payload produk**: `category`/`brand` berupa **slug** (bukan UUID);
  `images` berupa `[{ key, fileId }]`.
- **`GET /products/{id}`** menerima UUID **atau** `legacy_id` (id Firestore) untuk
  baca; **tulis** wajib UUID canonical (id legacy → `400`).

---

## 7. Resep: menambah sesuatu

### 7.1 Menambah endpoint baca
1. Tambah method di port (`application/port/repository.go`).
2. Query SQL di `db/queries/*.sql` → `make sqlc`.
3. Implementasikan di adapter `internal/adapter/out/postgres/*.go`.
4. Tambah use case di `application/<modul>/service.go`.
5. Tambah interface konsumen + handler di `adapter/in/httpapi/`, lalu rute di `router.go`.
6. Dokumentasikan di `api/openapi.yaml` (`npx --yes @redocly/cli lint api/openapi.yaml`).

### 7.2 Menambah endpoint tulis (admin)
Sama seperti di atas, tetapi daftarkan rute dengan `h.requireAdmin(...)` dan
tambahkan `security: [cookieAuth]` di `openapi.yaml`. Tanpa sesi → `401`.

### 7.3 Menambah kolom/tabel
1. `make migrate-create NAME=add_xyz` → isi SQL di `db/migrations/`.
2. Sesuaikan `db/queries/*.sql` → `make sqlc`.
3. Sesuaikan `domain`, `mapper.go`, DTO/handler.
4. `make migrate-up` + pastikan `~/go/bin/sqlc diff` kosong.

### 7.4 Membuat admin (lokal)
```bash
echo 'PasswordRahasia123!' | go run ./cmd/adminctl create --email admin@example.com
```

---

## 8. Testing

| Level | Lokasi | Cara |
|---|---|---|
| Unit | `internal/{domain,application}/**_test.go` | fake in-memory, tanpa DB/jaringan |
| Integrasi Postgres | `internal/adapter/out/postgres/**_test.go` | DB nyata via `TEST_DATABASE_URL` |
| HTTP | `internal/adapter/in/httpapi/**_test.go` | `httptest` (CORS, auth, error, ETag) |

Catatan integrasi: setiap test berjalan di dalam **satu transaksi yang selalu
di-rollback**, jadi data dev tidak berubah. Bila `TEST_DATABASE_URL` (fallback
`DATABASE_URL`) kosong, test **di-skip** sehingga `go test ./...` aman tanpa DB.

Sebelum membuka PR, jalankan minimal:

```bash
gofmt -l .            # harus kosong
go vet ./... && go build ./...
go test ./... -count=1
~/go/bin/golangci-lint run    # 0 issues
~/go/bin/sqlc diff            # (no diff)
npx --yes @redocly/cli lint api/openapi.yaml
```

---

## 9. CI

`.github/workflows/ci.yml` menjalankan pada PR: service `postgres:18-alpine` →
`go vet` → `golangci-lint` → migrasi goose → `go test ./...` (integrasi ikut
lewat `TEST_DATABASE_URL`) → `go build`. Jika kamu menambah query SQL, pastikan
`sqlc` sudah di-generate (jika tidak, code di CI tidak sinkron).

---

## 10. Alur branch

- `main` = rilis; `dev` = integrasi harian; fitur dikerjakan di `feat/*` lalu PR
  ke `dev` (dan `dev` → `main` saat rilis).
- Commit mengikuti gaya *conventional commits* (mis. `feat(auth): ...`,
  `fix(config): ...`, `chore(deploy): ...`).

---

## 11. Troubleshooting

| Gejala | Sebab umum | Solusi |
|---|---|---|
| `konfigurasi tidak valid: DATABASE_URL wajib diisi` | `.env` belum ada / kosong | `cp .env.example .env` dan isi `DATABASE_URL` |
| `konfigurasi tidak valid: COOKIE_SECURE wajib true saat APP_ENV=production` | menjalankan production tanpa cookie Secure | set `COOKIE_SECURE=true` |
| Integrasi "dilewati" (`t.Skip`) | `TEST_DATABASE_URL`/`DATABASE_URL` kosong | isi DSN, atau `make test-integration` |
| `sqlc diff` muncul | query diubah tapi kode belum di-generate | `make sqlc` lalu commit hasilnya |
| `GET /api/v1/media/signature` → `404` | `IMAGEKIT_PRIVATE_KEY` kosong | isi key (endpoint sengaja dinonaktifkan) |
| Login balas `500`/`504` (bukan 401) | DB bermasalah/timeout — **memang begitu** (bukan bug) | cek DB/`readyz`; lihat log `login gagal karena error internal` |
| `409 conflict` saat hapus kategori/brand | masih dipakai produk (`ON DELETE RESTRICT`) | pindahkan/hapus produk terkait dulu |

---

## 12. Checklist PR

- [ ] `gofmt -l .` kosong, `go vet ./...` & `go build ./...` hijau.
- [ ] `go test ./... -count=1` hijau (set `TEST_DATABASE_URL` untuk integrasi).
- [ ] `golangci-lint run` 0 issues; tidak melanggar `depguard`.
- [ ] Query SQL berubah → `make sqlc` sudah dijalankan, `sqlc diff` kosong.
- [ ] Endpoint baru/berubah → `api/openapi.yaml` diperbarui & lint valid.
- [ ] Tidak ada secret/token yang ter-commit.
