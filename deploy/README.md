# Deploy — Elvan Catalog API

Topologi satu VPS (ARCHITECTURE §15):

```
Internet ──▶ Caddy (TLS otomatis)
               ├── /          → web (:80, nginx + dist FE)
               ├── /api/*     → Go API (:8080) ──▶ PostgreSQL 18
               └── /healthz   → Go API
```

| Berkas | Peran |
|---|---|
| `Dockerfile` | multi-stage: target `api` (distroless non-root) dan `migrate` (goose) |
| `compose.yaml` | `postgres:18`, `migrate`, `api`, `web`, `caddy` |
| `Caddyfile` | rute `/` (proxy ke `web`) dan `/api/*` (proxy ke API) |
| `.env.example` | template konfigurasi → salin ke `.env` |
| `backup.sh` | `pg_dump` terjadwal (lewat container `postgres`) + rotasi + uji restore |

## 1. Persiapan

```bash
cd deploy
cp .env.example .env        # isi POSTGRES_PASSWORD, SITE_ADDRESS (domain asli), SITE_EMAIL
```

VPS hanya butuh Docker + Compose. Tidak ada Node/pnpm/Go, dan tidak ada
`./site` yang disalin manual: situs FE adalah **image** `elvan-web` (dibangun
dari repo FE, lihat §7) yang ditunjuk `WEB_IMAGE` di `.env`.

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
docker compose up -d api web caddy
curl -fsS https://<domain>/healthz    # -> ok  (mode uji :80: http://localhost/healthz)
```

`web` tidak mempublikasikan port ke host (`expose: 80` saja); satu-satunya
container yang membuka 80/443 adalah `caddy`. Kalau `web` belum punya image,
compose gagal dengan `pull access denied` — bangun dulu (§7.1) atau tarik
dari registry (§7.2).

## 5. Impor data Firestore

Ekspor JSON dari repo FE dulu (menulis satu berkas sesuai kontrak
ARCHITECTURE §16.1), lalu jalankan binari `importer` yang ikut di dalam image
(tanpa toolchain Go di VPS):

```bash
# di repo FE (butuh scripts/service-account.json project LAMA):
pnpm export:firestore -- --normalize   # preflight gagal (exit 1) = importer akan menolak
```

> Pakai `export-firestore.cjs` (dipanggil lewat `pnpm export:firestore`).
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

## 7. Situs FE (image terpisah)

FE dikirim sebagai image sendiri (`elvan-web`), bukan salinan `dist/` ke VPS.
Isi image: hasil `pnpm build` + nginx. TLS, header keamanan, gzip, dan rute
`/api/*` tetap milik Caddy, jadi hanya `caddy` yang membuka port host.

### 7.1 Bangun image FE (uji lokal / sebelum ada registry)

```bash
cd <repo FE>
docker build -f deploy/Dockerfile \
  --build-arg VITE_IMAGEKIT_URL_ENDPOINT=https://ik.imagekit.io/<imagekit-id> \
  --build-arg VITE_IMAGEKIT_PUBLIC_KEY=public_<imagekit-id> \
  -t elvan-web:dev .
```

`VITE_API_BASE_URL` sudah default `/api/v1` di Dockerfile (relatif: satu origin
lewat Caddy), jadi tidak perlu dioper. Dua arg ImageKit **wajib**: `.env` repo FE
tidak ikut ke build context, jadi tanpa endpoint gambar provider jatuh ke `local`
dan semua gambar produk di-resolve ke `/assets/...` (404). Dockerfile menolak
build kalau `VITE_STORAGE_PROVIDER=imagekit` sementara endpoint kosong.

Di `deploy/.env`, `WEB_IMAGE=elvan-web:dev` (default), lalu
`docker compose up -d web`.

### 7.2 Dari registry (alur VPS)

```bash
cd <repo FE>
docker build -f deploy/Dockerfile \
  --build-arg VITE_IMAGEKIT_URL_ENDPOINT=https://ik.imagekit.io/<imagekit-id> \
  --build-arg VITE_IMAGEKIT_PUBLIC_KEY=public_<imagekit-id> \
  -t ghcr.io/<owner>/elvan-web:1.4.0 .
docker push ghcr.io/<owner>/elvan-web:1.4.0
```

Di VPS, `deploy/.env` berisi `WEB_IMAGE=ghcr.io/<owner>/elvan-web:1.4.0`, lalu:

```bash
docker compose pull web
docker compose up -d web
```

Tag adalah mekanisme rollback: kembali ke tag sebelumnya, `pull`, `up -d`.
Tidak ada `rsync` dan tidak ada build di VPS.

- `VITE_*` dibekukan saat build (Vite meng-inline nilainya). Mengganti backend
  berarti build image baru, **bukan** mengubah env container.
- `VITE_API_BASE_URL=/api/v1` cukup: `src/config/api.ts` otomatis memilih
  adapter API untuk data, auth, dan media (Fase 3 sudah selesai).
- Jangan set `VITE_AUTH_BACKEND` / `VITE_MEDIA_BACKEND` ke `firebase`, dan
  jangan biarkan `VITE_API_BASE_URL` kosong: build akan kembali ke Firebase
  (dan Worker untuk upload).
- Fallback SPA (`/product/<id>`) ditangani nginx; Caddy menambahkan
  Cache-Control, CSP, dan gzip di atasnya.

### 7.3 Urutan cutover

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
docker compose up -d api web
# rollback ke tag sebelumnya:
docker compose pull && docker compose up -d api web   # bila memakai registry
```

Tag versi disarankan (`elvan-api:1.4.0`, `elvan-web:1.4.0`); rollback = kembali
ke tag sebelumnya. `api` dan `web` dirilis terpisah karena berasal dari dua
repo — `WEB_IMAGE` bisa dinaikkan tanpa menyentuh API, dan sebaliknya.

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
