# Elvan Catalog API — Arsitektur Backend

> Dokumen desain untuk backend katalog elektronik Elvan Electronic.
> Repo ini **terpisah** dari frontend (`Company_Elvan-Electronic-Catalouge`).
> Status: **Draft v1** — 2026-10-01

---

## Daftar Isi

1. [Konteks & Tujuan](#1-konteks--tujuan)
2. [Keputusan Arsitektur](#2-keputusan-arsitektur)
3. [Tech Stack](#3-tech-stack)
4. [Gaya Arsitektur & Aturan Dependensi](#4-gaya-arsitektur--aturan-dependensi)
5. [Struktur Repo](#5-struktur-repo)
6. [Domain](#6-domain)
7. [Application: Port & Use Case](#7-application-port--use-case)
8. [Adapter Inbound (HTTP)](#8-adapter-inbound-http)
9. [Adapter Outbound (Postgres, ImageKit, Security)](#9-adapter-outbound-postgres-imagekit-security)
10. [Auth & Kesiapan Multi-Domain](#10-auth--kesiapan-multi-domain)
11. [Media / Gambar](#11-media--gambar)
12. [Konfigurasi](#12-konfigurasi)
13. [Cross-Cutting Concerns](#13-cross-cutting-concerns)
14. [Strategi Testing](#14-strategi-testing)
15. [Deployment](#15-deployment)
16. [Migrasi dari Firebase](#16-migrasi-dari-firebase)
17. [Roadmap Implementasi](#17-roadmap-implementasi)
18. [Hal yang Perlu Diverifikasi](#18-hal-yang-perlu-diverifikasi)

---

## 1. Konteks & Tujuan

### Kondisi saat ini

| Komponen | Teknologi | Peran |
|---|---|---|
| Frontend | React + TypeScript + Vite | Katalog publik + area admin |
| Backend | Firebase (Spark) | Tidak ada BE sendiri; FE langsung ke Firestore |
| Database | Firestore | Produk, kategori, brand, admin |
| Auth admin | Firebase Authentication | Login admin |
| Image | ImageKit | Storage + CDN; upload langsung dari browser |
| BE tambahan | Cloudflare Worker | Menerbitkan signature upload ImageKit, menghapus file |

### Tujuan

- Memindahkan seluruh BE ke **VPS** dengan **Go** dan **PostgreSQL**.
- Arsitektur **bersih dan berlapis**, mudah diuji, mudah diganti bagian infrastrukturnya.
- **Kontrak kompatibel** dengan abstraksi yang sudah ada di FE (`ProductRepository`, `CategoryRepository`, `BrandRepository`, `AuthService`, `ImageUploadService`), sehingga adapter API di FE cukup mengikuti kontrak yang sama.
- Siap skala vertikal dulu (satu VPS), dengan jalur jelas untuk skala horizontal.

### Non-tujuan (untuk saat ini)

- Microservice, message broker, atau CQRS penuh. Belum dibutuhkan.
- Multi-tenant, multi-role (selain admin), checkout/pembayaran/keranjang. Di luar lingkup katalog.
- Soft delete dan riwayat perubahan. `is_active` hanya flag tampil/sembunyi.

---

## 2. Keputusan Arsitektur

| # | Keputusan | Alasan |
|---|---|---|
| D1 | **Modular monolith**, satu binary | Satu tim, satu VPS; kompleksitas operasional minimal |
| D2 | **Hexagonal / ports & adapters** | Domain dan use case tidak bergantung pada DB, HTTP, atau vendor |
| D3 | **SQL-first** (pgx v5 + sqlc), tanpa ORM | Query eksplisit, type-safe, tanpa magic; hasil generate tetap di adapter |
| D4 | **Session server-side** di Postgres (cookie) | Bisa dicabut seketika; cukup untuk admin-only |
| D5 | Transport sesi **bisa dipertukarkan** (cookie / bearer) | Siap jika FE dan API beda domain |
| D6 | **UUIDv7** (`uuidv7()` bawaan PG 18) sebagai id | Terurut waktu, index efisien, id tetap opaque di domain |
| D7 | **Keyset pagination** dengan cursor opaque | Sesuai kontrak FE (`cursor` opaque); stabil dan cepat |
| D8 | **OpenAPI** sebagai sumber kebenaran kontrak | FE dan BE repo terpisah; kontrak harus eksplisit dan terversi |
| D9 | Upload gambar **tetap langsung dari browser ke ImageKit** | File tidak lewat server; BE hanya menerbitkan signature dan menghapus file |
| D10 | Hapus produk = **hard delete**; kategori/brand yang masih dipakai tidak bisa dihapus (`RESTRICT`) | `is_active` hanya untuk tampil/sembunyi; integritas data terjaga |
| D11 | **Tidak ada snapshot read model** | Itu optimasi kuota Firestore; di Postgres cukup query proyeksi + index + cache HTTP |

---

## 3. Tech Stack

| Lapisan | Pilihan | Catatan |
|---|---|---|
| Bahasa | Go `1.26.4` | Pin di `go.mod` (`go 1.26`) |
| Database | PostgreSQL `18.1` | Fitur dipakai: `uuidv7()`, `pg_trgm` |
| Driver | `pgx/v5` (+ `pgxpool`) | |
| Query | `sqlc` | Generate ke `adapter/out/postgres/sqlcgen` |
| Migration | `goose` (atau `atlas`) | File SQL bertanggal, dijalankan eksplisit |
| HTTP | `net/http` standar | `ServeMux` dengan pola method+path (`GET /products/{id}`) |
| Logging | `log/slog` (JSON) | |
| Password | `argon2id` | `golang.org/x/crypto/argon2` |
| Validasi | Manual di domain/use case | Tanpa framework validasi |
| Testing | `testing`, `testcontainers-go` | Postgres 18 sungguhan untuk test adapter |
| Lint | `golangci-lint` + `depguard` | Menegakkan aturan dependensi layer |
| Kontrak | OpenAPI 3.1 | `api/openapi.yaml` |

---

## 4. Gaya Arsitektur & Aturan Dependensi

```
            ┌────────────────────────────────────────────┐
            │              cmd/api (main.go)             │   composition root
            │   config → adapter → use case → router     │   (satu-satunya tempat wiring)
            └──────────────────────┬─────────────────────┘
                                   │
   ┌───────────────────┐   ┌───────▼────────────┐   ┌──────────────────────┐
   │  adapter/in/      │──▶│    application     │◀──│  adapter/out/        │
   │  httpapi          │   │  use case + port   │   │  postgres, imagekit, │
   │  (handler, DTO)   │   │                    │   │  security            │
   └───────────────────┘   └───────┬────────────┘   └──────────────────────┘
                                   │
                           ┌───────▼────────────┐
                           │      domain        │   entity, aturan bisnis,
                           │  (tanpa dependensi)│   error domain
                           └────────────────────┘
```

### Aturan (ditegakkan lewat `depguard` di CI)

1. `domain` **tidak meng-import** paket internal lain maupun library pihak ketiga (hanya stdlib).
2. `application` hanya meng-import `domain`. Ia mendefinisikan **port** (interface); tidak tahu implementasinya.
3. `adapter/in/*` dan `adapter/out/*` boleh meng-import `application` dan `domain`, **tidak boleh saling meng-import**.
4. Hanya `cmd/*` yang boleh meng-import semuanya dan melakukan wiring (DI manual, tanpa framework).
5. Tipe library (mis. `pgx.Row`, `*http.Request`) tidak boleh bocor ke `domain` atau `application`.

---

## 5. Struktur Repo

Nama repo yang disarankan: `elvan-catalog-api`.

```
elvan-catalog-api/
├── cmd/
│   ├── api/                    # server HTTP (composition root)
│   │   └── main.go
│   ├── adminctl/               # CLI: buat/reset admin, dll
│   └── importer/               # CLI: impor data Firestore → Postgres (sekali pakai)
│
├── internal/
│   ├── domain/                 # entity + aturan bisnis murni
│   │   ├── product.go
│   │   ├── category.go
│   │   ├── brand.go
│   │   ├── admin.go
│   │   └── errors.go           # ErrNotFound, ErrConflict, ErrValidation, ErrUnauthorized, ErrForbidden
│   │
│   ├── application/            # use case + port
│   │   ├── port/               # interface yang dibutuhkan use case
│   │   │   ├── repository.go   # ProductRepository, CategoryRepository, BrandRepository, AdminRepository
│   │   │   ├── session.go      # SessionStore
│   │   │   ├── media.go        # ImageStorage
│   │   │   ├── security.go     # PasswordHasher, TokenGenerator
│   │   │   └── platform.go     # TxManager, Clock
│   │   ├── catalog/            # service produk
│   │   ├── taxonomy/           # service kategori & brand
│   │   ├── auth/               # login, logout, sesi
│   │   └── media/              # signature upload, hapus gambar
│   │
│   ├── adapter/
│   │   ├── in/
│   │   │   └── httpapi/        # router, handler, DTO, middleware, error mapper
│   │   └── out/
│   │       ├── postgres/       # repo impl, tx manager, mapper
│   │       │   └── sqlcgen/    # kode hasil generate sqlc (jangan diedit)
│   │       ├── imagekit/       # signer + batch delete
│   │       └── security/       # argon2id, generator token
│   │
│   └── platform/               # config, logger, clock, bootstrap helper
│
├── db/
│   ├── migrations/             # 0001_init.sql, ...
│   └── queries/                # *.sql untuk sqlc
│
├── api/
│   └── openapi.yaml            # kontrak API (sumber kebenaran)
│
├── deploy/
│   ├── Dockerfile
│   ├── compose.yaml            # api + postgres (+ caddy)
│   └── Caddyfile
│
├── docs/
│   └── ARCHITECTURE.md         # dokumen ini
├── sqlc.yaml
├── .golangci.yml               # termasuk aturan depguard
├── Makefile
├── go.mod
└── README.md
```

---

## 6. Domain

Domain mencerminkan tipe di FE (`src/core/types/`) agar kontrak tidak berubah.

```go
package domain

import "time"

type Product struct {
	ID          string
	Name        string
	Slug        string
	Price       int64          // satuan terkecil mata uang (lihat bagian 18)
	Description string
	Category    string         // slug kategori (bukan foreign key di domain)
	Brand       string         // slug brand; kosong jika tidak ada
	Images      []Image        // terurut; index 0 = thumbnail
	Rating      Rating
	IsActive    bool           // hanya flag tampil/sembunyi
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Image struct {
	Key    string // relative key, mis. "assets/images/products/television/x.jpg" (tanpa slash di depan)
	FileID string // id file di provider (untuk penghapusan); boleh kosong untuk gambar lama
}

type Rating struct {
	Rate  float64
	Count int
}
```

Prinsip yang dijaga dari FE:

- `id` adalah string opaque; server selalu membuat id (hint `id` dari klien diabaikan).
- `category` / `brand` di domain adalah **slug**. Di DB berupa foreign key; mapper Postgres yang menerjemahkan.
- Database **tidak** menyimpan URL absolut gambar, hanya relative key.
- Validasi invarian (nama tidak kosong, slug valid, harga ≥ 0, rating 0–5) dilakukan di domain, bukan di handler.

### Error domain

```go
var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")       // slug duplikat, kategori masih dipakai
	ErrValidation   = errors.New("validation")     // dibungkus dengan detail per field
	ErrUnauthorized = errors.New("unauthorized")   // belum login / kredensial salah
	ErrForbidden    = errors.New("forbidden")
)
```

---

## 7. Application: Port & Use Case

### Port (didefinisikan di `application/port`)

```go
type ProductRepository interface {
	List(ctx context.Context, q ProductQuery) (ProductPage, error)   // proyeksi ringan
	ListAll(ctx context.Context, includeInactive bool) ([]domain.CatalogProduct, error)
	GetByID(ctx context.Context, id string) (*domain.Product, error)
	Create(ctx context.Context, p domain.Product) (*domain.Product, error)
	Update(ctx context.Context, id string, patch domain.ProductPatch) (*domain.Product, error)
	Delete(ctx context.Context, id string) (removed []domain.Image, err error)
}

type ImageStorage interface { // ImageKit hari ini; bisa diganti S3/R2
	IssueUploadSignature(ctx context.Context) (UploadSignature, error)
	Delete(ctx context.Context, fileIDs []string) (DeleteResult, error)
}

type SessionStore interface {
	Create(ctx context.Context, s Session) error
	GetByTokenHash(ctx context.Context, hash []byte) (*Session, error)
	Delete(ctx context.Context, hash []byte) error
	DeleteExpired(ctx context.Context, before time.Time) (int64, error)
}

type TxManager interface { // unit of work
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

type Clock interface{ Now() time.Time }
```

`CategoryRepository`, `BrandRepository`, `AdminRepository`, `PasswordHasher`, dan `TokenGenerator` mengikuti pola yang sama.

### Use case (satu service per modul)

```go
package catalog

type Service struct {
	products port.ProductRepository
	images   port.ImageStorage
	tx       port.TxManager
	clock    port.Clock
	log      *slog.Logger
}

func New(products port.ProductRepository, images port.ImageStorage,
	tx port.TxManager, clock port.Clock, log *slog.Logger) *Service { /* ... */ }

func (s *Service) List(ctx context.Context, q ListQuery, actor Actor) (ProductPage, error)
func (s *Service) Get(ctx context.Context, id string) (*domain.Product, error)
func (s *Service) Create(ctx context.Context, in CreateProductInput) (*domain.Product, error)
func (s *Service) Update(ctx context.Context, id string, in UpdateProductInput) (*domain.Product, error)
func (s *Service) Delete(ctx context.Context, id string) error
```

| Modul | Use case |
|---|---|
| `catalog` | List (publik/admin), ListAll, Get, Create, Update, Delete |
| `taxonomy` | CRUD kategori, CRUD brand |
| `auth` | Login, Logout, Me (validasi sesi) |
| `media` | IssueUploadSignature, DeleteImages |

Aturan use case:

- `includeInactive` hanya dihormati jika `Actor` adalah admin terautentikasi; selain itu diabaikan dan hanya produk aktif yang dikembalikan.
- Operasi yang menyentuh lebih dari satu tabel (produk + gambar) berjalan dalam `TxManager.WithinTx`.
- Penghapusan file di provider dilakukan **setelah commit** (best-effort, lihat bagian 11).
- Use case tidak mengenal HTTP: input/output berupa struct Go biasa.

---

## 8. Adapter Inbound (HTTP)

### Prinsip

- Versi di path: `/api/v1`.
- JSON `camelCase`, waktu ISO-8601 (UTC), sama dengan tipe FE.
- Handler tipis: decode → panggil use case → encode. Tidak ada logika bisnis.
- Satu **error mapper** menerjemahkan error domain ke status HTTP.

### Endpoint

| Method | Path | Auth | Keterangan |
|---|---|---|---|
| GET | `/api/v1/catalog` | publik | Seluruh proyeksi katalog (`getAll()` di FE); `ETag` + `Cache-Control` |
| GET | `/api/v1/products` | publik | List dengan filter, sort, search, pagination |
| GET | `/api/v1/products/{id}` | publik | Produk lengkap (halaman detail/edit) |
| POST | `/api/v1/products` | admin | Buat produk |
| PATCH | `/api/v1/products/{id}` | admin | Ubah sebagian |
| DELETE | `/api/v1/products/{id}` | admin | Hapus produk + bersihkan gambar |
| GET | `/api/v1/categories` | publik | |
| GET | `/api/v1/categories/{id}` | publik | |
| POST / PATCH / DELETE | `/api/v1/categories[/{id}]` | admin | |
| GET | `/api/v1/brands` | publik | |
| GET | `/api/v1/brands/{id}` | publik | |
| POST / PATCH / DELETE | `/api/v1/brands[/{id}]` | admin | |
| POST | `/api/v1/auth/login` | publik (rate limit) | Buat sesi |
| POST | `/api/v1/auth/logout` | admin | Cabut sesi |
| GET | `/api/v1/auth/me` | admin | Sesi saat ini |
| GET | `/api/v1/media/signature` | admin | `{ token, expire, signature }` |
| DELETE | `/api/v1/media/files` | admin | `{ fileIds }` → `{ deleted, failed }` (kompatibilitas masa transisi) |
| GET | `/healthz` · `/readyz` | publik | Liveness · readiness (cek DB) |

### Query `GET /api/v1/products`

| Parameter | Contoh | Keterangan |
|---|---|---|
| `category` | `television` | Slug kategori |
| `brand` | `sharp` | Slug brand |
| `search` | `led 24` | Pencarian pada nama |
| `sort` | `default` · `price-asc` · `price-desc` · `rating-desc` | `default` = terbaru dulu |
| `limit` | `24` | Default 24, maksimum 100 |
| `cursor` | `eyJ...` | Token opaque dari respons sebelumnya |
| `includeInactive` | `true` | Hanya berlaku untuk admin terautentikasi |

Respons:

```json
{
  "products": [ { "id": "...", "name": "...", "slug": "...", "price": 1500000,
                  "category": "television", "brand": "sharp",
                  "thumbnail": "assets/images/products/television/x.jpg",
                  "rating": { "rate": 4.5, "count": 12 },
                  "isActive": true, "createdAt": "2026-10-01T03:00:00Z" } ],
  "hasMore": true,
  "cursor": "eyJ..."
}
```

### Pagination (keyset)

Cursor adalah base64url dari `{sort, lastValue, lastId}` dan **dianggap opaque oleh klien**. Urutan selalu diakhiri `id` sebagai pembeda agar stabil:

| `sort` | `ORDER BY` |
|---|---|
| `default` | `created_at DESC, id DESC` |
| `price-asc` | `price ASC, id ASC` |
| `price-desc` | `price DESC, id DESC` |
| `rating-desc` | `rating_rate DESC, rating_count DESC, id DESC` |

Cursor yang `sort`-nya tidak cocok dengan permintaan ditolak (`400 invalid_cursor`).

### Format error

```json
{ "error": { "code": "validation_failed", "message": "Data tidak valid",
             "details": [ { "field": "price", "message": "harus ≥ 0" } ] } }
```

| Error domain | HTTP | `code` |
|---|---|---|
| `ErrValidation` | 400 / 422 | `validation_failed` |
| `ErrUnauthorized` | 401 | `unauthorized` |
| `ErrForbidden` | 403 | `forbidden` |
| `ErrNotFound` | 404 | `not_found` |
| `ErrConflict` | 409 | `conflict` |
| lainnya | 500 | `internal` (detail hanya di log, tidak ke klien) |

### Middleware (urutan dari luar)

`recover` → `request id` → `access log` → `CORS` → `rate limit` → `CSRF/Origin check` → `auth (sesi)` → handler

---

## 9. Adapter Outbound (Postgres, ImageKit, Security)

### 9.1 Skema PostgreSQL

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE categories (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  name        text        NOT NULL,
  slug        text        NOT NULL UNIQUE,
  description text        NOT NULL DEFAULT '',
  legacy_id   text        UNIQUE,              -- id Firestore (hanya untuk migrasi)
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE brands (
  id          uuid PRIMARY KEY DEFAULT uuidv7(),
  name        text        NOT NULL,
  slug        text        NOT NULL UNIQUE,
  legacy_id   text        UNIQUE,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE products (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  name         text        NOT NULL,
  slug         text        NOT NULL UNIQUE,
  price        bigint      NOT NULL CHECK (price >= 0),
  description  text        NOT NULL DEFAULT '',
  category_id  uuid        NOT NULL REFERENCES categories(id) ON DELETE RESTRICT,
  brand_id     uuid                 REFERENCES brands(id)     ON DELETE RESTRICT,
  rating_rate  numeric(2,1) NOT NULL DEFAULT 0 CHECK (rating_rate BETWEEN 0 AND 5),
  rating_count integer     NOT NULL DEFAULT 0 CHECK (rating_count >= 0),
  is_active    boolean     NOT NULL DEFAULT true,
  legacy_id    text        UNIQUE,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE product_images (
  product_id uuid     NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  position   smallint NOT NULL CHECK (position >= 0),
  key        text     NOT NULL,        -- relative key, tanpa slash di depan
  file_id    text,                     -- NULL untuk gambar lama tanpa file id provider
  PRIMARY KEY (product_id, position)
);

CREATE TABLE admins (
  id            uuid PRIMARY KEY DEFAULT uuidv7(),
  email         text        NOT NULL UNIQUE,
  password_hash text        NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
  id           uuid PRIMARY KEY DEFAULT uuidv7(),
  admin_id     uuid        NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
  token_hash   bytea       NOT NULL UNIQUE,   -- SHA-256 dari token; token mentah tidak disimpan
  expires_at   timestamptz NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now()
);

-- Indeks untuk query katalog (hanya produk aktif untuk jalur publik)
CREATE INDEX products_newest_idx      ON products (is_active, created_at DESC, id DESC);
CREATE INDEX products_category_idx    ON products (category_id, is_active, created_at DESC, id DESC);
CREATE INDEX products_price_idx       ON products (is_active, price, id);
CREATE INDEX products_rating_idx      ON products (is_active, rating_rate DESC, rating_count DESC, id DESC);
CREATE INDEX products_name_trgm_idx   ON products USING gin (name gin_trgm_ops);
CREATE INDEX sessions_expires_idx     ON sessions (expires_at);
```

Catatan:

- `legacy_id` hanya untuk migrasi (idempoten dan penelusuran); bisa di-drop setelah migrasi stabil.
- `updated_at` diisi oleh query update (`SET updated_at = now()`), bukan trigger, supaya eksplisit.
- Produk tidak dipecah ke tabel `product_details`; proyeksi ringan cukup dengan memilih kolom (dan `thumbnail` = gambar `position = 0`).

### 9.2 Mapper & transaksi

- Mapper `sqlc row → domain` adalah satu-satunya tempat yang mengenal `category_id` / `brand_id`; ia me-resolve ke slug dengan `JOIN`.
- `TxManager` menyimpan `pgx.Tx` di `context`; repository memakai tx dari context jika ada, selain itu memakai pool. Dengan begitu use case tidak mengenal `pgx`.
- Pelanggaran constraint (`23505` unique, `23503` foreign key) diterjemahkan ke `ErrConflict` di adapter, bukan di use case.

### 9.3 ImageKit

Adapter `imagekit/` memindahkan logika dari Cloudflare Worker:

- `IssueUploadSignature`: `token = uuid`, `expire = now + 30 menit`, `signature = HMAC-SHA1(privateKey, token + expire)`.
- `Delete`: batch `POST /v1/files/batch/deleteByFileIds` (maks. 100 id per request), mengembalikan `deleted` dan `failed`.
- Private key hanya dibaca dari konfigurasi server, tidak pernah dikirim ke klien.

### 9.4 Security

- `argon2id` untuk password (parameter di konfigurasi).
- Token sesi: 32 byte acak (`crypto/rand`), dikirim ke klien; di DB hanya hash SHA-256.

---

## 10. Auth & Kesiapan Multi-Domain

### Model

- Hanya satu peran: **admin**. Endpoint baca katalog publik; semua operasi tulis dan media butuh sesi admin.
- Login: verifikasi email + password (argon2id), buat baris `sessions`, kembalikan token lewat **transport sesi**.
- Sesi berlaku 7 hari (dapat dikonfigurasi); `last_seen_at` diperbarui berkala. Job pembersihan menghapus sesi kedaluwarsa.
- Rate limit pada `/auth/login` (per IP dan per email) serta penundaan konstan untuk kredensial salah.
- Pembuatan admin pertama lewat CLI: `adminctl create --email ...` (tidak ada endpoint registrasi publik).

> **Perbaikan dari kondisi sekarang:** `/signature` dan `DELETE /files` di Worker hanya dijaga lewat header `Origin`, yang mudah dipalsukan di luar browser. Di BE baru keduanya wajib lewat sesi admin.

### Transport sesi bisa dipertukarkan

Use case `auth` tidak tahu cara token dibawa. Adapter HTTP menyediakan `SessionTransport`:

```go
type SessionTransport interface {
	Read(r *http.Request) (token string, ok bool)
	Write(w http.ResponseWriter, token string, expires time.Time)
	Clear(w http.ResponseWriter)
}
```

- `cookieTransport` (default): dibangun di fase awal.
- `bearerTransport` (`Authorization: Bearer`): **tidak dibangun dulu**, tetapi desain tidak menghalanginya; dipakai bila cookie lintas-domain tidak memungkinkan.

### Tiga skenario deployment

| Skenario | Cookie | CORS | Proteksi CSRF |
|---|---|---|---|
| **A. Satu domain** (`example.com`, API di `/api` lewat reverse proxy) — **rencana saat ini** | `HttpOnly; Secure; SameSite=Lax` | Tidak diperlukan | Cek `Origin` |
| **B. Subdomain beda** (`app.example.com` ↔ `api.example.com`) | `SameSite=Lax`, `Domain=.example.com` | Perlu, dengan credentials | Cek `Origin` + header wajib |
| **C. Domain beda total** | `SameSite=None; Secure` | Perlu, dengan credentials | Cek `Origin` + header wajib |

Catatan skenario C: browser modern semakin membatasi cookie lintas-situs, jadi jalur ini rawan. Jika bermasalah, pindah ke `bearerTransport` (access token berumur pendek + refresh), tanpa mengubah use case.

### Yang dikendalikan konfigurasi (bukan kode)

| Env | Fungsi |
|---|---|
| `CORS_ALLOWED_ORIGINS` | Daftar origin yang diizinkan (eksak, **tanpa wildcard**). Kosong = CORS mati (skenario A) |
| `COOKIE_DOMAIN` | Kosong (host-only) atau `.example.com` |
| `COOKIE_SAMESITE` | `lax` (default) atau `none` |
| `COOKIE_SECURE` | `true` di produksi (wajib jika `none`) |
| `SESSION_TTL` | Masa berlaku sesi |
| `AUTH_TRANSPORT` | `cookie` (default) atau `bearer` |

Dengan begitu pindah dari skenario A ke B/C hanya mengubah env dan konfigurasi proxy.

### Aturan CORS dan CSRF

- Preflight (`OPTIONS`) dijawab oleh middleware CORS dengan `Access-Control-Allow-Origin` = origin eksak (bukan `*`), `Access-Control-Allow-Credentials: true`, dan `Vary: Origin`.
- Untuk method non-aman (`POST/PATCH/PUT/DELETE`): `Origin` (atau `Sec-Fetch-Site`) harus cocok dengan daftar yang diizinkan, dan `Content-Type` harus `application/json` (memaksa preflight pada permintaan lintas-origin).
- FE yang beda origin harus memakai `fetch(..., { credentials: 'include' })`; base URL API dikonfigurasi lewat env FE (relatif `/api/v1` untuk satu domain).

---

## 11. Media / Gambar

Alur upload (tidak berubah bagi pengguna):

```
Admin (browser)                API (Go)                      ImageKit
      │  GET /media/signature     │                             │
      │ ────────────────────────▶ │  (cek sesi admin)           │
      │ ◀──── { token, expire, signature }                      │
      │                                                         │
      │  upload file + signature  ───────────────────────────▶  │
      │ ◀─────────────── { filePath, fileId } ───────────────── │
      │                                                         │
      │  POST/PATCH /products  { images: [{ key, fileId }] }    │
      │ ────────────────────────▶ │  simpan ke DB               │
```

Perubahan dibanding sekarang:

- **Penghapusan file dilakukan server.** Saat produk dihapus, atau gambar dilepas lewat `PATCH`, use case mengumpulkan `fileId` yang terlepas dan memanggil `ImageStorage.Delete` **setelah commit**. Hari ini FE memanggil delete terpisah sehingga bisa meninggalkan file yatim.
- Kegagalan hapus di provider tidak membatalkan operasi DB; dicatat di log dengan `fileId`-nya. Jika nanti perlu lebih ketat, tambahkan tabel `image_cleanup` kecil dengan worker retry.
- Gambar lama tanpa `fileId` (mis. aset lokal) disimpan dengan `file_id = NULL` dan dilewati saat pembersihan.
- Endpoint `DELETE /media/files` dipertahankan sementara untuk kompatibilitas FE, lalu dihapus setelah FE berhenti memanggilnya.

---

## 12. Konfigurasi

Semua dari environment, divalidasi saat startup (gagal cepat jika tidak valid). Tidak ada secret di repo.

| Env | Contoh | Keterangan |
|---|---|---|
| `APP_ENV` | `production` | `development` / `production` |
| `HTTP_ADDR` | `:8080` | |
| `DATABASE_URL` | `postgres://...` | |
| `DB_MAX_CONNS` | `10` | Ukuran pool |
| `IMAGEKIT_PRIVATE_KEY` | `private_...` | Secret |
| `IMAGEKIT_URL_ENDPOINT` | `https://ik.imagekit.io/...` | |
| `SESSION_TTL` | `168h` | |
| `AUTH_TRANSPORT` | `cookie` | |
| `COOKIE_DOMAIN` · `COOKIE_SAMESITE` · `COOKIE_SECURE` | | Lihat bagian 10 |
| `CORS_ALLOWED_ORIGINS` | `https://app.example.com` | Koma sebagai pemisah |
| `RATE_LIMIT_LOGIN` | `5/min` | |
| `LOG_LEVEL` | `info` | |

---

## 13. Cross-Cutting Concerns

- **Logging:** `slog` JSON, field `request_id`, `method`, `path`, `status`, `duration_ms`. Password, token, dan private key tidak pernah di-log.
- **Error handling:** use case mengembalikan error domain (dibungkus dengan `fmt.Errorf("...: %w", err)`); hanya error mapper HTTP yang memutuskan status dan isi respons.
- **Context:** `context.Context` dibawa dari handler sampai query; timeout per request dan per query.
- **Graceful shutdown:** tangkap `SIGTERM`, hentikan menerima koneksi baru, tunggu request berjalan, tutup pool.
- **Health:** `/healthz` (proses hidup), `/readyz` (DB dapat di-ping).
- **Caching:** `GET /catalog` dan list publik memakai `ETag` + `Cache-Control: public, max-age=60`; invalidasi mengikuti `updated_at` terbaru.
- **Keamanan:** batas ukuran body, header keamanan dasar, query selalu berparameter (sqlc), validasi `limit` dan `sort` (whitelist).
- **Observabilitas lanjutan** (metrics/tracing): ditunda; titik sambungnya ada di middleware.

---

## 14. Strategi Testing

| Level | Sasaran | Cara |
|---|---|---|
| Unit | `domain`, use case | Fake in-memory untuk port; tanpa DB dan tanpa jaringan |
| Integrasi | Adapter Postgres | `testcontainers-go` dengan Postgres 18; jalankan migration sungguhan |
| HTTP | Handler + middleware | `httptest`; termasuk skenario CORS/CSRF dan auth |
| Kontrak | API vs `openapi.yaml` | Validasi respons terhadap skema OpenAPI |
| Arsitektur | Aturan dependensi | `depguard` di `golangci-lint`; gagal di CI jika `domain` meng-import adapter |

Pipeline CI minimum: `go vet` → `golangci-lint` → `go test ./...` (termasuk integrasi) → validasi OpenAPI → cek `sqlc generate` tidak menghasilkan diff.

---

## 15. Deployment

Topologi satu VPS:

```
Internet ──▶ Caddy (TLS otomatis)
               ├── /          → file statis FE
               └── /api/*     → Go API (:8080) ──▶ PostgreSQL 18
```

- **Container:** Dockerfile multi-stage (build → image minimal non-root). `compose.yaml` berisi `api`, `postgres:18`, dan `caddy`.
- **Migration:** dijalankan sebagai langkah terpisah sebelum API naik (`goose up`), bukan otomatis saat startup.
- **Backup:** `pg_dump` terjadwal ke lokasi di luar VPS, dan uji restore berkala.
- **Rilis:** build image bertag versi → `compose pull && up -d`; rollback dengan tag sebelumnya.
- Jika nanti FE pindah ke domain lain, cukup ubah `Caddyfile` dan env di bagian 10.

---

## 16. Migrasi dari Firebase

1. **Ekspor Firestore** (skrip yang sudah ada di repo FE: `scripts/migrate-firestore.cjs`) ke JSON.
2. **`cmd/importer`** membaca JSON dan memasukkan ke Postgres dalam satu transaksi:
   - urutan: kategori → brand → produk (+ gambar);
   - `legacy_id` diisi dari id Firestore sehingga impor **idempoten** (boleh dijalankan ulang);
   - `category`/`brand` slug di dokumen produk di-resolve ke foreign key;
   - `images[]` dan `imageFileIds[]` dipasangkan berdasarkan index ke `product_images`;
   - dokumen `catalog/snapshot` **dilewati** (hanya proyeksi).
3. **Admin:** buat ulang lewat `adminctl`, lalu reset password (hash Firebase tidak dipakai).
4. **Verifikasi:** bandingkan jumlah baris, sampel produk, dan hasil `GET /catalog` vs Firestore.
5. **Cutover:** FE memakai adapter API (switch di composition root FE), Worker dimatikan setelah `media` API terbukti, Firebase dibiarkan read-only sementara sebagai cadangan.

---

## 17. Roadmap Implementasi

| Fase | Isi | Hasil |
|---|---|---|
| 0 | Skeleton repo, `go.mod`, config, Makefile, CI, `depguard` | Repo siap; lint lulus |
| 1 | Migration awal, `domain`, `port`, sqlc | Skema dan kontrak internal terkunci |
| 2 | Jalur baca publik: kategori, brand, produk, list + cursor, `/catalog` | FE bisa membaca dari API |
| 3 | `ApiXxxRepository` di FE + switch di composition root | FE berjalan di atas API (mode baca) |
| 4 | Auth admin + endpoint tulis (produk, kategori, brand) | Admin panel berfungsi penuh |
| 5 | Modul media (signature, penghapusan server-side) | Worker Cloudflare bisa dimatikan |
| 6 | Importer, verifikasi data, cutover | Produksi di VPS |
| 7 | Hardening: rate limit, backup terjadwal, metrics | Siap operasional jangka panjang |

---

## 18. Hal yang Perlu Diverifikasi

Belum dipastikan dari kode FE; cek sebelum menutup desain skema dan mapper.

| # | Pertanyaan | Dampak |
|---|---|---|
| V1 | **Tipe `price`**: apakah selalu bilangan bulat Rupiah, atau ada desimal? Skema di atas mengasumsikan `bigint` (tanpa desimal). | Tipe kolom, JSON, importer |
| V2 | **Rute FE untuk detail produk** memakai `id` atau `slug`? Jika `id`, id Firestore lama di URL yang sudah beredar akan putus; `legacy_id` bisa dipakai untuk redirect. | Endpoint `GET /products/{id}`; kebutuhan lookup by slug/legacy id |
| V3 | **Siapa yang membuat `slug`**: FE (`utils/hash`) atau server? Usul: server membuat dari `name` jika kosong, dan menolak duplikat dengan `409`. | Validasi dan kontrak payload |
| V4 | **Kebijakan hapus kategori/brand yang masih dipakai**: skema memakai `RESTRICT` (ditolak `409`). Pastikan sesuai perilaku admin yang diinginkan. | UX admin |
| V5 | **Jumlah data**: estimasi jumlah produk menentukan perlu tidaknya cache in-process di samping `ETag`. | Performa `/catalog` |
| V6 | **Nama repo dan module path** Go (mis. `github.com/<owner>/elvan-catalog-api`). | `go.mod`, import path |

---

## Lampiran: Pemetaan ke Kontrak FE

| Kontrak di FE | Padanan di BE |
|---|---|
| `ProductRepository.getAll()` | `GET /api/v1/catalog` |
| `ProductRepository.list(options)` | `GET /api/v1/products` |
| `ProductRepository.getById/create/update/delete` | `GET/POST/PATCH/DELETE /api/v1/products[/{id}]` |
| `Category/BrandRepository.*` | `/api/v1/categories`, `/api/v1/brands` |
| `AuthService.login/logout/onSessionChange` | `POST /auth/login`, `POST /auth/logout`, `GET /auth/me` (session change = cek `me` saat load) |
| `ImageUploadService.uploadProductImage` | `GET /api/v1/media/signature` (upload tetap langsung ke ImageKit) |
| `ImageUploadService.deleteProductImages` | Server-side saat hapus/ubah produk; `DELETE /api/v1/media/files` selama transisi |
| `ProductPayload.id` (hint) | Diabaikan; selalu pakai id dari respons |
| `ProductListOptions.cursor` (opaque) | Cursor keyset base64url |

