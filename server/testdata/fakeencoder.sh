#!/usr/bin/env bash
# Soxta ffmpeg: nvenc/vaapi so'ralsa xato beradi, libx264'da muvaffaqiyatli.
for a in "$@"; do
  case "$a" in
    h264_nvenc|h264_vaapi) echo "Driver does not support the required nvenc API version" >&2; exit 1;;
  esac
done
exit 0
