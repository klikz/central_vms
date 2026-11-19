# Runbook — vmsagent (client) xato holatlari (§13, C4.10)

## Loglar qayerda

| Qayer | Nima |
|---|---|
| Terminal (journald) | `journalctl -u vmsagent -f` — barcha darajalar |
| **Alohida CLIENT fayli** | `/var/log/vmsagent/vmsagent.log` (`log_file`; rotatsiya `log_max_mb`/`log_keep`) |
| Server logi (aralashtirmang!) | `/var/log/vmsd/vmsd.log` — vmsd o'z faylida |
| Tezkor | `make logs` (barchasi), `make logs-err` (faqat `level=ERROR`) |

Har qator strukturali: `level=… msg=… camera_id=… publish_id=… reason=…`
(§10.2; parol/token hech qaysi sinkda ko'rinmaydi).

## Monitoring (hb ichida, §4.10)

Agent har 30 s `hb` yuboradi: `uptime_s`, `mem_mb`, `publishes`.
Server panelida yoki `journalctl` orqali kuzatiladi:

- `mem_mb` o'sib borayotgan bo'lsa (sizib chiqish) — agentni
  `systemctl restart vmsagent` qiling (state yo'qolmaydi) va logni tekshiring.
- `publishes` kutilganidan ko'p — TTL watchdog (`ttl_expired`) ishlamayaptimi,
  logda `media_stopped` qatorlarini ko'ring.

## Reason jadvali (§13) va birinchi harakat

| `reason` | Ma'no | Birinchi tekshiruv |
|---|---|---|
| `camera_offline` | RTSP ulanmadi (3 retry tugadi) | `nc -vz <kamera> 554`; kamera rejimi; PoE |
| `camera_rejected` | 401/404 | parol `secrets.bin`da to'g'rimi (UI'dan qayta kiriting), RTSP yo'li |
| `token_rejected` | WHIP/RTSP publish 401/403 | server token muddati; MediaMTX ACL |
| `publish_failed` | 3 urinishdan keyin | MediaMTX jonlimi (`8889/8554`), `deploy/client-firewall.md` |
| `codec_mismatch` | SDP kodek o'zgardi | `codec_fix` avtomatik boshlanadi; kamera profilini tekshiring |
| `bframe_unsupported` | High+B-frame | kamerada B-frame o'chiring (qo'llanma §8.1) |
| `transcode_failed` | FFmpeg ochilmadi | `ffmpeg -encoders \| grep nvenc`; drayver; `make logs-err` |
| `limit_exceeded` | `max_uploads` band | server limiti (§10.4) yoki eski publish'larni yopish |
| `ttl_expired` | watchdog (viewer yo'q) | normal — foydalanuvchi yopdi |
| `network` | server/MediaMTXga yetmaydi | VPN/routing; `nc -vz <vmsd> 8081` |
| `agent_restart`/`shutdown` | agent o'zi | normal (upgrade/restart) |
| `no_viewers`/`server_stop`/`user_close`/`page_hidden`/`idle_ttl` | normal | — |

## Siks holatlar

**1. "Server bilan aloqa uzildi" WARN qatorlari, keyin ERROR (3-urinish):**
server pastda yoki IP/port yopiq. `make logs-err` + `nc -vz <vmsd-ip> 8081`.
Agent avtomatik qayta urinishda (backoff 1→2→4→8 s; auth xatolarida 60 s).

**2. UI'da sariq "Protokol versiyasi mos emas" banneri:**
server `compat.ok=false` yubordi — agent yoki vmsd versiyasini yangilang
(`SSH_HOST=… make remote-update`).

**3. Kamera qizil, logda `camera_rejected`:**
parol almashgan. UI'da kamerani qayta qo'shing — keystore yangilanadi,
`secrets.bin` faylini zaxiralashni unutmang (`make backup`).

**4. Oqim 10 s ichida ochilmayapti (`failed`):**
`media_start` → `media_ready` yo'q. `publish_id` bo'yiki log qatorlarini
izlang: `grep <publish_id> /var/log/vmsagent/vmsagent.log`.

**5. FFmpeg yetim jarayonlari:**
`pgrep -af ffmpeg` — agent boshqaruvida bo'lmagan PID bo'lsa
`kill <pid>` va agentni restart qiling (§9.4 graceful shutdown FFmpeg'ni
o'zi o'ldiradi).

## Xizmat amallari

```bash
sudo systemctl status vmsagent      # Restart=always, RestartSec=5
sudo systemctl restart vmsagent     # graceful: media_stopped{agent_restart} → WS 1001 → state flush
sudo kill -HUP $(pidof vmsagent)    # config + log faylini qayta o'qish
make backup                         # §10.1 zaxira (agent.key+secrets.bin+state.json)
make restore ARCHIVE=<fayl>         # tiklash
```
