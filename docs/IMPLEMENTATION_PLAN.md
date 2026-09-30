# Rencana Implementasi — Elvan Catalog API (Go)

> Dokumen pendamping [`ARCHITECTURE.md`](./ARCHITECTURE.md). Isinya langkah
> eksekusi yang eksplisit, checklist per fase, dan daftar yang perlu
> disiapkan di laptop ini.
>
> Status: **draft rencana v1** — 2026-10-01
> Repo ini: `ElectronicWebBE` (BE baru, saat ini docs-only)
> Frontend: `/home/rascal/Documents/Project/Web/Magang Hardware/ElectronicWeb`

---

## 0. Ringkasan & keputusan yang sudah dikunci

| Topik | Keputusan | Sumber |
|---|---|---|
| Gaya arsitektur | Modular monolith, hexagonal (ports & adapters) | ARCHITECTURE §2, §4 |
| Persistence | `pgx/v5` + `sqlc`, tanpa ORM | ARCHITECTURE §3 |
| **Module path Go** | **`elvan-catalog-api`** (local module, tanpa GitHub) | diputuskan di sesi ini |
| **DB development lokal** | **PostgreSQL 18.1 native** (sudah terinstall) | diputuskan di sesi ini |
| Tipe `price` | `bigint`, selalu bilangan bulat Rupiah | V1 — kode FE |
| Rute detail FE | `/product/:id` → `GET /products/{id}` (bukan slug) | V2 — `src/app/router.tsx` |
| Id lama (Firestore) | `GET /products/{id}` resolve UUID **atau** `legacy_id` | V2 — prioritas `legacy_id` |
| `slug` | FE sudah membuat; server generate jika kosong; duplikat → `409` | V3 — `src/utils/hash.ts` |
| Hapus kategori/brand terpakai | `ON DELETE RESTRICT` → `409` | V4 |
| Bentuk gambar di API | `images: [{ key, fileId }]`; adapter FE memetakan ke array paralel | mismatch dikunci di §4 |
| Auth | Session cookie HttpOnly, server-side di Postgres | ARCHITECTURE §10 |
| Upload gambar | Tetap langsung browser → ImageKit; BE hanya signature + delete | ARCHITECTURE §11 |

**Progres keseluruhan** (perbarui saat jalan):

| Fase | Isi | Status |
|---|---|---|
| 0 | Skeleton repo, toolchain, config, Makefile, CI, depguard | ✅ selesai (2026-10-01) |
| 1 | Migration, domain, port, sqlc, adapter Postgres | ✅ selesai (2026-10-01) |
| 2 | Jalur baca publik (katalog, produk, kategori, brand) | ✅ selesai (2026-10-01) |
| 3 | Adapter API di FE + switch composition root | ☐ |
| 4 | Auth admin + endpoint tulis | ☐ |
| 5 | Modul media (signature, hapus server-side) | ☐ |
| 6 | Importer + verifikasi + cutover VPS | ☐ |
| 7 | Hardening (rate limit, backup, metrics) | ☐ |

