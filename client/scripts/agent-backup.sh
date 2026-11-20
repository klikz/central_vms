#!/usr/bin/env bash
# C4.5 (§10.1): agent holatini zaxiralash — agent.key + secrets.bin + state.json.
# Ishlatish: sudo scripts/agent-backup.sh [chiqish_katalogi]
set -euo pipefail

LIB_DIR="${VMSAGENT_LIB:-/var/lib/vmsagent}"
ETC_DIR="${VMSAGENT_ETC:-/etc/vmsagent}"
OUT_DIR="${1:-/var/backups/vmsagent}"
TS="$(date +%Y%m%d-%H%M%S)"
ARCHIVE="$OUT_DIR/vmsagent-state-$TS.tar.gz"

command -v tar >/dev/null || { echo "tar topilmadi" >&2; exit 1; }
mkdir -p "$OUT_DIR"

# Xavfsizlik: arxivda parollar bo'lgani uchun faqat root o'qiy oladigan qilib
# saqlaymiz va agentni qisqa vaqt to'xtatamiz (yozilayotgan fayl buzilmasin).
stop_needed=0
if systemctl is-active --quiet vmsagent 2>/dev/null; then
  systemctl stop vmsagent
  stop_needed=1
fi
trap 'if [ "$stop_needed" = 1 ]; then systemctl start vmsagent || true; fi' EXIT

files=()
for f in "$LIB_DIR/secrets.bin" "$LIB_DIR/state.json" "$ETC_DIR/agent.key"; do
  [ -e "$f" ] && files+=("$f")
done
if [ "${#files[@]}" -eq 0 ]; then
  echo "XATO: zaxiralashga birorta fayl topilmadi ($LIB_DIR, $ETC_DIR)" >&2
  exit 1
fi

umask 077
tar -czf "$ARCHIVE" "${files[@]}"
chmod 0600 "$ARCHIVE"

echo "Zaxira tayyor: $ARCHIVE"
tar -tzf "$ARCHIVE" | sed 's/^/  /'
