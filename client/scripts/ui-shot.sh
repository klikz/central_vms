#!/usr/bin/env bash
# UI skrinshotlari + DOM tekshiruvi (§17). Har bir ekran uchun:
#  1) Chrome headless skrinshot → build/shots/<ekran>.png
#  2) dump-dom orqali JS haqiqatan ishlaganini tasdiqlaymiz
#     (JS ishlamasa barcha ekranlar yashirin qoladi — oq sahifa)
set -euo pipefail

BIN="build/vmsagent"
CFG="config/client.local.yaml"
BASE="http://127.0.0.1:8082"
OUT="build/shots"
RUN="build/run"

screens=(cams discovery status settings log)
headings=(Cameras Discovery "Status / Self-test" Settings "Event log")

command -v google-chrome >/dev/null 2>&1 || { echo "google-chrome yo'q — skrinshot olinmadi"; exit 0; }

OWN=0
if curl -fsS -m 2 "$BASE/healthz" >/dev/null 2>&1; then
  echo "ui-shot: ishlayotgan instansdan olinadi"
else
  [ -x "$BIN" ] || { echo "ui-shot: $BIN yo'q — avval 'make build'"; exit 1; }
  mkdir -p "$RUN"
  "$BIN" -config "$CFG" >/dev/null 2>&1 &
  echo $! > "$RUN/shot.pid"
  OWN=1
  sleep 2
fi

cleanup() {
  if [ "$OWN" = "1" ]; then
    kill "$(cat "$RUN/shot.pid")" 2>/dev/null || true
    rm -f "$RUN/shot.pid"
  fi
  return 0
}
trap cleanup EXIT

mkdir -p "$OUT"
fail=0
for i in "${!screens[@]}"; do
  s="${screens[$i]}"; h="${headings[$i]}"
  google-chrome --headless=new --disable-gpu --no-sandbox --hide-scrollbars \
    --window-size=1440,900 --virtual-time-budget=4000 \
    --screenshot="$OUT/$s.png" "$BASE/#/$s" >/dev/null 2>&1
  dom=$(google-chrome --headless=new --disable-gpu --no-sandbox \
    --virtual-time-budget=4000 --dump-dom "$BASE/#/$s" 2>/dev/null || true)
  if ! printf '%s' "$dom" | grep -qF "<h1>$h</h1>"; then
    echo "ui-shot: XATO — $s ekranida '$h' ko'rinmadi (JS ishlamagan bo'lishi mumkin)"
    fail=1
  fi
done

if [ "$fail" = "1" ]; then
  echo "ui-shot: DOM tekshiruvi yiqildi (skrinshotlar $OUT da)"
  exit 1
fi
echo "ui-shot: OK — skrinshotlar $OUT/ da, 5 ekran DOM tekshiruvidan o'tdi"
