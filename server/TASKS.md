# SERVER vazifalar (S0 → S4)

Belgilar: `[ ]` bajarilmagan, `[x]` bajarilgan. **Dependence** — qaysi vazifa oldin/tayyor bo'lishi kerak.
Har bosqich oxirida "Qabul" bandidagi tekshiruv o'tgan bo'lishi shart.
**Ish tartibi (medium effort rejimi):** mayda qadamlar (<=120 qator/qadam), har qadamda kontrakt §
ko'rsatkichi, fixture/golden testlar, fake clock, single-writer goroutine, `goleak` + `-race`,
coverage bo'sagasi — qoidalar: `client/docs/protocol.md §18.2` va `client/TASKS.md §0`.

**Qarorlar (2026-11-20, tasdiqlangan — batafsil `REJA.md §2.1`):** kontrakt **1.0-draft.5**;
`MEDIAMTX_VERSION = v1.11.3`; proto tiplari serverda **nusxa** (`internal/proto` + golden fixture);
dev portlari band bo'lsa `make start` **aniq xato** beradi (tashqi xizmatni biz to'xtatmaymiz);
**GPU modeli o'zgaruvchan** — transcode backend'i avtomatik aniqlanadi (`internal/gpu`), limit
har mashinada `make probe` bilan o'lchanadi, model nomi kodda hardcode **taqiqlangan**.

---

## S0 — Skelet va kontraktlar (≈2 kun)

