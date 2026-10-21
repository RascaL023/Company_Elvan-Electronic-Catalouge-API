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
| `.github/workflows/ci.yml` | verify + `publish`: image `elvan-api`/`elvan-api-migrate` ke GHCR (hanya `main`) |
| `.github/workflows/deploy.yml` | deploy API bergerbang (manual + persetujuan environment `production`) |

## 1. Persiapan

```bash
cd deploy
cp .env.example .env        # isi POSTGRES_PASSWORD, SITE_ADDRESS (domain asli), SITE_EMAIL
```

VPS hanya butuh Docker + Compose. Tidak ada Node/pnpm/Go, dan tidak ada
`./site` yang disalin manual: situs FE adalah **image** `elvan-web` (dibangun
dari repo FE, lihat §7) yang ditunjuk `WEB_IMAGE` di `.env`; API adalah image
`elvan-api`/`elvan-api-migrate` dari GHCR yang ditunjuk `API_IMAGE`/
`MIGRATE_IMAGE`.

Direktori `VPS_DEPLOY_DIR` (boleh di path mana pun) minimal berisi
`compose.yaml`, `Caddyfile`, `.env`, dan `backup.sh` — `backup.sh` wajib ada
karena deploy API menolak jalan tanpa backup sebelum migrasi.

Kalau package GHCR-nya **private**, login sekali di VPS sebagai user deploy:

```bash
docker login ghcr.io     # username: <owner>, password: PAT dengan read:packages
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

## 2. Image API (lokal vs VPS)

Di VPS **tidak ada build**. Image `elvan-api` dan `elvan-api-migrate` dibuat
oleh `job publish` di `.github/workflows/ci.yml` (hanya setelah `verify` hijau
dan hanya dari `main`), lalu ditarik dari GHCR. Build lokal hanya untuk
pengembangan:

```bash
docker compose build        # menandai image sesuai API_IMAGE/MIGRATE_IMAGE di .env
# atau: docker build -f deploy/Dockerfile --target api -t elvan-api:dev ..
```

Di VPS, perintah compose tidak pernah build: `up`/`pull` memakai `--no-build`,
dan `run` (mis. `migrate`) memang **tidak punya** flag itu — image sudah
ditarik lebih dulu, jadi compose memakai image lokal. Build context (`..`) boleh
saja tidak ada karena image selalu datang dari registry.

## 3. Migrasi (langkah TERPISAH — bukan saat startup API)

```bash
docker compose up -d postgres
docker compose pull migrate          # image goose+migrasi dari GHCR
docker compose run --rm migrate      # goose up (target `migrate`; tanpa --no-build)
```

Migrasi selalu dijalankan eksplisit sebelum API naik; binari `api` tidak
memuat mekanisme migrasi apa pun (ARCHITECTURE §15). Di VPS, langkah ini
dijalankan otomatis oleh `deploy.yml` **setelah** backup — dan kalau migrasi
gagal, `api` tidak disentuh.

## 4. Naikkan API + situs

```bash
docker compose up -d --no-build api web caddy
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

Dua repo, dua pipeline, dua tingkat gerbang — karena hanya BE yang memegang
data.

| | Repo FE | Repo BE |
|---|---|---|
| Tiap push `main` | build + push `elvan-web`, **langsung deploy** | test + push `elvan-api`/`-migrate` (produksi tidak berubah) |
| Menyentuh produksi | otomatis | `workflow_dispatch` + **Approve** |
| Ditulis ke `.env` | `WEB_IMAGE` | `API_IMAGE`, `MIGRATE_IMAGE` |
| Langkah wajib | health check `web` | backup → migrate → `api` → `/healthz` |
| Rollback | tukar tag `WEB_IMAGE` | tukar tag `API_IMAGE` (migrasi tidak ikut batal) |

Aktifkan gerbang sekali di repo BE: **Settings → Environments → production**,
tambah *Required reviewers* dan *Deployment branches: main*. Tanpa itu
workflow tetap jalan, tetapi tanpa persetujuan.

### 9.1 CD jalur uji (`deploy-test`)

