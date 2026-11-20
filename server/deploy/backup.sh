#!/usr/bin/env bash
# S4.4 zaxira: SQLite bazasini ISSIQ nusxa (VACUUM INTO) — server
# to'xtamaydi. Kunlik cron uchun mo'ljallangan (install-notes.md §6).
#
#   backup.sh <db_path> <backup_dir> [keep_count=14]
#
# Chiqish: <backup_dir>/vmsd-YYYYmmdd-HHMMSS.db (fayl nomi vaqtli),
# eski nusxalar soni bo'yicha tozalanadi.
set -euo pipefail

DB="${1:?db_path kerak (masalan /opt/vmsd/var/vmsd.db)}"
DIR="${2:?backup_dir kerak (masalan /var/backups/vmsd)}"
KEEP="${3:-14}"

# Ikki rejim:
#  1) VMSD_BIN + VMSD_CONFIG berilsa — vmsd'ning `-backup` rejimi
#     (sqlite3 CLI talab qilinmaydi, butunlik tekshiruvi ichida);
#  2) aks holda sqlite3 CLI ishlatiladi.
VMSD_BIN="${VMSD_BIN:-/opt/vmsd/vmsd}"
VMSD_CONFIG="${VMSD_CONFIG:-/opt/vmsd/config/config.yaml}"
USE_VMSD=no
if [ -x "$VMSD_BIN" ] && [ -f "$VMSD_CONFIG" ]; then
	USE_VMSD=yes
else
	command -v sqlite3 >/dev/null || { echo "XATO: sqlite3 o'rnatilmagan yoki VMSD_BIN/VMSD_CONFIG ko'rsatilmagan" >&2; exit 1; }
	[ -f "$DB" ] || { echo "XATO: baza topilmadi: $DB" >&2; exit 1; }
fi

mkdir -p "$DIR"
OUT="$DIR/vmsd-$(date -u +%Y%m%d-%H%M%S).db"

# VACUUM INTO — bir jihatdan izchil nusxa (WAL rejimida ham xavfsiz).
if [ "$USE_VMSD" = yes ]; then
	"$VMSD_BIN" -config "$VMSD_CONFIG" -backup "$OUT"
else
	sqlite3 "$DB" "VACUUM INTO '$OUT';"
	# Butunlik tekshiruvi — buzilgan nusxa saqlanmaydi.
	if [ "$(sqlite3 "$OUT" 'PRAGMA integrity_check;')" != "ok" ]; then
		echo "XATO: nusxa buzilgan: $OUT" >&2
		rm -f "$OUT"
		exit 1
	fi
fi
chmod 600 "$OUT"

# Eski nusxalarni soni bo'yicha o'chirish (eng yangi KEEP ta saqlanadi).
mapfile -t OLD < <(ls -1t "$DIR"/vmsd-*.db 2>/dev/null | tail -n +$((KEEP + 1)))
for f in "${OLD[@]:-}"; do
	[ -n "$f" ] && rm -f "$DIR/$f"
done

echo "Zaxira tayyor: $OUT ($(du -h "$OUT" | cut -f1)); saqlanadi: $KEEP"