> **Status Fase 0 (2026-10-01):** selesai. Yang ada: `go.mod` (module
> `elvan-catalog-api`, `go 1.26`), struktur direktori, `internal/platform/config`
> (env + validasi fail-fast) dan `internal/platform/logger` (slog JSON),
> `cmd/api` (`GET /healthz` + graceful shutdown), `Makefile`, `.golangci.yml`
> (depguard terverifikasi menolak pelanggaran layer), `.env.example`, `.gitignore`,
> `README.md`, dan CI. `gofmt`/`go vet`/`go build`/`golangci-lint`/`go test`
> hijau. **Ditunda ke Fase 1:** `sqlc.yaml` — baru bermakna saat `db/queries`
> sudah berisi query, supaya `make sqlc` tidak gagal.
>
> **Status Fase 1 (2026-10-01):** selesai. `db/migrations/0001_init.sql`
> diterapkan (`goose` v1, 6 tabel + `goose_db_version`). `internal/domain`
> (entity, error sentinel + `ValidationError`, validasi, `Slugify`, `SortOption`)
> + unit test; `internal/application/port` (product/category/brand/admin,
> session, media, security, platform). `sqlc.yaml` + 6 file query → kode di
> `internal/adapter/out/postgres/sqlcgen` (**idempoten**, tanpa diff). Adapter
> Postgres dasar: `pool.go`, `tx.go` (`TxManager` + tx di context), `errors.go`
> (SQLSTATE → error domain), `mapper.go`, plus repo kategori & brand penuh, dan
> `internal/platform/clock`. `gofmt`/`vet`/`build`/`test`/`lint` hijau.
> **Belum:** test integrasi `testcontainers-go` (Docker daemon sedang mati) —
> ditunda ke Fase 2.
>
> **Status Fase 2 (2026-10-01, sebagian):** `ProductRepository` selesai —
> `ListAll` (untuk `GET /catalog`), `List` keyset dengan 4 urutan
> (default/price-asc/price-desc/rating-desc; **satu query per `sort`** agar
> ramah index), cursor opaque base64url (`{s,c,p,r,rc,i}`) yang menolak cursor
> milik `sort` lain, `GetByID` dengan fallback `legacy_id` (URL lama tetap
> hidup), `Create`/`Update` (+ penggantian gambar), dan `Delete` yang
> mengembalikan gambar terlepas untuk dibersihkan di provider setelah commit.
> Total 35 query; `sqlc generate` tetap idempoten. Unit test cursor + `MapError`
> lulus; `gofmt`/`vet`/`build`/`test`/`lint` hijau.
>
> **Test integrasi (2026-10-01):** `postgres_integration_test.go` berjalan
> terhadap PostgreSQL 18.1 nyata via `DATABASE_URL`, dan setiap test dijalankan
> di dalam transaksi yang **selalu di-rollback** sehingga DB tidak berubah
> (diverifikasi: 0 baris sisa). Tanpa `DATABASE_URL`, test ini di-skip. Hasil
> awalnya menemukan satu bug nyata: `ON DELETE RESTRICT` menghasilkan SQLSTATE
> **23001** (bukan 23503), sehingga hapus kategori/brand terpakai tidak menjadi
> `ErrConflict`. Kini 23001 dipetakan ke `ErrConflict` (sesuai keputusan V4).
> Cakupan: CRUD kategori/brand, siklus hidup produk (gambar berurutan,
> fallback `legacy_id`, update ganti gambar, delete mengembalikan gambar),
> RESTRICT saat kategori terpakai, keyset pagination 4 urutan + `includeInactive`
> + cursor rusak/sort mismatch, dan proyeksi `ListAll`.
>
> **Status Fase 2b (2026-10-01):** adapter HTTP baca selesai — router `net/http`
> `ServeMux` (pola method+path), middleware `recover → request id → access log → CORS`, 
> DTO camelCase + waktu ISO-8601 UTC (`.000Z`, sama dengan
> `toISOString()` FE), error mapper domain→HTTP (`validation_failed`,
> `invalid_cursor`, `unauthorized`, `forbidden`, `not_found`, `conflict`,
> `internal`; detail 500 hanya ke log), **ETag** SHA-256 + `Cache-Control: public, max-age=60`
> + dukungan `If-None-Match` (304), dan `/readyz` yang
> mem-ping DB. Use case baca `application/catalog` & `application/taxonomy`
> dengan `Actor` (`includeInactive` hanya dihormati untuk admin, diuji unit).
> Smoke test end-to-end terhadap PostgreSQL nyata: `/healthz` & `/readyz` 200,
> `/catalog` ETag + 304, `/products` `cursor: null`, 400/404 sesuai, access log
> JSON dengan `request_id`, graceful shutdown. Semua `gofmt`/`vet`/`build`/
> `test`/`lint` hijau.
>
> **`api/openapi.yaml` (2026-10-01):** kontrak read path selesai — OpenAPI 3.1
> untuk `/healthz`, `/readyz`, `/catalog`, `/products`, `/products/{id}`,
> `/categories[/{id}]`, `/brands[/{id}]`, plus skema `Error` (envelope, enum
> `code`, detail per field), header `ETag`/`Cache-Control`/`X-Request-Id`, dan
> respons `304`. Lolos `redocly lint` (exit 0; tinggal 7 warning advisory
> `operation-4xx-response` pada GET sederhana yang memang tidak punya 4XX).
>
> **Belum:** test kontrak otomatis (validasi respons handler vs `openapi.yaml`).
>
> **Catatan untuk Fase 4:** subquery `category`/`brand` di `Create`/`Update`
> me-resolve slug ke FK. Category yang tidak ada memicu `NOT NULL` violation
> (→ `ErrValidation`); brand yang tidak ada menjadi `NULL` **diam-diam**, jadi
> use case sebaiknya memvalidasi keberadaan category/brand sebelum menulis.

