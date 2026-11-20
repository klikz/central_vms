#!/usr/bin/env bash
# S2 qabul mezoni (REAL MediaMTX + REAL FFmpeg):
#   agent publish qiladi → API play → token bilan RTSP o'qish →
#   token'siz / noto'g'ri token rad → sessiya yopilganda publish to'xtaydi →
#   server transcode (tc/...) ishlaydi.
# Ishlatish: make e2e-s2
set -uo pipefail
cd "$(dirname "$0")/.."
PASS=0; FAIL=0
ok()  { echo "  PASS: $1"; PASS=$((PASS+1)); }
bad() { echo "  FAIL: $1"; FAIL=$((FAIL+1)); }
say() { echo "== $1"; }
MTXPID=""; VMSDPID=""; AGENTPID=""
cleanup() {
  for p in "$AGENTPID" "$VMSDPID" "$MTXPID"; do
    [ -n "$p" ] && kill -INT "$p" 2>/dev/null
  done
  sleep 1
  for p in "$AGENTPID" "$VMSDPID" "$MTXPID"; do
    [ -n "$p" ] && kill -9 "$p" 2>/dev/null
  done
  rm -f /tmp/vms_e2e.db /tmp/vms_e2e.db-wal /tmp/vms_e2e.db-shm
}
trap cleanup EXIT

say "0. portlar bo'shmi"
BUSY=""
for p in 8554 8888 8889 8189 9997 8081; do
  if ss -ltn | awk '{print $4}' | grep -Eq ":$p$"; then BUSY="$BUSY $p"; fi
done
if [ -n "$BUSY" ]; then
  bad "band portlar:$BUSY (make port-check; tashqi MediaMTX'ni to'xtating)"; exit 1
fi
ok "portlar bo'sh"

say "1. qurish"
make build >/dev/null 2>&1 || { bad "make build"; exit 1; }
[ -x mediamtx/mediamtx ] || { bad "mediamtx/mediamtx yo'q — make media-get"; exit 1; }
ok "binarlar tayyor"

mkdir -p var/log

say "2. MediaMTX va vmsd'ni otish"
./mediamtx/mediamtx mediamtx/mediamtx.yml > var/log/e2e-mediamtx.log 2>&1 &
MTXPID=$!
./build/vmsd -config config/e2e.yaml > var/log/e2e-vmsd.log 2>&1 &
VMSDPID=$!
for _ in $(seq 1 40); do
  curl -sf http://127.0.0.1:8081/healthz >/dev/null 2>&1 && break
  sleep 0.25
done
if curl -sf http://127.0.0.1:8081/healthz >/dev/null 2>&1; then ok "vmsd /healthz 200"; else
  bad "vmsd ko'tarilmadi"; tail -8 var/log/e2e-vmsd.log; exit 1; fi
if curl -sf -u vmsapi:vmsapi-dev-only http://127.0.0.1:9997/v3/config/global/get >/dev/null 2>&1; then
  ok "MediaMTX API (9997) javob berdi"
else
  bad "MediaMTX API javob bermadi"; tail -8 var/log/e2e-mediamtx.log
fi

