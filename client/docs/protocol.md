# Central VMS — kontrakt: agent (`vmsagent`) ↔ server (`vmsd`)

**Versiya: 1.0-draft.12** · Sana: 2026-11-20 · Holat: **client tomonida yozildi, muzlatildi (frozen)**

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
- **Server qayta boshlanganda (1.0-draft.10):** ishga tushishda DB'da qolgan
  `clients.status='online'` va `cameras.status='online'` belgilari
  **offline** qilib tozalanadi (`registry.ResetOnlineStates`) — bu paytda
  birorta WS sessiya yo'q. (2026-11-20 amalda kuzatildi: restartdan keyin
  Boshqaruv sahifasi ulanmagan mijozni "online" deb ko'rsatardi.)
- Ketma-ketlik: agent barcha buyruqlarni **accept-then-execute** qiladi — darhol qabul qiladi,
  bajarish natijasi alohida xabar bilan (`media_ready` / `media_stopped`).

## 3. Ulanish: so‘rov va admin tasdig‘i (1.0-draft.10)

Avval: bir martalik kod orqali enroll. Operator talabi (2026-11-20):
„ulanish kodini olib tashla. ulanish ruhsati faqat server UI dan
tasdiqlanadi“. Kod oqimi butunlay olib tashlandi; `enrollment_codes`
jadvali migratsiya **0002**da tushirildi, o‘rniga `join_requests` va
`preauth_ips` keldi.

### 3.1 Ulanish so‘rovi (serverda ochiq endpoint)

```
POST /api/agent/join
Content-Type: application/json

{ "join_key": "<64 ta hex belgi>",     // agent birinchi marta yaratadi va doimiy saqlaydi
  "agent_secret": "<kamida 32 belgi>", // sirni AGENT yaratadi (serverda faqat sha256 hash)
  "hostname": "joy-oxangaron-01",
  "name": "Oxangaron",                 // ko‘rinadigan nom (ixtiyoriy, §3.5)
  "os": "Ubuntu 24.04", "lan_ip": "192.168.10.20", "agent_version": "0.3.1" }

200 { "status": "pending",  "request_id": 12 }           // admin tasdig‘i kutilmoqda
200 { "status": "approved", "agent_id": "ag_9f2c1a7e" }  // tasdiqlangan (polling javobi)
403 { "status": "rejected", "error": "rejected", "detail": "<admin izohi>" }
400 { "error": "bad_key" }        // join_key/agent_secret talablari bajarilmagan
429 { "error": "rate_limited" }   // IP bo‘yicha 5 urinish / 5 daqiqa
```

Qoidalar:
- So‘rov takrorlanishi mumkin: `join_key` bo‘yicha **bitta** qator ochiladi,
  ma‘lumotlar yangilanadi (idempotent polling). Agent tasdiqni shu
  chaqiruvni takrorlab kutadi (vmsagent: 15 s davr).
- **Sir sxemasi:** `agent_secret`ni agent o‘zi yaratadi. Server uni faqat
  sha256 hash ko‘rinishida saqlaydi va tasdiqdan keyin **hech qachon
  qaytarmaydi** — agentda allaqachon bor. (Eski oqimda sirni server
  yaratib bir marta qaytariardi; endi agent diski qayta tiklansa ham
  sir yo‘qolmaydi.)
- `agent_id` ni server beradi (`ag_` + 16 hex) — **tasdiq paytida**.
- Tasdiqdan oldin mijoz DB'da **yaratilmaydi**: serverda „noma‘lum
  client“ holati paydo bo‘lmaydi, chunki ulanish faqat ro‘yxatdan
  o‘tgan mijoz uchun ochiq.

### 3.2 Admin tasdig‘i (server UI)

Boshqaruv sahifasida **Ulanish so‘rovlari** jadvali: kompyuter nomi,
operator nomi, kelgan IP, tizim/versiya, vaqt. Amallar:
- **Tasdiqlash** — nom tahrirlanadi (bo‘sh bo‘lsa so‘rovdagi nom);
  nom band bo‘lsa `409 name_taken` (admin nomni o‘zgartiradi).
- **Rad etish** — izoh bilan; agent lokal UI‘sida „request rejected“
  ko‘rinadi va so‘rov rad holatida qoladi.
- **O‘chirish** — eski so‘rov; agent qayta so‘rov yuborsa yangi qator
  ochiladi.
- **Mijoz qatori o‘chirilsa** (`DELETE /api/admin/clients/{id}`, 1.0-draft.11):
  agent WS sessiyasi darhol yopiladi (`Hub.Kick`, close **4004**) va
  tasdiqlangan so‘rov keyingi polling‘da **rejected** holatiga o‘tadi
  (note: „mijoz qatori server tomonidan o‘chirilgan“). Aks holda agent
  abadiy „pending“ deb kutardi (2026-11-20 amalda kuzatildi).
