#!/usr/bin/env bash
# testdata/gen.sh — fixture bazasini FFmpeg bilan qayta generatsiya qiladi.
#
#   ./testdata/gen.sh          # generatsiya
#   ./testdata/gen.sh -c       # faqat tekshirish (fayllar bormi)
#
# Nima uchun FFmpeg: SDP va SPS/PPS haqiqiy bitlar bo'ladi — C2.6 (kodek/profile/
# B-frame aniqlash) testlari qo'lda o'ylab yozilgan matn ustida emas, real
# kodlash natijasi ustida ishlaydi (TASKS §0.2).
set -euo pipefail

cd "$(dirname "$0")/.."
TD=testdata
mkdir -p "$TD/sdp" "$TD/annexb" "$TD/wsdd" "$TD/csv" "$TD/protocol"

if [[ "${1:-}" == "-c" ]]; then
  missing=0
  for f in "$TD"/sdp/*.sdp "$TD"/annexb/*.h264 "$TD"/annexb/*.h265 \
           "$TD"/ffmpeg-encoders.txt "$TD"/wsdd/*.xml "$TD"/csv/*.csv; do
    [[ -e "$f" ]] || { echo "YO'Q: $f"; missing=1; }
  done
  exit $missing
fi

command -v ffmpeg >/dev/null || { echo "ffmpeg kerak (tashqi bog'liqlik)"; exit 1; }

# --- 1) Annex-B oqimlar (haqiqiy SPS/PPS + kadr tuzilishi) ---
gen_h264() { # $1=name $2=profile $3=bf
  ffmpeg -hide_banner -loglevel error -y \
    -f lavfi -i "testsrc=size=1280x720:rate=10" -frames:v 10 \
    -c:v libx264 -profile:v "$2" -level 31 -pix_fmt yuv420p \
    -g 20 -bf "$3" -b:v 700k -maxrate 700k -bufsize 700k -an \
    -f h264 "$TD/annexb/$1.h264"
}
gen_h264 h264-baseline baseline 0
gen_h264 h264-main-nob main 0
gen_h264 h264-high-bf3 high 3

ffmpeg -hide_banner -loglevel error -y \
  -f lavfi -i "testsrc=size=1280x720:rate=10" -frames:v 10 \
  -c:v libx265 -pix_fmt yuv420p -x265-params "log-level=none:crf=32:fps=10:keyint=20:bframes=0" \
  -an -f hevc "$TD/annexb/h265-main.h265"

# --- 2) SDP fayllar (Dahua/RTSP DESCRIBE ko'rinishiga yaqin) ---
python3 - "$TD" <<'PYEOF'
import base64, os, sys

td = sys.argv[1]

def nals(path):
    """Annex-B fayldan NAL boshlarini ajratadi."""
    data = open(path, 'rb').read()
    out, i, n = [], 0, len(data)
    while i < n - 3:
        if data[i] == 0 and data[i+1] == 0 and (data[i+2] == 1 or (data[i+2] == 0 and i+3 < n and data[i+3] == 1)):
            start = i + (4 if data[i+2] == 0 else 3)
            j = start
            while j < n - 3:
                if data[j] == 0 and data[j+1] == 0 and (data[j+2] == 1 or (data[j+2] == 0 and data[j+3] == 1)):
                    break
                j += 1
            out.append(data[start:j])
            i = j
        else:
            i += 1
    return out

def b64(b):
    return base64.b64encode(b).decode()

def h264_sdp(name, path, bitrate_kbps):
    ns = nals(path)
    sps = next(x for x in ns if (x[0] & 0x1F) == 7)
    pps = next(x for x in ns if (x[0] & 0x1F) == 8)
    pli = sps[1:4].hex()
    return f"""v=0
o=- 0 0 IN IP4 127.0.0.1
s=Video
c=IN IP4 127.0.0.1
t=0 0
a=range:npt=now-
m=video 0 RTP/AVP 96
b=AS:{bitrate_kbps}
a=rtpmap:96 H264/90000
a=fmtp:96 profile-level-id={pli};packetization-mode=1;sprop-parameter-sets={b64(sps)},{b64(pps)}
a=control:trackID=0
"""

def h265_sdp(path, bitrate_kbps):
    ns = nals(path)
    vps = next(x for x in ns if (x[0] >> 1) & 0x3F == 32)
    sps = next(x for x in ns if (x[0] >> 1) & 0x3F == 33)
    pps = next(x for x in ns if (x[0] >> 1) & 0x3F == 34)
    return f"""v=0
o=- 0 0 IN IP4 127.0.0.1
s=Video
c=IN IP4 127.0.0.1
t=0 0
a=range:npt=now-
m=video 0 RTP/AVP 96
b=AS:{bitrate_kbps}
a=rtpmap:96 H265/90000
a=fmtp:96 profile-id=1;sprop-max-smp-width-d=4096;sprop-max-smp-height-d=2160;sprop-vps={b64(vps)};sprop-sps={b64(sps)};sprop-pps={b64(pps)}
a=control:trackID=0
"""

jobs = [
    ("h264-baseline.sdp", "h264-baseline"),
    ("h264-main-nob.sdp", "h264-main-nob"),
    ("h264-high-bframes.sdp", "h264-high-bf3"),
]
for out, src in jobs:
    open(os.path.join(td, "sdp", out), "w").write(h264_sdp(out, os.path.join(td, "annexb", src + ".h264"), 700))
open(os.path.join(td, "sdp", "h265-main.sdp"), "w").write(h265_sdp(os.path.join(td, "annexb", "h265-main.h265"), 1200))
print("SDP yozildi:", len(jobs) + 1)
PYEOF

# --- 3) ffmpeg imkoniyatlari dump (caps probe testi uchun) ---
ffmpeg -hide_banner -encoders > "$TD/ffmpeg-encoders.txt"

# --- 4) CSV fixture (bulk import testi uchun) ---
cat > "$TD/csv/cameras-basic.csv" <<'CSVEOF'
name,host,port,main_path,sub_path,user,pass
Kirish-01,192.168.10.31,554,/cam/realmonitor?channel=1&subtype=0,/cam/realmonitor?channel=1&subtype=1,admin,dahua123
Kirish-02,192.168.10.32,554,/cam/realmonitor?channel=1&subtype=0,/cam/realmonitor?channel=1&subtype=1,admin,dahua123
Sari-01,192.168.10.33,554,/cam/realmonitor?channel=1&subtype=0,/cam/realmonitor?channel=1&subtype=1,admin,dahua123
CSVEOF

cat > "$TD/csv/cameras-bad.csv" <<'CSVEOF'
name,host,port,main_path,sub_path,user,pass
Yoq-01,,554,/cam/realmonitor?channel=1&subtype=0,/cam/realmonitor?channel=1&subtype=1,admin,x
Yomon-port,192.168.10.40,99999,/a,/b,admin,x
Kirish-03,192.168.10.34,554,/cam/realmonitor?channel=1&subtype=0,/cam/realmonitor?channel=1&subtype=1,admin,dahua123
CSVEOF

echo "fixturelar tayyor:"
find "$TD" -type f | sort | sed 's/^/  /'