---

## 1. Audit environment laptop ini

Dicek langsung di mesin (`2026-10-01`).

### 1.1 Yang sudah ada (tidak perlu install)

| Tool | Versi terdeteksi | Catatan |
|---|---|---|
| Go | `go1.26.4 linux/amd64` | Sesuai `go 1.26` di ARCHITECTURE |
| PostgreSQL | client + server `18.1` | Sudah punya `uuidv7()` native |
| Docker | `27.5.1` + Compose `v2.36.0` | Dipakai `testcontainers-go` & deploy |
| git | `2.50.1` | |
| Node.js / npm | `22.20.0` / `10.9.3` | Untuk tooling OpenAPI/FE nanti |
| `psql` | `18.1` | |
| Nix | tersedia (`/run/current-system/sw/bin/nix`) | Sistem ini NixOS |

### 1.2 Tool yang dipasang (sudah beres di laptop ini)

Terpasang lewat `go install` dan `nix profile install`, diakses dari **fish**.

| Tool | Versi | Lokasi |
|---|---|---|
| sqlc | `v1.31.1` | `~/go/bin/sqlc` |
| goose | `v3.28.0` | `~/go/bin/goose` |
| golangci-lint | `2.14.0` (go1.26.4) | `~/go/bin/golangci-lint` |
| make | GNU Make `4.4.1` | `~/.nix-profile/bin/make` |

`~/go/bin` belum ada di PATH secara bawaan, jadi didaftarkan ke `fish_user_paths`
(universal variable, persisten):

```fish
mkdir -p $HOME/go/bin         # fish_add_path mengabaikan folder yang belum ada
set -Ua fish_user_paths $HOME/go/bin
# verifikasi
string join : $PATH | tr ':' '\n' | grep go
```

<details>
<summary>Perintah fresh install (kalau setup di mesin lain)</summary>

```fish
mkdir -p $HOME/go/bin
set -Ua fish_user_paths $HOME/go/bin
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

# make (NixOS)
nix profile install nixpkgs#gnumake

# verifikasi
sqlc version && goose --version && golangci-lint --version && make --version
```

</details>

### 1.3 Dependency Go yang ditambahkan ke `go.mod`

Ditambahkan sesuai kebutuhan tiap fase (tabel ini jadi acuan `go get`).

| Paket | Fase | Fungsi |
|---|---|---|
| `github.com/jackc/pgx/v5` | 1 | Driver + `pgxpool` |
| `github.com/pressly/goose/v3` | 1 | Migration (bisa dipakai embedded; CLI sudah cukup) |
| `golang.org/x/crypto` | 4 | `argon2id` untuk password |
| `golang.org/x/time` | 4 | `rate.Limiter` untuk `/auth/login` |
| `github.com/google/uuid` | 1/6 | Generate/parse UUID (varian v7) untuk importer & token |
| `github.com/testcontainers/testcontainers-go` | 2 | Test integrasi Postgres nyata (butuh Docker) |
| `github.com/testcontainers/testcontainers-go/modules/postgres` | 2 | Helper container Postgres |
| `github.com/google/go-cmp` | 1 | Assertion test (opsional) |
| `github.com/getkin/kin-openapi` | 2 | Test kontrak respons vs `openapi.yaml` (opsional) |

Catatan: ARCHITECTURE §3 memilih **stdlib `testing`**, jadi `testify` tidak
wajib. Kalau ingin, boleh tambah `github.com/stretchr/testify` — tapi jaga
konsistensi satu gaya saja.

Non-Go (opsional, kualitas kontrak): linter OpenAPI lewat Node yang sudah ada.

```bash
npm i -g @redocly/cli      # opsional: redocly lint api/openapi.yaml
```

### 1.4 Setup PostgreSQL native (dev)

```bash
# cek service postgres aktif
pg_isready

# buat role + database untuk dev
sudo -u postgres createuser --pwprompt elvan
sudo -u postgres createdb -O elvan elvan_catalog_dev

# pastikan ekstensi bisa dibuat (dijalankan sebagai superuser sekali)
sudo -u postgres psql -d elvan_catalog_dev -c 'CREATE EXTENSION IF NOT EXISTS pg_trgm;'
```

`DATABASE_URL` contoh (simpan di `.env`, jangan di-commit):

