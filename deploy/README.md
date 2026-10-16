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
| `backup.sh` | `pg_dump` terjadwal (lewat container `postgres`) + rotasi + uji restore |

## 1. Persiapan

```bash
cd deploy
cp .env.example .env        # isi POSTGRES_PASSWORD, SITE_ADDRESS (domain asli), SITE_EMAIL
mkdir -p site               # build FE produksi disalin ke sini (lihat §7)
```

Tiga env yang paling gampang salah:

- **`SITE_ADDRESS` harus domain asli** (mis. `katalog.example.com`), bukan `:443`.
  Caddy hanya mengaktifkan TLS otomatis kalau ada hostname di config; alamat
  `:443` polos tidak akan mendapat sertifikat. DNS A/AAAA harus menunjuk ke VPS
  dan port 80 + 443 terbuka (port 80 dipakai ACME challenge dan redirect).
- **`COOKIE_DOMAIN` kosongkan untuk satu domain** (host-only). Kalau diisi
  domain yang tidak cocok dengan host asli, browser menolak cookie-nya: login
  tampak berhasil (`200`) tetapi `/auth/me` langsung `401`.
- **Uji lokal tanpa TLS**: pakai `SITE_ADDRESS=":80"` dan akses lewat
  `http://localhost` — Chrome/Firefox mengizinkan cookie `Secure` di localhost
  (Safari tidak; untuk Safari ubah `APP_ENV` di `compose.yaml` menjadi
  `development` **dan** `COOKIE_SECURE=false`).

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
curl -fsS https://<domain>/healthz    # -> ok  (mode uji :80: http://localhost/healthz)
```

## 5. Impor data Firestore

Ekspor JSON dari repo FE dulu (menulis satu berkas sesuai kontrak
ARCHITECTURE §16.1), lalu jalankan binari `importer` yang ikut di dalam image
(tanpa toolchain Go di VPS):

```bash
# di repo FE (butuh scripts/service-account.json project LAMA):
npm run export:firestore -- --normalize   # preflight gagal (exit 1) = importer akan menolak
```

> Pakai `export-firestore.cjs` (dipanggil lewat `npm run export:firestore`).
> `scripts/migrate-firestore.cjs` adalah alat lain: menyalin data antar-project
> Firebase, **bukan** sumber impor ini.

`docker compose run` membuat **container baru**, jadi berkas ekspor harus
ikut di-mount ke container itu. (`docker compose cp` ke container `api`
yang sedang melayani tidak akan terlihat oleh container baru tersebut — service
`api` juga tidak punya volume.)

```bash
docker compose run --rm --no-deps \
  -v "$PWD/export.json:/data/export.json:ro" \
  --entrypoint /usr/local/bin/importer api \
  import --input /data/export.json --dry-run    # validasi saja

docker compose run --rm --no-deps \
  -v "$PWD/export.json:/data/export.json:ro" \
  --entrypoint /usr/local/bin/importer api \
  import --input /data/export.json              # tulis (1 transaksi)

docker compose run --rm --no-deps --entrypoint /usr/local/bin/importer api \
  verify                                        # hitung baris per tabel
```

Impor **idempoten** (matched via `legacy_id`); jalankan ulang aman.

## 6. Admin

```bash
docker compose run --rm --no-deps --entrypoint /usr/local/bin/adminctl api \
  create --email admin@example.com             # password dari stdin
```

> Hash password Firebase tidak dipakai; buat/reset admin lewat `adminctl`.

## 7. Cutover

### 7.1 Build FE untuk cutover

```bash
cd <repo FE>
VITE_API_BASE_URL=/api/v1 npm run build            # relatif: satu origin lewat Caddy
rsync -a --delete dist/ vps:/opt/elvan/deploy/site/
```

- `VITE_API_BASE_URL=/api/v1` cukup: `src/config/api.ts` otomatis memilih
  adapter API untuk data, auth, dan media (Fase 3 sudah selesai — tidak ada
  switch manual di composition root).
- Jangan set `VITE_AUTH_BACKEND` / `VITE_MEDIA_BACKEND` ke `firebase`, dan
  jangan tinggalkan `VITE_API_BASE_URL` kosong: build tanpa itu akan kembali
  memakai Firebase (dan Worker untuk upload).
- `./site` di-mount read-only ke Caddy, jadi berkas yang disalin langsung
  terbaca tanpa restart.

### 7.2 Urutan cutover

1. Pastikan `importer verify` cocok dengan jumlah dokumen Firestore.
2. Bandingkan `GET /api/v1/catalog` dengan snapshot Firestore (sampel produk).
3. Terbitkan situs baru (§7.1) dan uji login admin + upload/hapus gambar dari
   UI admin di domain produksi.
4. Matikan Cloudflare Worker **setelah** jalur media (signature + delete)
   terbukti jalan.
5. Firebase dikembalikan **read-only** sementara sebagai cadangan; hapus
   fallback setelah N minggu tanpa hit `legacy_id` (issue #6).

## 8. Backup & uji restore (Fase 7)

`pg_dump` terjadwal ke lokasi **di luar VPS** (mis. object storage / server
lain), plus uji restore berkala.

`backup.sh` dijalankan di **host** dan tidak butuh klien PostgreSQL di sana:
service `postgres` tidak mempublikasikan port, jadi semua perintah
(`pg_dump`, `pg_restore`, `psql`) dijalankan di dalam container `postgres`
lewat `docker compose exec -T`.

```bash
# container postgres harus sedang jalan
BACKUP_DIR=/var/backups/elvan ./backup.sh                # dump + validasi + rotasi
BACKUP_DIR=/var/backups/elvan ./backup.sh --restore-test # restore ke database scratch
```

Alurnya: `pg_dump --format=custom` (di-stream ke berkas di host) → validasi
`pg_restore --list` (dump rusak ketahuan saat itu, bukan saat dibutuhkan) →
rotasi menyisakan `BACKUP_KEEP` (default 14) dump terbaru. `--restore-test`
memulihkan dump terbaru ke database scratch (`RESTORE_DB`, default
`elvan_restore_test`) di server yang sama dan memastikan tabel `products` bisa
dibaca.

```cron
# /etc/cron.d/elvan-backup
15 2 * * * root  BACKUP_DIR=/var/backups/elvan /opt/elvan/deploy/backup.sh >> /var/log/elvan-backup.log 2>&1
0  3 * * 0 root  BACKUP_DIR=/var/backups/elvan /opt/elvan/deploy/backup.sh --restore-test >> /var/log/elvan-restore.log 2>&1
```

Salin `BACKUP_DIR` ke luar VPS dengan rsync/rclone — backup lokal bukan backup.

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
- **Sumber IP klien**: rate limit `/auth/login` memakai entri pertama
  `X-Forwarded-For` (`internal/adapter/in/httpapi/guard.go`), dan Caddy secara
  default **mengabaikan** nilai XFF kiriman klien (anti-spoofing). Jangan
  menambahkan `trusted_proxies` selama Caddy adalah server terdepan — opsi itu
  justru membuat Caddy mempercayai header dari klien. Kalau nanti ada CDN di
  depan Caddy, konfigurasikan dengan hati-hati (Caddy memperingatkan risiko
  spoofing XFF, mis. lewat Cloudflare).