Push ke branch `deploy-test` menjalankan CI **dan** `deploy.yml` sekaligus,
tanpa gerbang: job `publish` menaruh image `sha-<commit>`, lalu workflow deploy
menariknya di VPS, memastikan `postgres` hidup, backup, `migrate`, lalu
`up -d api web caddy`, dan terakhir menembak `$SITE_URL/healthz`.

Karena keduanya berjalan paralel, langkah `docker pull` di VPS menunggu image
muncul (maks ±10 menit). Kalau `verify` gagal, image tidak pernah dibuat dan
deploy berakhir merah di langkah itu — bukan deploy setengah jalan.

Sekali pasang: `.env` di `VPS_DEPLOY_DIR` sudah ada, paket GHCR `elvan-api` dan
`elvan-api-migrate` bisa ditarik VPS (publik, atau `docker login ghcr.io` sekali
sebagai user deploy), dan variabel `VPS_HOST`, `VPS_USER`, `VPS_DEPLOY_DIR`,
`SITE_URL` sudah diisi. Environment `deploy-test` dibuat otomatis saat deploy
pertama; jangan dipasangi required reviewer kalau ingin tetap otomatis.

**Dua jalur yang berbeda.** Deploy memakai tailnet (SSH, `tag:ci` → `tag:vps`),
tetapi pengunjung memakai jalur publik (DNS → VPS:80/443, atau Cloudflare
Tunnel). Keduanya independen: SSH boleh hijau sementara HTTP belum terjangkau.
Karena itu langkah deploy membuktikan hasilnya **dari VPS sendiri** (Caddy
listen di port `SITE_ADDRESS`, `/healthz` dijawab lewat Caddy, `api`/`web`/`caddy`
running). Kalau bagian itu lolos, deploy-nya berhasil dan kegagalan `curl` di
runner adalah urusan jalur jaringan. Lihat §9.3 untuk memilih jalur masuk.

ACL tailnet hanya perlu mengizinkan port `SITE_URL` kalau `SITE_URL` memang
alamat tailnet (mis. `http://100.85.60.60`). Kalau `SITE_URL` domain publik,
cukup port 22:

```jsonc
// SITE_URL = domain publik (disarankan)
{ "action": "accept", "src": ["tag:ci"], "dst": ["tag:vps:22"] }

// SITE_URL = alamat tailnet (uji internal saja)
{ "action": "accept", "src": ["tag:ci"], "dst": ["tag:vps:22,80,443"] }
```

Kalau ACL hanya mengizinkan 22 sementara `SITE_URL` alamat tailnet, gejalanya
menyesatkan: SSH sukses, `pull`/`migrate`/`up` sukses, lalu langkah terakhir
gagal `curl: (7) couldn't connect`. `tailscale ping` tetap hijau karena ping
disco tidak melewati filter ACL — yang perlu diuji adalah TCP. Langkah
`Diagnosa jalur ke SITE_URL` mencetak `tailscale status`, `tailscale ping`, dan
hasil koneksi TCP sebelum health check, supaya beda "VPS sehat" vs "runner
tidak boleh lewat" langsung terlihat.

Uji dari node tailnet lain (mis. laptop Anda) hanya sah kalau `tailscaled` di
sana hidup — `100.64.0.0/10` bukan alamat publik, ia rute VPN:

```bash
sudo tailscaled &                            # tanpa ini, 100.x tidak punya rute
curl -fsS http://<ip-tailnet-vps>/healthz    # -> ok
```

Urutan rilis: **BE dulu, FE belakangan**. Kontrak API dijaga *additive*
(tambah endpoint/field, jangan hapus/rename dalam satu rilis) supaya FE yang
deploy otomatis tidak menabrak API lama. Untuk perubahan yang memutus
kompatibilitas, deploy BE bergerbang lebih dulu, baru merge PR FE.

Deploy FE dan BE menulis `.env` yang sama, jadi keduanya memakai `flock` di
`/tmp/elvan-deploy.lock` pada VPS: deploy kedua antre (maks 10 menit), bukan
saling menimpa.

Deploy manual (kalau perlu, tanpa GitHub):