```
DATABASE_URL=postgres://elvan:secret@localhost:5432/elvan_catalog_dev?sslmode=disable
```

### 1.5 Loader `.env` untuk dev

`cmd/api` memuat `.env` di root repo saat startup lewat
`internal/platform/dotenv` (hanya stdlib, tanpa dependensi):

- aktif hanya bila `APP_ENV` bukan `production`;
- variabel yang sudah ada di environment **tidak ditimpa** (environment nyata
  selalu menang);
- mendukung nilai berkutip dan komentar inline; lokasi bisa ditimpa via
  `DOTENV_PATH`.

Jadi `make run` cukup membaca `.env`. Di produksi konfigurasi tetap murni dari
environment (ARCHITECTURE §12).

---

## 2. Aturan main tiap fase (dari ARCHITECTURE §4)

Supaya tiap langkah di bawah patuh, ingat 5 aturan ini:

1. `internal/domain` hanya import stdlib.
2. `internal/application/*` hanya import `domain`; mendefinisikan **port**, bukan implementasinya.
3. `adapter/in/*` dan `adapter/out/*` boleh import `application` + `domain`, **tidak** saling import.
4. Hanya `cmd/*` yang boleh import semuanya dan melakukan wiring (DI manual).
5. Tipe library (`pgx.Row`, `*http.Request`) **tidak bocor** ke `domain`/`application`.

Ditegakkan `depguard` (lihat Fase 0).

---

## 3. Langkah per fase

### Fase 0 — Skeleton repo, toolchain, config, CI

**Hasil:** repo siap, `make lint` dan `make test` lulus, CI jalan.

- [ ] `go mod init elvan-catalog-api` lalu biarkan `go.mod` menulis `go 1.26`.
- [ ] Buat struktur direktori:
  ```bash
  mkdir -p cmd/api cmd/adminctl cmd/importer \
           internal/domain internal/application/port \
           internal/application/catalog internal/application/taxonomy \
           internal/application/auth internal/application/media \
           internal/adapter/in/httpapi internal/adapter/out/postgres/sqlcgen \
           internal/adapter/out/imagekit internal/adapter/out/security \
           internal/platform/config internal/platform/logger \
           db/migrations db/queries api deploy docs
  ```
- [ ] `internal/platform/config/config.go`: baca env, validasi saat startup, gagal cepat. Field sesuai ARCHITECTURE §12 (`APP_ENV`, `HTTP_ADDR`, `DATABASE_URL`, `DB_MAX_CONNS`, `IMAGEKIT_PRIVATE_KEY`, `IMAGEKIT_URL_ENDPOINT`, `SESSION_TTL`, `AUTH_TRANSPORT`, `COOKIE_*`, `CORS_ALLOWED_ORIGINS`, `RATE_LIMIT_LOGIN`, `LOG_LEVEL`).
- [ ] `internal/platform/logger/logger.go`: `slog` JSON handler; level dari config.
- [ ] `sqlc.yaml` (v2):
  ```yaml
  version: "2"
  sql:
    - engine: "postgresql"
      schema: "db/migrations"
      queries: "db/queries"
      gen:
        go:
          package: "sqlcgen"
          out: "internal/adapter/out/postgres/sqlcgen"
          sql_package: "pgx/v5"
          emit_pointers_for_null_types: true
  ```
- [ ] `.golangci.yml` dengan `depguard` (contoh inti):
  ```yaml
  linters:
    enable: [depguard, govet, staticcheck, errcheck, ineffassign, unused]
  linters-settings:
    depguard:
      rules:
        domain:
          files: ["**/internal/domain/**"]
          allow: ["$gostd"]
        application:
          files: ["**/internal/application/**"]
          allow: ["$gostd", "elvan-catalog-api/internal/domain"]
        adapter:
          files: ["**/internal/adapter/**"]
          allow:
            - "$gostd"
            - "elvan-catalog-api/internal/domain"
            - "elvan-catalog-api/internal/application"
            - "$gostd/github.com/jackc/pgx"
  ```
  (Sesuaikan detail impor adapter per paket.)
- [ ] `Makefile` target minimum: `run`, `build`, `test`, `lint`, `sqlc`, `migrate-up`, `migrate-down`, `migrate-create`, `admin-create`. Contoh:
  ```make
  DATABASE_URL ?= postgres://elvan:secret@localhost:5432/elvan_catalog_dev?sslmode=disable
  run:      ; go run ./cmd/api
  build:    ; go build -o bin/api ./cmd/api
  test:     ; go test ./...
  lint:     ; golangci-lint run
  sqlc:     ; sqlc generate
  migrate-up:   ; goose -dir db/migrations postgres "$(DATABASE_URL)" up
  migrate-down: ; goose -dir db/migrations postgres "$(DATABASE_URL)" down
  ```