DB: `join_requests(join_key_hash UNIQUE, agent_secret_hash, hostname, name,
os, lan_ip, source_ip, agent_version, status, decided_at, decided_by,
client_id, note)`.

### 3.3 IP bo‘yicha oldindan ruxsat (preauth)

Operator server UI‘dan mijoz IP manzilini yozib qo‘yadi (aniq IP yoki
CIDR, masalan `192.168.0.0/22`). Shu manzildan kelgan so‘rov **avtomatik
tasdiqlanadi** — admin kutmasdan client qo‘shiladi. Nom: preauth‘dagi
nom (bo‘sh bo‘lsa so‘rovdagi nom, u ham bo‘sh bo‘lsa `agent-…`);
nom band bo‘lsa avtomatik ` (2)` raqami qo‘shiladi (hech kim qarab
turgan emas, jarayon to‘xtamasligi kerak).
DB: `preauth_ips(ip UNIQUE, name, note, created_by)`.

### 3.4 Agent tomonida saqlash

`/var/lib/vmsagent/state.json` (0600, atomik yozish: `*.tmp` → `rename`):
```json
{ "schema": 1,
  "server_url": "ws://192.168.1.10:8081",
  "join_key": "<64 hex>",              // doimiy; so‘rov egalligini isbotlash uchun
  "join_status": "pending",            // pending | approved | rejected
  "agent_id": "ag_9f2c1a7e",           // tasdiqlangach to‘ladi
  "secret_ref": "agent_secret_enc",    // §10.1: sir state.json'da YO‘Q — faqat ref
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
Agent diski yo‘qolsa: yangi `join_key` bilan yangi so‘rov (admin tasdiqi
qayta kerak). Tasdiqlangan so‘rov qatori saqlangan bo‘lsa va agent eski
kalitini saqlab qolgan bo‘lsa, server bir xil `agent_id` ni qaytaradi.

### 3.5 Mijozning ko‘rinadigan nomi (1.0-draft.9)

Operator lokal UI‘da `client_name` kiritadi; agent uni join so‘rovida
(`name`) va har `hello`da yuboradi. Server: so‘rovda/tasdiqda bo‘sh
bo‘lsa avtomatik `agent-<agent_id qoldig‘i>` nomi qo‘yiladi;
`hello.name` bo‘sh bo‘lmasa mijoz nomi yangilanadi va UI‘ga
`client_status{name}` yuboriladi. Admin `PATCH /api/admin/clients/{id}`
bilan istalgan payt o‘zgartiradi. Nom **unikal** (`clients.name UNIQUE`):
band bo‘lsa admin tasdig‘ida `409 name_taken`.

### 3.6 WS auth qo'l berishish (handshake)

```
agent → server : { "type": "auth", "data": { "agent_id": "ag_...", "secret": "..." } }   // ulanishdan keyin ≤5 s
server → agent : { "type": "welcome", "data": { ... } }    // §4.1, yoki 4001/4003 bilan yopiladi
```
- `auth` kelmasa server 5 sdan keyin yopadi (code **4002**).
- Secret noto'g'ri → code **4003** (`agent_revoked` bo'lsa `4004`). Agent 4003'da backoff'ni
  60 s dan boshlaydi va lokal UI'da "secret rad etildi — qayta ulanish so‘rovi yuboring" deydi.
- **Eskirgan identity (1.0-draft.11):** server bazasi qayta boshlansa yoki mijoz
  qatori o'chirilsa, agent o'z `agent_id`/secret'i bilan cheksiz 4003 oladi va
  o'zi tiklana olmaydi (serverda hash, agentda esa eski sir). Shuning uchun
  agent 4003/4004'ni `status.auth_rejected` sifatida UI'ga chiqaradi va operator
  **Clear connection** (`POST /api/ui/reset`) → **Request access** bilan yangi
  `join_key`/secret orqali qayta so'rov yuboradi. Kameralar va sozlamalar
  saqlanadi; eski mijoz qatori server UI'da **O'chirish** bilan olib tashlanadi
  (nom band bo'lsa tasdiq `409 name_taken` qaytaradi).

## 4. Xabarlar: agent → server

### 4.1 `hello` (auth'dan keyin birinchi)
```json
{ "protocol_version": 1, "agent_version": "0.3.1", "os": "Ubuntu 24.04",
  "hostname": "joy-oxangaron-01", "lan_ip": "192.168.10.20",
  "name": "Oxangaron",              // mijoz nomi (bo‘sh → server nomni o‘zgartirmaydi)
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
- **Darhol yuborish (1.0-draft.12):** lokal UI orqali kamera qo'shilgani,
  tahrirlanganigi yoki o'chirilgani agentda `epoch`ni o'stiradi va agent
  **shu zahoti** `cams_sync` yuboradi (keyingi qayta ulanganda kutilmaydi).
  Server bu sync `done: true` bilan tugagach UI'ga `cams_changed`
  (`{client_id, agent_id, epoch}`) voqeasini uzatadi — brauzer sahifani
  yangilamasdan ro'yxatni ko'radi (`api.md` §wsui).

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
- **Snapshot (1.0-draft.12):** har sessiya ochilganda (`welcome`dan keyin)
  agent barcha kameralarning **hozirgi** holatini `cam_status` bilan
  yuboradi — server qayta boshlanganda yoki agent qayta ulanganda holat
  bo'sh qolmasin (holati bo'sh kamera `unknown` deb yuboriladi).
