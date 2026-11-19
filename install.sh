#!/usr/bin/env bash
# install.sh — vmsagent'ni o'rnatadi (root talab qiladi).
#
#   sudo ./install.sh                 # /opt/vmsagent + systemd + enable --now
#   DESTDIR=/tmp/root-staged ./install.sh --no-enable   # sinov (root'siz, staging)
#
# Tarkib (root REJA §4.1): vmsagent, config/client.yaml, systemd/vmsagent.service,
# install.sh, VERSION.
set -euo pipefail

SRC="$(cd "$(dirname "$0")" && pwd)"
DESTDIR="${DESTDIR:-}"
BIN_DIR="$DESTDIR/opt/vmsagent"
ETC_DIR="$DESTDIR/etc/vmsagent"
VAR_DIR="$DESTDIR/var/lib/vmsagent"
RUN_SERVICE=1

for arg in "$@"; do
  case "$arg" in
    --no-enable) RUN_SERVICE=0 ;;
    *) echo "noma'lum parametr: $arg"; exit 2 ;;
  esac
done

log() { printf 'install: %s\n' "$*"; }

[[ -x "$SRC/vmsagent" ]] || { echo "xato: $SRC/vmsagent topilmadi (avval 'make build')"; exit 1; }
[[ -f "$SRC/config/client.yaml" ]] || { echo "xato: $SRC/config/client.yaml yo'q"; exit 1; }
[[ -f "$SRC/systemd/vmsagent.service" ]] || { echo "xato: unit fayl yo'q"; exit 1; }

if [[ -z "$DESTDIR" && "$(id -u)" != "0" ]]; then
  echo "xato: root bo'lish kerak (yoki DESTDIR=... bilan staging sinovi qiling)"; exit 1
fi

# --- tashqi bog'liqlik: ffmpeg (H.265 zaxira transcode / remux) ---
if ! command -v ffmpeg >/dev/null 2>&1; then
  log "OG'OHLANTIRISH: ffmpeg topilmadi — H.265 transcode va remux ishlamaydi"
else
  if ffmpeg -hide_banner -muxers 2>/dev/null | grep -qE '^\s*E\s+whip\b'; then
    log "ffmpeg OK: $(ffmpeg -hide_banner -version | head -1)"
  else
    log "OG'OHLANTIRISH: ffmpeg versiyasi eski — '-f whip' yo'q (>= 7.1 kerak)"
  fi
fi

# --- foydalanuvchi (staging rejimida tashqi) ---
if [[ -z "$DESTDIR" ]] && ! id vmsagent >/dev/null 2>&1; then
  log "vmsagent foydalanuvchisi yaratiladi"
  useradd --system --home-dir /var/lib/vmsagent --shell /usr/sbin/nologin vmsagent
fi

install -d -m 0755 "$BIN_DIR" "$ETC_DIR" "$VAR_DIR"
install -m 0755 "$SRC/vmsagent" "$BIN_DIR/vmsagent"
[[ -f "$SRC/VERSION" ]] && install -m 0644 "$SRC/VERSION" "$BIN_DIR/VERSION"

# Konfiguratsiya: mavjud bo'lsa BOSHLAMAYDI (parol/sozlamalar yo'qolmasin).
if [[ -e "$ETC_DIR/client.yaml" ]]; then
  install -m 0644 "$SRC/config/client.yaml" "$ETC_DIR/client.yaml.new"
  log "mavjud konfiguratsiya saqlandi; yangisi: $ETC_DIR/client.yaml.new"
else
  install -m 0640 "$SRC/config/client.yaml" "$ETC_DIR/client.yaml"
fi

# Maxfiy kalit: faqat bir marta yaratiladi (0600). Yo'qolsa parollar tiklanmaydi —
# zaxira shart (deploy/agent-state-backup.md).
if [[ ! -e "$ETC_DIR/agent.key" ]]; then
  umask 077
  head -c 32 /dev/urandom > "$ETC_DIR/agent.key"
  chmod 0600 "$ETC_DIR/agent.key"
  log "yangi kalit yaratildi: $ETC_DIR/agent.key"
else
  log "kalit allaqachon mavjud: $ETC_DIR/agent.key"
fi

if [[ -z "$DESTDIR" ]]; then
  chown -R vmsagent:vmsagent "$VAR_DIR"
  chown root:vmsagent "$ETC_DIR/client.yaml" "$ETC_DIR/agent.key" 2>/dev/null || true
  chmod 0640 "$ETC_DIR/client.yaml" || true
  chmod 0600 "$ETC_DIR/agent.key" || true

  install -m 0644 "$SRC/systemd/vmsagent.service" /etc/systemd/system/vmsagent.service
  systemctl daemon-reload
  if [[ "$RUN_SERVICE" == "1" ]]; then
    systemctl enable --now vmsagent
    log "vmsagent ishga tushirildi: systemctl status vmsagent"
  else
    systemctl enable vmsagent || true
    log "unit yoqildi (enable), xizmat ko'tarilmadi (--no-enable)"
  fi
else
  install -m 0644 "$SRC/systemd/vmsagent.service" "$DESTDIR/etc/systemd/system/vmsagent.service" 2>/dev/null || {
    install -d -m 0755 "$DESTDIR/etc/systemd/system"
    install -m 0644 "$SRC/systemd/vmsagent.service" "$DESTDIR/etc/systemd/system/vmsagent.service"
  }
  log "staging rejimi: systemd amallari o'tkazib yuborildi (DESTDIR=$DESTDIR)"
fi

log "tayyor: $BIN_DIR/vmsagent"