- [ ] Perbarui `.gitignore`: tambah `bin/`, `.env`, `*.out`, `coverage.*`.
- [ ] `cmd/api/main.go` minimal: baca config → logger → `http.ListenAndServe` → `/healthz`; graceful shutdown (`SIGTERM`).
- [ ] `.github/workflows/ci.yml`: `go vet` → `golangci-lint` → `go test ./...` → cek `sqlc generate` tidak menghasilkan diff.
- [ ] `README.md`: cara setup, tool yang dibutuhkan, Makefile.

**Kriteria lulus:** `go build ./...` OK; `make lint` hijau; `make run` menyajikan `/healthz` → `200`.

---

### Fase 1 — Skema, domain, port, sqlc, adapter Postgres

**Hasil:** skema terkunci, kontrak internal (domain + port) ada, kode sqlc ter-generate.

- [ ] Buat migration pertama:
  ```bash
  goose -dir db/migrations create init sql
  ```
  - [ ] Salin skema **persis** dari ARCHITECTURE §9.1 (kategori, brand, produk, gambar, admin, sesi, semua index).
  - [ ] Pastikan `CREATE EXTENSION IF NOT EXISTS pg_trgm;` ada di baris awal.
  - [ ] `goose ... up` dan cek: `psql -c '\dt'`, `psql -c '\d products'`.
- [ ] `internal/domain/`: `product.go`, `category.go`, `brand.go`, `admin.go`, `errors.go`.
  - [ ] `Product` sesuai §6; `Image{Key, FileID}`; `Rating{Rate, Count}`.
  - [ ] Error sentinel: `ErrNotFound`, `ErrConflict`, `ErrValidation`, `ErrUnauthorized`, `ErrForbidden`.
  - [ ] Validasi invarian di sini (nama tidak kosong, slug valid, `price >= 0`, rating 0–5).
  - [ ] Tambah `ProductPatch` (field pointer) untuk update parsial.
- [ ] `internal/application/port/`: `repository.go`, `session.go`, `media.go`, `security.go`, `platform.go` (`TxManager`, `Clock`).
- [ ] `db/queries/*.sql` untuk sqlc (satu file per agregat: `products.sql`, `categories.sql`, `brands.sql`, `admins.sql`, `sessions.sql`).
- [ ] `make sqlc` → hasil di `internal/adapter/out/postgres/sqlcgen/` (jangan diedit manual).
- [ ] Adapter Postgres dasar:
  - [ ] `pool.go`: buat `pgxpool` dari `DATABASE_URL` + `DB_MAX_CONNS`.
  - [ ] `tx.go`: `TxManager` menyimpan `pgx.Tx` di `context`; repo pakai tx dari context bila ada.
  - [ ] `mapper.go`: **satu-satunya** tempat yang mengenal `category_id`/`brand_id` ↔ slug (lewat `JOIN`).
  - [ ] Terjemahkan error Postgres `23505` → `ErrConflict`, `23503` → `ErrConflict`.
- [ ] Unit test domain (murni): validasi produk, patch, rating.
- [ ] Test integrasi Postgres pakai `testcontainers-go` (butuh Docker) untuk mapper + constraint.

**Kriteria lulus:** `goose up/down` bersih; `make sqlc` idempoten (tanpa diff); `go test ./internal/domain/...` hijau.

---

### Fase 2 — Jalur baca publik

**Hasil:** FE bisa membaca katalog dari API (mode baca).

- [ ] Repo read: `CategoryRepository`, `BrandRepository`, `ProductRepository` (implementasi sqlc).
  - [ ] `List` produk: filter `category`, `brand`, `search` (`ILIKE`/`pg_trgm`), `sort`, `limit`, `cursor`.
  - [ ] **Keyset pagination**: cursor = base64url dari `{sort, lastValue, lastId}`; urutan selalu ditutup `id` (tabel `ORDER BY` di §8). Cursor dengan `sort` tidak cocok → `400 invalid_cursor`.
  - [ ] `GetByID`: coba UUID; **jika tidak ketemu, fallback `legacy_id`** (V2) supaya URL/bookmark lama tidak putus.
  - [ ] Proyeksi `CatalogProduct`: `thumbnail` = gambar `position = 0`; `brand` kosong → `""`.