```bash
docker compose pull api migrate
BACKUP_DIR=./backups sh ./backup.sh
docker compose run --rm migrate        # `run` tidak punya flag --no-build
docker compose up -d --no-build api
```

Rollback = tulis tag `sha-` sebelumnya ke `API_IMAGE`/`MIGRATE_IMAGE`, lalu
`pull` + `up -d --no-build api`. Migrasi yang sudah jalan tidak dibatalkan oleh
rollback; pulihkan dari dump di `BACKUP_DIR` bila perlu (§8).

### 9.2 Deploy merah: `curl: (7) couldn't connect` di langkah terakhir

Langkah terakhir (`Uji /healthz publik`) menembak `$SITE_URL/healthz` **dari
runner**, jadi `exit 7` hanya berarti *tidak ada yang menerima TCP di port
itu*. Itu gejala, bukan sebab — sebabnya selalu di VPS, dan hampir selalu
berupa rangkaian ini:

| Gejala di VPS (`docker compose ps -a`) | Sebab | Perbaikan |
|---|---|---|
| hanya `postgres` yang running **dan langkah `Deploy di VPS` tetap hijau** | skrip deploy terpotong: skrip dikirim lewat stdin (`ssh … bash -s <<'REMOTE'`), lalu `docker compose exec`/`run` meneruskan stdin dan menelan sisa skrip; bash membaca EOF dan keluar 0 | jangan panggil `docker compose` langsung di skrip itu — pakai pembungkus `dc()` (`docker compose "$@" </dev/null`) seperti di `deploy.yml`; sentinel `== deploy selesai` membuat kasus ini merah |
| hanya `postgres` yang running, langkah deploy merah | deploy berhenti setelah menyiapkan postgres: `backup.sh`, `migrate`, atau `pull web caddy` gagal | lihat log langkah itu; `migrate` gagal biasanya karena `MIGRATE_IMAGE` tidak bisa ditarik |
| `caddy` restart terus | `SITE_EMAIL` kosong (Caddy menolak baris `email`), `Caddyfile` invalid, atau `web:80` tidak resolve karena container `web` tidak ada | isi `SITE_EMAIL`/`WEB_IMAGE`, lalu `docker compose up -d --no-build api web caddy` |
| `api` restart terus | config menolak start (mis. `COOKIE_SECURE=false` dengan `APP_ENV=production`, `DATABASE_URL` salah) | `docker compose logs api` menyebut variabel yang salah |
| semua running, langkah deploy lolos, runner tetap `exit 7` | jalur jaringan dari runner: ACL tailnet menutup port itu, DNS belum menunjuk VPS, atau firewall VPS menutupnya | lihat §9.3; cek juga `curl -fsS http://127.0.0.1/healthz` di VPS |

Alur deploy (perhatikan apa yang sudah hidup saat tiap langkah gagal):

```
pull api → pull migrate → up postgres → BACKUP → MIGRATE → pull web caddy
   → set .env → up api web caddy → probe → cek running → barulah curl dari runner
```

Penyebab paling menyesatkan dari gejala ini adalah skrip yang **berhenti tanpa
error**: skrip dikirim ke VPS lewat stdin ssh, dan `docker compose exec` (serta
`run`) meneruskan stdin ke container — `-T` hanya mematikan TTY. Perintah
pertama yang memakainya, di sini `docker compose exec -T postgres pg_isready`,
menelan sisa skrip; bash lalu membaca EOF dan keluar dengan status 0. Langkah
`Deploy di VPS` tetap hijau padahal `backup.sh`, `migrate`, dan `up api web
caddy` tidak pernah jalan — dan port 80 memang tidak pernah dibuka. Karena itu
semua compose di skrip itu lewat `dc()`, dan runner memeriksa sentinel
`== deploy selesai` di akhir output.

Sebelum ini, kegagalan di tengah meninggalkan VPS setengah jadi: postgres hidup,
tidak ada yang membuka 80, dan workflow hanya melaporkan "couldn't connect" di
langkah jauh di hilir. `deploy.yml` sekarang (a) mem-validasi `POSTGRES_PASSWORD`,
`WEB_IMAGE`, `SITE_ADDRESS`, `SITE_EMAIL` **sebelum** container apa pun disentuh,
(b) mencetak `docker compose ps -a` + log terakhir saat langkah gagal, dan
(c) mengecek port 80 di host tepat setelah `up`, sehingga pesan errornya menunjuk
komponen yang benar.

