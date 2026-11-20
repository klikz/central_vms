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
| Server-side transcode (RTX 3070 NVENC) | zaxira transcode (joy GPU/CPU) |
| SQLite (WAL), migratsiyalar, backup | lokal holat fayli (shifrlangan parollar) |
| `/internal/auth` hook (MediaMTX → vmsd) | — |

**Serverda kamera paroli SAQLANMAYDI** (root §2) — faqat mask'langan URL va metadata.

## 2. Chegara kontraktlari (client bilan umumiy — yagona manba: **`client/docs/protocol.md`**, 1.0-draft.4, **yozilgan va muzlatilgan**)

S0'da bu fayl `docs/protocol.md` ga ko'chiriladi (yoki symlink); keyin o'zgarish faqat versiya ko'tarib changelog orqali.

1. **Agent WS** `/api/agent/ws?agent_id=…` — envelope `{type, seq, ts, data}`; handshake: `auth {agent_id, secret}` → `welcome {server_version, compat, last_seq, cams_epoch, limits, settings, needs_full_sync}`.
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

## 3. Komponentlar

```
cmd/vmsd/main.go          # bitta binary: API + WS + statika serve (-web ./web)
internal/auth/            # argon2id, JWT (access 15m + refresh cookie), rol middleware
internal/registry/        # sites, clients, cameras, favorites, layouts CRUD
internal/wsagent/         # agent WS hub (enroll, sync, media_start/stop)
internal/wsui/            # UI WS hub (status push, session_state)
internal/media/           # MediaMTX control API klienti, session manager (7.1), token mint,
                          # transcode manager (kamera bo'yicha dedup), idle stop (readers polling)
internal/store/           # SQLite modernc.org/sqlite (CGO yo'q) + migratsiya runner
internal/limits/          # user 16 / global 48 / per-client max_uploads / preview 1 va 8 / navbat
internal/audit/           # audit_log
internal/config/          # YAML (config/dev.yaml, prod.yaml.example)
migrations/               # .sql
webui/                    # React + TS + Vite → build/web (diskdan serve, embed YO'Q)
mediamtx/mediamtx.yml     # auth hook, path pattern cam/~ + tc/~, faqat RTSP+WebRTC+HLS,
                          # webrtcAdditionalHosts, udpReadBufferSize... (root §16.3-16.4)
```

## 4. Serverga xos qarorlar (tasdiqlangan)

- **Holat mashinasi §7.1:** `pending → publishing → playing → stopping → ended/failed`; taymautlar: `media_ready` 10 s, WHEP ulanish 10 s, transcode publish 15 s; TTL 5 min / uzaytirish 2 min.
- **Transcode:** default server (NVENC), **har kamera bitta FFmpeg** — barcha tomoshobilarga umumiy; NVENC sessiyalari = H.265 kamera soni. `server_max_transcodes` boshlang'ich 5, `make probe` bilan o'lchanadi (3–8). Zaxira: `transcode.by=client` deb agent'ga buyruq berish (server o'zi joy GPU'sini bilmaydi — `caps` dan).
- **H.265 ingest = RTSP/TCP** (WHIP emas) — MediaMTX'da barqaror yo'l.
- **MediaMTX versiyasi qulflangan:** Makefile'da `MEDIAMTX_VERSION = <git tag>` (S0'da tanlanadi), `make media-get` o'sha tag'ni oladi; prod'da ham xuddi shu.
- **Brauzer HEVC (2.5-qadam):** UI `browser_caps` yuboradi → HEVC'li brauzerda H.265 transcodesiz (`cam/...` WHEP).
- **UI:** o'zbek tili; grid default **3x3**; pagination; preview debounce **600 ms dwell / 2 s min / 1 s close**; IntersectionObserver + Page Visibility; "transcode boshlandi…", "navbatda", limit toastlari.
- **Kechikish mezoni:** ≤1.5 s (target 0.5–1.0 s, eng yaxshi ~0.3 s).
- **Operator PC minimal** (root §10) — UI shu konfiguratsiyada 4x4'ni ko'taradi.
- **Xavfsizlik (TLS'siz):** qisqa TTL tokenlar, WHEP/WHIP token bir marta, login rate-limit, agent_secret rotatsiyasi, audit. Eslatma: WebRTC media DTLS bilan shifrlangan — ochiq qolgan narsa asosan HTTP/WS boshqaruv kanali.
- **Retention:** `sessions`/`audit_log` 90 kun; SQLite kundalik `VACUUM INTO` backup.

## 5. Make targetlari

Root §4.2 server jadvali bilan aynan bir xil: `start, dev, build, run, media, media-get, probe, migrate-up/down/status, ui-build, ui-dev, test, lint, fmt, package, install, logs, clean`.
`build/` tarkibi root §4.1 bo'yicha: `vmsd`, `web/`, `migrations/`, `mediamtx/mediamtx.yml`, `config/config.yaml`, `VERSION`, `run.sh`.

## 6. Test strategiyasi (client tayyor bo'lmaganda ham)

- **`testagent`** (S2, server repoda oddiy utilita): soxta agent — cams_sync, cam_status va RTSP manbadan (masalan test pattern FFmpeg) haqiqiy WHIP/RTSP publish qiladi. Shu orqali session manager, limits, transcode, auth-hook to'liq tekshiriladi — client kutilmaydi.
- Integratsiya: `kameraning haqiqiy o'rniga` FFmpeg bilan generatsiya qilingan RTSP stream (`testsrc`, keyin real Dahua).
- Load: 48 copy sessiya 30 daq + NVENC limiti (`make probe`).

## 7. Qabul mezoni (server MVP)

- `cd server && make build` mustaqil ishlaydi; `make start` MediaMTX + vmsd'ni ko'taradi.
- Test agent orqali: 100 kamera ro'yxat/status/kodek; play → WHEP ≤1.5 s; idle 10 s → upload to'xtaydi.
- 2 user bitta H.265 kamerani ochsa — **bitta** FFmpeg jarayoni; "transcode boshlandi…" ko'rinadi.
- Limitlar, holat mashinasi taymautlari, auth-hook (token bir marta) ishlaydi.
- UI: bloklar, grid, pagination, layout, favorites, debounce, o'zbek til.