- [ ] `internal/adapter/in/httpapi/`:
  - [ ] `router.go`: `net/http.ServeMux` gaya Go 1.22+ (`GET /api/v1/products/{id}`).
  - [ ] `middleware.go` urutan dari luar: `recover → request id → access log → CORS → rate limit → CSRF/Origin → auth → handler`.
  - [ ] `errors.go`: satu mapper error domain → HTTP + `code` (tabel §8). Error 500 tidak bocorkan detail ke klien.
  - [ ] DTO camelCase, waktu ISO-8601 UTC.
  - [ ] `GET /api/v1/catalog` (proyeksi penuh) + `ETag` + `Cache-Control: public, max-age=60`.
  - [ ] `GET /api/v1/products`, `/products/{id}`, `/categories`, `/categories/{id}`, `/brands`, `/brands/{id}`.
  - [ ] `/healthz` (hidup) dan `/readyz` (ping DB).
- [ ] `api/openapi.yaml` (OpenAPI 3.1) untuk subset endpoint fase ini.
- [ ] Test HTTP `httptest` (termasuk CORS/CSRF) + test kontrak vs OpenAPI (opsional tapi disarankan).

**Kriteria lulus:** `GET /api/v1/catalog` dan `/products` mengembalikan bentuk yang sama dengan `CatalogProduct` FE; cursor bisa dipakai bolak-balik; `ETag` berubah saat data berubah.

---

### Fase 3 — Adapter API di FE + switch composition root

**Dikerjakan di repo FE** (`/home/rascal/Documents/Project/Web/Magang Hardware/ElectronicWeb`).

- [ ] `src/data/api/http-client.ts`: `fetch` dengan base URL dari env + `credentials: 'include'` + parsing error terstandar.
- [ ] `src/data/api/api-product.repository.ts`, `api-category.repository.ts`, `api-brand.repository.ts` — implement interface di `src/core/repositories/` **tanpa** import Firebase.
  - [ ] Mapper gambar: API `[{key, fileId}]` ↔ FE `images: string[]` + `imageFileIds?: string[]` (lihat §4).
  - [ ] `getById` meneruskan `legacy_id` apa adanya (BE yang resolve).
- [ ] Aktifkan cabang `api` di `src/app/composition.ts` (saat ini sengaja `throw`).
- [ ] Tambah `VITE_API_BASE_URL` di `.env` + `.env.example` (relatif `/api/v1` untuk satu domain).
- [ ] Setelah auth (Fase 4) selesai: `src/data/api/api-auth.service.ts` (`login`/`logout`/`onSessionChange` lewat `GET /auth/me`).
- [ ] Jalankan mode baca dulu: `VITE_API_BASE_URL` aktif, Firebase dibiarkan sebagai cadangan.

**Kriteria lulus:** halaman publik + halaman detail berjalan penuh dari API; tidak ada import Firebase di jalur `api`.

---

### Fase 4 — Auth admin + endpoint tulis

**Hasil:** admin panel berfungsi penuh.

- [ ] `cmd/adminctl`: buat/reset admin (`adminctl create --email ...`), tidak ada endpoint registrasi publik.
- [ ] `internal/adapter/out/security/`: `argon2id` + generator token 32 byte (`crypto/rand`).
- [ ] `internal/adapter/out/postgres/session.go`: simpan **hash SHA-256**, bukan token mentah.
- [ ] `internal/application/auth/`: `Login`, `Logout`, `Me`; TTL dari `SESSION_TTL`; update `last_seen_at` berkala.
- [ ] `SessionTransport` di adapter HTTP: `cookieTransport` (default, `HttpOnly; Secure; SameSite` sesuai env). `bearerTransport` didesain tapi belum dibangun.
- [ ] Endpoint: `POST /auth/login` (rate limit + delay konstan jika gagal), `POST /auth/logout`, `GET /auth/me`.
- [ ] CORS: `Access-Control-Allow-Origin` eksak (bukan `*`), `Credentials: true`, `Vary: Origin`. Untuk method non-aman wajib cek `Origin`/`Sec-Fetch-Site` + `Content-Type: application/json`.
- [ ] Endpoint tulis: `POST/PATCH/DELETE /products[/{id}]`, `/categories[/{id}]`, `/brands[/{id}]`.
  - [ ] Operasi multi-tabel (produk + gambar) dalam `TxManager.WithinTx`.
  - [ ] `DELETE` produk = hard delete; kategori/brand terpakai → `409`.
  - [ ] `includeInactive` hanya dihormati untuk admin terautentikasi.
