#!/usr/bin/env bash
# Test uchun soxta ffmpeg: bir necha progress chizig'ini bosadi, keyin
# XATO bilan chiqadi — transcode'ning jonli uzilishini simulyatsiya
# qiladi. Har ishga tushishda $FAKEFFMPEG_COUNT fayliga "start" yozadi.
if [ -n "$FAKEFFMPEG_COUNT" ]; then printf 'start\n' >> "$FAKEFFMPEG_COUNT"; fi
printf 'frame=1\nprogress=continue\n'
sleep 0.2
exit 1
