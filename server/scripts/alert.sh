#!/usr/bin/env bash
# vms-alert.sh — /metrics'ni o'qib, sirtki belgilarni tekshiradi va
# holat O'ZGARGANDA ogohlantiradi (har 2 daqiqada vms-alert.timer orqali).
#
#   METRICS_URL=http://127.0.0.1:8189/metrics ./vms-alert.sh
#
# Chiqish: `logger -t vms-alert` (journald) + stderr. Spam qilmaslik uchun
# ogohlantirishlar STATE_DIR'dagi faylda saqlanadi — xabar faqat holat
# o'zgarganda yoki 60 daqiqadan so'ng takrorlanadi (COOLDOWN_MIN).
#
# Tekshiriladigan belgilar (2026-11-22 barqarorlik ishlari):
#   vmsd_down            — /metrics javob bermadi (server ko'tarilmagan)
#   vmsd_restart         — uptime < 120 s (yaqinda qayta ko'tarilgan)
#   cameras_offline      — online < total (soni o'zgarganda xabar)
#   agent_flap           — handshake fail/ok nisbati yuqori (deltalar)
#   tc_flap              — transcode qayta ko'tarilishlari o'sib borayapti
set -euo pipefail

METRICS_URL="${METRICS_URL:-http://127.0.0.1:8189/metrics}"
STATE_DIR="${STATE_DIR:-/var/lib/vms-alert}"
OFFLINE_WARN="${OFFLINE_WARN:-1}"      # nechta offline kamera bo'lsa xabar
FLAP_FAIL_MIN="${FLAP_FAIL_MIN:-5}"    # 2 daqiqada kamida nechta fail
FLAP_RATIO="${FLAP_RATIO:-0.5}"        # fail/(fail+ok) nisbati
TC_RESTART_WARN="${TC_RESTART_WARN:-3}" # 2 daqiqada transcode restart
COOLDOWN_MIN="${COOLDOWN_MIN:-60}"

mkdir -p "$STATE_DIR"
STATE="$STATE_DIR/state"
touch "$STATE"

mtime=$(stat -c %Y "$STATE" 2>/dev/null || echo 0)
now=$(date +%s)
cooldown=$(( (now - mtime) < COOLDOWN_MIN * 60 ))

get() { # get <key> <metrics> — Prometheus line'dan qiymat
	echo "$1" | awk -v k="$2" '$1==k {print $2; found=1; exit} END{if(!found) print ""}'
}
sget() { # state'dan qiymat (yo'q bo'lsa 0)
	awk -v k="$1" -F= '$1==k {print $2; found=1; exit} END{if(!found) print ""}' "$STATE"
}

alerts=()
declare -A NEW=()

TXT=$(curl -fsS --max-time 5 "$METRICS_URL" 2>/dev/null || true)
if [ -z "$TXT" ]; then
	alerts+=("vmsd_down: $METRICS_URL javob bermadi (server o'chiq yoki ichki bind noto'g'ri)")
	NEW[down]=1
else
	NEW[down]=0
	up=$(get "$TXT" vmsd_uptime_seconds); up=${up:-0}
	if [ "$up" -lt 120 ]; then
		# Crash-loop belgisi: systemd Restart=always bilan ham 2 daqiqada
		# ko'rinadi. Restart holati har safar xabar beradi (cooldown'dan
		# tashqarida) — chunki bu eng qimmatli belgi.
		alerts+=("vmsd_restart: server $up s oldin ko'tarilgan (crash-loop ehtimoli)")
	fi

	total=$(get "$TXT" vmsd_cameras_total); total=${total:-0}
	online=$(get "$TXT" vmsd_cameras_online); online=${online:-0}
	off=$(( total - online ))
	NEW[cams_offline]=$off
	if [ "$off" -ge "$OFFLINE_WARN" ]; then
		prev=$(sget cams_offline); prev=${prev:--1}
		if [ "$prev" != "$off" ]; then
			alerts+=("cameras_offline: $off/$total kamera offline (oldin: $prev)")
		fi
	fi

	# Deltalar: handshake fail/ok va tc_restarts (hisoblagichlar
	# server restart'da 0'lanadi — pastga qaytish delta hisoblanmaydi).
	f=$(get "$TXT" 'vmsd_agent_handshakes_total{result="fail"}'); f=${f:-0}
	o=$(get "$TXT" 'vmsd_agent_handshakes_total{result="ok"}'); o=${o:-0}
	tcr=$(get "$TXT" vmsd_tc_restarts); tcr=${tcr:-0}
	pf=$(sget hs_fail); pf=${pf:-0}; po=$(sget hs_ok); po=${po:-0}; pt=$(sget tc_restarts); pt=${pt:-0}
	df=$(( f - pf )); [ "$df" -lt 0 ] && df=0
	do_=$(( o - po )); [ "$do_" -lt 0 ] && do_=0
	dt=$(( tcr - pt )); [ "$dt" -lt 0 ] && dt=0
	NEW[hs_fail]=$f; NEW[hs_ok]=$o; NEW[tc_restarts]=$tcr
	if [ "$df" -ge "$FLAP_FAIL_MIN" ] && awk -v a="$df" -v b="$do_" -v r="$FLAP_RATIO" 'BEGIN{exit !(a/(a+b+0.0001) >= r)}'; then
		alerts+=("agent_flap: $df muvaffaqiyatsiz handshake / $do_ muvaffaqiyatli (kalit qidirish yoki ulanish flap)")
	fi
	if [ "$dt" -ge "$TC_RESTART_WARN" ]; then
		alerts+=("tc_flap: $dt marta transcode qayta ko'tarildi (FFmpeg uzilmoqda)")
	fi
fi

# Xabar: holat o'zgarganda yoki cooldown tugaganda.
changed=0
for k in "${!NEW[@]}"; do
	[ "$(sget "$k")" != "${NEW[$k]}" ] && changed=1
done
if [ "${#alerts[@]}" -gt 0 ] && { [ "$changed" = 1 ] || ! $cooldown; }; then
	for a in "${alerts[@]}"; do
		logger -t vms-alert -p user.warning "$a" 2>/dev/null || true
		echo "VMS-ALERT: $a" >&2
	done
fi

# State'ni yangilaymiz (deltalar uchun bazaviy qiymatlar ham shu yerda).
tmp="$STATE.tmp"
: > "$tmp"
for k in "${!NEW[@]}"; do echo "$k=${NEW[$k]}" >> "$tmp"; done
mv "$tmp" "$STATE"
exit 0
