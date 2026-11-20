#!/usr/bin/env bash
# Qo'lda o'lchov: play(transcode_mode=server) dan keyin tc/... path'i
# MediaMTX'da necha sekundda "ready" bo'ladi (FFmpeg start-up kechikishi).
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
# §3: so‘rov + admin tasdiqi (kod bekor qilindi)
./build/testagent -server ws://127.0.0.1:8081 -join -name "Measure-joy" -cams 1 -publish -hostname m-joy > var/log/aa.log 2>&1 & PIDS+=($!)
RID=""
for i in $(seq 1 20); do
  RID=$(curl -s "http://127.0.0.1:8081/api/admin/joins?status=pending" "${A[@]}" | python3 -c 'import sys,json;d=json.load(sys.stdin);r=d.get("requests") or [];print(r[0]["id"] if r else "")')
  [ -n "$RID" ] && break
  sleep 0.5
done
curl -s -X POST "http://127.0.0.1:8081/api/admin/joins/$RID/approve" "${A[@]}" -d '{"name":"Measure-joy"}' >/dev/null
sleep 4

CID=$(curl -s http://127.0.0.1:8081/api/clients "${A[@]}" | python3 -c 'import sys,json;print(json.load(sys.stdin)[0]["id"])')
CAM=$(curl -s "http://127.0.0.1:8081/api/clients/$CID/cameras" "${A[@]}" | python3 -c 'import sys,json;print(json.load(sys.stdin)[0]["id"])')
curl -s -X PATCH "http://127.0.0.1:8081/api/admin/cameras/$CAM" "${A[@]}" -d '{"transcode_mode":"server"}' >/dev/null

# 1-urinish: agent publish'ni boshlashi uchun avval oddiy play (transcode off).
curl -s -X PATCH "http://127.0.0.1:8081/api/admin/cameras/$CAM" "${A[@]}" -d '{"transcode_mode":"off"}' >/dev/null
S1=$(curl -s -X POST "http://127.0.0.1:8081/api/cameras/$CAM/play" "${A[@]}" -d '{}' | python3 -c 'import sys,json;print(json.load(sys.stdin).get("session_id",""))')
sleep 4
curl -s -X DELETE "http://127.0.0.1:8081/api/sessions/$S1" "${A[@]}" >/dev/null
sleep 3   # idle stop: agent publish'ni to'xtatadi

# Endi transcode rejimida: play → FFmpeg cam→tc → tc ready vaqti.
curl -s -X PATCH "http://127.0.0.1:8081/api/admin/cameras/$CAM" "${A[@]}" -d '{"transcode_mode":"server"}' >/dev/null
T0=$(date +%s.%N)
S2=$(curl -s -X POST "http://127.0.0.1:8081/api/cameras/$CAM/play" "${A[@]}" -d '{}' | python3 -c 'import sys,json;print(json.load(sys.stdin).get("session_id",""))')
echo "sessiya(tc): $S2"
for _ in $(seq 1 60); do
  R=$(curl -s -u vmsapi:vmsapi-dev-only http://127.0.0.1:9997/v3/paths/list | python3 -c '
import sys,json
for p in json.load(sys.stdin).get("items",[]):
    if p["name"].startswith("tc/"): print(p.get("ready"))
')
  if [ "$R" = "True" ]; then
    T1=$(date +%s.%N)
    python3 -c "print(f'tc READY: {$T1-$T0:.2f} s')"
    break
  fi
  sleep 0.4
done
curl -s -u vmsapi:vmsapi-dev-only http://127.0.0.1:9997/v3/paths/list | python3 -c '
import sys,json
for p in json.load(sys.stdin).get("items",[]):
    print(" ", p["name"], "ready=", p.get("ready"), "readers=", len(p.get("readers") or []))'
grep -a "transcode\|auth-hook.*tc/" var/log/vv.log | tail -4 | sed 's/.*msg=/  /; s/svc=vmsd[^ ]*//'
