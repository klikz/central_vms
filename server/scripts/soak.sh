#!/usr/bin/env bash
# soak.sh — uzoq ishlash sinovi + uzilish in'ektsiyasi (2026-11-22
# barqarorlik ishlari). Maqsad: qisqa testlar EMAS, "haftalab ishlash"
# xatti-harakatini qisqartirilgan rejimda ko'rish.
#
#   HOURS=0  bash scripts/soak.sh     # ~10 daq tutun sinovi (in'ektsiyali)
#   HOURS=24 bash scripts/soak.sh     # tunlik soak
#
# Talab: stek ishlab turibdi (root `make run` yoki systemd vmsd).
# Har INTERVAL soniyada /healthz + /metrics o'qiladi; natijalar
# var/soak/soak-<ts>.csv ga yoziladi. INJECT=1 (default) bo'lsa
# MARKAZDA bitta jarayonni (vmsd) kill -9 qilamiz — systemd/Makefile
# qayta ko'tarishi kerak; qancha vaqtda tiklangani o'lchanadi.
#
# Chiqish: 0 = barcha tekshiruvlar o'tdi; 1 = kutilmagan uzilish/
# tiklanmaslik/agentlar tushib qolishi qayd etildi.
set -uo pipefail

HOURS="${HOURS:-0}"
INTERVAL="${INTERVAL:-30}"
INJECT="${INJECT:-1}"
METRICS_URL="${METRICS_URL:-http://127.0.0.1:8189/metrics}"
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:8081/healthz}"
OUT_DIR="${OUT_DIR:-var/soak}"
mkdir -p "$OUT_DIR"
RUNS="$OUT_DIR/soak-$(date -u +%Y%m%d-%H%M%S)"
CSV="$RUNS.csv"
LOG="$RUNS.log"

dur_s=$(( HOURS > 0 ? HOURS * 3600 : 600 ))
inject_at=$(( dur_s / 2 ))

m() { echo "$(date -u +%H:%M:%S) $*" | tee -a "$LOG"; }

getm() { # getm KEY — oxirgi /metrics matnidan qiymat
	echo "$LAST_TXT" | awk -v k="$1" '$1==k {print $2; f=1; exit} END{if(!f) print ""}'
}

# vmsd jarayonini topish (ikkala rejim ham): lokal stek yoki systemd.
vmsd_pid() {
	if [ -f build/run/vmsd-localhost.pid ] && kill -0 "$(cat build/run/vmsd-localhost.pid)" 2>/dev/null; then
		cat build/run/vmsd-localhost.pid; return
	fi
	systemctl show -p MainPID --value vmsd 2>/dev/null
}

health_fails=0; samples=0; restored_s=-1; cam_offline_max=0
prev5xx=0; prevtc=0; prevfail=0
echo "ts,healthz,agents,cams_online,cams_total,http5xx,tc_restarts,hs_fail" > "$CSV"

m "soak boshlandi: davomiylik ${dur_s}s, interval ${INTERVAL}s, inject=${INJECT} (t=${inject_at}s)"
end=$(( $(date +%s) + dur_s )); injected=0
while [ "$(date +%s)" -lt "$end" ]; do
	samples=$(( samples + 1 ))
	hz=$(curl -fsS --max-time 5 "$HEALTH_URL" >/dev/null 2>&1 && echo ok || echo FAIL)
	[ "$hz" = FAIL ] && health_fails=$(( health_fails + 1 ))
	LAST_TXT=$(curl -fsS --max-time 5 "$METRICS_URL" 2>/dev/null || true)
	agents=$(getm vmsd_agents_connected); agents=${agents:--1}
	ct=$(getm vmsd_cameras_total); ct=${ct:--1}
	co=$(getm vmsd_cameras_online); co=${co:--1}
	e5=$(getm 'vmsd_http_responses_total{class="5xx"}'); e5=${e5:-0}
	tcr=$(getm vmsd_tc_restarts); tcr=${tcr:-0}
	hsf=$(getm 'vmsd_agent_handshakes_total{result="fail"}'); hsf=${hsf:-0}
	[ "$ct" != "-1" ] && [ "$co" != "-1" ] && { off=$(( ct - co )); [ "$off" -gt "$cam_offline_max" ] && cam_offline_max=$off; }
	echo "$(date -u +%H:%M:%S),$hz,$agents,$co,$ct,$e5,$tcr,$hsf" >> "$CSV"
	prev5xx=$e5; prevtc=$tcr; prevfail=$hsf

	# In'ektsiya: vmsd'ni o'ldiramiz — tiklanish vaqtini o'lchaymiz.
	if [ "$INJECT" = 1 ] && [ "$injected" = 0 ] && [ "$samples" -ge $(( inject_at / INTERVAL )) ]; then
		injected=1
		pid=$(vmsd_pid)
		if [ -n "$pid" ] && [ "$pid" != "0" ]; then
			m "INJECT: kill -9 vmsd (pid $pid)"
			kill -9 "$pid" 2>/dev/null
			# systemd boshqarmasa (lokal stek) — qo'lda qayta ko'taramiz;
			# prod'da Restart=always o'zi qiladi. RESTART_CMD= bilan
			# boshqa buyruq ham qo'yish mumkin.
			if ! systemctl is-active --quiet vmsd 2>/dev/null; then
				if [ -n "${RESTART_CMD:-}" ]; then
					bash -c "$RESTART_CMD"
				else
					( setsid ./build/vmsd -config config/localhost.yaml > var/log/vmsd-localhost.out 2>&1 & echo $! > build/run/vmsd-localhost.pid )
				fi
			fi
			t0=$(date +%s)
			for i in $(seq 1 24); do
				sleep 5
				if curl -fsS --max-time 3 "$HEALTH_URL" >/dev/null 2>&1; then
					restored_s=$(( $(date +%s) - t0 )); break
				fi
			done
			if [ "$restored_s" -lt 0 ]; then m "XATO: vmsd 120 s ichida tiklanmadi"; else m "vmsd ${restored_s}s ichida tiklandi"; fi
		else
			m "INJECT: vmsd pid topilmadi (in'ektsiya o'tkazib yuborildi)"
		fi
	fi
	sleep "$INTERVAL"
done

m "soak tugadi: samples=$samples health_fails=$health_fails cam_offline_max=$cam_offline_max restored=${restored_s}s"
rc=0
# In'ektsiyadan keyin healthz 1-2 marta FAIL bo'lishi KUTILADI —
# undan ko'pi yoki agentlar qayta ulanmasa — muvaffaqiyatsizlik.
expected_hf=$(( INJECT == 1 ? 6 : 1 ))
if [ "$health_fails" -gt "$expected_hf" ]; then m "XATO: kutilmagan uzilishlar ($health_fails > $expected_hf)"; rc=1; fi
if [ "$INJECT" = 1 ] && [ "$injected" = 1 ] && [ "$restored_s" -lt 0 ]; then rc=1; fi
if [ "$agents" = "0" ] && [ "$INJECT" = 0 ]; then m "XATO: soak oxirida agent ulanmagan"; rc=1; fi
m "natija: $CSV (rc=$rc)"
exit $rc
