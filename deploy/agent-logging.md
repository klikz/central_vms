# Loglar: terminal + alohida fayllar (talab 2026-11-19)

## Qoida
Har bir jarayon xatolarini **bir vaqtda**:
1. **terminalga** (stderr) — batafsil: `level=ERROR`, xabar, `err=` zanjiri, kontekst maydonlari (camera_id, attempt…);
2. **o'z log fayliga** — hajm bo'yicha rotatsiyalanadigan.

**CLIENT va SERVER loglari HECH QACHON bitta faylga aralashtirilmaydi.**

| Jarayon | Log fayli (standart) | Sozlash |
|---|---|---|
| `vmsagent` (client) | `/var/log/vmsagent/vmsagent.log` | `log_file`, `log_max_mb` (4), `log_keep` (5) — `client.yaml` §11 |
| `vmsd` (server, S-bosqich) | `/var/log/vmsd/vmsd.log` | server konfiguratsiyasida alohida (server TASKS S0 talabi) |
| `devserver` (mock vmsd, dev) | `logs/devserver.log` | `-log` bayrog'i |

## Client (vmsagent)
- `log_level: debug|info|warn|error` — **ERROR darajasi har qanday sozlama bilan** terminalga ham, faylga ham tushadi (xato hech qachon yo'qolmaydi).
- Rotatsiya: fayl `log_max_mb` dan oshsa `vmsagent.log → .1 → .2 …`, eng eski `log_keep` tashqaridagisi o'chiriladi (disk to'lmaydi).
- `log_file: ""` bo'lsa — fayl yozuvi o'chadi (faqat terminal/journald).
- Konfig o'qilmagan bo'lsa ham xato **standart** CLIENT log yo'liga yoziladi; yo'l yozib bo'lmasa batafsil `ERROR` chiqadi va joriy katalogda `logs/vmsagent.log` zaxirasi urinish qilinadi.
- SIGHUP — config qayta o'qiladi, log fayli yo'li o'zgarsa qayta ochiladi (§9.4).
- §10.2 redaksiya: parol/token terminalga ham, faylga ham **yashirilgan** holda tushadi.

## systemd
`vmsagent.service` `StandardOutput=journal`da — journald ham to'liq oladi, fayl alohida. Ikkalasi bir xil mazmundagi qatorlar; qidiruv:
```bash
journalctl -u vmsagent -p err        # yoki
grep "level=ERROR" /var/log/vmsagent/vmsagent.log
```

## Server (vmsd) uchun talab (S-bosqichda bajariladi)
Server yozilganda shu tartib saqlanadi: stdlib `slog` → stderr + `/var/log/vmsd/vmsd.log` (rotatsiya `log_max_mb/log_keep`), agent logi bilan umumiy fayl YO'Q. `server/TASKS.md` S0 ga bu talab kiritilgan.
