# Devol (firewall) — Central VMS server uchun portlar (S4.4)

Serverda **faqat quyidagi** tarmoq eshiklari ochiq bo'lishi kerak.
Manbalar: `config/prod.yaml.example`, `mediamtx/mediamtx.yml`,
`docs/protocol.md` §2/§7/§15.

## Portlar

| Port | Protokol | Kim eshitadi | Kim kira oladi | Vazifa |
|---|---|---|---|---|
| 8081 | TCP (HTTP/WS) | `vmsd` | operator brauzeri + agentlar | UI, REST, agent WS (`/api/agent/ws`) |
| 8554 | TCP + UDP | MediaMTX | **faqat agentlar** | RTSP ingest (agent publish) va RTSP o'qish |
| 8889 | TCP + UDP | MediaMTX | operator brauzerlari | WHEP (WebRTC livoy) + ICE UDP |
| 8888 | TCP | MediaMTX | operator brauzerlari | HLS / LL-HLS fallback |
| 1935, 8890 | TCP | MediaMTX | **yo'q** — devor ortida qolsin | RTMP/SRT (ishlatilmaydi) |
| 9997 | TCP | MediaMTX API | **faqat 127.0.0.1** | vmsd control API |
| 8189 | TCP | vmsd auth-hook | **faqat 127.0.0.1** | MediaMTX → vmsd auth so'rovi |

Muhim:
- `8189` va `9997` hech qachon tashqariga ochilmaydi — ular orqali
  ruxsat/yo'l boshqaruvi ketadi.
- WebRTC UDP: MediaMTX `webrtcLocalUDPAddress` (dev'da `:8189`) bitta
  port; ICE host candidate shu portdan keladi. Agar server NAT ortida
  bo'lsa, tashqi manzilni `webrtcAdditionalHosts` orqali berish kerak.
- Agentlar uchun `8554` ni LAN/agent subnetiga cheklash xavfsizlik
  nuqtai nazaridan tavsiya etiladi (ulardan tashqari hech kim
  publish/o'qiy olmasin).

## Ubuntu (ufw) namunasi

```bash
ufw default deny incoming
ufw allow from 192.168.0.0/24 to any port 8081 proto tcp   # operator + agentlar
ufw allow from 192.168.0.0/24 to any port 8554             # RTSP (agent publish/o'qish)
ufw allow from 192.168.0.0/24 to any port 8889             # WHEP (brauzerlar)
ufw allow from 192.168.0.0/24 to any port 8889 proto udp   # WebRTC ICE
ufw allow from 192.168.0.0/24 to any port 8888 proto tcp   # HLS fallback
# 9997/8189 ochilmaydi (127.0.0.1 ichki aloqa)
ufw enable
```

Tashqi (internet) kirish kerak bo'lmasa, yuqoridagi `from` qatorlari
LAN subneti bilan cheklangan holdi qoldiring; TLS kerak bo'lsa oldiga
Caddy/nginx qo'yiladi (root §12: TLS — keyingi bosqich).

## Tekshirish

```bash
ss -ltnp | grep -E ':(8081|8554|8888|8889|9997|8189)\b'
# 9997 va 8189 faqat 127.0.0.1 da bo'lishi kerak
```
