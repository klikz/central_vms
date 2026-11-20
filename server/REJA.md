# Central VMS — SERVER qismi reyasi (vmsd + MediaMTX + web UI)

Mustaqil loyiha: `server/` — alohida go module, alohida `Makefile`, alohida `build/` chiqishi.
Client (`vmsagent`) bilan **faqat tarmoq orqali** bog'lanadi. Bu fayl root `../REJA.md` ga taylanadi;
u yerda band raqamlari (§…) bilan berilgan — o'sha bandlar aynan shu ma'noda ishlatilgan.

Vazifalar ro'yxati: `TASKS.md` (shu papkada). Implementatsiya bosqichlari: **S0 → S4**.

---

## 1. Doira (nima serverda, nima emas)

| Serverda | Clientda (qarang `../client/REJA.md`) |
|---|---|
| `vmsd`: auth, registry, session manager, limits, audit | kamera CRUD, RTSP/ONVIF probe, parol store |
| MediaMTX (ingest + fan-out), versiya qulflangan | WHIP / RTSP-TCP publish (ingest) |
| Web UI (React) — operator brauzeri | lokal UI `:8082` (faqat 127.0.0.1) — joy operatorlari |
| Server-side transcode (server GPU — NVENC/VAAPI/software, avtomatik tanlanadi) | zaxira transcode (joy GPU/CPU) |
| SQLite (WAL), migratsiyalar, backup | lokal holat fayli (shifrlangan parollar) |
| `/internal/auth` hook (MediaMTX → vmsd) | — |

**Serverda kamera paroli SAQLANMAYDI** (root §2) — faqat mask'langan URL va metadata.

## 2. Chegara kontraktlari (client bilan umumiy — yagona manba: **`client/docs/protocol.md`**, 1.0-draft.5, **yozilgan va muzlatilgan**)

S0'da bu fayl `docs/protocol.md` ga ko'chiriladi (yoki symlink); keyin o'zgarish faqat versiya ko'tarib changelog orqali.

1. **Agent WS** `/api/agent/ws?agent_id=…` — envelope `{type, seq, ts, data}`; handshake (amalda, protocol.md §18): `auth {agent_id, secret}` → `hello {…}` → `welcome {server_version, compat, last_seq, cams_epoch, limits, settings, needs_full_sync}` (4001/4002/4003 kodlari shu tartibga bog'liq).
   Xabarlar (protocol.md §4–§5): `hello/cams_sync(op:upsert|delete, epoch)/cam_status/codec_probe/media_ready(publish_id)/media_stopped(reason)/media_stats/hb/discovery_done/selftest_done` ← `cams_request/cams_ack/media_start/media_stop/media_refresh/codec_fix(apply_all)/secret_rotate/config_push/ping/error`.
   - `media_start` ichida: `session_id, camera_id, profile, kind, ttl_s, ingest: whip|rtsp_tcp,
     publish:{token, urls:{whip, rtsp, rtsp_tc}, header_name, query_name}, transcode:{mode, by, backend, params}, reason`
     (`whip_url`/`publish_token` eski maydonlari **bekor**).
2. **Enrollment:** `POST /api/agent/enroll` (ochiq endpoint, rate-limit 5/5 min) → `{agent_id, agent_secret, protocol_version}`;
   `agent_id` ni server beradi (`ag_…`), `agent_secret` DB'da sha256 hash. Kamera `id` ni **agent** beradi (UUIDv4, o'zgarmas).
3. **Ingest rejimi (tasdiqlangan):** H.264 → **WHIP (UDP, pion/webrtc v4)**; H.265 → **RTSP/TCP publish (gortsplib v4)**.
   **pion/webrtc v4'da H.265 packetizer yo'q** — H.265 WHIP orqali yuborilmaydi (protocol.md §8).