- [ ] CLI job: pembersihan sesi kedaluwarsa (`DeleteExpired`).
- [ ] Perbarui `openapi.yaml` + test.

**Kriteria lulus:** login/logout/me via cookie; CRUD produk/kategori/brand dari admin panel FE berhasil; akses tulis tanpa sesi → `401`.

---

### Fase 5 — Modul media

**Hasil:** Cloudflare Worker bisa dimatikan.

- [ ] `internal/adapter/out/imagekit/`:
  - [ ] `IssueUploadSignature`: `token = uuid`, `expire = now + 30m`, `signature = HMAC-SHA1(privateKey, token + expire)` (sama persis kontrak Worker).
  - [ ] `Delete`: batch `POST /v1/files/batch/deleteByFileIds`, maks 100 id/request, kembalikan `{ deleted, failed }`.
  - [ ] Private key **hanya** dari config server.
- [ ] `internal/application/media/`: `IssueUploadSignature`, `DeleteImages`.
- [ ] Endpoint: `GET /api/v1/media/signature` (wajib sesi admin — **bukan** cek `Origin` saja), `DELETE /api/v1/media/files` (transisi).
- [ ] Hapus gambar **server-side**: saat produk dihapus atau gambar dilepas via `PATCH`, kumpulkan `fileId` lepas lalu panggil `Delete` **setelah commit** (best-effort, dicatat di log). `fileId` kosong (gambar lama) dilewati.
- [ ] FE: arahkan `VITE_IMAGEKIT_AUTH_ENDPOINT` ke `.../api/v1/media/signature`; hentikan pemanggilan delete klien (`useDeleteProduct.ts`, `ProductForm.tsx`) pada jalur API.

**Kriteria lulus:** admin upload gambar lewat BE (signature), hapus produk membersihkan file di ImageKit tanpa file yatim; tanpa sesi → `401`.

---

### Fase 6 — Importer + verifikasi + cutover VPS

- [ ] `cmd/importer`: baca JSON hasil `scripts/migrate-firestore.cjs` (repo FE) → Postgres dalam satu transaksi.
  - [ ] Urutan: kategori → brand → produk (+ gambar).
  - [ ] `legacy_id` diisi id Firestore → impor **idempoten** (boleh dijalankan ulang).
  - [ ] Resolve `category`/`brand` slug → foreign key.
  - [ ] Pasangkan `images[]` + `imageFileIds[]` berdasarkan index ke `product_images`.
  - [ ] Lewati dokumen `catalog/snapshot` (hanya proyeksi).
- [ ] Admin: buat ulang via `adminctl`, reset password (hash Firebase tidak dipakai).
- [ ] Verifikasi: bandingkan jumlah baris, sampel produk, dan hasil `GET /catalog` vs Firestore.
- [ ] `deploy/Dockerfile` multi-stage (build → image minimal non-root).
- [ ] `deploy/compose.yaml`: `api` + `postgres:18` + `caddy`.
- [ ] `deploy/Caddyfile`: `/` → statis FE, `/api/*` → API `:8080`.
- [ ] Jalankan migration sebagai langkah **terpisah** sebelum API naik (`goose up`), bukan saat startup.
- [ ] Cutover: FE pakai adapter API penuh, Worker dimatikan setelah media terbukti, Firebase read-only sementara.

**Kriteria lulus:** data identik dengan Firestore; situs produksi berjalan di VPS lewat Caddy (TLS otomatis).

---

### Fase 7 — Hardening

- [ ] Rate limit `/auth/login` per IP + per email (`RATE_LIMIT_LOGIN`).
- [ ] Batas ukuran body; header keamanan dasar; validasi whitelist `limit` & `sort`.
- [ ] Timeout per request dan per query lewat `context`.
- [ ] Graceful shutdown: tunggu request berjalan, tutup pool.
- [ ] Backup: `pg_dump` terjadwal ke luar VPS + uji restore berkala.
- [ ] Titik sambung metrics/tracing di middleware (implementasi ditunda).
- [ ] Pastikan `depguard` masih hijau setelah semua fitur masuk.

