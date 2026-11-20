#!/usr/bin/env bash
# FFmpeg RTSP manbani ochish kechikishini o'lchaydi (turli bayroqlar bilan).
set -uo pipefail
cd "$(dirname "$0")/.."
PIDS=()
cleanup() { for p in "${PIDS[@]}"; do kill -9 "$p" 2>/dev/null; done; }
trap cleanup EXIT

./mediamtx/mediamtx mediamtx/mediamtx.yml > var/log/mm.log 2>&1 & PIDS+=($!)
sleep 1
./build/vmsd -config config/e2e.yaml > var/log/vv.log 2>&1 & PIDS+=($!)
sleep 2
./build/vmsd -config config/e2e.yaml -seed-admin "e2eadmin:E2e-Salom-2026" >/dev/null 2>&1
TOK=$(curl -s -X POST http://127.0.0.1:8081/api/auth/login -d '{"username":"e2eadmin","password":"E2e-Salom-2026"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["access"])')
A=(-H "Authorization: Bearer $TOK" -H 'Content-Type: application/json')
CODE=$(curl -s -X POST http://127.0.0.1:8081/api/admin/enrollments "${A[@]}" -d '{}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["code"])')
./build/testagent -server ws://127.0.0.1:8081 -code "$CODE" -cams 1 -publish -hostname m-joy > var/log/aa.log 2>&1 & PIDS+=($!)
sleep 2
CID=$(curl -s http://127.0.0.1:8081/api/clients "${A[@]}" | python3 -c 'import sys,json;print(json.load(sys.stdin)[0]["id"])')
CAM=$(curl -s "http://127.0.0.1:8081/api/clients/$CID/cameras" "${A[@]}" | python3 -c 'import sys,json;print(json.load(sys.stdin)[0]["id"])')
curl -s -X PATCH "http://127.0.0.1:8081/api/admin/cameras/$CAM" "${A[@]}" -d '{"transcode_mode":"off"}' >/dev/null
PLAY=$(curl -s -X POST "http://127.0.0.1:8081/api/cameras/$CAM/play" "${A[@]}" -d '{}')
RTOK=$(echo "$PLAY" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("token",""))')
sleep 3
SRC=$(curl -s -u vmsapi:vmsapi-dev-only http://127.0.0.1:9997/v3/paths/list | python3 -c '
import sys,json
for p in json.load(sys.stdin).get("items",[]):
    if p["name"].startswith("cam/"): print(p["name"])' | head -1)
URL="rtsp://127.0.0.1:8554/${SRC}?token=${RTOK}"
echo "manba: ${SRC}"

run() {
  local label="$1"; shift
  local t0 t1
  t0=$(date +%s.%N)
  if ffmpeg -hide_banner -loglevel error -nostdin "$@" -frames:v 3 -f null - >/dev/null 2>&1; then
    t1=$(date +%s.%N)
    python3 -c "print(f'  ${label}: {$t1-$t0:.2f} s (3 kadr)')"
  else
    echo "  ${label}: XATO"
  fi
}

run "standart        " -rtsp_transport tcp -i "$URL"
run "analyzeduration1" -rtsp_transport tcp -analyzeduration 1000000 -probesize 500000 -i "$URL"
run "nobuffer+lowdly " -rtsp_transport tcp -analyzeduration 500000 -probesize 100000 -fflags nobuffer -flags low_delay -i "$URL"
run "icestuck(nobuf) " -rtsp_transport tcp -fflags nobuffer -i "$URL"
