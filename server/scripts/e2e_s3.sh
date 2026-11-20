#!/usr/bin/env bash
# S3 qabul mezoni: haqiqiy Chrome (headless) + real MediaMTX + testagent.
# UI: login → kamera ro'yxati → grid'da ochish (WHEP kadr) → sevimlilar →
# klaviatura grid → Page Visibility'da oqim to'xtash → sessiyalar to'zalanadi.
# Ishlatish: make e2e-s3
set -uo pipefail
cd "$(dirname "$0")/.."
PASS=0; FAIL=0
ok()  { echo "  PASS: $1"; PASS=$((PASS+1)); }
bad() { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }
say() { echo "== $1"; }
MTXPID=""; VMSDPID=""; AGENTPID=""; CHROMEPID=""
NODE_BIN="$(command -v node || echo "$HOME/.local/node/bin/node")"
cleanup() {
  for p in "$CHROMEPID" "$AGENTPID" "$VMSDPID" "$MTXPID"; do
    [ -n "$p" ] && kill -INT "$p" 2>/dev/null
  done
  sleep 1
  for p in "$CHROMEPID" "$AGENTPID" "$VMSDPID" "$MTXPID"; do
    [ -n "$p" ] && kill -9 "$p" 2>/dev/null
  done
  rm -rf /tmp/vms_chrome
  rm -f /tmp/vms_e2e.db /tmp/vms_e2e.db-wal /tmp/vms_e2e.db-shm /tmp/vms_e2e_ui.yaml
}
trap cleanup EXIT

# Eski test ma'lumotlari (o'ldirilgan ishga o'xshash holatdan qolgan
# mijozlar) UI testini chalkashtirmasin — DB yangidan boshlanadi.
rm -f /tmp/vms_e2e.db /tmp/vms_e2e.db-wal /tmp/vms_e2e.db-shm

say "0. portlar bo'shmi"
BUSY=""
for p in 8554 8888 8889 8189 9997 8081 9222; do
  if ss -ltn | awk '{print $4}' | grep -Eq ":$p$"; then BUSY="$BUSY $p"; fi
done
if [ -n "$BUSY" ]; then bad "band portlar:$BUSY"; exit 1; fi
ok "portlar bo'sh"

say "1. qurish (server + UI)"
make build >/dev/null 2>&1 || { bad "make build"; exit 1; }
make ui-build > /dev/null 2>&1 || { bad "make ui-build"; exit 1; }
[ -x mediamtx/mediamtx ] || { bad "mediamtx/mediamtx yo'q — make media-get"; exit 1; }
[ -f build/web/index.html ] || { bad "build/web/index.html yo'q"; exit 1; }
[ -x "$NODE_BIN" ] || { bad "node topilmadi (UI testi uchun kerak)"; exit 1; }
ok "binarlar + UI tayyor"

mkdir -p var/log

say "2. MediaMTX va vmsd (UI statikasi bilan)"
sed 's#^web_dir:.*#web_dir: build/web#' config/e2e.yaml > /tmp/vms_e2e_ui.yaml
./mediamtx/mediamtx mediamtx/mediamtx.yml > var/log/e2e-mediamtx.log 2>&1 &
MTXPID=$!
./build/vmsd -config /tmp/vms_e2e_ui.yaml > var/log/e2e-vmsd.log 2>&1 &
VMSDPID=$!
for _ in $(seq 1 40); do
  curl -sf http://127.0.0.1:8081/healthz >/dev/null 2>&1 && break
  sleep 0.25
done
curl -sf http://127.0.0.1:8081/healthz >/dev/null 2>&1 || { bad "vmsd ko'tarilmadi"; tail -8 var/log/e2e-vmsd.log; exit 1; }
ok "vmsd /healthz 200"
if curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8081/monitor | grep -q 200; then
  ok "UI statikasi serve qilinapti (SPA fallback)"
else
  bad "UI statikasi yo'q"; exit 1
fi

