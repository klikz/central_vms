# Client tomonida firewall / chiqish portlari (§10.5, C4.9)

Agent **hech qanday tashqi kirish eshigini ochmaydi** — barcha aloqa faqat
chiqish (egress) yo'nalishida. Joy firewallida quyidagi chiqishlar ruxsat
etilsa yetarli:

| Yo'nalish | Port/protokol | Nima uchun |
|---|---|---|
| `vmsd` serveriga | **TCP 8081** | WS `/api/agent/ws` + ulanish so‘rovi (`/api/agent/join`) |
| MediaMTX WHIP/WHEP | **TCP 8889** | publish (WHIP) SDP almashinuvi |
| MediaMTX ICE | **UDP 8189** (fallback TCP 8189) | RTP media oqimi (STUN/TURN o'sha portda) |
| MediaMTX RTSP publish | **TCP 8554** | `rtsp_tcp` ingest zaxira zanjiri |
| Kameralar (joy ichida) | **TCP 554** | RTSP probe + pull + ANNOUNCE/RECORD |
| Kameralar (joy ichida) | **TCP 80 / 8080** | ONVIF / HTTP konfiguratsiya |
| Kameralar (joy ichida) | **UDP 3702** (multicast `239.255.255.250`) | WS-Discovery (kashf etish, §16) |

## Lokal UI (faqat joyga)

- `ui_bind` default `127.0.0.1:8082` — tarmoqqa chiqmaydi, firewall qoidasi
  talab qilinmaydi. Boshqa interfeysga bind qilinsa (masalan operator paneli
  uchun `0.0.0.0:8082`), **PIN sozlanishi shart** (§17) va faqat joy tarmog'iga
  ruxsat berilishi kerak.

## `ufw` namunasi (agent mashinasi — chiqishlar default ruxsat, kirishlar default rad)

```bash
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw enable
```

Ziyonat tarmog'ida (masalan VLAN orqali filtr) qo'shimcha:

```bash
# vmsd va MediaMTXga chiqish (ziyonat router tomonida)
iptables -A FORWARD -d 192.168.1.10 -p tcp --dport 8081 -j ACCEPT
iptables -A FORWARD -d 192.168.1.10 -p tcp --dport 8889 -j ACCEPT
iptables -A FORWARD -d 192.168.1.10 -p udp --dport 8189 -j ACCEPT
iptables -A FORWARD -d 192.168.1.10 -p tcp --dport 8554 -j ACCEPT
# kameralar segmentiga
iptables -A FORWARD -d 10.20.0.0/24 -p tcp -m multiport --dports 554,80,8080 -j ACCEPT
iptables -A FORWARD -d 10.20.0.0/24 -p udp --dport 3702 -j ACCEPT
```

## Tekshirish ro'yxati (yangi joyda)

```bash
nc -vz <vmsd-ip> 8081            # WS/HTTP
nc -vz <mtx-ip> 8889             # WHIP
nc -vzu <mtx-ip> 8189            # ICE (UDP — "succeeded" chiqmasligi normal)
nc -vz <mtx-ip> 8554             # RTSP publish
nc -vz <kamera-ip> 554           # RTSP
curl -s http://<kamera-ip>/onvif/device_service | head -c 200   # ONVIF
```

Agar biron biri yopiq bo'lsa: agent logida `network` reason ko'rinadi
(`make logs-err`), UI'da "Server bilan bog'lanish yo'q" banneri chiqadi.