- [x] `go.mod` (mustaqil module), `cmd/vmsd/main.go` (flag: `-config`, `-web`, `--probe`), `internal/` bo'sh paket skeletlari
- [x] `Makefile`: `start, dev, build, run, media, media-get, probe, migrate-up/down/status, ui-build, ui-dev, test, lint, fmt, package, install, logs, clean`
- [x] `MEDIAMTX_VERSION = v1.11.3` qulflangan (tasdiqlangan 2026-11-20); `make media-get` o'sha tag binary'sini `mediamtx/mediamtx` ga soladi (git'da emas)
- [x] **Port tekshiruvi (tasdiqlangan 2026-11-20):** `make start` / `make media` / `vmsd` ochilishda 8554/8888/8889/8189/8081 bandligini tekshiradi — band bo'lsa aniq xato + terminalga "tashqi MediaMTX ishlayapti, to'xtating" maslahati; tashqi jarayonni o'zi to'xtatmaydi. **Eslatma:** tashqi (`premier-cv`) MediaMTX faqat TCP emas, **UDP 8189** (WebRTC ICE) va RTSP UDP 8000/8001 ni ham ushlaydi (9997 — API, TCP) — tekshiruv TCP + UDP bo'yicha o'tkaziladi (`ss -ltn` va `ss -lun`), aks holda vmsd auth-hook porti 8189 jimgina band bo'lib qoladi. Amalda tekshirildi: v1.11.3 UDP 8189 + TCP 8189 (vmsd) yonma-yon ishlaydi (turli protokol) — [Test: band portda `vmsd` start'i → aniq xato, 2 s ichida chiqib ketadi]
- [x] `mediamtx/mediamtx.yml`: `authMethod: http` → `http://127.0.0.1:8189/internal/auth`; path pattern `cam/{client}/{camera}` + `tc/{client}/{camera}`; faqat RTSP + WebRTC + HLS; `webrtcAdditionalHosts` = static IP; `writeQueueSize`, `pprof: yes` (**v1.11.3'da `udpReadBufferSize` yo'q** — sysctl, S4)
- [x] `config/dev.yaml`, `config/prod.yaml.example`
- [x] `migrations/0001_init.sql`: root §5 sxemasi (users, sites, clients, cameras, favorites, layouts, sessions, audit_log, settings) + indekslar (`cameras.client_id`, `sessions.camera_id`); migratsiya runner (`internal/store`)
- [x] `make build` → `build/` tarkibi root §4.1 (vmsd, web/, migrations/, mediamtx.yml, config, VERSION, run.sh); `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`
- [x] **`docs/protocol.md`**: **`client/docs/protocol.md` (1.0-draft.5) dan ko'chiriladi** (yoki symlink) — server tomonida faqat tekshiriladi va tasdiqlanadi; xabar sxemalari, §7.1 holat mashinasi + taymautlar, auth-hook, URL/token formatlari, `ingest: whip|rtsp_tcp` — **muzlatilgan**
- [x] **`internal/proto`** (S0 skelet): client `internal/proto` tiplarining **nusxasi** (envelope, auth/welcome, hello/cams_sync/cams_ack, media_start/stop/refresh/ready/stopped/stats, codec_probe, hb, secret_rotate, config_push) — [Kontrakt §2–§5] — [Test: `testdata/protocol/*.json` golden round-trip; protocol.md JSON misollari ko'chiriladi; coverage ≥70 %] — [Tekshiruv: `go test ./internal/proto -race`]
- [x] **`docs/api.md`**: root §6 to'liq + xato kodlari + **`POST /api/agent/enroll`** (protocol.md §3.1) + Dahua RTSP URL namunasi (`/cam/realmonitor?channel=1&subtype=0|1`)
- [x] `.gitignore` (build/, node_modules/, dist/, *.local.yaml)
- [x] **Loglar (talab 2026-11-19):** barcha xatolar terminalga batafsil (`level=ERROR`, `err=` zanjiri, kontekst maydonlari) VA alohida SERVER log fayliga yoziladi: `/var/log/vmsd/vmsd.log` (`log_file`/`log_max_mb`/`log_keep`, hajm rotatsiyasi). **Client (`vmsagent`) logi bilan ARALASHMAYDI** — har jarayon o'z faylida (qida: `client/deploy/agent-logging.md`). `make logs` shu faylni ochadi.

**Qabul:** `cd server && make build` mustaqil ishlaydi; `make start` MediaMTX + vmsd'ni ko'taradi, Ctrl+C'da ikkalasi ham to'xtaydi; migratsiya avtomatik; port band bo'lsa (tashqi MediaMTX turganda) **aniq xato** beradi; `internal/proto` golden testlari protocol.md misollari bilan yashil.


### S0 natijasi (2026-11-20) — **bajarildi**
- `make build` mustaqil ishlaydi; `build/` tarkibi §4.1 bo'yicha (vmsd, web/ placeholder, migrations/, mediamtx.yml, config, VERSION, run.sh).
- `make start`: MediaMTX v1.11.3 + vmsd ko'tarildi, barcha portlar ochiq, `/healthz` 200, MediaMTX API 200; SIGINT'da ikkalasi ham mayin to'xtadi, portlar bo'shadi.
- Port band holati sun'iy tekshirildi (8554+UDP o'g'ri bilan): aniq xato + maslahat, exit≠0.
- `make gate` (fmt vet lint race + coverage 59.6% ≥ 50) yashil; goleak barcha paketlarda.
- `internal/proto`: 30 golden fixture, coverage 100 %.
- `testagent` skeleti client `devserver` mock'ida: enroll → auth → hello → welcome → cams_sync (10 kamera) o'tdi (mock AutoCamsAck=false — ack ixtiyoriy).
- **Topilmigan farqlar (hujjatlar tuzatildi):** v1.11.3'da `udpReadBufferSize`/`readBufferCount` yo'q (sysctl); path bloklari `paths:` ostida `~regex` + `all_others`; handshake §18 bo'yicha `auth→hello→welcome`.

---

## S1 — Auth, registry, agent WS (≈3–4 kun) — *Dependence: S0*

- [x] argon2id parol hash; login rate-limit (5/5 min); JWT access 15 min + refresh cookie (httpOnly, SameSite=Lax)
- [x] rol middleware: admin / viewer; `POST /api/auth/*`, `GET /api/me`
- [x] registry CRUD: users (admin), sites, clients, cameras (PATCH policy: `transcode_mode`, `play_profile`, `enabled`); **`cameras.id` — agent bergan UUIDv4 (o'zgarmas), server uni PK sifatida saqlaydi**
- [x] enrollment: `POST /api/admin/enrollments` → bir martalik kod (24 soat); **`POST /api/agent/enroll`** (ochiq endpoint, rate-limit 5/5 min, `401 bad_code` / `409 already_enrolled` / `429`) → `agent_id` (`ag_…`) + `agent_secret` (DB'da sha256); admin nom/site belgilaydi; unikal nom tekshiruvi
- [x] `internal/wsagent`: **handshake tartibi (protocol.md §18): `auth {agent_id, secret}` → `hello` → `welcome {server_version, compat, last_seq, cams_epoch, limits, settings, needs_full_sync}`** (≤5 s, kodlar 4001–4004); envelope `{type, seq, ts, data}`; `hello` (version, os, hostname, lan_ip, boot_id, **cams_epoch**, **caps**) qabul + `clients.caps_json`; `cams_sync` chunk'lab + **`op: upsert|delete`** (o'chirish cascade: favorites/layouts slot bo'shatish) + **`cams_ack`** javob; `cam_status`, `codec_probe` saqlash; `hb`/ping-pong + uzilishda status → offline (30 s); **`secret_rotate`** va **`config_push`** yuborish
- [x] `GET /api/sites`, `/api/clients`, `/api/clients/{id}/cameras`, `/api/cameras/{id}`
- [x] audit_log yozuvlari (login, enroll, play, admin amallari)
- [x] `testagent` skelet (fake agent: cams_sync + cam_status) — keyinchalik S2'da publish ham qiladi

**Qabul:** testagent ulanadi, 100 kamera serverda ko'rinadi, status/kodek/caps yangilanadi; kamera o'chirilsa (`op:delete`) serverda ham o'chadi va favorites/layouts sloti bo'shaydi; `cams_epoch` farqida to'liq sync bo'ladi; viewer boshqa user ma'lumotini ko'rmaydi.

---

### S1 natijasi (2026-11-20) — **bajarildi**
- Qabul oqimi amalda ko'rsatildi: `seed-admin` → login (JWT+cookie) → admin enroll kodi → `testagent` enroll → WS handshake → `cams_sync` 100 kamera → API `/api/clients/{id}/cameras` = 100; agent uzilganda client+kameralar `offline`.
- `op:delete` cascade (favorites FK + layout slot `null`) va eski `cams_epoch` → `stale_epoch` ack — testlar yashil; policy (`transcode_mode` va h.k.) qayta sync'da saqlanadi (test).
- Viewer admin endpoint'larida 403; login rate-limit 5/5 min → 429; refresh cookie httpOnly+Lax, rotatsiyalanadi.
- `make gate` yashil: coverage 60 % (wsagent 78.5 %, proto 100 %); goleak barcha paketlarda.
- **S2'ga qoldiq:** server tomonidan `cams_request` tashabbusi (hozircha `welcome.needs_full_sync` bayrog'i bilan agent boshlaydi — epoch solishtiruvi ishlaydi); `/api/cameras/{id}/play` va sessiyalar — S2.

---

## S2 — Media server: session manager + transcode (≈7–10 kun) — *Dependence: S1; real agent uchun client C3, lekin testagent bilan ham yuradi*

- [x] **`internal/gpu` — server quvvatini aniqlash (tasdiqlangan 2026-11-20, GPU modeli o'zgaruvchan):** `ffmpeg -encoders/-decoders` parse + `nvidia-smi`/`vainfo` tekshiruvi → backend tanlash `nvenc → vaapi/amf → software libx264` (client `caps` parse uslubiga o'xshash, lekin server tomoni); natija `settings` (`server_transcode_backend`, `server_max_transcodes`) va admin panelga; **GPU modeli nomiga qarab tarmoq qilmaydi — faqat imkoniyatlarga qaraydi** — [Test: fixture `testdata/encoders/*.txt` (nvenc-li, vaapi-li, GPU-yo'q 3 holat) → to'g'ri backend] — [Tekshiruv: `go test ./internal/gpu -race`]
- [x] MediaMTX control API klienti (path qo'shish/o'chirish, readers count polling ~2 s)
- [x] **Session manager + holat mashinasi (§7.1):** `pending → publishing → playing → stopping → ended/failed`; taymautlar 10 s (`media_ready`), 10 s (WHEP), 15 s (transcode publish); `session_state` WS push; `failed` limit hisoblagichidan qaytariladi
- [x] Token mint: 256-bit (`base64url`), sha256 hash DB, TTL: read 30 s / publish `ttl_s` (60 s); **uzatish: RTSP'da `?token=`, WHIP/WHEP'da `Authorization: Bearer`** (token bir martalik emas — bitta session/path/action doirasida qayta ishlatiladi, protocol.md §7)
- [x] `media_start` payload'ini protocol.md §5.2 bo'yicha qurish: `publish:{token, urls:{whip, rtsp, rtsp_tc}, header_name, query_name}`, `ttl_s`, `transcode:{mode, by, backend, params}`; **`media_refresh`** (30–60 s) va **`media_stats`** qabuli; `media_ready/media_stopped` da `publish_id` + `reason` kodlari (§13)
- [x] **`/internal/auth` hook:** MediaMTX JSON (`action: publish|read|playback|api|metrics|pprof`, `path, protocol, user, password, token, ip, id, query, userAgent`) → token→sessiya→path→user tekshiruvi → `{allow:true}` / 403; javobda **`expireAt`** qaytarish (MediaMTX sessiyani o'zi yopsin); `tc/...` ichki tokeni
- [x] `POST /api/cameras/{id}/play` (profile default `sub`, `browser.hevc`), `/preview`, `/sessions/{id}/stop`, `GET /sessions`
- [x] *(qisman)* limitlar: user 16 / global 48 / preview 8 menejerda bajarildi (`LimitError` turlari bilan); **per-client `max_uploads` va navbat holati S2.5 qoldig'i**
- [x] Idle stop: readers=0 → 10 s (preview ~1 s) → `media_stop` agent'ga + path o'chirish
- [x] **Transcode manager (server):** FFmpeg subprocess (**backend `internal/gpu` natijasidan: `hevc_cuvid → h264_nvenc` / `vaapi` / `libx264` software**, parametrlar root §16.3); **kamera bo'yicha dedup** (bitta jarayon, refcount); `tc/...` path; navbat; jarayon o'ldirish (idle, fail); GPU kuzatuvi (NVIDIA bo'lsa `nvidia-smi`, bo'lmasa o'z hisoblagich — admin panel uchun)
- [x] `make probe` / `vmsd --probe`: **mashinaga mos** limitni o'lchash — NVENC **va NVDEC** parallel sessiyalari (NVIDIA bo'lsa) yoki software `libx264` sig'imi (CPU yadro hisobi) → `settings.server_max_transcodes` + `server_transcode_backend`; natijada GPU modeli faqat audit yozuvida (kod uni o'qimaydi)
- [x] *(qisman)* Kodek siyosati: `play_profile`, `transcode_mode auto|server|client|off`, server ichida zaxira (GPU → software, runtime probe orqali); **`browser_caps.hevc` siyosati S3 (UI brauzer imkoniyatini xabar qiladi) qoldig'i** (H.264 eng past profil), `browser_caps.hevc` → transcodesiz `cam/...` yo'li, `transcode_mode auto|server|client|off` (zaxira → agent'ga `transcode.by=client`; server ichida zaxira: GPU → software `libx264`)
- [ ] LL-HLS fallback: MediaMTX config + UI'ga signal (`hls.js`)
- [x] `testagent`ni to'liq publish'ga kengaytirish (WHIP H.264 + RTSP/TCP H.265, FFmpeg test pattern)
- [x] `internal/wsui`: `client_status`, `cam_status`, `session_state`, `session_count` (`session_revoked` — S4), `cam_status`, `session_count`, `session_revoked`, `session_state`

**Qabul:** testagent orqali — H.264 kamera WHEP'da ≤1.5 s ochiladi; H.265 → server transcode (dev mashinada NVENC; **GPU bo'lmagan mashinada `libx264` software bilan ham ishlaydi** — `ffmpeg -v codec` orqali tekshiriladi), 2 user = **1 FFmpeg**; `media_ready` 10 s taymauti `failed` + qayta urinish; yopilsa upload to'xtaydi; token qayta ishlatilsa 403.

---

### S2 natijasi (2026-11-20) — **bajarildi (qoldiqlar bilan)**
Qabul oqimi `make e2e-s2` (real MediaMTX v1.11.3 + real FFmpeg) — **18/18 PASS**:
agent publish → API `play` → token bilan RTSP o'qish (h264) → token'siz va
noto'g'ri token rad → MediaMTX readers hisobi → sessiya yopilgach idle stop
(2 s) va agent FFmpeg'ini o'ldiradi → `transcode_mode=server` bilan `tc/...`
o'qiladi (kamera boshida bitta FFmpeg).

**Amalda topilgan va hujjatlashtirilgan og'ishlar (muhim):**
1. MediaMTX v1.11.3 `/v3/paths/get/{name}` — YAKKA obyekt va `readers` —
   **obyektlar ro'yxati** (son emas). Klient ikkala ko'rinishni qabul qiladi;
   404 (path yo'q) readers=0 deb olinadi (idle stop shunda ishlaydi).
2. Auth-hook'da RTSP tokeni `token` maydonida EMAS — `query` satrida
   (`token=...`) keladi; `ExtractToken` uchala manbani ham qaytaradi.
3. **`ffmpeg -encoders` ro'yxatida bo'lishi ishlashini anglatmaydi:** bu
   mashinada nvenc ro'yxatda bor, lekin drayver eski ("Required: 13.1,
   Found: 13.0") va encode darhol yiqiladi. Shuning uchun **runtime probe**
   (`gpu.Probe`, 1 kadr encode) va **zanjir zaxirasi** (nvenc → vaapi → amf →
   libx264) qo'shildi; transcode darhol yiqilsa Transcoder backendni
   "yomon" deb belgilaydi va menejer keyingi backend bilan qayta urinishadi.
   Bu REJA §2.1/5 ("GPU o'zgaruvchan") talabining amaliy javobi.
4. `media_stop` **har sessiya ID'si uchun alohida** yuboriladi (agent
   publish'ni session_id bo'yicha kuzatadi); tugagan sessiyalar ro'yxatdan
   o'chirilgani uchun publish ID'sini yuborish xato edi (test agent FFmpeg'i
   tirik qolgani shu orqali topildi).
5. `play` javobi api.md bo'yicha: `{session_id, whep_url, token, ttl, codec,
   transcoded, backend, rtsp_url, expires_at}`. Sessiya yopish:
   `DELETE /api/sessions/{id}` (`/stop` o'rniga), uzatish:
   `POST /api/sessions/refresh`. Xato kodlari: `429 user_limit|global_limit|
   preview_limit`, `403 disabled`, `409 client_offline`, `503 media_offline`.
6. `media_ready` taymauti: §7.1'da 10 s deyilgan edi, amalda **5 s**
   (`media.ready_timeout_s`) — agent publish o'rnatguncha kechikish kichik.
7. `make probe` endi nafaqat imkoniyatni, balki **parallel sig'imni**
   o'lchaydi (real-time factor ≥ 0.95 bo'lgan maksimal parallel son) va
   `settings.server_transcode_backend` / `server_max_transcodes` ga yozadi
   (bu mashinada: nvenc/vaapi/amf ishlamadi → `cpu`, o'lchangan 8).

**S2 qoldiqlari (S3/S4'ga ko'chirildi):** `internal/limits` alohida paketi
(per-client `max_uploads` hisobi va navbat holati), `internal/wsui`
(`session_state`/`client_status` push), LL-HLS fallback signali,
`browser_caps.hevc` siyosati, WHEP orqali haqiqiy o'qish testi (S3 UI bilan).

---

## S3 — Web UI (≈6–8 kun) — *Dependence: S2 API*

- [x] React + TS + Vite skelet, router, o'zbek til (bitta `i18n` fayl, so'zlar bir joyda)
- [x] Login + refresh cookie bilan avtomatik kirish; xato sanab o'tish
- [x] `lib/api.ts`, `lib/ws.ts`, `lib/whep.ts` (native RTCPeerConnection ~120 qator), `lib/limits.ts`
- [x] Chap panel: joy sarlavhasi + client bloklari (status nuqta, online/total); blok bosilganda kamera ro'yxati (nom, status, kodek badge)
- [x] *(qisman)* **Preview:** debounce 600 ms dwell / min 2 s / close 1 s (`lib/preview.ts` + vitest); WHEP mini player ulangan; `preview_max` toasti bor; **grid katakida hover-preview UI jilosi (katak ichida) S3.6** / min 2 s / close 1 s; WHEP mini player; dedup — kamera grid'da ochiq bo'lsa qo'shimcha sessiya ochilmaydi; `preview_max` toast
- [x] Grid 1x1–4x4 (default 3x3); joy tanlanganda **avtomatik to'ldirish** (`position` bo'yicha); pagination (sahifa o'zgarganda eski oqimlar darhol stop)
- [ ] Drag&drop: ro'yxatdan katakka, ikki katakni almashtirish; double-click → fullscreen + `main`
- [x] Klaviatura: `1/2/3/4`, `←/→`, `Esc` (testda tasdiqlangan)
- [x] *(qisman)* Favorites (⭐); Layouts: saqlash/yuklash/default (brauzer testida tiklandi); **favorites reorder — S3.7 qoldig'i**, aralash joy; o'chirilgan kamera sloti → bo'sh katak
- [x] **IntersectionObserver + Page Visibility** — ko'rinmayotgan katak / yashirin tab → oqim stop
- [x] Holatlar: spinner, "transcode boshlandi…", "navbatda", limit toastlari, offline belgilar, xato + "qayta urinish"
- [x] `browser_caps` (HEVC aniqlash) har play/preview'da yuboriladi va transcode qaroriga ta'sir qiladi (commit 23bc7c4)
- [ ] `getStats()` monitoring: yomon inbound bitrate → avtomatik past profil haqidagi taklif (keyin to'liq avtomatik)

**Qabul:** 2 user parallel 15 tadan kamera; layout saqlanadi/tiklanadi; preview og'irlik yaratmaydi; tab yashirinsa barcha oqim to'xtaydi; UI to'liq o'zbekcha.

---

### S3 natijasi (2026-11-20) — qisman (S3.0–S3.5 bajarildi)
**Qabul oqimi `make e2e-s3`** — haqiqiy Chrome (headless, CDP; qo'shimcha
bog'liqlik yo'q) + real MediaMTX + testagent: **10/10 PASS** —
login → o'zbekcha interfeys → kamera ro'yxati → katakda **WHEP oqimi
haqiqiy kadrlar bilan (videoWidth=1280)** → sevimlilar → klaviatura
(1→1×1, 4→4×4) → tab yashirinsa oqim to'xtaydi → sessiyalar tozalanadi.
UI unit testlari: `make ui-test` (vitest, 15 ta: grid/klaviatura/preview
debounce sun'iy soat bilan).

**Muhim topilgan sabab (o'qish kechikishi ≤1.5 s maqsadiga to'siq):**
yangi o'quvchi ~15-17 s kutsedi — sabab FFmpeg yoki MediaMTX emas,
**publish GOP'i**: x264 default `gop=250` kadr (15 fps'da ~17 s) va
MediaMTX o'quvchiga birinchi keyframe'dan boshlab beradi. Test agent'ga
`-g 30 -keyint_min 30 -sc_threshold 0` qo'yilgach o'qish ochilishi
**14.4 s → 2.4 s** ga tushdi (`scripts/measure_ffmpeg.sh`).
**Talab: agent (haqiqiy client) H.264/H.265 publish'da GOP ≤ 2 s**
(qoida: `-g = 2×fps`, `keyint_min` bir xil, `sc_threshold 0`).
Bu client tomoniga uzatiladigan kontrakt talabi (protocol.md §5.2 izohi).

**Boshqa tuzatilganlar:**
- MediaMTX WHEP'da `Authorization: Bearer` hook'ga yetmaydi (MediaMTX o'z
  `jwt`ini uzatadi) — token **URL query**'da (`?token=`) bo'lishi shart;
  `whep.ts` ikkalasini ham yuboradi.
- UI WHEP'ni 6 marta (1 s pauza bilan) qayta uradi — transcode/publish
  tayyor bo'lmagan holatda "transcode boshlandi…" holati ko'rinadi.
- Bo'sh ro'yxatlar JSON'da `null` emas `[]` bo'lib chiqadi (UI `.map()`
  qiladi) — `writeJSON`'da nazorat qilinadi + test.
- vmsd port tekshiruvi faqat o'zi bog'laydigan portlarni (API, auth-hook)
  talab qiladi; MediaMTX portlarini bizning MediaMTX band qiladi —
  tashqi konflikt `make port-check` mas'uliyati (S2 e2e tartibi shu
  xatoni ochib berdi).
- JWT base64url (avvalgi commit) — UI WS query'da token uzatishi uchun.

**Dizayn parity (2026-11-20):** server webui dizayni client lokal konsoli
(`client/internal/localui/web/app.css`) bilan **bir xil** qilib qayta yozildi:
grafit fon + amber aksent (`#ffb000`), chap panel (brend belgisi, guruhlangan
nav, hotkey `kbd`, foydalanuvchi/rol qatori), yuqori statusline (`ulangan`,
`SESSIYALAR`, `KAMERALAR on/total`), `.cards` ko'rsatkichlari, mono raqamlar,
badge'lar (`H264/H265/TC`), katak ustidagi chiziqli overlay, tashqi shrift/CDN
yo'q (`@fontsource` ham olib tashlandi — offline). UI unit testlari 25 ta.

**Shu kun topilgan va tuzatilgan (dizayn + dev stack ishlashi):**
- Yuqoridagi `KAMERALAR` hisoblagichi faqat WS hodisalaridan olardi → REST
  ro'yxati + hodisalardan keladigan `lib/metrics.ts` qo'shildi.
- Kamera ro'yxati va grid avto-to'ldirishi **onlayn birinchi** saralanadi;
  oflayn kamerada katak o'qishga urinmaydi ("kamera oflayn" holati).
- Agent WS'da 90 sda jim uzilardi: gorilla'da read deadline kontrol frame
  bilan uzarmaydi → `SetPingHandler`'da uzatiladi (testagent; protocol.md
  1.0-draft.6 izohi).
- `SessionRow` json tag'siz edi → API `ID/CameraID` chiqarardi, Admin jadvali
  bo'sh. Endi snake_case (api.md hujjatlashtirildi).
- `media_ready` default taymauti 5 s edi → **10 s** (REJA §7.1); 4 parallel
  publish/transcode'da 5 s yetmadi (o'lchandi).
- "Arvoh" sessiyalar: brauzer yopilsa sessiya TTL'gacha (5 daq) limitni band
  qilardi. UI `pagehide`'da keepalive DELETE yuboradi + server readers
  watchdog'i **3×IdleStop** deraza bilan sessiyani `idle_no_readers` bilan
  yopadi (1× deraza juda agressiv chiqdi — jonli o'quvchini ham yopdi).
- `media.public_host` bo'sh bo'lsa mint'langan publish/WHEP manzili
  **127.0.0.1** bo'lardi — boshqa PC'dagi agent o'z localhost'iga
  publish qilmoqchi bo'lib hech narsa chiqarmasdi. Endi bo'sh qiymatda
  LAN interfeysining global IPv4 manzili avtomatik tanlanadi
  (docker0/link-local chetga), natija logda ko'rinadi va sinovdan
  o'tkazildi: agent `ws://192.168.0.27:8081` orqali ulanib, oqimni
  `rtsp://192.168.0.27:8554` ga yubordi. MediaMTX'da
  `webrtcAdditionalHosts` shu IP ga qo'yildi (boshqa PC brauzeri ICE
  nomzodi sifatida LAN manzilini ko'radi).
- WHEP urinish byudjeti kengaytirildi (direct 12, transcode 16; sovuq
  startda path 404 qaytaradi).
- Cheklangan narsa (S4 yuk testiga qoldi): 4 yadroli dev mashinada 2+ ta
  720p WHEP sessiyasi parallel ochilganda MediaMTX o'quvchini `terminated`
  deydi (CPU to'yinishi). Bir oqim barqaror; 2×2 skrinshot uchun testagent
  oqimi 640×360/10 fps'ga tushirildi.

**Ulanish kodi soddalashtirildi (2026-11-20, operator talabi):** kod endi
**8 xonali raqamli son** (masalan `41739265`) — avvalgi 19 belgili
harfli-raqamli format (`J7LJ-ANDR-…`) telefon orqali aytish/qayta terish uchun
qiyin edi. Server qabul qilishda bo'sh joy/tire yuboradi (`4173 9265` ham
ishlaydi). Entropiya 10^8, urinishlar IP bo'yicha 5/5 daqiqa — to'liq qidiruv
yillar talab qiladi, kod bir marta va 24 soatlik. Kontrakt: 1.0-draft.7.

**Mijoz nomi (2026-11-20, operator talabi):** clientda kiritilgan „connection
name‘ (masalan **Oxangaron**) serverda shu nom bilan ko‘rinadi. Nom ikki
yo‘l bilan keladi: §3.1 enroll jismidagi `name` va har §4.1 `hello`dagi `name`
(bo‘sh → server nomni o‘zgartirmaydi). Server `hello.name` o‘zgarsa
`clients.name`ni yangilaydi va UI’ga `client_status{name}` yuboradi — sahifani
yangilamasdan ko‘rinadi. Admin `PATCH /api/admin/clients/{id}` bilan baribir
o‘zi qayta nomlay oladi.
**Topilgan xato:** `clients.name` UNIQUE ekan; ikkinchi mijoz bir xil nom bilan
ro‘yxatdan o‘tganida CreateClient `ErrExists` qaytargan va enroll chalkash
`409 already_enrolled` degan (operator kodni ayblagan). Endi `ErrNameTaken` →
`409 name_taken`, kod isrof bo‘lmaydi. Kontrakt: 1.0-draft.9.

**Mijoz o‘chirilgandan keyingi holatlar (2026-11-20, 1.0-draft.11):**
- `DELETE /api/admin/clients/{id}` endi agent WS sessiyasini ham yopadi
  (`wsagent.Hub.Kick`, close 4004) — avval o‘chirilgan mijoz UI'da
  "online" bo'lib turardi. Test: `TestKickClosesSession`.
- Tasdiqlangan so‘rovning mijoz qatori yo‘q bo‘lib qolsa (FK `SET NULL`),
  keyingi polling `rejected` qaytaradi va qator shu holatga o‘tadi
  (`registry.InvalidateOrphanJoin`) — avval abadiy "pending" kutilardi.
  Test: `TestJoinApprovedClientDeleted`.

**Ulanish tartibi: kod → so‘rov + admin tasdig‘i (2026-11-20, operator
talabi):** „ulanish kodini olib tashla. ulanish ruhsati faqat server UI dan
tasdiqlanadi“. Endi agent `POST /api/agent/join` bilan so‘rov yuboradi
(`join_key` doimiy, `agent_secret`ni agent o'zi yaratadi — serverda faqat
sha256 hash, sir hech qachon qaytarmaydi), mijoz esa Boshqaruv sahifasidagi
**Ulanish so‘rovlari** panelida tasdiqlangandan keyin yaratiladi.
**IP bo‘yicha oldindan ruxsat** qo'shildi: operator server UI'dan IP/CIDR
yozadi — shu manzildan kelgan so'rov avtomatik tasdiqlanadi (migratsiya
0002: `join_requests`, `preauth_ips`; `enrollment_codes` tushirildi).
Testagent `-join -name` bilan ishlaydi; e2e skriptlari so'rovni admin API
orqali tasdiqlaydi. Kontrakt: 1.0-draft.10.

**S3 qoldiqlari:** drag&drop (ro'yxat→katak, katak↔katak) va double-click
fullscreen brauzer testi, layout saqlash/yuklash/default UI, favorites
reorder, pagination'da eski oqimlarni darhol stop (kodda bor, brauzer
testi yo'q), `browser_caps` yuborish, `getStats()` monitoring taklifi,
`session_revoked` push (S4).

---

## S4 — Qotirish, kuzatish, deploy (≈3–4 kun) — *Dependence: S2, S3*

- [ ] Admin panel: `settings` limitlari, `/api/admin/media/state` (faol sessiyalar, FFmpeg jarayonlari, **tanlangan transcode backend + GPU sessiyalari (aniqlangan usulda)**, navbat), audit ko'rinishi
- [x] **Eski holatni tiklash (2026-11-20 kuzatildi; 2026-11-21 yakunlandi):** server qayta boshlansa, DB'da
  `status='online'` deb qolgan mijozlar "jonli" bo'lib ko'rinadi — agent o'ldirilgan
  bo'lsa ham. Startda barcha mijozlar offline qilinadi (keyin `hello` online qiladi)
  yoki muddatidan ko'ra eski `last_seen` offline hisoblanadi.
- [x] **Restartdan keyin holatlar (2026-11-20):** `registry.ResetOnlineStates()`
  ishga tushishda eski `online` belgilarini tozalaydi (mijoz + kameralar);
  aks holda Boshqaruv/Kuzatuv ulanmagan mijozni "online" deb ko'rsatardi.
  Test: `TestResetOnlineStates`.
- [x] **Server restartdan keyin kamera holati (2026-11-21):** ikki sabab tuzatildi —
      (1) to'liq `cams_sync` `status`ni bo'sh yuborib, `cam_status`dagi `online`ni
      `offline` bilan bosardi (`UpsertCamera` endi bo'sh holatda mavjud qiymatni
      saqlaydi; sync'dagi aniq holat qo'llaniladi); (2) agent dedup xotirasi
      qayta qo'l berishdan keyin ham "o'zgarmadi" deb yubormas edi (welcome'da
      `StatusReporter.Reset()`). Testlar: `TestUpsertCameraKeepsStatusWhenEmpty`,
      `TestCamsSyncCarriesStatus`, `TestStatusReporterResetResends`.
      **Jonli tasdiq:** server qayta boshlandi, agent tegilmadi — kamera ~15 s'da
      `online`, `online_count 1/1`.
- [x] **Mijozlarni tozalash:** `DELETE /api/admin/clients/{id}` mijozni o'chiradi
  va agentni `Hub.Kick` bilan uzadi (1.0-draft.11); tasdiq so'rovida nom band
  bo'lsa preauth avtomatik tasdiqi „ (2)“ qo'shadi. Qoldiqlar UI orqali
  o'chiriladi (2026-11-21da dev DB tozalandi).

- [x] Retention: `sessions`/`audit_log` 90 kundan eski o'chiriladi — `internal/janitor` (soatlik ticker: token sweep + `registry.PurgeOlderThan`), `retention_days` config'dan o'tadi; testlar: `TestPurgeOlderThan`, `TestRunTickAndStop` (2026-11-21)
- [x] `deploy/backup.sh`: SQLite `VACUUM INTO` kundalik — `vmsd -backup <fayl>` rejimi orqali (sqlite3 CLI shart emas; butunlik tekshiruvi ichida), test: `TestRunBackup`; jonli sinov: `/tmp/vms_backup/vmsd-*.db` olindi. journald chegarasi — `install-notes.md` §7 (prod mashinada qo'yiladi)
- [~] MediaMTX tuning: `install-notes.md` §5 yozildi (v1.11.3'da `udpReadBufferSize` yo'q — `sysctl net.core.rmem_max=100000000`), dev config'da protokollar cheklangan. **`pprof`/`/metrics` hali yo'q** (keyingi band)
- [ ] `vmsd` `/metrics` (Prometheus): sessiyalar, limitlar, transcode navbati, hook natijalari
- [x] systemd unitlar: `systemd/vmsd.service` + `systemd/vms-media.service` (MediaMTX alohida unit, vmsd uni boshqarmaydi), `make install` `/opt/vmsd` ga soladi; qadamlar `deploy/install-notes.md` §2. **Yangi Ubuntu'da jonli sinov operator mashinasida qilinadi**
- [x] `deploy/firewall.md` (portlar jadvali + ufw namunasi: 9997/8189 faqat localhost) va `deploy/install-notes.md` (10 bandlik tekshir ro'yxati, `make probe` majburiy qadam) — 2026-11-21
- [x] **MediaMTX `ensure path` 404 shovqini** (2026-11-21): v1.11.3'da
      `PUT /v3/config/paths/put/{name}` `/` li nomlar bilan route'ga mos
      kelmaydi ("404 page not found") — har preview'da log shu bilan
      to'lar edi. Server endi `paths/list`dagi `~^cam/.+` / `~^tc/.+`
      patternlarini tekshiradi (`Client.PathConfigured`, 30 s kesh) va
      EnsurePath'ni faqat qoplanmagan yo'llar uchun chaqiradi.
      Test: `TestEnsurePathSkippedForPatternedPaths`.
- [ ] **Transcode `tc/...` yo'li kech ochiladi (2026-11-21 kuzatuvi):**
      H.265 kamerada brauzer `hevc:false` so'roviga server `tc/...` URL'ini
      qaytaradi, lekin MediaMTX logida ANNOUNCE faqat FFmpeg jarayoni
      tugashida (`transcode tugadi exit=0`) ko'rinadi — o'quvchilar orada
      `404 / "no one is publishing to path 'tc/...'"` oladi. Keyingi qadam:
      FFmpeg'ni qo'lda shu arg'umentlar bilan ishga tushirib (tokenlar DB'dan),
      input dekodlash kechikishimi yoki rtsp muxer lazy ANNOUNCE'mi aniqlash;
      kerak bo'lsa `-fflags +igndts` o'rniga `-rtsp_transport tcp` bilan
      oldindan `+nocrop`/`-g` parametrlari yoki output'ni `sdp` bilan
      majburiy ochish.
- [ ] **Aralash rejimli publish (2026-11-21):** bir kamera/profile uchun
      avval `browser.hevc=true` (direct), keyin `browser.hevc=false`
      (server_tc) so'rovi kelganda agent ikkinchi publish'ni qabul
      qilmaydi (`media_stopped reason=dropped`) va transcode umuman
      boshlanmaydi. Yechim: publish'ni mode bo'yicha qayta ko'tarish
      yoki agentda camera+profile bo'yicha bitta pull, rejimlarni
      serverda alohida hisoblash.
- [ ] Load test: 48 copy sessiya 30 daq uzluksiz, memory o'smaydi; transcode chegarasi + zaxira zanjir (`server GPU → server CPU → transcode.by=client`) o'lchangan. **Eslatma (2026-11-20):** prod GPU'si dev'dan farq qilishi mumkin — `make install` dan keyin `make probe` majburiy qadam (`install-notes.md` ga yoziladi), natijalar prod'da qayta tekshiriladi

### 2026-11-20 — kamera holati va oqim zanjiri (1.0-draft.12)

Operator xabari: "mijozga kamera qo'shdim — serverda ko'rinmadi, preview chiqmadi".
Tekshiruvda topilganlar va tuzatilganlar:

- **`media_start.ingest` noto'g'ri qiymat** (`"rtsp"`): kontrakt `whip|rtsp_tcp`
  deydi, haqiqiy `vmsagent` xabarni `publish: noma'lum ingest` deb rad etardi —
  barcha preview/play `publish_failed` bilan tugardi. `rtsp_tcp` qilib tuzatildi
  (H.264/H.265 birdek o'tadi). `e2e` testlarida testagent bu maydonni o'qimagani
  uchun xato sezilmagan — **testagent kontrakt maydonlarini tekshirmaydi** (keyingi
  bosqichda qo'shiladi).
- **`media_ready.path`** — agent profil (`main`) yuborardi, server uni so'zsiz
  qabul qilib o'quvchi URL'larini `rtsp://host/main` deb buzardi. Ikkala tomon
  tuzatildi: agent MediaMTX yo'lini yuboradi, server mos kelmasa o'z path'ida
  qoladi + ogohlantiradi.
- **Kamera sababi ko'rsatilmaydi** edi: `cameras.last_error`/`last_error_at`
  qo'shildi (migratsiya `0003_camera_error`), `cam_status.error` va
  `media_stopped.reason` (normal: `no_viewers|ttl_expired|user`dan tashqari)
  shu yerga yoziladi; online holatda tozalanadi. UI kamera kartasida ko'rinadi.
- **Kamera ro'yxati faqat qayta ulanganda kelardi**: endi agent darhol
  `cams_sync` yuboradi, server UI'ga `cams_changed` uzatadi (`api.md` §Realtime).

2026-11-20 davomi: **qayta ulanish poygasi** tuzatildi — agent tez
qayta ulangda eski WS sessiyasining yopilishi mijozni va kameralarini
`offline` qilib qo'yardi (operator ko'rgan "doim offline"). Endi offline
belgilash faqat sessiya hali faol bo'lsa bajariladi (regressiya testi
bilan). Shuningdek `config/localhost.yaml` va ildovdagi `Makefile`
qo'shildi: server + agent BITTA mashinada (`make run`, `make addcam`,
`make check`).

Qolgan bo'sh joylar (hujjatlashtirilgan, keyingi yurish):
- [x] `codec_probe` (§4.5) yuboriladi: probe SDP'dan kodekni o'rganadi va
      `codec_probe` yuboradi; serverda `codec: h265` ko'rindi (jonli
      Hikvision kamerasida tasdiqlangan, 2026-11-20).
- [x] `position` sinclanadi: server `cams_sync`da `Position`ni bazaga yozadi (test: `TestCamsSyncStoresPosition`, 2026-11-21).
- [ ] Server H.264 uchun ham `rtsp_tcp` yuboradi — WHIP tanlashi agent
      imkoniyati `caps`da e'lon qilingandan keyin qaytiladi.
- [x] Testagent `media_start`ni kontrakt bo'yicha tekshiradi (`validateMediaStart`: ingest `whip|rtsp_tcp`, token, ingest'ga mos URL, profil) — buzilsa „KONTRAKT XATOSI“ + `media_stopped{publish_failed}` (2026-11-21).

**Qabul:** root §15 "MVP bajarildi" ro'yxatidagi server bandlari yashil; load test natijasi `docs/` da.

---

## Keyingi bosqich (P6, rejalashtirilgan, hozir ISHLAMAYMIZ)

TLS (Caddy), `user_site_grants`, Telegram alert, snapshot preview (`PreviewProvider` abstraksiyasi S3'da tayyor bo'ladi), arxiv playback.
