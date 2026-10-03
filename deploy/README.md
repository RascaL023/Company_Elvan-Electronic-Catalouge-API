# Deploy — Elvan Catalog API

Topologi satu VPS (ARCHITECTURE §15):

```
Internet ──▶ Caddy (TLS otomatis)
               ├── /          → file statis FE  (./site)
               ├── /api/*     → Go API (:8080) ──▶ PostgreSQL 18
               └── /healthz   → Go API
```

| Berkas | Peran |
|---|---|
| `Dockerfile` | multi-stage: target `api` (distroless non-root) dan `migrate` (goose) |
| `compose.yaml` | `postgres:18`, `migrate`, `api`, `caddy` |
| `Caddyfile` | rute `/` (FE statis) dan `/api/*` (proxy ke API) |
| `.env.example` | template konfigurasi → salin ke `.env` |
| `backup.sh` | `pg_dump` terjadwal + rotasi + uji restore |

## 1. Persiapan

```bash
cd deploy
cp .env.example .env        # isi POSTGRES_PASSWORD, SITE_ADDRESS, SITE_EMAIL, COOKIE_DOMAIN
mkdir -p site               # hasil `npm run build` repo FE disalin ke sini
```

## 2. Build image

```bash
docker compose build        # atau: docker build -f deploy/Dockerfile -t elvan-api:dev ..
```

## 3. Migrasi (langkah TERPISAH — bukan saat startup API)

```bash
docker compose up -d postgres
docker compose run --rm migrate      # goose up (target `migrate`)
```

Migrasi selalu dijalankan eksplisit sebelum API naik; binari `api` tidak
memuat mekanisme migrasi apa pun (ARCHITECTURE §15).

## 4. Naikkan API + situs

```bash
docker compose up -d api caddy
curl -fsS http://localhost/healthz    # -> ok
```

## 5. Impor data Firestore

Ekspor JSON dari repo FE (`scripts/migrate-firestore.cjs`), lalu jalankan
binari `importer` yang ikut di dalam image (tanpa toolchain Go di VPS):

```bash
docker compose cp export.json api:/tmp/export.json

docker compose run --rm --no-deps --entrypoint /usr/local/bin/importer api \
  import --input /tmp/export.json --dry-run    # validasi saja
docker compose run --rm --no-deps --entrypoint /usr/local/bin/importer api \
  import --input /tmp/export.json              # tulis (1 transaksi)
docker compose run --rm --no-deps --entrypoint /usr/local/bin/importer api \
  verify                                       # hitung baris per tabel
```

Impor **idempoten** (matched via `legacy_id`); jalankan ulang aman.

## 6. Admin

```bash
docker compose run --rm --no-deps --entrypoint /usr/local/bin/adminctl api \
  create --email admin@example.com             # password dari stdin
```

> Hash password Firebase tidak dipakai; buat/reset admin lewat `adminctl`.

## 7. Cutover

1. Pastikan `importer verify` cocok dengan jumlah dokumen Firestore.
2. Bandingkan `GET /api/v1/catalog` dengan snapshot Firestore (sampel produk).
3. Di repo FE: aktifkan cabang `api` di composition root (Fase 3), set
   `VITE_API_BASE_URL=/api/v1`.
4. Matikan Cloudflare Worker **setelah** jalur media (signature + delete)
   terbukti jalan.
5. Firebase dikembalikan **read-only** sementara sebagai cadangan; hapus
   fallback setelah N minggu tanpa hit `legacy_id` (issue #6).

## 8. Backup & uji restore (Fase 7)

`pg_dump` terjadwal ke lokasi **di luar VPS** (mis. object storage / server
lain), plus uji restore berkala:

```cron
# /etc/cron.d/elvan-backup
15 2 * * * root  /opt/elvan/deploy/backup.sh >> /var/log/elvan-backup.log 2>&1
0  3 * * 0 root  RESTORE_URL=postgres://.../elvan_restore_test /opt/elvan/deploy/backup.sh --restore-test >> /var/log/elvan-restore.log 2>&1
```

`backup.sh` memvalidasi dump (`pg_restore --list`) setiap kali dan mengrotasi
menyisakan `BACKUP_KEEP` (default 14) berkas terbaru. Salin `BACKUP_DIR` ke
luar VPS dengan rsync/rclone — backup lokal bukan backup.

## 9. Rilis & rollback

```bash
docker compose build
docker compose up -d api
# rollback ke tag sebelumnya:
docker compose pull && docker compose up -d api   # bila memakai registry
```

Tag versi disarankan (`elvan-api:1.4.0`); rollback = kembali ke tag sebelumnya.

## 10. Observabilitas

- Akses log JSON ke stdout (`docker compose logs -f api`).
- Titik sambung metrics/tracing: `Deps.Observer` di
  `internal/adapter/in/httpapi` (Fase 7) — pasang implementasi tanpa menyentuh
  handler.
- Batas waktu: `REQUEST_TIMEOUT` (per request, via context) dan
  `DB_STATEMENT_TIMEOUT` (per query, `statement_timeout`); lewat batas →
  `504 {"code":"timeout"}`.
