#!/usr/bin/env bash
# stress.sh — 2000 kamera miqyosida xotira/probe sikli (TASKS C4.8).
# Hozircha stub: mock kameralar paydo bo'lgach to'ldiriladi.
set -euo pipefail
CAMS="${1:-2000}"
echo "stress: CAMS=$CAMS — Go stress testlari (C4.8, §12)"
# 2000 kamera probe sikli + parallelik ≤5 + goroutine chegarasi:
go test ./internal/cams -run Stress -race -v -count=1
# 100 kamera + 8 publish ≤ 150 MB:
go test ./internal/statestore -run Stress -race -v -count=1
echo "stress: O'TDI"
