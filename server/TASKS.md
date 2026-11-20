# SERVER vazifalar (S0 → S4)

Belgilar: `[ ]` bajarilmagan, `[x]` bajarilgan. **Dependence** — qaysi vazifa oldin/tayyor bo'lishi kerak.
Har bosqich oxirida "Qabul" bandidagi tekshiruv o'tgan bo'lishi shart.
**Ish tartibi (medium effort rejimi):** mayda qadamlar (<=120 qator/qadam), har qadamda kontrakt §
ko'rsatkichi, fixture/golden testlar, fake clock, single-writer goroutine, `goleak` + `-race`,
coverage bo'sagasi — qoidalar: `client/docs/protocol.md §18.2` va `client/TASKS.md §0`.

---

## S0 — Skelet va kontraktlar (≈2 kun)

- [ ] `go.mod` (mustaqil module), `cmd/vmsd/main.go` (flag: `-config`, `-web`, `--probe`), `internal/` bo'sh paket skeletlari
- [ ] `Makefile`: `start, dev, build, run, media, media-get, probe, migrate-up/down/status, ui-build, ui-dev, test, lint, fmt, package, install, logs, clean`
- [ ] `MEDIAMTX_VERSION = <git tag>` qulflanadi; `make media-get` o'sha tag binary'sini `mediamtx/mediamtx` ga soladi (git'da emas)
- [ ] `mediamtx/mediamtx.yml`: `authMethod: http` → `http://127.0.0.1:8189/internal/auth`; path pattern `cam/{client}/{camera}` + `tc/{client}/{camera}`; faqat RTSP + WebRTC + HLS; `webrtcAdditionalHosts` = static IP; `udpReadBufferSize`, `writeQueueSize`, `pprof: yes`
- [ ] `config/dev.yaml`, `config/prod.yaml.example`
- [ ] `migrations/0001_init.sql`: root §5 sxemasi (users, sites, clients, cameras, favorites, layouts, sessions, audit_log, settings) + indekslar (`cameras.client_id`, `sessions.camera_id`); migratsiya runner (`internal/store`)
- [ ] `make build` → `build/` tarkibi root §4.1 (vmsd, web/, migrations/, mediamtx.yml, config, VERSION, run.sh); `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`
- [ ] **`docs/protocol.md`**: **`client/docs/protocol.md` (1.0-draft.4) dan ko'chiriladi** (yoki symlink) — server tomonida faqat tekshiriladi va tasdiqlanadi; xabar sxemalari, §7.1 holat mashinasi + taymautlar, auth-hook, URL/token formatlari, `ingest: whip|rtsp_tcp` — **muzlatilgan**
- [ ] **`docs/api.md`**: root §6 to'liq + xato kodlari + **`POST /api/agent/enroll`** (protocol.md §3.1) + Dahua RTSP URL namunasi (`/cam/realmonitor?channel=1&subtype=0|1`)
- [ ] `.gitignore` (build/, node_modules/, dist/, *.local.yaml)
- [ ] **Loglar (talab 2026-11-19):** barcha xatolar terminalga batafsil (`level=ERROR`, `err=` zanjiri, kontekst maydonlari) VA alohida SERVER log fayliga yoziladi: `/var/log/vmsd/vmsd.log` (`log_file`/`log_max_mb`/`log_keep`, hajm rotatsiyasi). **Client (`vmsagent`) logi bilan ARALASHMAYDI** — har jarayon o'z faylida (qida: `client/deploy/agent-logging.md`). `make logs` shu faylni ochadi.

**Qabul:** `cd server && make build` mustaqil ishlaydi; `make start` MediaMTX + vmsd'ni ko'taradi, Ctrl+C'da ikkalasi ham to'xtaydi; migratsiya avtomatik.

---

## S1 — Auth, registry, agent WS (≈3–4 kun) — *Dependence: S0*

- [ ] argon2id parol hash; login rate-limit (5/5 min); JWT access 15 min + refresh cookie (httpOnly, SameSite=Lax)
- [ ] rol middleware: admin / viewer; `POST /api/auth/*`, `GET /api/me`
- [ ] registry CRUD: users (admin), sites, clients, cameras (PATCH policy: `transcode_mode`, `play_profile`, `enabled`); **`cameras.id` — agent bergan UUIDv4 (o'zgarmas), server uni PK sifatida saqlaydi**
- [ ] enrollment: `POST /api/admin/enrollments` → bir martalik kod (24 soat); **`POST /api/agent/enroll`** (ochiq endpoint, rate-limit 5/5 min, `401 bad_code` / `409 already_enrolled` / `429`) → `agent_id` (`ag_…`) + `agent_secret` (DB'da sha256); admin nom/site belgilaydi; unikal nom tekshiruvi
- [ ] `internal/wsagent`: **`auth {agent_id, secret}` → `welcome {server_version, compat, last_seq, cams_epoch, limits, settings, needs_full_sync}`** handshake (≤5 s, kodlar 4001–4004); envelope `{type, seq, ts, data}`; `hello` (version, os, hostname, lan_ip, boot_id, **cams_epoch**, **caps**) qabul + `clients.caps_json`; `cams_sync` chunk'lab + **`op: upsert|delete`** (o'chirish cascade: favorites/layouts slot bo'shatish) + **`cams_ack`** javob; `cam_status`, `codec_probe` saqlash; `hb`/ping-pong + uzilishda status → offline (30 s); **`secret_rotate`** va **`config_push`** yuborish
- [ ] `GET /api/sites`, `/api/clients`, `/api/clients/{id}/cameras`, `/api/cameras/{id}`
- [ ] audit_log yozuvlari (login, enroll, play, admin amallari)
- [ ] `testagent` skelet (fake agent: cams_sync + cam_status) — keyinchalik S2'da publish ham qiladi

**Qabul:** testagent ulanadi, 100 kamera serverda ko'rinadi, status/kodek/caps yangilanadi; kamera o'chirilsa (`op:delete`) serverda ham o'chadi va favorites/layouts sloti bo'shaydi; `cams_epoch` farqida to'liq sync bo'ladi; viewer boshqa user ma'lumotini ko'rmaydi.

---

## S2 — Media server: session manager + transcode (≈7–10 kun) — *Dependence: S1; real agent uchun client C3, lekin testagent bilan ham yuradi*

- [ ] MediaMTX control API klienti (path qo'shish/o'chirish, readers count polling ~2 s)
- [ ] **Session manager + holat mashinasi (§7.1):** `pending → publishing → playing → stopping → ended/failed`; taymautlar 10 s (`media_ready`), 10 s (WHEP), 15 s (transcode publish); `session_state` WS push; `failed` limit hisoblagichidan qaytariladi
- [ ] Token mint: 256-bit (`base64url`), sha256 hash DB, TTL: read 30 s / publish `ttl_s` (60 s); **uzatish: RTSP'da `?token=`, WHIP/WHEP'da `Authorization: Bearer`** (token bir martalik emas — bitta session/path/action doirasida qayta ishlatiladi, protocol.md §7)
- [ ] `media_start` payload'ini protocol.md §5.2 bo'yicha qurish: `publish:{token, urls:{whip, rtsp, rtsp_tc}, header_name, query_name}`, `ttl_s`, `transcode:{mode, by, backend, params}`; **`media_refresh`** (30–60 s) va **`media_stats`** qabuli; `media_ready/media_stopped` da `publish_id` + `reason` kodlari (§13)
- [ ] **`/internal/auth` hook:** MediaMTX JSON (`action: publish|read|playback|api|metrics|pprof`, `path, protocol, user, password, token, ip, id, query, userAgent`) → token→sessiya→path→user tekshiruvi → `{allow:true}` / 403; javobda **`expireAt`** qaytarish (MediaMTX sessiyani o'zi yopsin); `tc/...` ichki tokeni
- [ ] `POST /api/cameras/{id}/play` (profile default `sub`, `browser.hevc`), `/preview`, `/sessions/{id}/stop`, `GET /sessions`
- [ ] `internal/limits`: user 16 / global 48 / per-client `max_uploads` / preview 1 va 8; chegarada aniq xato + navbat holati
- [ ] Idle stop: readers=0 → 10 s (preview ~1 s) → `media_stop` agent'ga + path o'chirish
- [ ] **Transcode manager (server):** FFmpeg subprocess (`hevc decode → h264_nvenc`, parametrlar root §16.3); **kamera bo'yicha dedup** (bitta jarayon, refcount); `tc/...` path; navbat; jarayon o'ldirish (idle, fail); `nvidia-smi` kuzatuvi (admin panel uchun)
- [ ] `make probe` / `vmsd --probe`: NVENC **va NVDEC** parallel sessiya limitini o'lchash → `settings.server_max_transcodes`
- [ ] Kodek siyosati server qismi: `play_profile` tanlash (H.264 eng past profil), `browser_caps.hevc` → transcodesiz `cam/...` yo'li, `transcode_mode auto|server|client|off` (zaxira → agent'ga `transcode.by=client`)
- [ ] LL-HLS fallback: MediaMTX config + UI'ga signal (`hls.js`)
- [ ] `testagent`ni to'liq publish'ga kengaytirish (WHIP H.264 + RTSP/TCP H.265, FFmpeg test pattern)
- [ ] `internal/wsui`: `client_status`, `cam_status`, `session_count`, `session_revoked`, `session_state`

**Qabul:** testagent orqali — H.264 kamera WHEP'da ≤1.5 s ochiladi; H.265 → server NVENC transcode, 2 user = **1 FFmpeg**; `media_ready` 10 s taymauti `failed` + qayta urinish; yopilsa upload to'xtaydi; token qayta ishlatilsa 403.

---

## S3 — Web UI (≈6–8 kun) — *Dependence: S2 API*

- [ ] React + TS + Vite skelet, router, o'zbek til (bitta `i18n` fayl, so'zlar bir joyda)
- [ ] Login + refresh cookie bilan avtomatik kirish; xato sanab o'tish
- [ ] `lib/api.ts`, `lib/ws.ts`, `lib/whep.ts` (native RTCPeerConnection ~120 qator), `lib/limits.ts`
- [ ] Chap panel: joy sarlavhasi + client bloklari (status nuqta, online/total); blok bosilganda kamera ro'yxati (nom, status, kodek badge)
- [ ] **Preview:** debounce 600 ms dwell / min 2 s / close 1 s; WHEP mini player; dedup — kamera grid'da ochiq bo'lsa qo'shimcha sessiya ochilmaydi; `preview_max` toast
- [ ] Grid 1x1–4x4 (default 3x3); joy tanlanganda **avtomatik to'ldirish** (`position` bo'yicha); pagination (sahifa o'zgarganda eski oqimlar darhol stop)
- [ ] Drag&drop: ro'yxatdan katakka, ikki katakni almashtirish; double-click → fullscreen + `main`
- [ ] Klaviatura: `1/2/3/4`, `←/→`, `Esc`
- [ ] Favorites (⭐, reorder); Layouts: saqlash/yuklash/default, aralash joy; o'chirilgan kamera sloti → bo'sh katak
- [ ] **IntersectionObserver + Page Visibility** — ko'rinmayotgan katak / yashirin tab → oqim stop
- [ ] Holatlar: spinner, "transcode boshlandi…", "navbatda", limit toastlari, offline belgilar, xato + "qayta urinish"
- [ ] `browser_caps` (HEVC aniqlash) birinchi ulanishda yuboriladi
- [ ] `getStats()` monitoring: yomon inbound bitrate → avtomatik past profil haqidagi taklif (keyin to'liq avtomatik)

**Qabul:** 2 user parallel 15 tadan kamera; layout saqlanadi/tiklanadi; preview og'irlik yaratmaydi; tab yashirinsa barcha oqim to'xtaydi; UI to'liq o'zbekcha.

---

## S4 — Qotirish, kuzatish, deploy (≈3–4 kun) — *Dependence: S2, S3*

- [ ] Admin panel: `settings` limitlari, `/api/admin/media/state` (faol sessiyalar, FFmpeg jarayonlari, NVENC sessiyalari, navbat), audit ko'rinishi
- [ ] Retention: `sessions`/`audit_log` 90 kundan eski o'chiriladi (cron-ish job vmsd ichida)
- [ ] `deploy/backup.sh`: SQLite `VACUUM INTO` kundalik; journald disk chegarasi
- [ ] MediaMTX tuning tekshiruvi: `udpReadBufferSize: 1000000` + sysctl, `writeQueueSize: 1024`, protokollar cheklangan; `pprof` + `/metrics`
- [ ] `vmsd` `/metrics` (Prometheus): sessiyalar, limitlar, transcode navbati, hook natijalari
- [ ] systemd unitlar: `vmsd` + `vms-media` (MediaMTX prod alohida unit); `make install` /vmsd'ni `/opt/vmsd` ga soladi
- [ ] `deploy/firewall.md` (WebRTC UDP port oralig'i, RTSP 8554, WHEP 8889, WS), `install-notes.md`
- [ ] Load test: 48 copy sessiya 30 daq uzluksiz, memory o'smaydi; NVENC chegarasi + zaxira zanjir (`transcode.by=client`) o'lchangan

**Qabul:** root §15 "MVP bajarildi" ro'yxatidagi server bandlari yashil; load test natijasi `docs/` da.

---

## Keyingi bosqich (P6, rejalashtirilgan, hozir ISHLAMAYMIZ)

TLS (Caddy), `user_site_grants`, Telegram alert, snapshot preview (`PreviewProvider` abstraksiyasi S3'da tayyor bo'ladi), arxiv playback.
