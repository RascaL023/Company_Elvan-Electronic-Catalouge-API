#!/usr/bin/env sh
# backup.sh — backup PostgreSQL terjadwal (ARCHITECTURE §15, Fase 7).
#
# Dijalankan di HOST (mis. lewat cron). **Tidak perlu klien PostgreSQL di
# host**: semua perintah pg_dump/pg_restore/psql dijalankan DI DALAM container
# `postgres` lewat `docker compose exec -T postgres`, memakai POSTGRES_USER /
# POSTGRES_DB yang sudah di-set compose untuk container itu. Ini penting karena
# service `postgres` tidak mempublikasikan port ke host — DSN dari host memang
# tidak terjangkau.
#
# Pemakaian:
#   ./backup.sh                 # dump + validasi + rotasi
#   ./backup.sh --restore-test  # restore dump terbaru ke database scratch
#
# Env:
#   BACKUP_DIR   direktori tujuan di host (default: /var/backups/elvan)
#   BACKUP_KEEP  jumlah dump yang disimpan (default: 14)
#   RESTORE_DB   database scratch untuk --restore-test (default: elvan_restore_test)
#   DEPLOY_DIR   direktori berisi compose.yaml (default: direktori skrip ini)
#
# Cron (user yang boleh memakai docker):
#   # /etc/cron.d/elvan-backup
#   15 2 * * * root  /opt/elvan/deploy/backup.sh >> /var/log/elvan-backup.log 2>&1
#   0  3 * * 0 root  /opt/elvan/deploy/backup.sh --restore-test >> /var/log/elvan-restore.log 2>&1
#
# Dump ditulis ke BACKUP_DIR di host; salin ke luar VPS dengan rsync/rclone
# (backup lokal bukan backup).

set -eu

BACKUP_DIR="${BACKUP_DIR:-/var/backups/elvan}"
BACKUP_KEEP="${BACKUP_KEEP:-14}"
RESTORE_DB="${RESTORE_DB:-elvan_restore_test}"
DEPLOY_DIR="${DEPLOY_DIR:-$(cd "$(dirname "$0")" && pwd)}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"

# pg meneruskan perintah ke container `postgres`. stdin/stdout tetap tersambung
# ke host, jadi dump cukup di-stream dan tidak perlu ditulis di dalam container.
pg() {
	docker compose --project-directory "$DEPLOY_DIR" -f "$DEPLOY_DIR/compose.yaml" \
		exec -T "$@"
}

# dump: tulis dump baru, validasi bisa dibaca, lalu rotasi.
dump() {
	out="$BACKUP_DIR/elvan-$STAMP.dump"
	mkdir -p "$BACKUP_DIR"

	echo "[backup] pg_dump -> $out"
	pg postgres sh -c 'pg_dump --format=custom --no-owner -U "$POSTGRES_USER" -d "$POSTGRES_DB"' > "$out"

	# Dump rusak ketahuan di sini, bukan saat restore dibutuhkan.
	# pg_restore membaca stdin bila tidak diberi nama berkas ("-" bukan stdin).
	pg postgres sh -c 'pg_restore --list' < "$out" > /dev/null
	echo "[backup] dump valid ($(wc -c < "$out") bytes)"

	# Rotasi: sisakan BACKUP_KEEP dump terbaru.
	ls -1t "$BACKUP_DIR"/elvan-*.dump 2>/dev/null | tail -n +$((BACKUP_KEEP + 1)) |
		while IFS= read -r old; do
			echo "[backup] hapus lama: $old"
			rm -f "$old"
		done

	echo "[backup] selesai"
}

# restore_test: pulihkan dump ke database scratch di server yang sama, lalu
# pastikan tabelnya benar-benar bisa dibaca.
restore_test() {
	dump_file="$1"

	echo "[backup] uji restore: $dump_file -> database scratch $RESTORE_DB"

	# Database scratch dibuat bila belum ada (idempoten).
	if [ "$(pg postgres sh -c "psql -U \"\$POSTGRES_USER\" -d postgres -tAc \"SELECT 1 FROM pg_database WHERE datname='$RESTORE_DB'\"")" != "1" ]; then
		pg postgres sh -c "createdb -U \"\$POSTGRES_USER\" '$RESTORE_DB'"
		echo "[backup] database $RESTORE_DB dibuat"
	fi

	# Schema dibersihkan dulu supaya hasil restore deterministik.
	pg postgres sh -c "psql -v ON_ERROR_STOP=1 -q -U \"\$POSTGRES_USER\" -d '$RESTORE_DB' -c 'DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;'"
	pg postgres sh -c "pg_restore --no-owner -U \"\$POSTGRES_USER\" -d '$RESTORE_DB'" < "$dump_file"

	rows="$(pg postgres sh -c "psql -v ON_ERROR_STOP=1 -q -U \"\$POSTGRES_USER\" -d '$RESTORE_DB' -tAc 'SELECT count(*) FROM products'")"
	echo "[backup] uji restore OK (products: $rows)"
}

case "${1:-}" in
"")
	dump
	;;
--restore-test)
	# Nama database disisipkan ke perintah di dalam container, jadi batasi
	# karakternya.
	case "$RESTORE_DB" in
	*[!A-Za-z0-9_]*)
		echo "[backup] RESTORE_DB hanya boleh huruf, angka, dan underscore: $RESTORE_DB" >&2
		exit 1
		;;
	esac
	dump_file="$(ls -1t "$BACKUP_DIR"/elvan-*.dump 2>/dev/null | head -n 1 || true)"
	if [ -z "$dump_file" ]; then
		echo "[backup] tidak ada dump di $BACKUP_DIR" >&2
		exit 1
	fi
	restore_test "$dump_file"
	;;
*)
	echo "pemakaian: $0 [--restore-test]" >&2
	exit 1
	;;
esac