say "3. admin → enroll → testagent (-publish)"
./build/vmsd -config /tmp/vms_e2e_ui.yaml -seed-admin "e2eadmin:E2e-Salom-2026" >/dev/null 2>&1
TOK=$(curl -s -X POST http://127.0.0.1:8081/api/auth/login -d '{"username":"e2eadmin","password":"E2e-Salom-2026"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("access",""))')
[ -n "$TOK" ] && ok "login (JWT)" || { bad "login"; exit 1; }
# §3 (1.0-draft.10): kod yo'q — agent so'rov yuboradi, admin tasdiqlaydi.
./build/testagent -server ws://127.0.0.1:8081 -join -name "E2e-joy" -cams 1 -publish -hostname e2e-joy > var/log/e2e-agent.log 2>&1 &
AGENTPID=$!
RID=""
for i in $(seq 1 20); do
  RID=$(curl -s "http://127.0.0.1:8081/api/admin/joins?status=pending" -H "Authorization: Bearer $TOK" \
    | python3 -c 'import sys,json;d=json.load(sys.stdin);r=d.get("requests") or [];print(r[0]["id"] if r else "")')
  [ -n "$RID" ] && break
  sleep 0.5
done
[ -n "$RID" ] && ok "ulanish so'rovi keldi (#$RID)" || { bad "so'rov kelmadi"; cat var/log/e2e-agent.log; exit 1; }
AP=$(curl -s -X POST "http://127.0.0.1:8081/api/admin/joins/$RID/approve" -H "Authorization: Bearer $TOK" -d '{"name":"E2e-joy"}')
echo "$AP" | grep -q '"agent_id"' && ok "admin tasdiqladi" || { bad "tasdiq: $AP"; exit 1; }
sleep 4
CID=$(curl -s http://127.0.0.1:8081/api/clients -H "Authorization: Bearer $TOK" \
  | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d[0]["id"] if d else "")')
CAM=$(curl -s "http://127.0.0.1:8081/api/clients/$CID/cameras" -H "Authorization: Bearer $TOK" \
  | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d[0]["id"] if d else "")')
[ -n "$CAM" ] && ok "kamera ro'yxatda: ${CAM:0:8}…" || { bad "kamera ko'rinmadi"; tail -5 var/log/e2e-agent.log; exit 1; }

say "4. Chrome (headless, CDP 9222)"
rm -rf /tmp/vms_chrome
google-chrome --headless=new --no-sandbox --disable-gpu --remote-debugging-port=9222 \
  --user-data-dir=/tmp/vms_chrome --autoplay-policy=no-user-gesture-required \
  --window-size=1280,800 about:blank > var/log/e2e-chrome.log 2>&1 &
CHROMEPID=$!
for _ in $(seq 1 40); do
  curl -sf http://127.0.0.1:9222/json/version >/dev/null 2>&1 && break
  sleep 0.25
done
curl -sf http://127.0.0.1:9222/json/version >/dev/null 2>&1 && ok "CDP tayyor" || { bad "Chrome CDP javob bermadi"; tail -8 var/log/e2e-chrome.log; exit 1; }

say "5. UI oqimi (login → grid → WHEP → sevimli → klaviatura → visibility)"
"$NODE_BIN" scripts/ui_smoke.mjs http://127.0.0.1:8081 e2eadmin E2e-Salom-2026
UIRC=$?
if [ "$UIRC" -eq 0 ]; then ok "ui_smoke o'tdi"; else bad "ui_smoke (exit $UIRC)"; fi

say "6. sessiyalar tozalandi (oqim qolmagan)"
sleep 2
LEFT=1
for _ in $(seq 1 20); do
  LEFT=$(curl -s http://127.0.0.1:8081/api/sessions -H "Authorization: Bearer $TOK" | python3 -c 'import sys,json;print(len([s for s in json.load(sys.stdin) if s.get("state") in ("pending","publishing","playing")]))')
  [ "$LEFT" = "0" ] && break
  sleep 1
done
[ "$LEFT" = "0" ] && ok "faol sessiya qolmagan" || {
  bad "faol sessiyalar: $LEFT"
  curl -s http://127.0.0.1:8081/api/sessions -H "Authorization: Bearer $TOK" | python3 -c '
import sys,json
for x in json.load(sys.stdin):
    print("   ", x.get("id"), x.get("state"), x.get("kind"), x.get("profile"), x.get("expires_at"))'
}

say "7. xulosa"
echo "PASS=$PASS FAIL=$FAIL"
if [ "$FAIL" -eq 0 ]; then echo "S3 QABUL: O'TDI"; exit 0; else echo "S3 QABUL: JAMBOQ"; exit 1; fi
