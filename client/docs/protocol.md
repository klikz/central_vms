# Central VMS — kontrakt: agent (`vmsagent`) ↔ server (`vmsd`)

**Versiya: 1.0-draft.7** · Sana: 2026-11-20 · Holat: **client tomonida yozildi, muzlatildi (frozen)**

Bu fayl **yagona manba** (single source of truth). Server S0 bosqichida bu fayl `docs/protocol.md`
ga ko'chiriladi (yoki symlink bilan bog'ladi) va undan keyin o'zgarish faqat versiya ko'tarib,
`§20 Changelog` ga yozib bajariladi. Server `server/REJA.md` §2 va root `REJA.md` §6–§7 shu
faylga bo'ysunadi; ziddiyat bo'lsa **shu fayl** kuchli.

Aniqlangan asosiy tuzatishlar (eski root §7.1 matniga nisbatan):
- MediaMTX auth-hook `action` ro'yxati: `publish|read|playback|api|metrics|pprof` (`update` YO'Q).
- Token uzatish: RTSP'da URL query (`?token=`), **WebRTC/WHIP/WHEP'da `Authorization: Bearer` header**.
- Token "bir martalik" emas: **sessiya/TTL davomida bir publish uchun qayta ishlatiladi** (WHIP POST+PATCH+DELETE).
- `media_start` da `whip_url` yetarli emas — `publish.urls` bloki to'liq beriladi (§5.2).
- Kamera `id` ni **agent** yaratadi (UUIDv4, o'zgarmas) — §6.

---

## 1. Versiyalash va moslik

| Maydon | Qiymat |
|---|---|
| `protocol_version` | butun son (hozir **1**) |
| `agent_version` | `git describe --tags` (ldflags) |

- Agent `hello.protocol_version = 1` yuboradi. Server `min_supported…max_supported` oralig'ida
  bo'lmasa `welcome.compat {ok:false, server_version, supported:[min,max]}` yuboradi va
  **connection'ni 5 s ichida yopadi (code 4001)**. Agent `4001`ni olgach qayta urinishni
  sekinlashtiradi (backoff 5→10→20…300 s) va lokal UI'da aniq xato ko'rsatadi.
- Kontrakt qoidasi: **yangi maydonlar ixtiyoriy (optional) qo'shiladi, mavjud maydon o'chirilmaydi**.
  Noma'lum `type`li xabar qabul qilinsa — jimgina e'tiborga olinmaydi (forward-compatible).

## 2. Transport va umumiy qoidalar

- Bitta chiqish WebSocket: `ws://SERVER:8081/api/agent/ws?agent_id={agent_id}` (vmsd HTTP/WS API porti
  **default 8081**, config orqali sozlanadi; TLS hozircha yo'q → `ws://`, keyin `wss://`).
- Envelope (har xabar):
  ```json
  { "type": "cams_sync", "seq": 42, "ts": "2026-11-19T10:00:00Z", "data": { ... } }
  ```
  - `seq` — agent tomonidan 1 dan boshlab o'sib boradi; server `welcome.last_seq` qaytaradi.
  - `ts` — UTC RFC3339 (serverda UTC saqlanadi, UI `+05` ko'rsatadi).
  - `data` — turiga xos obyekt (yuqoridagi misollarda `data` ichidagi maydonlar ko'rsatilgan).
- O'lcham: bitta xabar ≤ **1 MiB**; `cams_sync` chunk'lari ≤ 900 KiB.
- Heartbeat: server har **30 s** `ping` → agent `pong`. Agent har **5 s** ichida javob berishi shart
  (server 30 s javobsiz → client `offline`).
- Ping/pong tashqarisida agent **idle heartbeat** yuboradi: `type: "hb"` (har 30 s) — server
  `clients.last_seen_at` shundan yangilanadi.
- Ketma-ketlik: agent barcha buyruqlarni **accept-then-execute** qiladi — darhol qabul qiladi,
  bajarish natijasi alohida xabar bilan (`media_ready` / `media_stopped`).

## 3. Enrollment (birinchi ulanish)

### 3.1 HTTP endpoint (serverda ochiq, auth talab qilmaydi)

```
POST /api/agent/enroll
Content-Type: application/json

{ "code": "41739265",             // admin yaratgan bir martalik kod (8 xonali son, 24 soat)
  "hostname": "joy-oxangaron-01",
  "os": "Ubuntu 24.04",
  "lan_ip": "192.168.10.20",
  "agent_version": "0.3.1",
  "caps": { "gpu": "nvenc|vaapi|amf|none", "cpu_cores": 8, "ram_mb": 8192,
            "ffmpeg_version": "n9.0.2" } }

200:
{ "agent_id": "ag_9f2c1a7e",
  "agent_secret": "<base64url(32 bait tasodifiy)>",
  "server_time": "2026-11-19T10:00:00Z",
  "protocol_version": 1 }

401 { "error": "bad_code" }        // kod noto'g'ri/muddati o'tgan/ishlatilgan
409 { "error": "already_enrolled" } // bu kod allaqachon bitta agentga biriktirilgan
429 { "error": "rate_limited", "retry_after_s": 300 }
```

Qoidalar:
- Kod formati: **8 xonali raqamli son** (masalan `41739265`) — telefon orqali aytish
  va terib kiritish oson bo‘lishi uchun (1.0-draft.7; eski 19 belgili aralash format
  bekor qilindi). Server qabul qilishda bo‘sh joy, tire va pastki chiziqni yuboradi
  (`4173 9265` ham ishlaydi). Entropiya 10^8; urinishlar IP bo‘yicha 5 ta / 5 daqiqa.
Qoidalar:
- Kod **bir marta** ishlatiladi: muvaffaqiyatli enroll'dan keyin yopiladi. `already_enrolled`
  bo'lsa admin yangi kod yaratadi (lokal UI'da aniq xabar).
- Rate-limit: **5 urinish / 5 min / IP**.
- `agent_secret` DB'da **sha256 hash** saqlanadi, faqat enroll javobida bir marta chiqadi.
- `agent_id` ni **server** beradi (`ag_` + 8 hex); `client_id` (registry) server ichida
  `agent_id` orqali topiladi.
- Enroll'dan keyin admin `name`/`site` ni belgilaydi; agent ismni **hech qachon o'zi yubormaydi**.


**Kod qayerdan:** `vmsagent -enroll <KOD>` (bir marta, keyin o'chiriladi) yoki lokal
UI'dagi **Sozlamalar → Ulanish (enroll)** orqali. Kod diskda saqlanmaydi.

### 3.2 Agent tomonida saqlash

`/var/lib/vmsagent/state.json` (0600, atomik yozish: `*.tmp` → `rename`):
```json
{ "schema": 1,
  "server_url": "ws://192.168.1.10:8081",
  "agent_id": "ag_9f2c1a7e",
  "secret_ref": "agent_secret_enc",   // §10.1: parol/secret state.json'da YO'Q — faqat ref
  "cams_epoch": 7,
  "cameras": [ { "id": "UUIDv4", "name": "...", "host": "...", "port": 554,
                 "main_path": "/cam/realmonitor?channel=1&subtype=0",
                 "sub_path":  "/cam/realmonitor?channel=1&subtype=1",
                 "onvif_xaddr": "http://...:80/onvif/device_service",
                 "enabled": true, "position": 12,
                 "codec_main": "h264", "codec_sub": "h264",
                 "bframe_main": false, "bframe_sub": false,
                 "status": "online", "last_check_at": "...", "codec_probe_at": "..." } ] }
```
Parollar va `agent_secret` alohida **shifrlangan** faylda (§11).

### 3.3 WS auth qo'l berishish (handshake)

```
agent → server : { "type": "auth", "data": { "agent_id": "ag_...", "secret": "..." } }   // ulanishdan keyin ≤5 s
server → agent : { "type": "welcome", "data": { ... } }    // §4.1, yoki 4001/4003 bilan yopiladi
```
- `auth` kelmasa server 5 sdan keyin yopadi (code **4002**).
- Secret noto'g'ri → code **4003** (`agent_revoked` bo'lsa `4004`). Agent 4003'da backoff'ni
  60 s dan boshlaydi va lokal UI'da "secret rad etildi — qayta enroll qiling" deydi.

## 4. Xabarlar: agent → server

### 4.1 `hello` (auth'dan keyin birinchi)
```json
{ "protocol_version": 1, "agent_version": "0.3.1", "os": "Ubuntu 24.04",
  "hostname": "joy-oxangaron-01", "lan_ip": "192.168.10.20",
  "boot_id": "b7d3…",                  // har ishga tushishda yangi (journal bog'lash uchun)
  "started_at": "2026-11-19T09:58:11Z",
  "cams_epoch": 7,                     // §6.4
  "caps": { "gpu": "nvenc", "cpu_cores": 8, "ram_mb": 8192,
            "ffmpeg_version": "n9.0.2", "hevc_decode": true } }
```
Server javobi — `welcome`:
```json
{ "server_version": "0.5.2", "protocol_version": 1, "compat": { "ok": true },
  "last_seq": 128, "cams_epoch": 7,
  "limits": { "max_uploads": 8, "max_local_transcodes": 4, "media_ttl_s": 60 },
  "settings": { "probe_pacing_parallel": 5, "codec_probe_interval_h": 24,
                "log_level": "info" },
  "needs_full_sync": false }
```
`welcome.cams_epoch != hello.cams_epoch` yoki `needs_full_sync: true` → agent to'liq `cams_sync` yuboradi.

### 4.2 `cams_sync` (chunk'lab, ≤100 kamera/xabar)
```json
{ "epoch": 8, "full": true, "seq_no": 1, "chunk_count": 2, "done": false,
  "cameras": [ { "op": "upsert", "id": "UUIDv4", "name": "Kirish-01",
                 "rtsp_masked": "rtsp://192.168.10.31:554/cam/realmonitor?channel=1&subtype=0 (user=***)",
                 "profiles": { "main": "...", "sub": "..." },
                 "codecs": { "main": "h264", "sub": "h264" },
                 "bframe": { "main": false, "sub": false },
                 "onvif_ok": true, "status": "online", "position": 12, "enabled": true } ] }
```
- `op: "upsert" | "delete"` — **o'chirish shu kanal orqali** (`cams_delete` alohida xabar YO'Q).
- `full: true` bo'lsa server uchun semantika: "bu clientning to'liq kameralar to'plami" —
  `done: true` kelgach ro'yxatda bo'lmagan barcha kameralar serverda o'chiriladi
  (cascade: `favorites`, `layouts.slots` bo'shatiladi).
- `epoch` o'sgan bo'lsa server eski `epoch`li xabarlarni rad etadi (`cams_ack {ok:false, reason:"stale_epoch"}`).

### 4.3 Server javobi: `cams_ack` (server → agent)
```json
{ "epoch": 8, "ok": true, "applied": 137, "reason": null }
```
Agent `cams_ack` olmasa 10 sdan keyin chunk'ni qayta yuboradi (3 marta), bo'lmasa lokal UI'da xato.

### 4.4 `cam_status`
```json
{ "camera_id": "UUID", "status": "online|offline|error",
  "error": "connection_refused", "checked_at": "...", "rtt_ms": 42 }
```
Event-driven (holat o'zgarganda); davomiy `error` holatida 5 min'da bir marta takrorlanadi.

### 4.5 `codec_probe`
```json
{ "camera_id": "UUID", "codecs": { "main": "h264", "sub": "h265" },
  "bframe": { "main": true, "sub": false },
  "profiles_sdp": { "main": "High", "sub": "Baseline" },
  "onvif_switch_ok": false, "reason": null }
```

### 4.6 `media_ready` / `media_stopped` / `media_stats`
```json
// media_ready
{ "session_id": "se_…", "camera_id": "UUID", "profile": "sub", "kind": "play",
  "publish_id": "UUID",                 // §9 refcount: bitta publish uchun bir marta
  "path": "cam/ag_9f2c1a7e/UUID",
  "ingest": "whip|rtsp_tcp", "transport": "udp|tcp",
  "transcoded": false, "backend": "none|gpu|cpu",
  "fps": 10.0, "bitrate_kbps": 700, "bytes_sent": 0, "ready_ms": 640 }

// media_stopped
{ "session_id": "se_…", "camera_id": "UUID", "publish_id": "UUID",
  "reason": "no_viewers|ttl_expired|camera_offline|publish_failed|token_rejected|
             camera_rejected|network|codec_mismatch|agent_restart|shutdown|server_stop|
             bframe_unsupported|transcode_failed|limit_exceeded" }

// media_stats (har 10 s, faqat faol publish'lar bo'lsa)
{ "publishes": [ { "publish_id": "UUID", "camera_id": "UUID", "bytes_sent": 1048576,
                   "fps": 9.8, "bitrate_kbps": 690, "rtt_ms": 18,
                   "packet_loss_pct": 0.2, "transport": "udp" } ] }
```

### 4.7 `discovery_done` (§10.2) va `selftest_done` (§10.4)
```json
{ "scan_id": "UUID", "mode": "onvif|rtsp_scan", "timeout_s": 10,
  "found": [ { "mac": "aa:bb:cc:dd:ee:ff", "serial": "KX5C…", "ip": "192.168.10.31",
               "port": 554, "http_port": 80, "vendor": "Dahua", "model": "IPC-HFW1230S",
               "onvif_xaddr": "http://192.168.10.31:80/onvif/device_service",
               "rtsp_probe_ok": true, "codecs": { "main": "h265", "sub": "h264" },
               "already_known": true, "auth_required": true } ],
  "errors": [ "cidr 10.0.0.0/8: skipped (too large)" ] }

{ "scan_id": "UUID", "checks": [ { "name": "server_ws", "ok": true, "ms": 24 },
  { "name": "whip_udp", "ok": true, "ms": 31 }, { "name": "whip_tcp", "ok": false, "ms": 0 },
  { "name": "rtsp_publish_tcp", "ok": true, "ms": 12 },
  { "name": "camera:UUID", "ok": true, "ms": 35, "bitrate_kbps": 700 } ] }
```

### 4.8 `hb`, `pong`
`hb { uptime_s, publishes, mem_mb, cams_total, cams_online }` — yengil holat heartbeat'i.

## 5. Xabarlar: server → agent

### 5.1 `cams_request`
```json
{ "full": true }        // yoki { "full": false, "camera_ids": ["UUID", ...] }
```

### 5.2 `media_start` — **to'liq maydonlar (shu kontrakt bilan tugallandi)**
```json
{ "session_id": "se_7c1f…",
  "camera_id": "UUID",
  "profile": "sub",                       // sub | main
  "kind": "play|preview",
  "ttl_s": 60,                            // publish TTL (watchdog, §9.2)
  "ingest": "whip|rtsp_tcp",              // H.264→whip, H.265→rtsp_tcp (majburiy)
  "publish": {
    "token": "<base64url(32 bait)>",      // §7.1
    "urls": {
      "whip":     "http://SERVER:8889/cam/ag_9f2c1a7e/UUID/whip",
      "rtsp":     "rtsp://SERVER:8554/cam/ag_9f2c1a7e/UUID",
      "rtsp_tc":  "rtsp://SERVER:8554/tc/ag_9f2c1a7e/UUID"   // faqat transcode.by=client bo'lsa
    },
    "header_name": "Authorization",       // ixtiyoriy, faqat informativ: WHIP'da `Authorization: Bearer <token>`
    "query_name": "token"                 // ixtiyoriy, faqat informativ: RTSP'da URL'ga `?token=<token>`
                                          // (xatti-harakat §7.2 bo'yicha QAT'IQ — bu maydonlarga bog'liq emas)
  },
  "transcode": {
    "mode": "auto|server|client|off",
    "by":   "server|client|none",
    "backend": "gpu|cpu",                 // by=client bo'lsa agent o'zi tanlamaydi — server `caps` dan beradi
    "params": { "width": 1280, "height": 720, "fps": 10, "bitrate_kbps": 800,
                "maxrate_kbps": 1000, "gop_s": 2, "profile": "baseline" } },
  "reason": "user_open|preview_open|retry" }
```
- `publish.urls` dan **faqat `ingest` ga mos biri** ishlatiladi; qolganlari yuborilishi shart emas.
- `ingest: whip` → `urls.whip` + Bearer header. `ingest: rtsp_tcp` → `urls.rtsp` + `?token=`.
- `transcode.by = client` bo'lsa: manba H.265, natija H.264 → `urls.rtsp_tc` **yo** `urls.whip`
  (qaysi biri kelgani agent aniqlaydi; default: `whip`).
- Server `media_start` yuborib **10 s** ichida `media_ready` kutadi (§12 taymaut).

### 5.3 `media_stop`
```json
{ "session_id": "se_…", "camera_id": "UUID", "reason": "no_viewers|user_close|page_hidden|idle_ttl" }
```

### 5.4 `media_refresh` (TTL uzaytirish, har 30–60 s)
```json
{ "session_id": "se_…", "camera_id": "UUID", "extend_s": 60 }
```

### 5.5 `codec_fix`
```json
{ "camera_id": "UUID", "target": { "profile": "sub", "codec": "h264",
        "no_bframes": true, "width": 1280, "height": 720, "fps": 10,
        "bitrate_kbps": 700, "gop_s": 2 },
  "apply_all": false }        // true → bir xil modeldagi barcha kameralarga (bulk)
```
Agent javobi: `codec_probe` (natija bilan) + `codec_fix_done {camera_id, ok, reason}`.

### 5.6 `secret_rotate`
```json
{ "new_secret": "<base64url(32 bait)>", "grace_s": 300 }
```
Agent: atomik saqlaydi → **darhol uzilishni boshlamaydi**; keyingi qayta ulanishda yangi secret
ishlatiladi. Server eski secretni `grace_s` dan keyin bekor qiladi. Muvaffaqiyat: `secret_rotated {ok:true}`.

### 5.7 `config_push`
```json
{ "limits": { "max_uploads": 8, "max_local_transcodes": 4 },
  "settings": { "probe_pacing_parallel": 5, "codec_probe_interval_h": 24, "log_level": "info" } }
```
Agent `ok:true` bilan javob beradi; qiymatlar `client.yaml` dan **ustun** (server — yagona sozlash manbai).

### 5.8 `ping`, `error`
`error { code, message, session_id?, camera_id? }` — agent xatoni log + lokal UI'ga chiqaradi.

## 6. Kamera identifikatori (agent yaratadi)

1. Kamera qo'shilganda (lokal UI, bulk add, discovery) agent **UUIDv4** yaratadi (`crypto/rand`).
2. `id` **hech qachon o'zgarmaydi**: nom/host/port o'zgartirilsa ham `id` saqlanadi →
   serverdagi `favorites` va `layouts.slots` buzilmaydi.
3. Kamera o'chirilsa → `cams_sync {op:"delete", id}`; `id` qayta ishlatilmaydi.
4. Server `cameras.id` — shu UUID (PK). `cameras.client_id` bilan bog'lanadi.
5. **Epoch mexanizmi:** har lokal o'zgarishda `cams_epoch` o'sadi; `hello` da yuboriladi,
   server `welcome` da o'z saqlagan qiymatini qaytaradi. Farq qilsa → to'liq `cams_sync`.
   Bu qayta ulanishdan keyingi drift'ni arzon yopadi (har soatda to'liq sync YO'Q).
6. Zaxira tiklanganda (`state.json`) `id` lar saqlanadi — layout/favorites saqlanib qoladi.

## 7. Token va URL formatlari

### 7.1 Token semantikasi (tuzatilgan)
- 256-bit tasodifiy, `base64url` (43 belgi), DB'da **sha256 hash**.
- **Bir martalik EMAS**: token mint qilingan paytdan `ttl` gacha (publish uchun `media_start.ttl_s`,
  o'qish uchun 30 s) **bir path + bir action** doirasida qayta ishlatilishi mumkin.
  Sabab: WHIP/WHEP'da bitta sessiya POST(SDP) + PATCH(ICE) + DELETE(yopish) so'rovlaridan iborat.
- Qayta ishlatish cheklovi: token faqat **bitta peer-connection/session** ga bog'lanadi
  (birinchi so'rov `session_id` ni bog'laydi, keyingilari shu session'ga tegishli bo'lishi shart).

### 7.2 Uzatish usuli (protokol bo'yicha)
| Protokol | Qanday |
|---|---|
| RTSP (publish/read) | URL query: `rtsp://host:8554/cam/…?token=T` |
| WHIP / WHEP (HTTP) | `Authorization: Bearer T` header (MediaMTX shu usulni qo'llab-quvvatlaydi) |
| LL-HLS fallback | `Authorization: Bearer T` yoki `?token=` (hls.js `xhrSetup` orqali) |

### 7.3 URL namunasi
```
publish WHIP   : POST http://SERVER:8889/cam/{agent_id}/{camera_id}/whip   (+Bearer)
publish RTSP   : rtsp://SERVER:8554/cam/{agent_id}/{camera_id}?token=T     (ANNOUNCE→SETUP(interleaved)→RECORD)
transcode RTSP : rtsp://SERVER:8554/tc/{agent_id}/{camera_id}?token=T
browser play   : POST http://SERVER:8889/(cam|tc)/{agent_id}/{camera_id}/whep (+Bearer)
```
`webrtcAdditionalHosts` ga server static IP'si yoziladi; agent uchun ahamiyati: WHIP URL'da
host to'g'ri bo'lishi kerak (aks holda ICE relay orqali bo'lib kanal yukini oshiradi).

## 8. Ingest/publish kutubxonalari (tanlov — tasdiqlangan)

**Qoida: sof Go, iloji boricha FFmpeg subprocess'siz.**

| Vazifa | Kutubxona | Izoh |
|---|---|---|
| Kameradan RTSP pull | **`bluenviron/gortsplib` v4** | H.264/H.265, TCP interleaved, `GET_PARAMETER`/`OPTIONS` keepalive, SDP parsing |
| H.264 → WHIP publish | **`pion/webrtc` v4** (+ `pion/interceptor`) | H.264 packetizer mavjud |
| H.265 → RTSP/TCP publish | **`bluenviron/gortsplib` v4** (ANNOUNCE + `mediacommon` H.265 depacketizer/packetizer) | MediaMTX bilan bir xil RTSP implementatsiyasi → eng kam interop riski |
| Remux `-c copy` (profile/level muammosi) | **FFmpeg** subprocess | `hevc_cuvid/hevc → h264_nvenc/vaapi/libx264` |
| B-frame re-encode, client transcode | **FFmpeg** subprocess | yagona FFmpeg yozish qoidasi (§8.2) |
| Zaxira publish (Go xatosi/interop) | **FFmpeg** `-f rtsp` / `-f whip` | faqat Go yo'li ishlamasa |

**Sabab (majburiy eslatma):** `pion/webrtc` v4'da **H.265 (HEVC) packetizer'i YO'Q** — media qatori
faqat `Opus, PCM, H264, VP8, VP9` (rasmiy README). Shuning uchun **H.265 hech qachon WHIP orqali
yuborilmaydi**; H.265 ingest yo'li — RTSP/TCP (`gortsplib`). MediaMTX'da H.265 WHIP ingest'i
nisbatan yangi, RTSP esa barqaror (root §17).

### 8.1 Qaror matritsasi
| Holat | Ingest | Mexanizm |
|---|---|---|
| H.264, B-frame yo'q, profile ≤ High | `whip` | gortsplib pull → pion/webrtc publish (UDP) |
| H.264, UDP bloklangan | `rtsp_tcp` | gortsplib ANNOUNCE/RECORD (TCP) |
| H.264, `High` + B-frame, ONVIF yo'q | `whip` | FFmpeg re-encode (`-profile:v baseline -bf 0`) → WHIP |
| H.265, brauzer HEVC yo'q, `transcode.by=server` | `rtsp_tcp` | gortsplib ANNOUNCE (H.265, TCP) → server FFmpeg |
| H.265, `transcode.by=client` | `whip` (yoki `rtsp_tcp`) | FFmpeg `hevc_cuvid/vaapi → h264_nvenc/vaapi/libx264` |
| H.265, brauzer HEVC bor | `rtsp_tcp` | gortsplib ANNOUNCE, transcode yo'q |

### 8.2 FFmpeg yozish qoidalari (barcha FFmpeg chaqiruvlari uchun)
- `-hide_banner -loglevel error -nostats -progress pipe:1` → `progress` dan `out_bytes`,
  `fps`, `bitrate` o'qiladi (`media_stats`).
- `-analyzeduration 500000 -probesize 500000` (tez ochilish), `-rtsp_transport tcp`,
  `-max_delay 500000`, `-fflags +discardcorrupt`.
- H.264 baseline: `-c:v libx264 -preset veryfast -tune zerolatency -profile:v baseline -bf 0 -pix_fmt yuv420p -g 20 -r 10`.
- GPU: `-c:v h264_nvenc -preset p1 -tune ll -rc cbr -b:v 800k -maxrate 1M -bufsize 1M -g 20 -profile:v baseline`
  (yoki `h264_vaapi`).
- Subprocess boshqaruvi: `exec.CommandContext`, `Wait()` goroutine, **yetim (zombie) processlar
  taqiqlangan**; agent o'lsa `SIGKILL` zanjiri (`cmd.Process.Kill` + process group).
- FFmpeg versiyasi tekshiriladi: `-f whip` muxer **FFmpeg ≥ 7.1** da bor (bizning dev muhitda 9.0.2 — mavjud).

## 9. Publish refcount, TTL watchdog, graceful shutdown

### 9.1 Refcount (agent tomonida)
- `publish_key = camera_id + profile + ingest` → bitta publish, `sessions: [session_id…]`.
- Birinchi `media_start` → publish ochiladi → `media_ready` (bir marta, `publish_id` bilan).
- Qo'shimcha `media_start` (2- user, yoki `preview` + `play`) → **yangi publish ochilmaydi**,
  faqat refcount oshadi; `media_ready` takrorlanmaydi.
- `media_stop` → refcount kamayadi; **0 bo'lsa 1 s kutiladi** (tezkor qayta ochilishlarga chidamli),
  keyin publish yopiladi → `media_stopped {reason:"no_viewers"}`.
- `kind` (play/preview) va `profile` har xil bo'lsa — **har xil publish** (`sub` va `main` alohida).

### 9.2 TTL watchdog
- Har publish uchun `deadline = now + ttl_s`; `media_refresh` `deadline` ni siljitadi.
- Har 5 s tekshiriladi; `deadline` o'tsa → publish yopiladi → `media_stopped {reason:"ttl_expired"}`.
- **WS uzilgan bo'lsa ham watchdog ishlaydi** (joy kanali band qolmasin).
- `ttl_s` default **60 s** (server `welcome.limits.media_ttl_s` orqali sozlaydi).

### 9.3 Retry (server taymautiga yetmasdan)
- Publish ochilishi yoki uzilishda: **1 s / 2 s / 4 s** (3 urinish, jami ~7 s) — 10 s taymautga yetmaydi.
- Muvaffaqiyatsiz → `media_stopped {reason:"publish_failed"}` (yoki aniqroq reason, §13).
- Kamera RTSP uzilsa: 3 marta qayta ulanish (2 s interval), bo'lmasa `camera_offline` + `media_stopped {reason:"camera_offline"}`.

### 9.4 Graceful shutdown
- `SIGTERM` (systemd `TimeoutStopSec=10`): barcha publish'larni yopish → `media_stopped
  {reason:"agent_restart"}` (server `sessions.state → stopping/ended`) → WS'ni `1001` bilan yopish →
  `state.json` ni flush qilish → chiqish.
- `SIGINT` shu yo'l; `SIGHUP` → config qayta o'qish (publish'lar uzilmaydi).
- Panika: `recover` → journald'ga stack → `systemd Restart=always` (RestartSec=5).

## 10. Lokal holat, xavfsizlik, redaksiya

### 10.1 Parol store
- `/etc/vmsagent/agent.key` — 32 bait tasodifiy, **0600**, `make install` da yaratiladi (bo'lmasa).
- `/var/lib/vmsagent/secrets.bin` — AES-256-GCM, kalit yuqoridagi fayl; format `gcm1:<b64(nonce||ct)>`.
- `state.json` da parol **yo'q** — faqat `secret_ref: "cam:<uuid>:main"`.
- Zaxira: `agent.key` + `secrets.bin` + `state.json` **uchala** birga olinadi (aks holda tiklanmaydi).
- `agent_secret` ham shu faylda, `agent_secret_enc` ref'i ostida. **state.json'da
  faqat `secret_ref: "agent_secret_enc"` yoziladi** — shifrlangan matn ham state.json'ga
  tushmaydi (1.0-draft.4 da aniqlashtirildi; §3.2 avvalgi yozuvi xato edi).

### 10.2 Log redaksiyasi (majburiy)
- Har log xabarida parol/secret/token almashtiriladi: `rtsp://u:****@host/…`, `token=****`,
  `agent_secret=****`. Redaktor `internal/redact` — bitta funksiya, **barcha** log/output/shelfa
  xabarlari shu orqali chiqadi.
- Xato xabarlari (UI'ga ko'rsatiladigan) ham redaktor orqali o'tadi.

### 10.3 Lokal UI bind va himoyasi
- Bind: `127.0.0.1:8082` (config'da o'zgartirib bo'lmaydi; LAN'ga chiqarish faqat `--allow-lan`
  + `ui_pin` **majburiy** holatda).
- `ui_pin` (4–8 belgi) bo'lsa barcha yozuv endpoint'lari PIN bilan (`X-Agent-Pin`); PIN
  `secrets.bin` da hash holda. Sessiya tokeni `HttpOnly` cookie, 12 soat.

### 10.4 Resurs chegaralari (defensive)
- `max_uploads` (server `config_push`) — agent o'zi ham tekshiradi: limit band bo'lsa
  `media_stopped {reason:"limit_exceeded"}` (server limitidan mustaqil ikkinchi himoya).
- `max_local_transcodes` (GPU 8 / CPU-only 4) — FFmpeg transcode navbati.
- FFmpeg processlari soni ≤ `max_local_transcodes + 2` (remux/re-encode hisobga olingan).
- `LimitNOFILE=4096`; xotira nishoni: bo'sh holatda ~30 MB, 100 kamera + 8 publish ≤ 150 MB.

### 10.5 Client tomonida ochiq portlar (faqat chiqish)
| Yo'nalish | Port/protokol |
|---|---|
| server → `vmsd` | TCP 8081 (`/api/agent/ws`, `/api/agent/enroll`) |
| server → MediaMTX WHIP | TCP 8889 (HTTP) |
| server → MediaMTX ICE | **UDP 8189** (fallback: TCP 8189) |
| server → MediaMTX RTSP publish | TCP 8554 |
| joy ichida (kameralarga) | TCP 554 (RTSP), TCP 80/8080 (ONVIF/HTTP), UDP 3702 (WS-Discovery, multicast) |

## 11. `client.yaml` to'liq sxemasi

```yaml
server_url: ws://192.168.1.10:8081      # ws:// yoki wss://
ui_bind: 127.0.0.1:8082
ui_pin: ""                              # bo'sh = PIN yo'q (faqat localhost holatida ruxsat)
state_dir: /var/lib/vmsagent
key_file: /etc/vmsagent/agent.key
log_level: info                         # debug|info|warn|error
probe:
  status_parallel: 5                    # pacing (root §16.4)
  status_keepalive: auto                # auto|get_parameter|options|describe
  codec_probe_interval_h: 24
  full_probe_on_publish_fail: true      # §12.3
limits:
  max_uploads: 8                        # server config_push bilan yangilanadi
  max_local_transcodes: 4
onvif:
  enabled: true                         # ixtiyoriy modul
  switch_codec: true                    # SetVideoEncoderConfiguration urinishi
discovery:
  cidrs: ["192.168.10.0/24"]            # rtsp_scan uchun; /22 dan keng bo'lsa rad etiladi
  onvif_timeout_s: 10
  rtsp_scan_ports: [554, 8554, 10554]   # RTSP bo'yicha qidiruv portlari
  max_hosts: 1024
publish:
  whip_fallback: rtsp_tcp               # UDP ishlamasa
  ffmpeg_fallback: true                 # Go yo'li ishlamasa (8-bob)
```

## 12. Probe siyosati (agent)

1. **Status:** arzon probe — `GET_PARAMETER` (RTSP keepalive). Ba'zi firmware (eski Dahua)
   `GET_PARAMETER`ni qo'llab-quvvatlamaydi → avtomatik **`OPTIONS`**, u ham bo'lmasa qisqa
   `DESCRIBE`. Tanlangan usol `cam_status.error` da ko'rinadi (`keepalive_unsupported`).
2. **Pacing:** bir vaqtda **5 parallel** probe; 100 kamera uchun to'liq sikl ~20 daqiqa
   (har 60 s'da bir sikl boshlanadi). Sikl davomiyligi log'da: `probe_cycle_s`.
3. **Kodek probe:** kamera ulanganda + kuniga 1 marta + **publish muvaffaqiyatsiz bo'lsa darhol**
   (`full_probe_on_publish_fail`) — kodek SDP'da o'zgargan holatini yopadi.
4. Probe'lar **hech qachon** publish'ni to'xtatmaydi (ayni bir kamerada bo'lsa ham kechiktiriladi).

## 13. Xato kodlari (reason)

| `reason` | Qachon | UI xabari |
|---|---|---|
| `camera_offline` | RTSP ulanib bo'lmadi | "Kamera javob bermayapti" |
| `camera_rejected` | 401/404 (parol/noto'g'ri yo'l) | "Kamera paroli yoki RTSP yo'li xato" |
| `token_rejected` | publish'da 401/403 | "Ruxsat rad etildi (token)" |
| `publish_failed` | 3 urinishdan keyin | "Oqimni yubolib bo'lmadi" |
| `codec_mismatch` | SDP'da kutilgan kodek yo'q | "Kodek o'zgardi, qayta tekshirilmoqda" |
| `bframe_unsupported` | High+B-frame, tuzatib bo'lmadi | "Kamerada B-frame o'chirilsin (qo'llanma)" |
| `transcode_failed` | FFmpeg ochilmadi/encoders yo'q | "Transcode ishlamadi (joy GPU/CPU)" |
| `limit_exceeded` | `max_uploads` band | "Joy upload chegarasi band" |
| `ttl_expired` | watchdog | (foydalanuvchiga ko'rsatilmaydi) |
| `network` | server/MediaMTXga yetmaydi | "Server bilan bog'lanish yo'q" |
| `agent_restart` / `shutdown` | agent o'zi | — |
| `no_viewers` / `server_stop` / `user_close` / `page_hidden` / `idle_ttl` | normal | — |

## 14. Sessiya holat mashinasi (moslashtirilgan, root §7.1)

```
pending ──media_start──► [agent] publishing ──media_ready──► ready ──WHEP reader≥1──► playing
   │                          │                                │
   │                          └── 10 s taymaut ────────────────┴──► failed
   └── limit/registry tekshiruvi ──► rejected (UI: sabab + "qayta urinish")
playing ──reader=0 (10 s; preview 1 s)──► stopping ──media_stopped──► ended
```
Taymautlar: `media_start→media_ready` **10 s**; WHEP ulanishi **10 s**; server transcode publish **15 s**;
session TTL 5 min (har 2 min uzaytiriladi); **publish TTL 60 s** (`media_refresh`).
Har o'tish `session_state` bilan UI'ga, `session_id` + `publish_id` audit zanjiriga yoziladi.

## 15. MediaMTX auth-hook shartnomasi (tuzatilgan)

`authMethod: http` → `POST http://127.0.0.1:8189/internal/auth` (vmsd, faqat localhost).
MediaMTX yuboradi (rasmiy maydonlar):
```json
{ "action": "publish|read|playback|api|metrics|pprof",
  "path": "cam/ag_…/UUID", "protocol": "rtsp|webrtc|hls|…",
  "user": "", "password": "", "token": "T",
  "ip": "…", "id": "session id", "query": "token=T", "userAgent": "…" }
```
- **`update` action'i YO'Q** (root §7.1 dagi xato tuzatildi).
- Javob: `200 {"allow": true}` yoki `403`. Qo'shimcha imkoniyat (tavsiya etiladi):
  `200 {"expireAt": "2026-11-19T10:01:00Z"}` — MediaMTX sessiyani o'zi yopadi, TTL watchdog ikki tomonlama ishlaydi.
- Tekshiruv zanjiri: `token` → sha256 → DB → session → `path` mosligi → user/client huquqi.
- **v1.11.3'da tekshirildi (2026-11-20):** RTSP so'rovlarida token `token`
  maydonida BO'SH, `query` satrida (`token=...`) keladi; WHIP/WHEP'da esa
  `Authorization: Bearer` `token` maydoniga tushadi. Klient uchala manbani
  ham o'qiydi (`ExtractToken`).
- **WHEP'da token URL query'da bo'lishi shart (v1.11.3'da tekshirildi):**
  `Authorization: Bearer` MediaMTX auth-hook'ga uzatilmaydi (u o'z `jwt`ini
  qo'shadi). Shuning uchun UI `?token=` qo'shadi va header'ni ham saqlaydi.
- **Publish GOP talabi (o'qish kechikishi uchun hal qiluvchi):** MediaMTX
  yangi o'quvchiga birinchi keyframe'dan yuboradi. x264 default `gop=250`
  (15 fps'da ~17 s) — o'lchangan holda o'qish ochilishi 14.4 s edi; GOP 30
  (2 s) bilan **2.4 s**. Agent publish'da `-g = 2×fps` (va `keyint_min` bir xil,
  `sc_threshold 0`) ishlatishi shart; kamera sozlamasida GOP 1-2 s bo'lishi
  tavsiya etiladi.
- Control API (`/v3/paths/get/{name}`) — YAKKA obyekt, `readers` maydoni —
  **obyektlar ro'yxati**; path bo'lmasa 404 (idle stop uchun readers=0
  deb olinadi).
- `tc/...` publish'i uchun alohida **ichki token** (server o'zi FFmpeg'ga beradi).

## 16. Discovery va ommaviy qo'shish (agent, lokal UI)

### 16.1 Ikki usul
1. **ONVIF WS-Discovery** (afzal): UDP multicast **`239.255.255.250:3702`** (WS-Discovery
   standart IPv4 multicast manzili/porti; IPv6 link-local — `ff02::c`), 10 s timeout, `Probe` / `ProbeMatch`, `Scopes` dan model/vendor,
   `XAddrs` dan ONVIF manzili.
   Kameralarda **ONVIF auth** so'ralsa, UI'dan beriladigan umumiy user/parol bilan
   `GetDeviceInformation` + `GetProfiles/GetStreamUri` qayta tekshiriladi.
2. **RTSP port-scan** (ONVIF bo'lmagan Dahua uchun zaxira): `discovery.cidrs` bo'yicha
   TCP `554` connect (64 parallel, timeout 500 ms) + anonim `DESCRIBE`
   (`/cam/realmonitor?channel=1&subtype=0|1`) → javob kelsa kamera topilgan.

### 16.2 Natijalar va birlashtirish
- Unikal kalit: **MAC** (yoki `serial`), bo'lmasa `ip+port`. Bir xil kamera qayta topilsa
  `already_known: true` — mavjud `id` saqlanadi (layout/favorites buzilmaydi).
- Natija ro'yxati: IP, model, vendor, ONVIF bor/yo'q, RTSP javob beradi, kodeklar (agar
  anonim DESCRIBE ishlasa), allaqachon qo'shilganmi.

### 16.3 Ommaviy qo'shish (bulk add)
- UI'da checkbox bilan tanlash → **bitta amaliyot**: `name_pattern` (`{model}-{index:02d}`
  yoki `Kirish-{n}`), umumiy user/parol (bitta yoki har biriga tahrirlash), `position`
  avtomatik, `enabled`.
- Agent har kamera uchun UUID yaratadi, parolni `secrets.bin` ga soladi, `cams_epoch++`,
  `cams_sync` delta yuboradi.
- **Bulk tahrirlash:** tanlangan kameralarga bir vaqtda `enabled`, `play_profile`, ONVIF
  `codec_fix` (`apply_all: true`), parol almashtirish.
- **Import/eksport:** CSV (`name,host,port,main_path,sub_path,user,pass`) — parol eksportda
  faqat `--with-secrets` bilan, aks holda `***`.

### 16.4 Cheklovlar va xavfsizlik
- Skan faqat `discovery.cidrs`; `/22` dan keng to'r rad etiladi (`max_hosts: 1024`).
- Skan **parallel publish'lar bilan birga** ishlaydi, lekin probe pacing'i bilan bir xil navbatdan
  o'tadi (LAN bombardmoni bo'lmasin).
- Skan natijalari faqat lokal UI'da ko'rinadi, serverga **yuborilmaydi** (server faqat qo'shilgan
  kameralarni biladi).

## 17. Lokal UI (agent) — dizayn talabi (tasdiqlangan)

**Uslub:** zamonaviy, ixcham, qorong'i (dark) interfeys; operator joyda tez ish qiladi — sichqoncha
harakati minimal, klaviatura bilan boshqariladi.

- **Texnologiya:** `go:embed` bitta HTML+CSS+vanilla JS (framework yo'q, build'ga `npm` kerak emas).
  Server UI'dan farqli: **client UI binary'ga embed qilinadi** (deploy oddiy bo'lsin).
- **Ekranlar (SPA, hash routing):** `Kameralar` · `Kashf etish` · `Sozlamalar` · `Holat/Self-test` · `Jurnal`.
- **Layout:** chapda tor ikonka paneli (56 px), yuqorida status chizig'i (server holati, publish
  soni, upload Mbps, versiya), asosiy maydon — zich jadval (28 px qator).
- **Ranglar:** `--bg #0f1115`, `--panel #171a21`, `--text #e6e9ef`, `--accent #3b82f6`,
  `--ok #22c55e`, `--warn #f59e0b`, `--err #ef4444`; radius 8 px; soya yo'q (chiziq chegaralar).
- **Tipografiya:** system-ui, 13 px asos, 11 px yordamchi; monospace (`JetBrains Mono` fallback) —
  IP, bitrate, ID lar uchun.
- **Komponentlar:** inline edit (hujayra bosiladi), toast (o'ng pastda, 4 s), modal (skan natijasi,
  parol), progress chizig'i (bulk add), status nuqtasi (yashil/kul/qizil), badge (H.264/H.265/⚠),
  checkbox bilan ko'p tanlash, drag bilan ustun tartibi.
- **Klaviatura:** `/` — filtr, `n` — yangi kamera, `d` — kashf etish, `Space` — tanlash,
  `Enter` — tahrirlash, `Del` — o'chirish, `?` — hotkey yordami, `Esc` — modal yopish.
- **Til:** o'zbek (bitta `i18n` obyekti), sana/soat `Asia/Tashkent (+05)`.
- **Responsiv:** ≥1280 px optimal; 1024 px'da jadval ustunlari kamayadi; mobil uchun mo'ljallanmagan.
- **Mavjud holatlar:** server offline (banner), skan bo'sh, parol xato, FFmpeg yo'q (banner),
  NVENC/VAAPI topilmadi (badge), disk to'lgan (state yozib bo'lmadi).


### 17.1 Lokal UI HTTP API (agent ichida, faqat `ui_bind` manzilida)

UI — SPA; ma'lumotlar shu endpoint'lar orqali olinadi. Barcha javoblar JSON
(`Content-Type: application/json`), statik fayllar `go:embed`dan.

| Endpoint | Metod | Mazmuni |
|---|---|---|
| `/healthz` | GET | `{status, version}` — systemd/probe uchun |
| `/api/ui/status` | GET | `{version, server_online, cams_total, cams_online, publishes, cams_epoch, ffmpeg_ok}` |
| `/api/ui/settings` | GET / PUT | `server_url`, `log_level`. PUT — PIN talab qiladi (§10.3) |
| `/api/ui/log?limit=N` | GET | `{lines:[…], count}` — oxirgi N qator, **eski → yangi**. `limit` default 200, maksimal 1000. Qatorlar §10.2 bo'yicha **yashirilgan** holida beriladi |
| `/api/ui/cams` | GET | `{cams:[…], count}` — §3.2 sxemasi. **Parol qiymati hech qachon chiqmaydi**, faqat `secret_ref` (§10.2) |
| `/api/ui/cams` | POST | Yangi kamera (`id` agentda UUIDv4, §5). Jism: `name,host,port,main_path,sub_path,enabled,position,secret`. PIN talab |
| `/api/ui/cams/{id}` | PUT | Maydonlarni yangilash; `id` o'zgarmaydi. `secret` maydoni: yo'q → tegilmaydi, `""` → o'chiriladi, qiymat → yangilanadi. PIN talab |
| `/api/ui/cams` | DELETE | `{ids:[…]}` → `{deleted:N}`; parollar keystore'dan ham o'chiriladi. PIN talab |
| `/api/ui/cams/import` | POST | CSV (body `text/csv`); `?with_secrets=1` bo'lsa `secret` ustuni o'qiladi. Hisobot `{created,updated,errors:[…]}`. PIN talab |
| `/api/ui/cams/export` | GET | `text/csv` — parollar `***`. `?with_secrets=1` faqat PIN bilan (§16.3) |
| `/api/ui/discovery*` | POST/GET | WS-Discovery boshlash/natija (C2) |
| `/api/ui/selftest` | POST | Self-test ishga tushirish (C4) |
| `/favicon.ico`, `/favicon.svg` | GET | SVG ikonka — brauzer avtomatik so'raydi, 404 bo'lmasligi kerak |

**Qoidalar:**
- Noma'lum `/api/*` → **JSON 404** `{error:"unknown_endpoint", path:"…"}` (HTML 404 emas).
  `path` so'roq qatorisiz qaytariladi — token/parol aks etmasin (§10.2).
- `/api/ui/log` buferi xotirada saqlanadi (default 500 qator) va SIGHUP reload'da
  **to'zmaydi** — tarix davom etadi.
- Yozuv talab qiladigan barcha endpoint'lar (`PUT/POST/DELETE`) PIN tekshiradi.
- Kamera CRUD ning har bir amali `cams_epoch` ni o'stiradi (§6.5) va keyingi delta
  `cams_sync` da serverga yetadi. CRUD xizmati ulanmagan bo'lsa `/api/ui/cams*` →
  **501** `{error:"cams_not_ready"}`.
- UI JS'ida regex literal ishlatilmaydi (`scripts/jscheck.py` node'siz tekshiruv
  uchun); matnli atributlarda apostrofdi ehtiyot bo'lish kerak — bir qadrqli
  JS satrini uzib, butun ekran bo'sh qoladi (`make ui-lint` ushlaydi).

## 18. Test va mock (`devserver`)

- `make devserver` — client repoda **mock vmsd**: `/api/agent/enroll`, `/api/agent/ws`,
  `auth/hello/welcome`, `cams_request`, `cams_sync` qabul (log + hisob), `media_start/stop/refresh`,
  `codec_fix`, `config_push`, `secret_rotate`, token tekshiruvi (soxta), `media_ready` taymaut simulyatsiyasi.
- **Handshake tartibi (amalda):** `auth` → `hello` → `welcome`. `auth` yoki `hello` 5 s ichida
  kelmasa **4002**, secret noto'g'ri **4003**, `protocol_version` oralikdan tashqarida avval
  `welcome{compat.ok:false}` yuborilib **4001** bilan yopiladi (§1, §3.3).
- **Dev persistensiyasi:** `-state build/devserver.state.json` — enroll qilingan agentlar faylga
  yoziladi, mock qayta boshlansa ham agentlar yo'qolmaydi (faqat mock; haqiqiy serverda Postgres).
  Server har 30 s WS `ping` yuboradi (§2).
- Mock MediaMTX o'rniga: lokal `mediamtx` (server repodagi binary nusxasi) yoki `gortsplib` server.
- Test manbalar: FFmpeg `testsrc` → RTSP **H.264 baseline** va **H.265** (`hevc_nvenc`/`libx265`),
  B-frame li `High` varianti, parol bilan himoyalangan variant.
- Matritsa (C3 qabul mezoni): H.264 WHIP · H.264 UDP bloklangan → RTSP/TCP · H.265 → RTSP/TCP ·
  H.265 + `transcode.by=client` (GPU/CPU) · B-frame re-encode · token muddati o'tishi ·
  `media_ready` 10 s · refcount (2 user + preview) · WS uzilishi (watchdog) · SIGTERM (graceful) ·
  kamera paroli xato (`camera_rejected`) · discovery bulk add (10 soxta kamera).

### 18.2 Test intizomi (medium effort rejimi — server tomonida ham tavsiya qilinadi)
- **Golden testlar:** shu fayldagi barcha JSON misollar `testdata/protocol/*.json` ga ko'chiriladi;
  encode/decode round-trip tekshiriladi. Kontrakt ↔ kod bog'lanishi shu yerda — qo'lda JSON yozilmaydi.
- **Fixture'lar:** `testdata/sdp/*` (H.264 baseline / Main B-frame'siz / High B-frame li / H.265),
  `testdata/rtp/*`, `testdata/wsdd/*.xml`, `testdata/ffmpeg-encoders*.txt`, `mediamtx-auth/*.json`
  — tarmoq talab qilmaydi; `testdata/gen.sh` FFmpeg bilan generatsiya qiladi.
- **Fake clock:** watchdog (60 s), retry (1/2/4 s), backoff, idle stop, TTL — testlarda haqiqiy uyqu taqiqlangan.
- **Single-writer goroutine:** publish registry / session map / state store — bitta egasi, o'zgarishlar
  channel orqali; murakkab `Mutex` tartibi taqiqlangan (race xatolarining asosiy manbai).
- **`goleak` + `-race`** har testda; `contextcheck` linteri; coverage bo'sagasi:
  `proto/cams/publish` ≥ 70 %, qolgan paketlar ≥ 50 %.
- **Reason matritsasi:** §13 jadvalidagi har kod uchun trigger → kutilgan natija (table-driven test);
  "noma'lum xato" holati qoldirilmaydi.

## 19. Qabul mezoni (kontrakt bo'yicha)

- Agent `devserver` mock bilan to'liq siklni o'tadi: enroll → auth → hello → cams_sync (chunk) →
  cam_status → media_start → media_ready → media_stats → media_stop → media_stopped.
- `media_ready` 10 s ichida; refcount tufayli bitta kameraga **bitta** publish; TTL watchdog
  WS uzilganda ham publish'ni yopadi.
- Loglarda parol/token/secret **hech qayerda** ko'rinmaydi (redaktor testi).
- Discovery: 10 soxta kamera topiladi, checkbox bilan qo'shiladi, `id` lar qayta yaratilmaydi.
- `protocol_version` mos kelmasa agent aniq xato ko'rsatadi va backoff'ni oshiradi.

## 20. Changelog

**Agent tomoni (2026-11-20 o‘lchandi):** server har 30 sda WS `ping`
(kontrol frame) yuboradi. Gorilla‘da read deadline faqat DATA frame bilan
uzaradi — kontrol frame uzarmaydi. Shuning uchun agent **kelgan ping‘da**
(`SetPingHandler`) o‘z read deadline‘ini uzatishi yoki muntazam `hb`
(data) yuborishi shart; aks holda agent ~90 sda jim uziladi (testagent‘da
aynan shu xato topildi va tuzatildi; haqiqiy vmsagent `hb` yuborgani uchun
tasodifan qutulgan).

| Versiya | Sana | O'zgarish |
|---|---|---|
| 1.0-draft.7 | 2026-11-20 | §3.1: enrollment kodi **8 xonali raqamli son** qilib o‘zgartirildi (operator talabi — kiritish oson bo‘lsin); qabulda bo‘sh joy/tire yuboriladi. Eski formatdagi kodlar endi yuborilmaydi. |
| 1.0-draft.6 | 2026-11-20 | §15 izohlar: agent WS `ping` kelganda read deadline'ini uzatishi shart (kontrol frame deadline uzarmaydi); WHEP read so'rovida token URL query'da bo'lishi shart (`Authorization` hook'ga yetmaydi); publish GOP 2 s dan katta bo'lmasligi talabi (o'quvchi birinchi keyframe'ni kutadi - 15 fps'da default gop=250 bilan 14.4 s kechikish olchandi); `paths/get` bir object qaytaradi, `readers` massivi. |
| 1.0-draft.5 | 2026-11-19 | §17.1 ga kamera CRUD endpoint'lari to'liq yozildi (`/api/ui/cams` GET/POST/DELETE, `/api/ui/cams/{id}` PUT, import/eksport) va 501 qoidasi. §8.1 B-frame aniqlash: `profile-level-id` yetarli emas — H.264 SPS VUI `max_num_reorder_frames` o'qiladi (o'z parserimiz, golden + ffprobe orakoli bilan tasdiqlandi). |
| 1.0-draft.4 | 2026-11-19 | §3.2/§10.1 aniqlashtirildi: state.json'da `secret_ref` yoziladi, shifrlangan secret emas. §3.1 ga kod berish usuli qo'shildi (`-enroll` yoki UI). §18: devserver handshake tartibi (auth→hello→welcome; 4001/4002/4003) va dev persistensiyasi. C1.1–C1.10 amalda ishlagani tasdiqlandi |
| 1.0-draft.3 | 2026-11-19 | Lokal UI porti **8080 → 8082** (§10.3, §11) — 8080 bu muhitda band xizmatlar bilan to'qnashdi. Kamera HTTP porti (joy ichida 80/8080, §10.5) o'zgarmagan; server API 8081 va MediaMTX 8889/8189/8554 ham o'zgarmagan. **§17.1** qo'shildi: lokal UI HTTP API ro'yxati (`/api/ui/log`, favicon, noma'lum `/api/*` → JSON 404) — brauzer konsolidagi 404 xatolari sababi shu edi |
| 1.0-draft.2 | 2026-11-19 | §18.2 qo'shildi: medium effort test intizomi (golden/fixture, fake clock, single-writer, goleak/-race, coverage bo'sagasi, reason matritsasi); WS-Discovery multicast manzili tuzatildi (`239.255.255.250:3702`); vmsd API porti (default 8081) aniqlashtirildi |
| 1.0-draft.1 | 2026-11-19 | Client tomonida tuzilgan: enrollment endpoint, WS `auth`/`welcome`, `media_start.publish.urls`, token semantikasi tuzatildi, `cams_sync.op/epoch`, kamera `id` agentda, refcount/TTL watchdog/graceful shutdown, ingest kutubxonalari (gortsplib + pion, FFmpeg fallback), discovery/bulk add, lokal UI dizayn talabi, `client.yaml` sxemasi, reason kodlari, auth-hook `action` ro'yxati tuzatildi |