4. **UI WS** `/ws/ui` — root §7-B + `session_state` (§7.1 holat mashinasi).
5. **MediaMTX auth-hook** — protocol.md §15: `authMethod: http` → `POST http://127.0.0.1:8189/internal/auth`;
   `action: publish|read|playback|api|metrics|pprof` (**`update` yo'q**); token: RTSP'da `?token=`,
   **WHIP/WHEP'da `Authorization: Bearer`**; token **bir martalik emas** (TTL davomida bitta session/path/action);
   javobda `expireAt` berilsa MediaMTX sessiyani o'zi yopadi; `tc/...` publish'i uchun alohida ichki token.
6. **HTTP API** — root §6 (+ `POST /api/agent/enroll`).
7. `client/docs/protocol.md` **muzlatilgan** — client shu shartnomaga qarab kod yozadi; o'zgarish faqat kelishib.
8. **Proto Go tiplari (tasdiqlangan 2026-11-20):** server `internal/proto` paketi — client tiplarining **nusxasi** (modullar mustaqil qoladi, umumiy `shared/` modul YO'Q). Sinxronlik `testdata/protocol/*.json` golden fixture'lari orqali (protocol.md'dagi JSON misollar ko'chiriladi, encode/decode round-trip) — kontrakt o'zgarsa fixture'da ko'rinadi.

### 2.1 Tasdiqlangan qarorlar (2026-11-20, server ishi boshlanishida)

| # | Qaror |
|---|---|
| 1 | **Dev portlar to'qnashuvi:** dev mashinada tashqi (`premier-cv`) MediaMTX 8554/8888/8889 ni band qiladi (manba: `~/Desktop/count_machines` → `/opt/premier-cv`, unit `premier-cv-mediamtx.service`, user `premiercv`). Dev ishga tushmasdan oldin u xizmatni **operator o'zi to'xtatadi** (`sudo systemctl stop premier-cv-mediamtx`); `make start`/`make media` ochilishda 8554/8888/8889/8189/8081 band-emasligini tekshiradi va band bo'lsa **aniq xato** beradi. VMS o'zi tashqi xizmatni to'xtatmaydi. **Diqqat:** u xizmatning MediaMTX'i **UDP 8189** (WebRTC ICE) va **UDP 9997** ni ham ishlatadi — bizning auth-hook TCP 8189'da, shuning uchun tekshiruv TCP + UDP bo'yicha o'tkaziladi. |
| 2 | **Kontrakt versiyasi: 1.0-draft.5** (`client/docs/protocol.md` hozirgi holati) — server `docs/protocol.md` shu nusxani oladi; eski draft.4 havolalari hujjatlarda yangilandi. |
| 3 | **`MEDIAMTX_VERSION = v1.11.3`** (dev mashinada o'rnatilgan versiya) — `make media-get` shu tag'ni oladi. |
| 4 | **Proto tiplari serverda nusxa** (§2.8) — `shared/` modul chiqarilmaydi. |
| 5 | **GPU modeli oldindan belgilangan emas** — prod serverga istalgan GPU o'rnatilishi mumkin yoki umuman bo'lmasligi mumkin. Transcode backend'i ishga tushishda avtomatik aniqlanadi: `h264_nvenc`(+`hevc_cuvid`) → `h264_vaapi`/`h264_amf` → `libx264` (software). Parallel limit `make probe` bilan har mashinada o'lchanadi. Kodda GPU modeli **hardcode taqiqlangan**. |
| 6 | **Runtime probe (2026-11-20, amalda kuzatilgan):** `ffmpeg -encoders` ro'yxatida bo'lishi encoder HAQIQATAN ishlashini anglatmaydi — dev mashinada `h264_nvenc` ro'yxatda bor, lekin drayver eski (`Required: 13.1, Found: 13.0`) va birinchi kadrdanoq yiqiladi. Shuning uchun: (a) ishga tushishda `gpu.Probe` har backend'da 1 kadr encode qilib ko'radi va ishlaydiganini tanlaydi; (b) transcode jarayoni 5 s ichida yiqilsa Transcoder o'sha backend'ni "yomon" deb belgilaydi, menejer zanjir bo'yicha (nvenc → vaapi → amf → libx264) qayta urinishadi; (c) `make probe` parallel sig'imni o'lchab `settings.server_max_transcodes` ga yozadi. GPU almashtirilsa yoki drayver eskirsa qayta qurish/sozlash talab qilinmaydi. |

## 3. Komponentlar

```
cmd/vmsd/main.go          # bitta binary: API + WS + statika serve (-web ./web)
internal/auth/            # argon2id, JWT (access 15m + refresh cookie), rol middleware
internal/registry/        # sites, clients, cameras, favorites, layouts CRUD
internal/wsagent/         # agent WS hub (enroll, sync, media_start/stop)
internal/wsui/            # UI WS hub (status push, session_state)
internal/proto/           # WS xabar Go tiplari (client nusxasi, mantiq yo'q) + golden testlar (§2.8)
internal/gpu/             # server quvvatini aniqlash: backend (nvenc/vaapi/amf/software) +
                          # probe (NVENC/NVDEC/CPU limit) → settings
internal/media/           # MediaMTX control API klienti, session manager (7.1), token mint,
                          # transcode manager (kamera bo'yicha dedup, backend internal/gpu dan),
                          # idle stop (readers polling)
internal/store/           # SQLite modernc.org/sqlite (CGO yo'q) + migratsiya runner
internal/limits/          # user 16 / global 48 / per-client max_uploads / preview 1 va 8 / navbat
internal/audit/           # audit_log
internal/config/          # YAML (config/dev.yaml, prod.yaml.example)
migrations/               # .sql
webui/                    # React + TS + Vite → build/web (diskdan serve, embed YO'Q)
mediamtx/mediamtx.yml     # auth hook, path pattern cam/~ + tc/~, faqat RTSP+WebRTC+HLS,
                          # webrtcAdditionalHosts, writeQueueSize... (root §16.3-16.4;
                          # v1.11.3'da udpReadBufferSize YO'Q — sysctl bilan)
```

## 4. Serverga xos qarorlar (tasdiqlangan)

- **Holat mashinasi §7.1:** `pending → publishing → playing → stopping → ended/failed`; taymautlar: `media_ready` 10 s, WHEP ulanish 10 s, transcode publish 15 s; TTL 5 min / uzaytirish 2 min.
- **Transcode:** default server, **har kamera bitta FFmpeg** — barcha tomoshobilarga umumiy; GPU sessiyalari = H.265 kamera soni. **GPU modeli o'zgaruvchan (tasdiqlangan 2026-11-20):** backend avtomatik tanlanadi — `h264_nvenc`(+`hevc_cuvid`) → `h264_vaapi`/`h264_amf` → `libx264` software; `server_max_transcodes` boshlang'ich 5, `make probe` bilan **har mashinada** o'lchanadi (NVENC: 3–8; software: CPU yadrolar hisobiga). Zaxira: `transcode.by=client` deb agent'ga buyruq berish (server o'zi joy GPU'sini bilmaydi — `caps` dan). Model nomi kodda hardcode qilinmaydi.
- **H.265 ingest = RTSP/TCP** (WHIP emas) — MediaMTX'da barqaror yo'l.
- **MediaMTX versiyasi qulflangan:** Makefile'da **`MEDIAMTX_VERSION = v1.11.3`** (tasdiqlangan 2026-11-20), `make media-get` o'sha tag'ni oladi; prod'da ham xuddi shu.
- **Brauzer HEVC (2.5-qadam):** UI `browser_caps` yuboradi → HEVC'li brauzerda H.265 transcodesiz (`cam/...` WHEP).
- **UI:** o'zbek tili; grid default **3x3**; pagination; preview debounce **600 ms dwell / 2 s min / 1 s close**; IntersectionObserver + Page Visibility; "transcode boshlandi…", "navbatda", limit toastlari.
- **Kechikish mezoni:** ≤1.5 s (target 0.5–1.0 s, eng yaxshi ~0.3 s).
- **Operator PC minimal** (root §10) — UI shu konfiguratsiyada 4x4'ni ko'taradi.
- **Xavfsizlik (TLS'siz):** qisqa TTL tokenlar, WHEP/WHIP token bir marta, login rate-limit, agent_secret rotatsiyasi, audit. Eslatma: WebRTC media DTLS bilan shifrlangan — ochiq qolgan narsa asosan HTTP/WS boshqaruv kanali.
- **Retention:** `sessions`/`audit_log` 90 kun; SQLite kundalik `VACUUM INTO` backup.

## 5. Make targetlari

Root §4.2 server jadvali bilan aynan bir xil: `start, dev, build, run, media, media-get, probe, migrate-up/down/status, ui-build, ui-dev, test, lint, fmt, package, install, logs, clean`.
`build/` tarkibi root §4.1 bo'yicha: `vmsd`, `web/`, `migrations/`, `mediamtx/mediamtx.yml`, `config/config.yaml`, `VERSION`, `run.sh`.
**Port tekshiruvi (tasdiqlangan 2026-11-20):** `make start` / `make media` / `vmsd` ochilishda 8554/8888/8889/8189/8081 bandligini tekshiradi — band bo'lsa aniq xato ("port band — tashqi MediaMTX'ni to'xtating"), avtomatik to'xtatish YO'Q.

## 6. Test strategiyasi (client tayyor bo'lmaganda ham)

- **`testagent`** (S2, server repoda oddiy utilita): soxta agent — cams_sync, cam_status va RTSP manbadan (masalan test pattern FFmpeg) haqiqiy WHIP/RTSP publish qiladi. Shu orqali session manager, limits, transcode, auth-hook to'liq tekshiriladi — client kutilmaydi.
- Integratsiya: `kameraning haqiqiy o'rniga` FFmpeg bilan generatsiya qilingan RTSP stream (`testsrc`, keyin real Dahua).
- Load: 48 copy sessiya 30 daq + transcode limiti (`make probe`). **Diqqat:** dev mashina GPU'si prod'dan farq qilishi mumkin — probe natijalari faqat orientir, prod'ga o'rnatilganda `make probe` qayta ishga tushiriladi (`install-notes.md` majburiy qadami).

## 7. Qabul mezoni (server MVP)

- `cd server && make build` mustaqil ishlaydi; `make start` MediaMTX + vmsd'ni ko'taradi; port band bo'lsa (masalan tashqi MediaMTX turganda) **aniq tushunarli xato** beradi, ko'r yopilmaydi.
- Transcode backend'i mashinaga moslashadi: NVIDIA'li mashinada `nvenc`, Intel/AMD'da `vaapi/amf`, GPU bo'lmaganda `libx264` — dastur GPU yo'qda ham ishlaydi, limit probe bilan to'g'ri o'lchanadi.
- Test agent orqali: 100 kamera ro'yxat/status/kodek; play → WHEP ≤1.5 s; idle 10 s → upload to'xtaydi.
- 2 user bitta H.265 kamerani ochsa — **bitta** FFmpeg jarayoni; "transcode boshlandi…" ko'rinadi.
- Limitlar, holat mashinasi taymautlari, auth-hook (token bir marta) ishlaydi.
- UI: bloklar, grid, pagination, layout, favorites, debounce, o'zbek til.
