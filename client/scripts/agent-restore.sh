#!/usr/bin/env bash
# C4.5 (§10.1): zaxiradan tiklash. Ishlatish:
#   sudo scripts/agent-restore.sh vmsagent-state-YYYYMMDD-HHMMSS.tar.gz
set -euo pipefail

ARCHIVE="${1:-}"
if [ -z "$ARCHIVE" ] || [ ! -f "$ARCHIVE" ]; then
  echo "ishlatish: $0 <arxiv.tar.gz>" >&2
  exit 1
fi

LIB_DIR="${VMSAGENT_LIB:-/var/lib/vmsagent}"
ETC_DIR="${VMSAGENT_ETC:-/etc/vmsagent}"

# Arxiv tarkibi (yo'llar / ga nisbatan saqlangan).
tar -tzf "$ARCHIVE" | sed "s/^/  /"

if systemctl is-active --quiet vmsagent 2>/dev/null; then
  systemctl stop vmsagent
  RESTART=1
else
  RESTART=0
fi

# Eskisini belgilab olamiz (tiklash xato bo'lsa orqaga qaytaramiz).
TS="$(date +%Y%m%d-%H%M%S)"
for f in "$LIB_DIR/secrets.bin" "$LIB_DIR/state.json" "$ETC_DIR/agent.key"; do
  [ -e "$f" ] && cp -a "$f" "$f.pre-restore-$TS"
done

tar -xzf "$ARCHIVE" -C /

# Egalik/ruhsatni tiklaymiz (agent foydalanuvchisi o'qishi kerak).
if id vmsagent >/dev/null 2>&1; then
  chown vmsagent:vmsagent "$LIB_DIR/secrets.bin" "$LIB_DIR/state.json" 2>/dev/null || true
  chown root:vmsagent "$ETC_DIR/agent.key" 2>/dev/null || true
fi
chmod 0600 "$LIB_DIR/secrets.bin" "$LIB_DIR/state.json" "$ETC_DIR/agent.key" 2>/dev/null || true

if [ "$RESTART" = 1 ]; then
  systemctl start vmsagent
fi
echo "Tiklandi: $ARCHIVE (eski fayllar *.pre-restore-$TS)"
