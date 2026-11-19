#!/usr/bin/env bash
# itest.sh — integratsiya matritsasi (docs/protocol.md §18, TASKS C3.20).
#
#   make itest SCENARIO=c1
#   ./scripts/itest.sh all
#
# Hozirgi qamrov: c0 (unit + fixture). Qolgan ssenariylar tegishli bosqichda
# qo'shiladi va shu yerda ro'yxatdan o'tadi — "yo'q ssenariy" jimgina o'tib
# ketmasin uchun exit 2 qaytaradi.
set -euo pipefail

cd "$(dirname "$0")/.."

SCENARIO="${1:-c0}"

run_go() { go test ./... -count=1 "$@"; }

case "$SCENARIO" in
  c0)
    echo "== C0: unit + fixture testlari =="
    run_go
    ;;
  c1)
    echo "== C1: enroll → ulanish → secret_rotate → uzilish → qayta ulanish =="
    go test ./tests/integration -run TestC1 -race -count=1 -timeout 120s
    ;;
  c2|c3_h264_whip|c3_h265_rtsp|c3_udp_blocked|c3_client_transcode|c3_bframes|\
  c3_remux|c3_ffmpeg_fallback|c3_codec_change|c3|shutdown|restore|version_mismatch|all)
    echo "SSSENARIY '$SCENARIO' hali qo'shilmagan — C1.14 / C3.20 da qo'shiladi."
    echo "Ro'yxat: docs/protocol.md §18"
    exit 2
    ;;
  *)
    echo "noma'lum ssenariy: $SCENARIO"
    echo "hozircha: c0"
    exit 2
    ;;
esac

echo "itest OK: $SCENARIO"
