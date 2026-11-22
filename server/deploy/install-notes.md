# O'rnatish bo'yicha izohlar (prod, Ubuntu 22.04/24.04) — S4.4

Bu fayl `make install` va systemd unitlarini birinchi marta qo'yishda
bajariladigan qadamlarni yozib turadi (root §4.2, REJA §3).

## 1. Tayyorgarlik

```bash
sudo apt-get update && sudo apt-get install -y ffmpeg sqlite3 ufw
# GPU bo'lsa: nvidia-driver + ffmpeg'ning NVENC qo'llab-quvvatlashi tekshiriladi
ffmpeg -hide_banner -encoders | grep -E "nvenc|vaapi|amf" || echo "GPU encoder yo'q"
```

## 2. Yig'ish va o'rnatish

```bash
make ui-build build package      # build/ ichida vmsd, web, mediamtx, config
sudo make install                # /opt/vmsd ga (unitlar: systemd/)
sudo systemctl daemon-reload
sudo systemctl enable --now vms-media vmsd
```

`vms-media` (MediaMTX) **alohida unit** — vmsd uni boshqarmaydi
(root §1: tashqi xizmatni avtomatik to'xtatmamiz).

## 3. Konfiguratsiya

- `/opt/vmsd/config/config.yaml` — `config/prod.yaml.example` dan
  nusxa; `bind`, `db_path`, `mediamtx.public_host` (LAN IP yoki
  DNS nomi) to'g'rilanadi.
- JWT sirri faylda SAQLANMAYDI: `/etc/vmsd/secret.env` (0600) ichida
  `VMSD_JWT_SECRET=<32+ belgi>`.
- Birinchi admin: `sudo /opt/vmsd/vmsd -config /opt/vmsd/config/config.yaml -seed-admin "login:parol"`.
- Mijozlarni qo'shish: agent lokal UI'da **Request access** → server
  UI'sida tasdiq (yoki `preauth_ips` orqali avtomatik), protocol.md §3.

## 4. GPU / transcode — MAJBURIY QADAM

Prod GPU'si dev mashinadan farq qilishi mumkin. Shuning uchun
`make install` dan keyin **albatta**:

```bash
make probe    # yoki: /opt/vmsd/vmsd -config ... -probe
```

Natija `media.server_max_transcodes` va tanlangan backendga yoziladi;
`ffmpeg -encoders` ro'yxatida bo'lishi encoder HAQIQATAN ishlashini
anglatmaydi (2026-11-20 kuzatuvi) — shu sababli runtime probe ishlatiladi.

## 5. Tizim sozlamalari (MediaMTX UDP)

MediaMTX v1.11.3'da `udpReadBufferSize` parametri YO'Q — UDP bufferi
kernel sozlamasi orqali:

```bash
printf 'net.core.rmem_max=100000000\nnet.core.wmem_max=100000000\n' | sudo tee /etc/sysctl.d/99-vms.conf
sudo sysctl --system
```

MediaMTX konfiguratsiyasida: `playback: no` (arxiv yo'q), faqat
RTSP/WebRTC/HLS yoqilgan, `paths` patternlari `cam/...` va `tc/...`,
auth-hook `http://127.0.0.1:8189/internal/auth` (protocol.md §15).

## 6. Zaxira

`make install` endi `vmsd-backup.timer`'ni ham o'rnatadi (kuniga 03:15
+0..15 daq tasodifiy; `Persistent=true` — mashina o'chiq bo'lsa keyingi
yukda bitta zaxira darhol). Qo'lda:

```bash
sudo install -m 755 deploy/backup.sh /opt/vmsd/backup.sh
sudo systemctl enable --now vmsd-backup.timer   # tavsiya etilgan
# yoki cron (eski usul):
sudo crontab -l 2>/dev/null | { cat; echo '15 3 * * * VMSD_BIN=/opt/vmsd/vmsd VMSD_CONFIG=/opt/vmsd/config/config.yaml /opt/vmsd/backup.sh /var/lib/vmsd/vmsd.db /var/backups/vmsd 14'; } | sudo crontab -
```

`backup.sh` SQLite'ni `VACUUM INTO` bilan issiq nusxa oladi (server
to'xtamaydi), butunlikni tekshiradi va eski nusxalarni soni bo'yicha
tozalaydi. Holat: `systemctl list-timers vmsd-backup.timer`; qo'lda
ishga tushirish: `make backup-now` yoki
`sudo systemctl start vmsd-backup.service`.

## 6b. Kuzatuv va ogohlantirishlar (2026-11-22)

- `GET http://127.0.0.1:8189/metrics` (ichki bind; `make metrics`) —
  Prometheus matni: uptime, agent/UI ulanishlari + handshake ok/fail,
  HTTP so'rovlar soni/status klasslari/davomiylik histogrammasi,
  kameralar, transcode'lar va `vmsd_tc_restarts`.
- `vms-alert.timer` (har 2 daqiqa) — `scripts/alert.sh`: server
  o'chiqmi, yaqinda qayta ko'tarilganmi (crash-loop), kameralar
  offline, agent flap, FFmpeg flap — holat O'ZGARGANDA `journalctl
  -t vms-alert`ga yozadi (spam yo'q; cooldown 60 daq, state
  `/var/lib/vms-alert`).
- Bir martalik tekshiruv: `vmsd -doctor` (`make doctor`) — config/DB/
  migratsiyalar/MediaMTX/portlar/ffmpeg/zaxira yangiligi/disk joyi.
  Agent tomonda: `vmsagent -doctor` (`make -C client doctor`).
- Uzoq sinov: `make soak` (HOURS=24 tunlik) — kill -9 in'ektsiyasi bilan
  tiklanish vaqtini o'lchaydi; natija `var/soak/*.csv`.

## 7. Jurnallar

- `vmsd`: `/var/log/vmsd/vmsd.log` (hajm chegarasi config'da:
  `log_max_mb`, `log_keep`) va systemd journal.
- `vms-media`: journal (`journalctl -u vms-media -f`).
- Agent (mijoz PC'sida): `/var/log/vmsagent/vmsagent.log` — alohida,
  serverniki bilan aralashmaydi.

## 8. Qisqa tekshir ro'yxati (10 band)

1. `systemctl status vms-media vmsd` — ikkalasi ham active.
2. `curl -s localhost:8081/healthz` → `{"ok":true...}`.
3. `ss -ltnp` — 9997/8189 faqat 127.0.0.1 (firewall.md).
4. Server UI'da `/admin` ochiladi, admin paroli ishlaydi.
5. Agent `Request access` yuboradi, tasdiqdan keyin `online`.
6. Kamera qo'shilganda ~30 s ichida serverda `online` va kodek ko'rinadi.
7. Preview ochilganda kadr keladi (WebRTC), `session_state` UI'da ko'rinadi.
8. `journalctl -u vmsd | grep media_stopped` — sababsiz to'xtash yo'q.
9. Zaxira skripti ishlaydi: `/var/backups/vmsd/vmsd-*.db` ochiladi (`sqlite3 ... "PRAGMA integrity_check;"`); taymer faol: `systemctl list-timers vmsd-backup.timer`.
10. Mashina qayta boshlansa ikkala unit ko'tariladi, agentlar qayta ulanadi.
11. `vmsd -doctor` va `vmsagent -doctor` — `0 FAIL` (ogohlantirishlar o'rnini toping); `journalctl -t vms-alert`da yangi ogohlantirish yo'q.