Catatan `SITE_URL`: kalau isinya IP tailnet (`100.64.0.0/10`, mis.
`http://100.85.60.60`), situs itu hanya terjangkau dari tailnet — runner ikut
tailnet di langkah pertama, tetapi pengunjung biasa tidak. Untuk situs publik,
pakai domain yang A/AAAA-nya menunjuk VPS, lalu samakan `SITE_ADDRESS` di `.env`
dengan domain tersebut (`SITE_ADDRESS=":80"` hanya untuk uji HTTP polos).

### 9.3 Jalur masuk: tailnet untuk deploy, domain publik untuk pengunjung

Ada dua jalur yang sering tertukar, dan sebagian besar kebingungan
`curl: (7)` berasal dari situ:

| | Dipakai oleh | Alamat | Penentu akses |
|---|---|---|---|
| Kontrol | runner CI (`ssh`, `docker compose`) | `VPS_HOST` = `100.x.y.z` / MagicDNS | ACL tailnet (`tag:ci` → `tag:vps:22`) |
| Data | pengunjung situs | `SITE_URL` = domain publik | DNS + firewall VPS, atau Cloudflare Tunnel |

`100.64.0.0/10` adalah rentang CGNAT milik Tailscale, bukan alamat publik.
Alamat itu hanya punya rute kalau `tailscaled` hidup di perangkat yang
memakainya — itu perilaku normal VPN, bukan kerusakan:

- **Laptop Anda**: `tailscaled` mati → tidak ada rute `100.64.0.0/10`, curl
gagal. Hidupkan → `ok`. Di NixOS, `services.tailscale.enable = true` membuat
ini otomatis saat boot (jangan jalankan `tailscaled` manual sebagai daemon
tetap).
- **Runner GitHub**: `tailscale/github-action` menyalakan `tailscaled` di dalam
runner, jadi rutenya ada. Yang menentukan port mana yang boleh lewat adalah
ACL, bukan ping.

Jadi jawaban "apakah GitHub perlu konek ke Tailscale?" — **hanya kalau
`SITE_URL` berisi alamat tailnet**. Untuk produksi, jawabannya seharusnya tidak:
`SITE_URL` adalah domain publik, dan uji terakhir menguji jalur yang sama
dengan pengunjung.

Pilihan jalur masuk:

| Opsi | IP publik | Buka 80/443 | TLS | Catatan |
|---|---|---|---|---|
| **A. DNS A/AAAA → VPS**, Caddy ACME | ya, tetap | ya | Caddy otomatis | paling sedikit komponen; `SITE_ADDRESS=<domain>`, `SITE_EMAIL` terisi |
| **B. Cloudflare Tunnel** (`cloudflared` di VPS, koneksi keluar) | tidak perlu | tidak perlu | di edge Cloudflare | VPS di balik CGNAT/IP dinamis; origin cukup HTTP lokal |
| C. tailnet saja (kondisi sekarang) | tidak perlu | tidak perlu | Caddy | hanya untuk uji internal — pengunjung tidak bisa membuka situs |

Rekomendasi: **A** kalau VPS punya IP publik tetap, **B** kalau tidak (atau
kalau tidak ingin membuka port apa pun). Keduanya berakhir sama dari sisi
workflow: `SITE_URL=https://katalog.example.com`, dan langkah uji terakhir tidak
lagi menyentuh tailnet sama sekali.

Untuk opsi B, `cloudflared` mengarah ke Caddy di `127.0.0.1:80`, jadi
pengalihan HTTP→HTTPS Caddy harus dimatikan (TLS sudah di edge): set
`SITE_ADDRESS=":80"` dan tambahkan `auto_https off` di blok global Caddyfile,
lalu daftarkan `ingress` tunnel ke `http://localhost:80`. Tanpa itu, Caddy
membalas 308 ke `https://<domain>` dan Cloudflare meneruskannya kembali ke
origin — loop.

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
