#!/usr/bin/env sh
# backup.sh — backup PostgreSQL terjadwal (ARCHITECTURE §15, Fase 7).
#
# pg_dump -Fc (format custom, kompresi) ke luar VPS. Jalankan lewat cron:
#
#   # /etc/cron.d/elvan-backup — tiap hari 02:15
#   15 2 * * * root /opt/elvan/deploy/backup.sh >> /var/log/elvan-backup.log 2>&1
#
#   # uji restore berkala (mis. mingguan) ke database scratch:
#   0 3 * * 0 root /opt/elvan/deploy/backup.sh --restore-test
#
# Env (wajib untuk backup biasa):
#   DATABASE_URL   DSN PostgreSQL
#   BACKUP_DIR     direktori tujuan (default: /var/backups/elvan)
#   BACKUP_KEEP    jumlah backup yang disimpan (default: 14)
#
# Env (khusus --restore-test):
#   RESTORE_URL    DSN database scratch tempat restore diuji

set -eu

BACKUP_DIR="${BACKUP_DIR:-/var/backups/elvan}"
BACKUP_KEEP="${BACKUP_KEEP:-14}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"

# container-mode: dipanggil di dalam compose (pg_dump dari image postgres).
# Contoh: docker compose exec -T postgres sh -c 'DATABASE_URL=... backup.sh'
restore_test() {
	: "${RESTORE_URL:?RESTORE_URL wajib diisi (database scratch)}"
	dump_file="$1"

	echo "[backup] uji restore: $dump_file -> $RESTORE_URL"
	# Bersihkan tabel lama supaya restore deterministik.
	psql "$RESTORE_URL" -v ON_ERROR_STOP=1 -q \
		-c 'DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;'
	pg_restore --no-owner --dbname="$RESTORE_URL" "$dump_file"
	psql "$RESTORE_URL" -v ON_ERROR_STOP=1 -q \
		-c 'SELECT count(*) AS products FROM products;'
	echo "[backup] uji restore OK"
}

if [ "${1:-}" = "--restore-test" ]; then
	# Paling baru dulu; kalau belum ada, gagal cepat.
	dump_file="$(ls -1t "$BACKUP_DIR"/elvan-*.dump 2>/dev/null | head -n 1 || true)"
	if [ -z "$dump_file" ]; then
		echo "[backup] tidak ada dump di $BACKUP_DIR" >&2
		exit 1
	fi
	restore_test "$dump_file"
	exit 0
fi

: "${DATABASE_URL:?DATABASE_URL wajib diisi}"
mkdir -p "$BACKUP_DIR"

out="$BACKUP_DIR/elvan-$STAMP.dump"
echo "[backup] pg_dump -> $out"
pg_dump --format=custom --no-owner --dbname="$DATABASE_URL" --file="$out"

# Verifikasi dump terbaca (corrupt file ketahuan di sini, bukan saat restore).
pg_restore --list "$out" > /dev/null
echo "[backup] dump valid ($(wc -c < "$out") bytes)"

# Rotasi: sisakan BACKUP_KEEP terbaru.
ls -1t "$BACKUP_DIR"/elvan-*.dump 2>/dev/null | tail -n +$((BACKUP_KEEP + 1)) |
	while IFS= read -r old; do
		echo "[backup] hapus lama: $old"
		rm -f "$old"
	done

echo "[backup] selesai"