- `error` matnini server `cameras.last_error` + `last_error_at` deb saqlaydi
  va `GET /api/clients/{id}/cameras` javobida qaytaradi; **online** holatda
  sabab tozalanadi (masalan MediaMTX'ning `keepalive_unsupported`
  eslatmasi operatorga xato bo'lib ko'rinmasin).

### 4.5 `codec_probe`
```json
{ "camera_id": "UUID", "codecs": { "main": "h264", "sub": "h265" },
  "bframe": { "main": true, "sub": false },
  "profiles_sdp": { "main": "High", "sub": "Baseline" },
  "onvif_switch_ok": false, "reason": null }
```
> 1.0-draft.12: agent `codec_probe`ni **probe siklida** ham yuboradi —
> kamera kodi noma'lum bo'lsa, probe bitta ulanishda `DESCRIBE` qilib
> SDP'dan kodekni oladi (`h264|h265`), modelga yozadi (§4.2 orqali
> serverga ham boradi) va shu xabarni yuboradi. Qo'shimcha `DESCRIBE`
> faqat kodek noma'lum bo'lganda bajariladi (kamera sessiyalarini
> tejash uchun; Hikvision bir vaqtda cheklangan sessiyaga ruxsat beradi).

### 4.6 `media_ready` / `media_stopped` / `media_stats`
```json
// media_ready
{ "session_id": "se_…", "camera_id": "UUID", "profile": "sub", "kind": "play",
  "publish_id": "UUID",                 // §9 refcount: bitta publish uchun bir marta
  "path": "cam/ag_9f2c1a7e/UUID",   // MediaMTX yo'li; profil EMAS (1.0-draft.12)
  "ingest": "whip|rtsp_tcp", "transport": "udp|tcp",
  "transcoded": false, "backend": "none|gpu|cpu",
  "fps": 10.0, "bitrate_kbps": 700, "bytes_sent": 0, "ready_ms": 640 }

// media_stopped
{ "session_id": "se_…", "camera_id": "UUID", "publish_id": "UUID",
  // 1.0-draft.12: `path` profil bo'lib yuborilgani (eski agent xatosi)
  // o'quvchi URL'larini buzardi; server endi mos kelmaydigan `path`ni
  // qabul qilmaydi va ogohlantiradi.
  // media_stopped sababi (normal: no_viewers/ttl_expired/user'dan tashqari)
  // kameraning `last_error` maydoniga yoziladi.
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
  "ingest": "whip|rtsp_tcp",              // qiymatlar shu ikkisi; "rtsp" YO'Q
                                          // (1.0-draft.12: server avval "rtsp"
                                          // yuborardi — agent media_start'ni
                                          // rad etardi, preview publish_failed)
                                          // H.264→whip, H.265→rtsp_tcp;
                                          // hozircha server H.264 uchun ham
                                          // rtsp_tcp yuboradi (agent WHIP'ni
                                          // caps'da e'lon qilmaydi)
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

### 8.3a RTP bo'laklash (H.265) — 1.0-draft.12
RFC 7798: AP turi **48**, FU turi **49**, PACI turi 50 (HEVC specida
62 — "unspecified", RTP uni qayta-yigmaydi). Payload tartibi:
`[NAL bayt0 (tur=49, 1..6-bit)] [NAL bayt1 nusxasi] [FU boshi S|E|tur]
[bo'lak]`. Agentda ham paketlash, ham depaketlash shu formatda
(round-trip testi bilan tekshirilgan).
**Tarix (2026-11-20/21):** dastlab paketlashda tur `62<<2` yozilgan edi
(layer-id buzilardi); keyin `62<<1` qilingan — lekin 62 noto'g'ri TUR:
MediaMTX uni oddiy NAL deb uzatardi, FFmpeg esa `Unsupported (HEVC)
NAL type (62)` deb rad etardi. To'g'ri tur 49 (FFmpeg `rtpdec_hevc.c`
`HEVC_NAL_FRAGMENT=49`). Tuzatishdan keyin jonli Hikvision (HEVC
2688x1520) oqimi MediaMTX orqali `ffprobe`da 4 s'da 66 kadr, xatosiz
dekodlandi.

### 8.3b Kamera autentifikatsiyasi — 1.0-draft.12
Hikvision digest URI'sini **standart port tushirilgan** ko'rinishda
kutadi: `rtsp://192.168.0.24:554/...` bilan 401, `rtsp://192.168.0.24/...`
bilan 200. Agent avvalgi URI bilan urinib, 401 kelsa URI'ni
normallashtirib (554/322 portini tushirib) qayta uradi.

### 8.3c Yozish muddati — 1.0-draft.12
Interleaved (RTSP/TCP) yozish har freymda write-deadline'ni yangilaydi:
aks holda RECORD so'rovining 5 s'lik dediti butun oqimga meros bo'lib,
publish ~5 s'da `i/o timeout` bilan uzilar edi.

### 8.4 Parameter setlar (SPS/PPS/VPS) — 1.0-draft.12
Manba (kamera yoki boshqa RTSP server) `sprop-parameter-sets`ni faqat
DESCRIBE SDP'ida berib, oqimda takrorlamasa, agent NAL darajasidagi
qayta-yuborishda parameter set'larni yo'qotadi va MediaMTX'dagi oqim
dekodlanmaydi (kalit kadrga qadar kutish ham yordam bermaydi). Shuning
uchun agent pull qatlami SDP'dagi `sprop-parameter-sets` (H.264) /
`sprop-vps|sps|pps` (H.265) qiymatlarini saqlaydi va ular **oqimda
bo'lmagan har bir kalit kadr oldiga qo'shib** yuboradi. Bu qoida
`cams.Pull`da bajariladi — publish transportlari to'liq AU oladi deb
hisoblaydi.

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

### 10.3 Lokal UI bind va himoyasi (1.0-draft.10)
- Bind: `127.0.0.1:8082` (standart). **PIN tizimi olib tashlandi** — operator
  talabi (2026-11-20: „ui pin olib tashla“). Yozuv amallari (kameralar,
  sozlamalar, discovery, snapshot, join) PIN'siz bajariladi.
- `ui_pin` kaliti eski konfiguratsiyalarda **o'qiladi, lekin ishlatilmaydi**
  (agent ogohlantiradi). `ui_bind` LAN manziliga qo'yilsa agent ishga
  tushishda xavf haqida ogohlantiradi: tarmoqdagi har qanday PC lokal UI
  orqali kamera qo'sha oladi.
- Eski qoida (PIN `secrets.bin`da hash, `X-Agent-Pin`, 12 soatlik cookie)
  bekor qilindi; kod va hujjatlarda tarix sifatida qoldi.

### 10.4 Resurs chegaralari (defensive)
- `max_uploads` (server `config_push`) — agent o'zi ham tekshiradi: limit band bo'lsa
  `media_stopped {reason:"limit_exceeded"}` (server limitidan mustaqil ikkinchi himoya).
- `max_local_transcodes` (GPU 8 / CPU-only 4) — FFmpeg transcode navbati.
- FFmpeg processlari soni ≤ `max_local_transcodes + 2` (remux/re-encode hisobga olingan).
- `LimitNOFILE=4096`; xotira nishoni: bo'sh holatda ~30 MB, 100 kamera + 8 publish ≤ 150 MB.

### 10.5 Client tomonida ochiq portlar (faqat chiqish)
| Yo'nalish | Port/protokol |
|---|---|
| server → `vmsd` | TCP 8081 (`/api/agent/ws`, `/api/agent/join`) |
| server → MediaMTX WHIP | TCP 8889 (HTTP) |
| server → MediaMTX ICE | **UDP 8189** (fallback: TCP 8189) |
| server → MediaMTX RTSP publish | TCP 8554 |
| joy ichida (kameralarga) | TCP 554 (RTSP), TCP 80/8080 (ONVIF/HTTP), UDP 3702 (WS-Discovery, multicast) |

## 11. `client.yaml` to'liq sxemasi

```yaml
server_url: ws://192.168.1.10:8081      # ws:// yoki wss://
ui_bind: 127.0.0.1:8082
ui_pin: ""                              # BEKOR qilindi (1.0-draft.10) — o‘qiladi, ishlatilmaydi
client_name: ""                         # serverda ko‘rinadigan nom (masalan "Oxangaron")
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

> 1.0-draft.12: probe sikli (30 s) natijasi faqat lokal holatda
> qolmasligi kerak — agent har bir natijani `cam_status` bilan serverga
> yuboradi (§4.4). Kamera qo'shilgani/o'chirilgani darhol `cams_sync`
> (§4.2) va qayta probe (`ProbeNow`) chaqiriladi.

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
| `/api/ui/settings` | GET / PUT | `server_url`, `log_level`, `name` (1.0-draft.9). PIN talab qilinmaydi (1.0-draft.10).
  PUT qiymatlarni **konfiguratsiya fayliga yozadi**, `state.json`dagi manzilni yangilaydi va
  sessiyani qayta boshlaydi (eski xulq: faqat xotirada qolardi, agent esa eski manzilga
  ulanaverardi — 1.0-draft.8). `name` maydoni qo‘shildi: UI’da „Connection name‘
  kiritiladi, faylga `client_name` deb yoziladi va sessiya qayta boshlanadi
  (1.0-draft.9) |
| `/api/ui/log?limit=N` | GET | `{lines:[…], count}` — oxirgi N qator, **eski → yangi**. `limit` default 200, maksimal 1000. Qatorlar §10.2 bo'yicha **yashirilgan** holida beriladi |
| `/api/ui/cams` | GET | `{cams:[…], count}` — §3.2 sxemasi. **Parol qiymati hech qachon chiqmaydi**, faqat `secret_ref` (§10.2) |
| `/api/ui/cams` | POST | Yangi kamera (`id` agentda UUIDv4, §5). Jism: `name,host,port,main_path,sub_path,enabled,position,secret`. PIN talab |
| `/api/ui/cams/{id}` | PUT | Maydonlarni yangilash; `id` o'zgarmaydi. `secret` maydoni: yo'q → tegilmaydi, `""` → o'chiriladi, qiymat → yangilanadi. PIN talab |
| `/api/ui/cams` | DELETE | `{ids:[…]}` → `{deleted:N}`; parollar keystore'dan ham o'chiriladi. PIN talab |
| `/api/ui/cams/import` | POST | CSV (body `text/csv`); `?with_secrets=1` bo'lsa `secret` ustuni o'qiladi. Hisobot `{created,updated,errors:[…]}`. PIN talab |
| `/api/ui/cams/export` | GET | `text/csv` — parollar `***`. `?with_secrets=1` parollarni ochadi, lekin **bir martalik `nonce` shart** (`POST /api/ui/cams/export-nonce`, 60 s TTL) — bolmasa 403 `nonce_kerak` (§10.2) |
| `/api/ui/cams/export-nonce` | POST | Parol eksporti uchun bir martalik tasdiq: `{nonce, ttl_s}`. Har eksportda yangisi so'raladi |
| `/api/ui/discovery*` | POST/GET | WS-Discovery boshlash/natija (C2) |
| `/api/ui/selftest` | POST | Self-test ishga tushirish (C4) |
| `/api/ui/reset` | POST | Lokal identity'ni tozalash (1.0-draft.11): `agent_id`,
  `join_key` va sir o'chiriladi; kameralar/sozlamalar saqlanadi. Server mijozni
  o'chirganda yoki secret aylantirilganda (cheksiz 4003) ishlatiladi. Jism
  talab qilinmaydi; javob `{status:"cleared"}` |
| `/api/ui/join` | POST | §3.1 ulanish so‘rovi (jism talab qilinmaydi). Agent
  so‘rovni yuboradi/holatini so‘raydi; tasdiqlansa `agent_id` saqlanadi va sessiya
  qayta boshlanadi. `status`da `join_status` va `auth_rejected` (4003/4004)
  qaytariladi. Xatolar: `409 already_enrolled`, `403 rejected`,
  `429 rate_limited`, `400 bad_key`, `502 join_failed`. Kod maydoni yo‘q
  (1.0-draft.10) |
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

- `make devserver` — client repoda **mock vmsd**: `/api/agent/join`, `/api/agent/ws`,
  `auth/hello/welcome`, `cams_request`, `cams_sync` qabul (log + hisob), `media_start/stop/refresh`,
  `codec_fix`, `config_push`, `secret_rotate`, token tekshiruvi (soxta), `media_ready` taymaut simulyatsiyasi.
- **Handshake tartibi (amalda):** `auth` → `hello` → `welcome`. `auth` yoki `hello` 5 s ichida
  kelmasa **4002**, secret noto'g'ri **4003**, `protocol_version` oralikdan tashqarida avval
  `welcome{compat.ok:false}` yuborilib **4001** bilan yopiladi (§1, §3.3).
- **Dev persistensiyasi:** `-state build/devserver.state.json` — ulangan (join_key bo‘yicha) agentlar faylga
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

- Agent `devserver` mock bilan to'liq siklni o'tadi: join → auth → hello → cams_sync (chunk) →
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
| 1.0-draft.12 | 2026-11-20 | **Qo'shimcha (haqiqiy Hikvision kamerasi bilan sinov, 192.168.0.24):** §4.5 `codec_probe` endi haqiqatan yuboriladi (probe SDP'dan kodekni o'rganadi); §8.3a H.265 FU turi 62 va 3 baytli FU boshi (ikkala tomonda ham noto'g'ri edi — round-trip testi qo'shildi); §8.3b Hikvision digest URI quyi izohi (standart portsiz URI bilan qayta urinish); §8.3c interleaved yozish muddati har freymda yangilanadi (publish 5 s'da uzilardi); serverda qayta ulanish poygasi tuzatildi — eski WS sessiyasi yopilishi mijozni (va kameralarini) offline qilmaydi. **Hali to'liq yopilmagan:** H.265 qayta-yuborilgan oqimning brauzer/ffprobe tomonidan dekodlanishi (o'quvchi "NAL type 62" va kalit kadr yetishmasligini xabar qiladi) — davomiy ish. |
| 1.0-draft.12 | 2026-11-21 | **Mijoz (agent) auditi natijalari:** (a) §12.1: kamera qurilmasiga RTSP murojaatlari endi hisobga olinadi — oqim (publish/pull) faol paytida probe va preview rasm **o'tkazib yuboriladi**, band kameraning probe natijasi `unknown` (holat buzilmaydi). Bu tuzatilgunga qadar probe jonli oqimni "offline" qilib ko'rsatishi yoki kameraning sessiya limitini buzib oqimni uzishi mumkin edi; (b) §9.3: `sub` profili endi haqiqatan SUB yo'lini tortadi (avval doim MAIN — masalan 2688x1520 oqim 640x360 o'rniga sarflanardi); (c) §2: agent ham har 20 s'da WS `ping` yuboradi (server eski bo'lsa ham ulanish tirikligi bilinadi), sog'lom sessiyadan keyin qayta ulanish kutishi bazaga qaytadi; (d) §4.2/§4.3: `cams_sync` chunk'lari ketma-ketlashtirildi va parallel sync so'rovlari birlashtiriladi (ikkita sync o'zaro ack'ini "yeyishi" yo'q); (e) §10.2: parol eksporti bir martalik nonce talab qiladi, `ui_bind` localhost bo'lmasa ogohlantirish; (f) §17: `hb`da `rtp_discarded`/`au_forwarded` hisoblagichlari, pull loglarida `session/camera_id/profile` maydonlari; (g) transport kech ochilsa ham yopiladi (sizib chiqish yo'q), state holatiga qulf ostida tegiladi. |
| 1.0-draft.12 | 2026-11-21 | **H.265 zanjiri yopildi (§8.3a):** FU NAL turi 49 (RFC 7798) — 62 emas; agent paketlashi va depaketlashi tuzatildi, natijada qayta-yuborilgan H.265 oqim MediaMTX orqali toza dekodlanadi (jonli kamera: 4 s'da 66 kadr, xato yo'q). **MediaMTX API:** `PUT /v3/config/paths/put/{name}` `/` li nomlar bilan route'ga mos kelmaydi (v1.11.3) — server endi `~^cam/.+` / `~^tc/.+` patternlarini `paths/list` orqali tekshiradi va EnsurePath'ni zarur bo'lganda chaqiradi (log'dagi 404 shovqini yo'qoldi). **Yangi ochiq xatolar:** (1) `server_tc` rejimida FFmpeg `tc/...` ga ANNOUNCE'ni kech (jarayon tugashida) qiladi — o'quvchilar 404 oladi; (2) bir kameraga avval `hevc:true` keyin `hevc:false` so'rovi kelganda agent ikkinchi publish'ni tushirib yuboradi (`media_stopped reason=dropped`) va transcode umuan boshlanmaydi. |
| 1.0-draft.12 | 2026-11-21 | **Yurishni davomi (S4 qoldiqlari):** (a) §2 heartbeat: server har 30 sda `ping` **envelope**ini yuboradi — WS control ping agentning o'qish muddatini uzamaydi (agent `PingHandler` qo'shildi); bu tuzatilgunga qadar agent har 60 s'da `i/o timeout` bilan uzilib qayta ulanar edi; (b) §4.2 `position` endi sync'da server bazasiga yoziladi; (c) kamera RTSP foydalanuvchisi (`user`) agent modelida saqlanadi (standart `admin`) — pull shu nom bilan kirishadi; (d) serverda retention (`sessions`/`audit_log`, `retention_days`) va token sweep ishga tushdi (soatlik janitor); (e) `vmsd -backup <fayl>` — `VACUUM INTO` bilan issiq nusxa + butunlik tekshiruvi (`deploy/backup.sh`); (f) `deploy/firewall.md`, `deploy/install-notes.md`; (g) **§4.2/§4.4 o'zaro ta'siri:** to'liq `cams_sync` `status`ni bo'sh yuborgani uchun server uni `offline` deb yozardi va `cam_status` orqali kelgan `online`ni bosardi — endi sync'dagi `status` qo'llaniladi, bo'sh bo'lsa mavjud holat saqlanadi; agent har qayta qo'l berishda dedup xotirasini tozalaydi. Server qayta boshlansa kameralar ~15 s ichida o'z-o'zidan `online` bo'ladi (jonli tasdiqlandi). |
| 1.0-draft.12 | 2026-11-20 | **Kamera holati va oqim zanjiri tuzatildi** (operator kuzatuvi: "mijozda kamera qo'shsam serverda ko'rinmadi, preview chiqmadi"): (a) `cam_status` endi haqiqatan yuboriladi — sessiya ochilganda to'liq snapshot + holat o'zgarganda (§4.4); (b) kamera qo'shish/tahrirlash/o'chirish `cams_sync`ni **darhol** yuboradi, server UI'ga `cams_changed` uzatiladi (§4.2); (c) `media_start.ingest` qiymati tuzatildi — server "rtsp" yuborardi, kontrakt esa `whip|rtsp_tcp` (§5.2); (d) `media_ready.path` — MediaMTX yo'li, profil emas; server mos kelmasa o'z path'ida qoladi (§4.6); (e) `cameras.last_error`/`last_error_at` — probe va `media_stopped` sababi UI'da ko'rinadi (§4.4); (f) §8.4: SDP'dagi sprop parameter setlari kalit kadr oldiga qo'shiladi. |
| 1.0-draft.11 | 2026-11-20 | **Eskirgan identity holati** (operator kuzatuvi:
  client „connected as ag_…“ deb turardi, Request access o'chirilgan edi,
  server esa uni tanimasdi). §3.6: agent 4003/4004'ni `status.auth_rejected`
  qilib UI'ga chiqaradi; lokal UI'da **Clear connection** (`POST /api/ui/reset`,
  §17.1) — `agent_id`/`join_key`/sir o'chiriladi, kameralar va sozlamalar
  qoladi; so'ng Request access bilan yangi kalitli so'rov. Server tomonida eski
  mijoz qatori „O'chirish“ bilan tozalanadi. Qo'shimcha tuzatishlar: mijoz o'chirilganda agent sessiyasi darhol
  yopiladi (`Hub.Kick`, 4004) va tasdiqlangan so'rov keyingi polling'da
  `rejected` holatiga o'tadi (yetim tasdiq abadiy "pending" qaytarmaydi);
  agentning join kuzatuvi Clear connection'dan keyin o'zi davom etadi; lokal
  UI holat matni 5 s davrdagi status polling bilan yangilanadi. |
| 1.0-draft.10 | 2026-11-20 | **Ulanish tartibi: kod → so‘rov + admin tasdiqi**
  (operator talabi). §3 qayta yozildi: `POST /api/agent/join` (`join_key`,
  agent o‘zi yaratgan `agent_secret`, `name`, hostname, IP), holatlar
  `pending/approved/rejected`, polling; `agent_id` tasdiq paytida beriladi va
  sir serverdan qaytmaydi. §3.3 IP bo‘yicha oldindan ruxsat (preauth)
  qo‘shildi — operator server UI'dan IP yozadi, shu manzildan kelgan
  so‘rov avtomatik tasdiqlanadi. `enrollment_codes` jadvali tushirildi
  (migratsiya 0002), `join_requests`/`preauth_ips` keldi. Server UI'da
  „Ulanish so‘rovlari“ va preauth paneli. **Lokal UI PIN'i butunlay
  olib tashlandi** (§10.3): yozuv amallari PIN'siz; `ui_pin` kaliti
  o'qiladi-yu ishlatilmaydi. §17.1: `/api/ui/enroll` o'rniga `/api/ui/join`. |
| 1.0-draft.9 | 2026-11-20 | Mijozning **ko‘rinadigan nomi** endi agentdan keladi:
  §3.1 enroll jismiga `name`, §4.1 `hello`ga `name`, §11 ga `client_name` kaliti,
  §17.1 `/api/ui/settings`ga `name` qo‘shildi. Server: `hello.name` bo‘sh bo‘lmasa
  mijoz nomini yangilaydi va UI’ga `client_status{name}` yuboradi; nom band bo‘lsa
  enroll `409 name_taken` qaytaradi (avval bunda chalkash `already_enrolled`
  chiqardi). Avvalgi „agent ismni hech qachon o‘zi yubormaydi“ qoidasi bekor
  qilindi (operator nomi afzal; admin `PATCH /api/admin/clients/{id}` bilan baribir
  o‘zgartira oladi). |
| 1.0-draft.8 | 2026-11-20 | §17.1: `/api/ui/settings` PUT endi qiymatlarni faylga yozadi va
  sessiyani qayta boshlaydi (avval faqat xotirada edi — operator „Save‘ qilsa ham agent eski
  manzilga ulanaverardi). `/api/ui/enroll` POST qo‘shildi: UI orqali §3.1 ro‘yxatdan o‘tish.
  UI‘dagi „Enroll‘ tugmasi shu endpoint‘ga ulandi.
| 1.0-draft.7 | 2026-11-20 | §3.1: enrollment kodi **8 xonali raqamli son** qilib o‘zgartirildi (operator talabi — kiritish oson bo‘lsin); qabulda bo‘sh joy/tire yuboriladi. Eski formatdagi kodlar endi yuborilmaydi. |
| 1.0-draft.6 | 2026-11-20 | §15 izohlar: agent WS `ping` kelganda read deadline'ini uzatishi shart (kontrol frame deadline uzarmaydi); WHEP read so'rovida token URL query'da bo'lishi shart (`Authorization` hook'ga yetmaydi); publish GOP 2 s dan katta bo'lmasligi talabi (o'quvchi birinchi keyframe'ni kutadi - 15 fps'da default gop=250 bilan 14.4 s kechikish olchandi); `paths/get` bir object qaytaradi, `readers` massivi. |
| 1.0-draft.5 | 2026-11-19 | §17.1 ga kamera CRUD endpoint'lari to'liq yozildi (`/api/ui/cams` GET/POST/DELETE, `/api/ui/cams/{id}` PUT, import/eksport) va 501 qoidasi. §8.1 B-frame aniqlash: `profile-level-id` yetarli emas — H.264 SPS VUI `max_num_reorder_frames` o'qiladi (o'z parserimiz, golden + ffprobe orakoli bilan tasdiqlandi). |
| 1.0-draft.4 | 2026-11-19 | §3.2/§10.1 aniqlashtirildi: state.json'da `secret_ref` yoziladi, shifrlangan secret emas. §3.1 ga kod berish usuli qo'shildi (`-enroll` yoki UI). §18: devserver handshake tartibi (auth→hello→welcome; 4001/4002/4003) va dev persistensiyasi. C1.1–C1.10 amalda ishlagani tasdiqlandi |
| 1.0-draft.3 | 2026-11-19 | Lokal UI porti **8080 → 8082** (§10.3, §11) — 8080 bu muhitda band xizmatlar bilan to'qnashdi. Kamera HTTP porti (joy ichida 80/8080, §10.5) o'zgarmagan; server API 8081 va MediaMTX 8889/8189/8554 ham o'zgarmagan. **§17.1** qo'shildi: lokal UI HTTP API ro'yxati (`/api/ui/log`, favicon, noma'lum `/api/*` → JSON 404) — brauzer konsolidagi 404 xatolari sababi shu edi |
| 1.0-draft.2 | 2026-11-19 | §18.2 qo'shildi: medium effort test intizomi (golden/fixture, fake clock, single-writer, goleak/-race, coverage bo'sagasi, reason matritsasi); WS-Discovery multicast manzili tuzatildi (`239.255.255.250:3702`); vmsd API porti (default 8081) aniqlashtirildi |
| 1.0-draft.1 | 2026-11-19 | Client tomonida tuzilgan: enrollment endpoint, WS `auth`/`welcome`, `media_start.publish.urls`, token semantikasi tuzatildi, `cams_sync.op/epoch`, kamera `id` agentda, refcount/TTL watchdog/graceful shutdown, ingest kutubxonalari (gortsplib + pion, FFmpeg fallback), discovery/bulk add, lokal UI dizayn talabi, `client.yaml` sxemasi, reason kodlari, auth-hook `action` ro'yxati tuzatildi |

