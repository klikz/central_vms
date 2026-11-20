#!/usr/bin/env bash
# Test uchun soxta ffmpeg: argumentlarni $FAKEFFMPEG_ARGS fayliga yozadi,
# progress chizig'larini bosadi, signal bo'ychi chiqadi.
if [ -n "$FAKEFFMPEG_ARGS" ]; then printf '%s\n' "$@" > "$FAKEFFMPEG_ARGS"; fi
i=0
trap 'exit 0' INT TERM
while true; do
  i=$((i+1))
  printf 'frame=%d\nfps=15.0\nbitrate=1200.5kbits/s\nout_time_ms=%d\nprogress=continue\n' "$i" "$((i*1000000))"
  sleep 0.05
done