say "3. admin → enroll → testagent (-publish)"
./build/vmsd -config config/e2e.yaml -seed-admin "e2eadmin:E2e-Salom-2026" >/dev/null 2>&1
TOK=$(curl -s -X POST http://127.0.0.1:8081/api/auth/login -d '{"username":"e2eadmin","password":"E2e-Salom-2026"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("access",""))')
[ -n "$TOK" ] && ok "login (JWT)" || { bad "login"; exit 1; }
CODE=$(curl -s -X POST http://127.0.0.1:8081/api/admin/enrollments -H "Authorization: Bearer $TOK" -d '{}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("code",""))')
[ -n "$CODE" ] && ok "enroll kodi: $CODE" || { bad "enroll kodi olinmadi"; exit 1; }
./build/testagent -server ws://127.0.0.1:8081 -code "$CODE" -cams 1 -publish -hostname e2e-joy > var/log/e2e-agent.log 2>&1 &
AGENTPID=$!
sleep 2
read -r CIDX AGID ST < <(curl -s http://127.0.0.1:8081/api/clients -H "Authorization: Bearer $TOK" \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d[0]["id"], d[0]["agent_id"], d[0]["status"]) if d else print("","","")')
[ "$ST" = "online" ] && ok "agent online ($AGID)" || bad "agent holati: '$ST'"
CAM=$(curl -s "http://127.0.0.1:8081/api/clients/$CIDX/cameras" -H "Authorization: Bearer $TOK" \
  | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d[0]["id"]) if d else print("")')
[ -n "$CAM" ] && ok "kamera ro'yxatda: ${CAM:0:13}…" || { bad "kamera yo'q"; exit 1; }

say "4. play (direct) → token bilan RTSP o'qish"
# Bu mashinada GPU bor — "auto" server transcode'ga tushadi; direct oqimni
# tekshirish uchun kamerani avval transcode_mode=off qilamiz.
curl -s -X PATCH "http://127.0.0.1:8081/api/admin/cameras/$CAM" -H "Authorization: Bearer $TOK" \
  -d '{"transcode_mode":"off"}' >/dev/null
PLAY=$(curl -s -X POST "http://127.0.0.1:8081/api/cameras/$CAM/play" -H "Authorization: Bearer $TOK" -d '{"profile":"sub"}')
SID=$(echo "$PLAY"  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("session_id",""))')
TOKR=$(echo "$PLAY" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))')
WHEP=$(echo "$PLAY" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("whep_url",""))')
CODEC=$(echo "$PLAY"| python3 -c 'import sys,json;print(json.load(sys.stdin).get("codec",""))')
if [ -n "$SID" ] && [ -n "$TOKR" ]; then ok "play: session=${SID:0:8}… codec=$CODEC whep=$WHEP"; else
  bad "play javob: $PLAY"; tail -6 var/log/e2e-vmsd.log; exit 1; fi
RPATH=$(echo "$WHEP" | python3 -c 'import sys,re;u=sys.stdin.read().strip();m=re.match(r"https?://[^/]+/(.*?)/whep$",u);print(m.group(1) if m else "")')
[ -n "$RPATH" ] && ok "o'qish path'i: $RPATH" || bad "whep_url parse: $WHEP"
SRC="rtsp://127.0.0.1:8554/$RPATH?token=$TOKR"
for _ in $(seq 1 40); do
  ffprobe -v error -rtsp_transport tcp -i "$SRC" -select_streams v \
    -show_entries stream=codec_name -of csv=p=0 2>/dev/null | grep -q h264 && break
  sleep 0.5
done
if ffprobe -v error -rtsp_transport tcp -i "$SRC" -select_streams v \
     -show_entries stream=codec_name -of csv=p=0 2>/dev/null | grep -q h264; then
  ok "token bilan RTSP o'qildi (h264 oqim bor)"
else
  bad "token bilan RTSP o'qib bo'lmadi"; tail -6 var/log/e2e-agent.log
  grep -a "auth-hook" var/log/e2e-vmsd.log | tail -4
fi

say "5. token himoyasi (auth-hook)"
if ffprobe -v error -rtsp_transport tcp -i "rtsp://127.0.0.1:8554/$RPATH" \
     -select_streams v -show_entries stream=codec_name -of csv=p=0 >/dev/null 2>&1; then
  bad "token'siz o'qib bo'ldi — hook ishlamayapti"
else
  ok "token'siz ulanish rad etildi"
fi
if ffprobe -v error -rtsp_transport tcp -i "rtsp://127.0.0.1:8554/$RPATH?token=soxta" \
     -select_streams v -show_entries stream=codec_name -of csv=p=0 >/dev/null 2>&1; then
  bad "noto'g'ri token o'tib ketdi"
else
  ok "noto'g'ri token rad etildi"
fi

say "6. MediaMTX readers hisobi (idle stop manbasi)"
RD=$(curl -s -u vmsapi:vmsapi-dev-only "http://127.0.0.1:9997/v3/paths/get/$RPATH" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("readers",-1))' 2>/dev/null)
if [ "$RD" != "-1" ]; then ok "paths/get readers=$RD"; else
  bad "paths/get javobi: $(curl -s -u vmsapi:vmsapi-dev-only "http://127.0.0.1:9997/v3/paths/get/cam/$AGID/$CAM")"
fi

say "7. sessiyani yopish → idle stop (2 s)"
curl -s -X DELETE "http://127.0.0.1:8081/api/sessions/$SID" -H "Authorization: Bearer $TOK" >/dev/null
sleep 4
if grep -q "publish to'xtatildi" var/log/e2e-agent.log; then
  ok "agent media_stop oldi (FFmpeg o'chirildi)"
else
  bad "agent media_stop olmadi"; tail -8 var/log/e2e-agent.log
fi
if ! pgrep -f "testsrc=size=1280x720" >/dev/null 2>&1; then ok "FFmpeg jarayoni qolmagan"; else
  bad "FFmpeg tirik qoldi:"; pgrep -af "testsrc=size=1280x720" | head -3
fi

say "8. server transcode (transcode_mode=server)"
curl -s -X PATCH "http://127.0.0.1:8081/api/admin/cameras/$CAM" -H "Authorization: Bearer $TOK" \
  -d '{"transcode_mode":"server"}' >/dev/null
PLAY2=$(curl -s -X POST "http://127.0.0.1:8081/api/cameras/$CAM/play" -H "Authorization: Bearer $TOK" -d '{"profile":"sub"}')
SID2=$(echo "$PLAY2"  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("session_id",""))')
TOK2=$(echo "$PLAY2"  | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))')
TRC=$(echo "$PLAY2"   | python3 -c 'import sys,json;print(json.load(sys.stdin).get("transcoded",""))')
BE=$(echo "$PLAY2"    | python3 -c 'import sys,json;print(json.load(sys.stdin).get("backend",""))')
[ -n "$SID2" ] && ok "play (tc): transcoded=$TRC backend=$BE" || bad "play2: $PLAY2"
TC="rtsp://127.0.0.1:8554/tc/$AGID/$CAM?token=$TOK2"
for _ in $(seq 1 40); do
  ffprobe -v error -rtsp_transport tcp -i "$TC" -select_streams v \
    -show_entries stream=codec_name -of csv=p=0 2>/dev/null | grep -q h264 && break
  sleep 0.5
done
if ffprobe -v error -rtsp_transport tcp -i "$TC" -select_streams v \
     -show_entries stream=codec_name -of csv=p=0 2>/dev/null | grep -q h264; then
  ok "tc/... o'qildi — server transcode ishlayapti (backend=$BE)"
else
  bad "tc/... o'qib bo'lmadi"; grep -a "transcode" var/log/e2e-vmsd.log | tail -6
fi
if [ -n "$SID2" ]; then curl -s -X DELETE "http://127.0.0.1:8081/api/sessions/$SID2" -H "Authorization: Bearer $TOK" >/dev/null; fi

say "9. xulosa"
echo "  PASS=$PASS FAIL=$FAIL"
if [ "$FAIL" -eq 0 ]; then echo "S2 QABUL: O'TDI"; else echo "S2 QABUL: MUVATTAQIYATSIZ"; fi
exit "$FAIL"