---

## 4. Kontrak FE ↔ BE yang dikunci (jangan diubah tanpa sinkron FE)

### 4.1 Bentuk gambar (mismatch paling material)

| Sisi | Bentuk |
|---|---|
| FE domain (`src/core/types/product.ts`) | `images: string[]` + `imageFileIds?: string[]` (array paralel) |
| API/BE | `images: [{ key, fileId }]` |

**Aturan:** API memakai `[{ key, fileId }]`; adapter FE (`ApiProductRepository`) yang memetakan ke/dari array paralel supaya UI/form tidak berubah besar. `fileId` tanpa nilai → `""`/`null`.

### 4.2 Id & URL lama (V2)

- FE rute: `/product/:id` (`src/app/router.tsx`), ambil lewat `getById(id)`.
- BE: `GET /products/{id}` — cari UUID dulu; jika tidak ada, fallback ke `legacy_id` (id Firestore). Ini menjaga bookmark/URL lama tetap hidup setelah cutover ke UUIDv7.
- Hint `id` dari klien pada `create` **diabaikan**; server selalu membuat id.

### 4.3 Slug (V3)

- FE mengirim `slug` (dari `toSlug()`); BE menerima jika valid.
- Jika `slug` kosong → server generate dari `name`.
- Duplikat → `409 conflict` dengan pesan jelas (FE sudah punya `toast.error`).

### 4.4 Hapus kategori/brand (V4)

- `ON DELETE RESTRICT`; penghapusan yang masih dipakai → `409`.

### 4.5 Auth transport

- BE: sesi cookie HttpOnly + `GET /auth/me`; FE `ApiAuthService` pakai `credentials: 'include'`, map `AuthUser.uid` ← id admin.

### 4.6 Filter `brand`

- BE `GET /products` mendukung `brand`; `ProductListOptions` FE belum punya `brand` (UI publik hanya filter kategori). Endpoint tetap ada; FE menambah nanti bila perlu.

---

## 5. Testing & CI

| Level | Sasaran | Cara |
|---|---|---|
| Unit | `domain`, use case | fake in-memory untuk port; tanpa DB/jaringan |
| Integrasi | adapter Postgres | `testcontainers-go` + Postgres 18; jalankan migration nyata |
| HTTP | handler + middleware | `httptest`; termasuk CORS/CSRF & auth |
| Kontrak | API vs `openapi.yaml` | validasi respons terhadap skema |
| Arsitektur | aturan dependensi | `depguard` di `golangci-lint` |

Pipeline CI minimum:
```
go vet → golangci-lint → go test ./... → validasi OpenAPI → sqlc generate tidak menghasilkan diff
```

---

## 6. Urutan commit / PR yang disarankan

1. `chore: scaffold module, config, Makefile, lint, CI` (Fase 0)
2. `feat(db): initial schema + goose migration` (Fase 1)
3. `feat(domain): entities, errors, ports` (Fase 1)
4. `feat(postgres): sqlc queries, pool, tx manager, mappers` (Fase 1)
5. `feat(http): public read endpoints + error mapper + cursor` (Fase 2)
6. `chore(openapi): document public read contract` (Fase 2)
7. `feat(auth): admin sessions, login/logout/me + middleware` (Fase 4)
8. `feat(api): write endpoints for products/categories/brands` (Fase 4)
9. `feat(media): imagekit signature + server-side delete` (Fase 5)
10. `feat(importer): firestore JSON import (idempotent)` (Fase 6)
11. `chore(deploy): dockerfile, compose, caddy, backup` (Fase 6–7)

(FE: PR terpisah di repo FE untuk Fase 3 & penyesuaian Fase 5.)

---

## 7. Definition of Done (global)

- [ ] `make lint` dan `make test` hijau; `depguard` menegakkan aturan layer.
- [ ] `sqlc generate` bersih (tidak ada diff).
- [ ] `openapi.yaml` sinkron dengan handler.
- [ ] Tidak ada secret di repo (semua dari env yang divalidasi saat startup).
- [ ] `GET /catalog` dan list publik memakai `ETag` + `Cache-Control`.
- [ ] Semua endpoint tulis & media wajib sesi admin (`401` tanpa sesi).
- [ ] Hapus gambar dilakukan server-side setelah commit; kegagalan dicatat di log.
- [ ] Migration dijalankan eksplisit sebelum deploy; deploy + rollback terdokumentasi.
