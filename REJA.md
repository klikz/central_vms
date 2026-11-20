# Central VMS — texnik reja (kod yo'q, faqat loyiha)

Holat: tasdiqlangan talablar asosida yakuniy arxitektura + bosqichlar.
Sana: 2026-11-17, yangilandi: 2026-11-19 (transcode dedup, sessiya holat mashinasi + auth-hook, H.265 ingest = RTSP/TCP, MediaMTX versiya pin, preview debounce, kechikish mezoni, operator PC minimal, Dahua/RTSP aniqligi)
Yangilandi (2026-11-19, 2): **agent↔server kontrakti yozildi va muzlatildi — `client/docs/protocol.md` (1.0-draft.4)**; enrollment endpoint, `media_start` maydonlari, token uzatish (RTSP query / WHIP Bearer), kamera `id` ni agent yaratadi, publish refcount + TTL watchdog, ingest kutubxonalari (gortsplib + pion, FFmpeg zaxira), kamera kashf etish + ommaviy qo'shish, lokal UI dizayn talabi.
Loyiha **2 qismga bo'lib bajariladi**: `server/` va `client/` — har birida o'z `REJA.md` + `TASKS.md`, implementatsiya alohida. Umumiy kontrakt: `docs/protocol.md` (**client tomonida tuzildi**, server S0'da ko'chiradi va muzlaydi).

---

## 1. Talablar (tasdiqlangan)

| # | Talab |
|---|-------|
| 1 | To'g'ridan-to'g'ri IP kameralar, RTSP / ONVIF orqali |
| 2 | Video **server orqali** o'tadi, faqat **ochilgan** kameralar oqimi (on-demand) |
| 3 | Client: **Ubuntu**, Docker yo'q, **lokal brauzer UI**, **systemd autostart** |
| 4 | Server bizniki, static IP. Real holat: 2 user × 15 kamera = ~30 concurrent, maksimal aniq emas |
| 5 | Auth: faqat o'z bazasi (username/password). Rollar: **admin**, **viewer** |
| 6 | Faqat **live**. Arxiv playback, audio, PTZ **yo'q**. Snapshot saqlash hozircha **yo'q** (preview live qilinadi; og'ir bo'lsa keyin snapshot rejimiga o'tkazish mumkin) |
| 7 | Grid: 1x1 / 2x2 / 3x3 / 4x4. Location tanlanganda faqat o'sha joy kameralari |
| 8 | Har bir user **o'z layoutini saqlaydi** (moslash), kataklar orasida drag&drop almashtirish, aralash location layout mumkin |
| 9 | UI: ulangan **clientlar bitta blok** ko'rinadi, bosilganda ichidagi kameralar ro'yxati + preview varianti **(a)**: ro'yxatda hover/qisqa bosishda **vaqtinchalik past sifatli live oqim** (og'irlik qilsa keyin snapshot rejimiga almashtiriladi) |
| 10 | Qidiruv/filter yo'q. Offline kamera — UI'da belgi |
| 11 | UDP ruxsat etilgan (WebRTC mumkin). TLS hozircha yo'q (tashqi himoya boshqacha) |
| 12 | UI tili: **o'zbek**. Kelingida ~1500–2000 kamera |
| 13 | `server/` va `client/` papkalari **alohida**; har birida `make start` (ishga tushirish) va `make build`; `make build` backend + frontend + migrationsni `build/` papkasiga tushiradi. Root Makefile **kerak emas** |
| 14 | **Kodek aniq emas** va kelajakda har xil kamera ulanadi → **H.264 va H.265 ikkalasi ham hisobga olinishi kerak**. H.265 bo'lsa transcode: **CPU yoki GPU** orqali. Kamera ulanganda kodek avtomatik aniqlanadi va kerakli amal bajariladi. **Serverda GPU bor: RTX 3070 8 GB (NVENC)** → transcode default **server** tomonda |
| 15 | Location tanlanganda grid **avtomatik** o'sha joy kameralari bilan to'ldiriladi, 16 tadan ko'p bo'lsa **pagination** |
| 16 | MediaMTX `make start` ichida **avtomatik** ishga tushadi (prod'da alohida systemd unit) |
| 17 | Bitta shaharda **bir nechta client** (agent) bo'lishi mumkin, lekin **joy/client nomlari har xil — bir xil nomli client yo'q** (nom unikal) |
| 18 | Kameralarda **uchinchi (preview) profil yo'q** → preview `sub` profilidan olinadi (qo'shimcha siqish yo'q) |
| 19 | Kameralar asosan **Dahua**, **RTSP** orqali ulanadi (ONVIF ixtiyoriy: discovery/switching bo'lmasa ham ishlaydi) |
| 20 | Server offline bo'lsa joyda **lokal ko'rish kerak emas** (tasdiqlangan) |

---

## 2. Texnologik tanlovlar

| Qism | Tanlov | Nega |
|------|--------|------|
| Media server | **MediaMTX** (bitta Go binary, versiya Makefile'da **qulflangan**: `MEDIAMTX_VERSION`) | Dinamik path, **WHIP** (H.264 publish), **RTSP/TCP publish** (H.265 ingest — barqarorroq, 8.1), **WHEP** (browser→play), LL-HLS fallback, reader count API, external auth hook (shartnoma: 7.1). H.264 holatida transcode yo'q (copy); H.265 bo'lsa FFmpeg (8.1) |
| Browser pleyer | **WebRTC / WHEP**, native `RTCPeerConnection` (~120 qator), fallback **LL-HLS + hls.js** | Eng yengil: GPU/native decoder, kechikish ~0.3–1.0s (eng yaxshi holat 0.3s; qabul mezoni **≤1.5s** — 14/15), 16 katakda ham CPU past. Kutubxona og'irligi yo'q |
| Server backend | **Go** (chi/Gin + gorilla/websocket) | Bitta binary, WS hub, MediaMTX control API bilan birga yengil |
| DB | **SQLite (WAL)** + migratsiyalar → kerak bo'lsa Postgres | 2000 kamera + 10 user uchun yetadi, deploy oddiy |
| Server UI | **React + TypeScript + Vite**, CSS (Tailwind yoki oddiy CSS) | Grid/player komponentlari oson, build statik — Go'ga embed |
| Client | **Go agent** + ichiga embed qilingan lokal web UI (`:8082`) + **systemd unit** | Autostart, headless, past xotira (~30 MB), update oson. Electron/Tauri kerak emas |
| Transcode (zarur bo'lsa) | **FFmpeg** subprocess. H.264 bo'lsa faqat remux (`-c copy`). H.265 bo'lsa H.264'ga siqish — **default: serverda, RTX 3070 NVENC**; zaxira: joyda GPU (`h264_nvenc/vaapi/amf`) yoki CPU (`libx264 -preset veryfast -tune zerolatency`) | H.265 kameralar brauzerda o'ynalmaydi — 8.1 siyosat. Faqat ochiq kameralar uchun, sessiya cheklovi bilan (NVENC: 8) |
| Kamera bilan ish | RTSP probe + publish: **`bluenviron/gortsplib` v4** (probe, RTSP pull, H.265 RTSP/TCP publish) va **`pion/webrtc` v4** (H.264 WHIP); FFmpeg faqat remux/re-encode/transcode/zaxira | Client o'zi tekshiradi, serverga faqat natija beradi. **pion'da H.265 packetizer yo'q** (faqat Opus/PCM/H264/VP8/VP9) — H.265 WHIP orqali yuborilmaydi |

**Muhim dizayn qarori:** kamera RTSP login/paroli **faqat client'da** saqlanadi (shifrlangan fayl). Serverga mask'langan URL va metadata uzatiladi. Server buzilsa ham kameralar paroli o'g'irlanmaydi.

---

## 3. Arxitektura

```
JOY (Oxangaron / Angren / Toshkent)              SERVER (static IP)
┌───────────────────────────────┐                ┌──────────────────────────────────┐
│ IP kamera ── RTSP/ONVIF ──┐   │                │ MediaMTX :8554/:8889/:8189       │
│                           ▼   │ WHIP/RTSP-TCP  │   path: cam/{client}/{camera}    │
│ vmsagent (Go) ────────────────┼───────────────►│        ▲                ▲       │
│  :8082 lokal UI               │                │ vmsd (Go) ── WS ─────── │        │
│  - kamera CRUD / ONVIF        │◄──────────────►│  - auth (admin/viewer)  │        │
│  - server adresi + kod        │  bitta chiqish │  - registry (joy/client │        │
│  - status heartbeat           │  WebSocket     │    /kamera/favorites)   │        │
│  systemd: vmsagent.service    │                │  - session manager      │        │
└───────────────────────────────┘                │  - limits, audit        │        │
                                                 └───────────▲─────────────┘        │
                                                     HTTPS/WS│  browser (React UI) ─┘
```

- Client **tashqariga** WS chiqaradi → joylarda port ochish / port-forward shart emas.
- Server **fan-out** qiladi: bitta kamera 5 user ko'rsa ham joy→server upload **bitta**.
- Oqim faqat tomoshobi bor payt yuboriladi; tomoshobi qolmasa 10 s ichida to'xtatiladi.
- Serverda H.265 kamera bo'lsa FFmpeg (RTX 3070 NVENC) transcode jarayoni qo'shiladi: `cam/...` → `tc/...` (8.1).
- **Ingest rejimi:** H.264 → WHIP (UDP); H.265 → **RTSP/TCP publish** (MediaMTX'da H.265 WHIP ingest'i nisbatan yangi — RTSP ancha barqaror, risk P3 dan olib tashlanadi). **Qo'shimcha sabab:** agent kutubxonasi `pion/webrtc` v4'da **H.265 packetizer yo'q** — texnik jihatdan ham H.265 WHIP bilan yuborib bo'lmaydi (protocol.md §8).
- **Transcode dedup:** har kamera uchun **bitta** FFmpeg jarayoni — barcha tomoshobilarga umumiy (8.1, 16.3).

---

## 4. Repo tuzilishi (server va client **alohida**)

Qoida: `server/` va `client/` — **bir-biridan mustaqil** loyihalar. Har birida o'z `go.mod`, o'z `Makefile`, o'z `build/` chiqishi bo'ladi. Bittasi qurilmaydi ikkinchisiga bog'liq bo'lib qolmaydi; alohida mashinada, alohida vaqtda build qilinadi. Umumiy kod kerak bo'lsa `shared/` (ayrim go module) keyin qo'shiladi — hozira talab qilinmaydi.

```
central_vms/
  REJA.md  README.md
  .gitignore                          # build/, node_modules/, dist/, *.local.yaml

  server/                             # SERVER LOYIHASI (mustaqil go module)
    Makefile
    go.mod
    cmd/vmsd/main.go                  # bitta binary: API + WS + statika serve
    internal/
      auth/        argon2id, JWT (access 15m + refresh), rol tekshiruvi
      registry/    sites, clients, cameras, favorites, layouts CRUD
      wsagent/     agent WebSocket hub (enroll, sync, media_start/stop)
      wsui/        UI WebSocket hub (status push)
      media/       MediaMTX control API klienti, session manager, token mint
      store/       SQLite (modernc.org/sqlite, CGO yo'q) + migratsiya runner
      limits/      concurrent limit hisoblagich (user/global/per-client)
      audit/       log
      config/      YAML konfiguratsiya
    migrations/                       # .sql fayllar (build'ga ko'chiriladi)
    webui/                            # SERVER UI (React) — server build'ining qismi
      package.json  vite.config.ts
      src/
        pages/       Login.tsx, Console.tsx
        components/  ClientBlock, CameraList, Grid, GridCell, Player,
                     GridLayoutBar, FavoritesBar, StatusDot, Toast
        lib/         api.ts, ws.ts, whep.ts (WHEP klient), limits.ts
    mediamtx/mediamtx.yml             # auth hook, path pattern, protokollar
    mediamtx/mediamtx                 # binary (git'da emas, `make media-get` yuklaydi)
    config/dev.yaml  config/prod.yaml.example
    build/                            # QURILISH CHIQISHI (git'ga kirmaydi)

  client/                             # CLIENT LOYIHASI (mustaqil go module)
    Makefile                        # + devserver, itest, stress, secretscan, ui-lint,
    #                               vet, race, package, remote-update, deps
    go.mod
    cmd/vmsagent/main.go
    cmd/devserver/                    # mock vmsd (`make devserver`) — server kutilmaydi
    cmd/repro/                        # minimal reproduksiya dasturlari (xato tuzatishda)
    testdata/                         # golden/fixture: protocol/*.json, sdp/*, rtp/*, wsdd/*.xml (gen.sh)
    internal/
      proto/       WS xabarlari Go tiplari (mantiq yo'q) + golden round-trip testlari
      config/      client.yaml load + default + validatsiya
      clock/       Now/After/NewTicker interfeysi + fake impl (taymaut testlari deterministik)
      agent/       WS ulanish (auth/welcome, envelope, epoch), backoff, cams sync, buyruq bajarish,
                   secret_rotate, config_push, hb
      keystore/    AES-256-GCM parol store (agent.key 0600, secrets.bin, secret_ref)
      statestore/  state.json atomik yozish + schema versiya + cams_epoch
      caps/        `ffmpeg -encoders` parse (nvenc/vaapi/amf/libx264/hevc decode) + CPU/RAM
      rtspclient/  gortsplib wrapper: Describe/Options/GetParameter, timeout, retry (yagona kirish nuqtasi)
      cams/        SDP tahlili (mediacommon — qo'lda regex taqiqlangan), status watcher (pacing 5),
                   kamera CRUD + CSV, ONVIF, B-frame bayrog'i
      discovery/   ONVIF WS-Discovery (239.255.255.250:3702) + RTSP port-scan (cidrs), bulk add (UUID)
      publish/     registry (**single-writer goroutine**, refcount), TTL watchdog, retry 1/2/4 s,
                   H.264 → WHIP (pion/webrtc v4) + H.265 → RTSP/TCP (gortsplib v4 ANNOUNCE/RECORD),
                   media_stats; zaxira: FFmpeg -f rtsp / -f whip
      ffmpeg/      subprocess boshqaruvi (CommandContext, process group kill, -progress parse)
      transcode/   H.265→H.264 (hevc_cuvid/vaapi → nvenc/vaapi/amf/libx264, max 4, navbat),
                   `-c copy` remux, B-frame re-encode (baseline, -bf 0)
      localui/     lokal UI (:8082, **go:embed**, dizayn talabi client REJA §8) — kamera CRUD + bulk,
                   kashf etish, self-test, jurnal, sozlamalar
      selftest/    server WS / WHIP UDP-TCP / RTSP publish / kamera connectivity o'lchovi
      redact/      parol/token/secret redaksiyasi (barcha log va UI xabarlari)
    docs/protocol.md                # AGENT↔SERVER KONTRAKTI (1.0-draft.4, yozilgan) → S0'da docs/ ga ko'chiriladi
    systemd/vmsagent.service          # unit fayl (build'ga ko'chiriladi)
    config/client.yaml.example        # to'liq sxema: protocol.md §11
    build/                            # QURILISH CHIQISHI (git'ga kirmaydi)

  docs/            protocol.md (client/docs/protocol.md dan ko'chiriladi), api.md, runbook.md
  deploy/          firewall.md, backup.sh, install-notes.md, client-firewall.md, agent-state-backup.md
```

### 4.1 `make build` chiqishi (majburiy tarkib)

```
server/build/
  vmsd                     # linux/amd64, CGO_ENABLED=0 (statik)
  web/                     # frontend: index.html + assets/  (diskdan serve qilinadi)
  migrations/              # barcha .sql fayllar
  mediamtx/mediamtx.yml    # media server konfiguratsiyasi
  config/config.yaml       # ishga tushirish konfiguratsiyasi
  VERSION                  # git tag + commit + build sanasi
  run.sh                   # build papkasidan ishga tushirish skripti

client/build/
  vmsagent                 # linux/amd64, CGO_ENABLED=0
  config/client.yaml
  systemd/vmsagent.service
  install.sh               # /opt/vmsagent + systemctl enable --now
  VERSION
```

Frontend binary'ga **embed qilinmaydi** — `build/web/` alohida papka bo'ladi, server uni diskdan serve qiladi (`-web ./web`). Sabab: UI yangilash uchun qayta kompilyatsiya shart emas, `rsync build/web/` bilan yetadi. Migrations ham shu sababli diskda.
**Istisno — client:** lokal UI (`:8082`) binary'ga **`go:embed` qilinadi** (`build/` ichida alohida `web/` papka yo'q). Sabab: client UI statik, o'zgarishi kam; deploy bitta fayl orqali bo'ladi va joyda rsync/papka boshqaruvi kerak bo'lmaydi.

### 4.2 Make targetlari

**`server/Makefile`**

| Target | Vazifa |
|---|---|
| `make start` | Hammasini quradi → migratsiyani ishga tushiradi → MediaMTX + `vmsd` ni **dev** rejimda ishga tushiradi (Ctrl+C bilan to'xtaydi) |
| `make dev` | Tez ish: backend `go run`, frontend `vite dev` (proxy), MediaMTX alohida — hot reload |
| `make build` | `build/` ichiga yuqoridagi tarkibni yig'adi (frontend build + backend binary + migrations + config) |
| `make run` | Faqat `build/vmsd` ni mavjud `build/config` bilan ishga tushiradi |
| `make media` | MediaMTX ni `mediamtx/mediamtx.yml` bilan ishga tushiradi (`make start` buni o'zi qiladi) |
| `make media-get` | MediaMTX binary'sini **qulflangan versiyadan** yuklab `mediamtx/mediamtx` ga qo'yadi (git'da saqlanmaydi). Makefile'da `MEDIAMTX_VERSION = <git tag>` — P0'da tanlanib qulflanadi; WHEP/H.265 xatti-harakati versiyalar orasida o'zgaradi, prod'da ham xuddi shu versiya ishlatiladi |
| `make probe` | Server quvvatini o'lchaydi: NVENC sessiya limiti, CPU transcode tezligi, kanal — natija `settings` ga yoziladi (16.3) |
| `make migrate-up` / `migrate-down` / `migrate-status` | SQLite migratsiyalari |
| `make ui-build` / `ui-dev` | Frontend ichki targetlari |
| `make test` / `lint` / `fmt` | `go test ./...`, `golangci-lint`, `gofmt` |
| `make package` | `build/` ni `vmsd-<version>-linux-amd64.tar.gz` qilib qadoqlaydi (serverga ko'chirish uchun) |
| `make install` | `/opt/vmsd` ga nusxa + systemd unit + `enable --now` (prod) |
| `make logs` | `journalctl -u vmsd -f` |
| `make clean` | `build/` ni tozalaydi |

**`client/Makefile`**

| Target | Vazifa |
|---|---|
| `make start` | `build/vmsagent` ni `config/client.yaml` bilan ishga tushiradi (lokal UI `http://localhost:8082`) |
| `make dev` | `go run` + lokal UI hot reload, sozlamalar `./config/client.local.yaml` |
| `make build` | `build/` ichiga binary + config + systemd unit + install.sh |
| `make install` | `/opt/vmsagent` + systemd unit + `systemctl daemon-reload && enable --now` |
| `make uninstall` | Unit o'chiriladi, fayllar `/opt/vmsagent` dan olib tashlanadi |
| `make run` | Build qilmaydi: `build/vmsagent`ni ishga tushiradi (`config/client.local.yaml` bo'lsa shu) |
| `make stop` | `make start / dev / devserver` ko'targan jarayonlarni mayin to'xtatadi (`build/run/*.pid` + aniq pattern; `go run` bolasi ham kiritilgan) |
| `make stop-service` | `sudo systemctl stop vmsagent` (prod o'rnatilgan bo'lsa) |
| `make logs` | `journalctl -u vmsagent -f` |
| `make status` | `systemctl status vmsagent` + agent holati |
| `make test` / `lint` / `fmt` / `clean` | Yuqoridagilar kabi |
| `make vet` / `race` | `go vet` + `go test -race -cover` (coverage bo'sagasi: `proto/cams/publish` ≥ 70 %, qolganlar ≥ 50 %) |
| `make itest SCENARIO=…` | Integratsiya matritsasi (H.264 WHIP, UDP bloklangan, H.265 RTSP/TCP, client transcode, B-frame, token expiry, refcount, watchdog, shutdown, restore) |
| `make stress CAMS=2000` | Xotira/probe sikli stressi (`probe_cycle_s` ≤ 4 soat) |
| `make secretscan` | Log/fayllarda parol/token/secret qidiruvi (DoD bandi) |
| `make ui-lint` | Lokal UI HTML/CSS tekshiruvi (node talab qilmaydi) |
| `make devserver` | **mock vmsd** (client repoda): enroll + WS + `media_start/stop/refresh` + `codec_fix` + `config_push` + `secret_rotate` — server tayyor bo'lmaganda ham C1–C3 ni tekshirish (protocol.md §18) |
| `make package` | `build/` ni `vmsagent-<version>-linux-amd64.tar.gz` qilib qadoqlaydi |
| `make remote-update` | `SSH_HOST=… make remote-update` — `rsync build/` + `systemctl restart vmsagent` (joylarda yangilash) |
| `make deps` | Tashqi bog'liqliklar va qulflangan kutubxona versiyalarini chiqaradi (`ffmpeg` ≥ 7.1, pion v4, gortsplib v4) |

**Root `Makefile` — yo'q** (tasdiqlangan): faqat `server/Makefile` va `client/Makefile` ishlatiladi. Kerak bo'lsa keyin qo'shiladi.

**MediaMTX boshqaruvi (tasdiqlangan):** `make start` MediaMTX'ni o'zi ishga tushiradi (dev rejimda alohida jarayon sifatida; Ctrl+C'da `vmsd` ham MediaMTX ham to'xtaydi). MediaMTX binary git'da saqlanmaydi — `make media-get` yuklab qo'yadi yoki config'da mavjud binary yo'li ko'rsatiladi. **Prod'da** MediaMTX alohida systemd unit (`vms-media.service`) bo'ladi, `vmsd` uni boshqarmaydi.

### 4.3 Build qoidalari

- Binary'lar **`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`** — Ubuntu server va Ubuntu client'larda bir xil fayl ishlaydi, SQLite uchun `modernc.org/sqlite` (sof Go) ishlatiladi.
- Versiya `ldflags` orqali: `-X main.version=$(git describe --tags) -X main.commit=$(git rev-parse --short HEAD)`.
- `make build` **idempotent**: `build/` tozalanadi, keyin qayta yig'iladi; frontend `npm ci` (lockfile bilan) ishlatiladi.
- `make start` oldin `make build` ni chaqiradi — "klon qildim → `cd server && make start`" bitta buyruq bilan ishlaydi.
- Maxfiy sozlamalar (`config/*.local.yaml`, agent parollari) git'da emas, `.gitignore`'da.
- **Client tashqi bog'liqligi:** `ffmpeg` (zaxira H.265→H.264 transcode va RTSP remux uchun). `make install` uni tekshiradi, bo'lmasa ogohlantiradi; `make build` ichida `deps` ro'yxati chiqaradi.
- **Server tashqi bog'liqligi:** `ffmpeg` (NVENC/CUVID bilan) + NVIDIA drayveri — H.265 transcode default shu yerda. `make start` va `make install` buni tekshiradi (`nvidia-smi`, `ffmpeg -hide_banner -encoders | grep nvenc`), topilmasa ogohlantiradi va transcode zaxira zanjiri (joy GPU → joy CPU) ishlatiladi.

---

## 5. Ma'lumot modeli

```
users        id, username, pass_hash(argon2id), role(admin|viewer),
             display_name, created_at, last_login_at, disabled
sites        id, name("Oxangaron") UNIQUE, code, position
clients      id, site_id, name UNIQUE, agent_secret_hash, enrolled_at,
             last_seen_at, status(online|offline), version, os, lan_ip,
             caps_json {gpu, cpu_cores, ram_mb, ffmpeg_version},
             max_uploads (default 8),
             max_transcodes (GPU: 8 default, CPU-only: 4 default),
             transcode_mode(auto|server|client|off)   # default auto
cameras      id (UUIDv4 — **agent yaratadi, o'zgarmas**), client_id, name, onvif_xaddr, rtsp_masked,
             profile_main, profile_sub,             # uchinchi (preview) profil YO'Q
             codec_main, codec_sub,                 # h264|h265|av1|unknown
             play_profile(main|sub),                # default: sub (preview ham sub)
             transcode_mode(auto|server|client|off),# default: auto → server GPU
             enabled, position,
             status(online|offline|error), last_check_at, codec_probe_at
favorites    user_id, camera_id, position
layouts      id, user_id, name, grid(1|2|3|4), slots_json, is_default,
             updated_at
sessions     id, camera_id, user_id, started_at, ended_at, profile,
             kind(play|preview), transcoded(bool),
             transcode_backend(gpu|cpu|none), transcode_by(server|client|none),
             state(pending|publishing|playing|stopping|ended|failed — 7.1)
audit_log    id, user_id, action, target, at
settings     key, value    # global limitlar: global_max_sessions=48,
                           # server_max_transcodes=5 (NVENC limiti o'lchanadi: 3–8),
                           # preview_max=8, idle_stop_s=10, user_max_sessions=16
```

Server quvvati (ma'lum): **GPU RTX 3070 8 GB** → NVENC encode + HEVC dekod. Talab: NVIDIA drayveri + `ffmpeg` nvenc/cuvid bilan (`ffmpeg -hide_banner -encoders | grep nvenc`). VRAM yetarli: 720p sessiya ≈ 200–400 MB, 8 sessiya ≈ 2–3 GB.

Izoh (tasdiqlangan):
- **Nomlar unikal** — `sites.name` va `clients.name` `UNIQUE`. Bir xil nomli client yo'q, joy nomlari har xil, shuning uchun UI'da chalkashlik bo'lmaydi va blok sarlavhasi to'g'ridan-to'g'ri joy nomi bo'ladi.
- Bitta shaharda **bir nechta client (agent)** bo'lishi mumkin → `sites` real guruh. Hozirgi holat (Oxangaron / Angren / Toshkent) — har joyda bittadan client, shuning uchun UI'da qo'shimcha guruh sarlavhasi kerak emas; bir nechta bo'lsa ostida qo'shimcha blok ko'rinadi (11-bo'lim).
- **Kamera `id` ni agent yaratadi** (UUIDv4, o'zgarmas; server PK shu) — kamera nomi/hosti o'zgartirilsa yoki qayta qo'shilsa ham `id` saqlanadi, shuning uchun `favorites` va `layouts.slots` buzilmaydi. Sync drift'i `cams_epoch` orqali tekshiriladi (protocol.md §6).
- **Kameralarni joyda topish:** agent lokal UI orqali tarmoqdagi kameralarni **kashf etadi** (ONVIF WS-Discovery + RTSP port-scan) va **ommaviy qo'shadi** (checkbox + naming pattern + umumiy parol) — server bu jarayonni bilmaydi, faqat qo'shilgan kameralarni oladi (client REJA §8, protocol.md §16).

---

## 6. HTTP API (server UI ↔ server)

```
POST   /api/auth/login            {username,password} → access + refresh cookie
POST   /api/auth/refresh          yangi access
POST   /api/auth/logout
GET    /api/me                    user, role, limitlar

GET    /api/sites                 joylar: name, clients_count, cameras online/total
GET    /api/clients               bloklar: name, site, status, cameras_count, online_count
GET    /api/clients/{id}/cameras  kamera ro'yxati + status + codec/preview imkoniyati
GET    /api/cameras/{id}          bitta kamera detali

POST   /api/cameras/{id}/play     {profile:sub|main, browser:{hevc:true|false}} → {session_id, whep_url, token, ttl, codec, transcoded}
POST   /api/cameras/{id}/preview  {on:true|false}    → preview session (kind=preview, profile=sub)
POST   /api/sessions/{id}/stop
GET    /api/sessions              faol oqimlar (admin: barchasi, viewer: o'ziniki)

GET    /api/favorites             user favorites
POST   /api/favorites/{cameraId}  qo'shish
DELETE /api/favorites/{cameraId}
PUT    /api/favorites/reorder     tartib

GET    /api/layouts               user layoutlari
POST   /api/layouts               yangi (grid + slots)
PUT    /api/layouts/{id}          saqlash / nom o'zgartirish
DELETE /api/layouts/{id}
PUT    /api/layouts/{id}/default  kirishda ochiladigan layout

# admin
GET/POST/PATCH/DELETE /api/admin/users
POST   /api/admin/enrollments     {site_name} → bir martalik kod (24 soat)
GET/POST/PATCH /api/admin/clients nom, site, max_uploads, max_transcodes, transcode_mode, o'chirish
PATCH  /api/admin/cameras/{id}    {transcode_mode, play_profile, enabled} (policy, kamera sozlamasi emas)
GET    /api/admin/media/state     faol sessionlar, yuklama, limit statistikasi
GET    /api/admin/audit

# agent (ochiq endpoint, auth yo'q — bir martalik kod + rate-limit 5/5 min)  [protocol.md §3]
POST   /api/agent/enroll          {code, hostname, os, lan_ip, agent_version, caps}
                                  → {agent_id, agent_secret, protocol_version}
                                  401 bad_code | 409 already_enrolled | 429 rate_limited
```

---

## 7. WebSocket xabarlari

**A) Agent kanali** `/api/agent/ws?agent_id=…` (auth: `agent_secret`, enrollment kodidan keyin beriladi;
ulanishdan keyin ≤5 s `auth {agent_id, secret}` → server `welcome`; envelope `{type, seq, ts, data}` —
to'liq sxemalar `client/docs/protocol.md` da)

```
agent → server :
  auth         {agent_id, secret}                    # handshake
  hello        {protocol_version, agent_version, os, hostname, lan_ip, boot_id,
                cams_epoch,
                caps:{gpu:nvenc|vaapi|amf|none, cpu_cores, ram_mb, ffmpeg_version, hevc_decode}}
  cams_sync    {epoch, full:true, seq_no, chunk_count, done,
                cameras:[{op:upsert|delete, id, name, rtsp_masked,
                profiles:{main,sub}, codecs:{main,sub}, bframe:{main,sub},
                onvif_ok, status, position, enabled}]}
               yoki chunk'lab (100 kamera = 2-3 xabar); o'chirish shu kanal orqali (op:delete)
  cam_status   {camera_id, status, error, checked_at, rtt_ms}
  codec_probe  {camera_id, codecs:{main,sub}, bframe:{main,sub}, profiles_sdp:{main,sub},
                onvif_switch_ok, reason}
  media_ready  {session_id, camera_id, publish_id, path, ingest, transport,
                bytes_sent, fps, bitrate_kbps, ready_ms, transcoded, backend}
  media_stopped{session_id, camera_id, publish_id, reason}   # reason kodlari: protocol.md §13
  media_stats  {publishes:[{publish_id, camera_id, bytes_sent, fps, bitrate_kbps,
                rtt_ms, packet_loss_pct, transport}]}          # har 10 s
  hb           {uptime_s, publishes, mem_mb, cams_total, cams_online}
  discovery_done / selftest_done                             # ixtiyoriy (audit/monitoring)
  pong

server → agent :
  welcome      {server_version, protocol_version, compat, last_seq, cams_epoch,
                limits:{max_uploads, max_local_transcodes, media_ttl_s},
                settings:{probe_pacing_parallel, codec_probe_interval_h, log_level},
                needs_full_sync}
  cams_request {full:true} | {full:false, camera_ids:[...]}
  cams_ack     {epoch, ok, applied, reason}
  media_start  {session_id, camera_id, profile, kind:play|preview, ttl_s,
                ingest:whip|rtsp_tcp,                       # H.264→whip, H.265→rtsp_tcp (8.1)
                publish:{token, urls:{whip, rtsp, rtsp_tc},
                         header_name:"Authorization", query_name:"token"},
                transcode:{mode:auto|server|client|off, by:server|client|none,
                           backend:gpu|cpu,
                           params:{width,height,fps,bitrate_kbps,maxrate_kbps,gop_s,profile}},
                reason}
  media_stop   {session_id, camera_id, reason:"no_viewers"}
  media_refresh{session_id, camera_id, extend_s}            # publish TTL uzaytirish (60 s watchdog)
  codec_fix    {camera_id, target:{profile,codec,no_bframes,width,height,fps,
                                   bitrate_kbps,gop_s}, apply_all:bool}
                # ONVIF orqali sub profilni H.264 ga o'tkazish; apply_all → bulk
  secret_rotate{new_secret, grace_s}                        # agent atomik saqlaydi, keyingi ulanishdan
  config_push  {limits:{...}, settings:{...}}               # server — yagona sozlash manbai
  ping
```

**B) UI kanali** `/ws/ui` (auth: JWT)

```
UI → server   : hello {browser_caps:{hevc:bool, ua}} (8.1, 2.5-qadam)
                subscribe {client_ids:[...]}, unsubscribe {}
server → UI   : client_status {client_id, status, last_seen_at}
                cam_status    {camera_id, status}
                session_count {camera_id, viewers}
                session_revoked {session_id, reason}
                session_state   {session_id, state}   # 7.1 holat mashinasi
```

### 7.1 Sessiya holat mashinasi va MediaMTX auth-hook (to'liq spetsifikatsiya `docs/protocol.md` da)

**Holatlar** (`sessions.state`):

```
pending ──media_ready──► publishing ──WHEP ulandi (reader≥1)──► playing ──idle/stop──► stopping ──► ended
   │                         │                                     │
   └──── taymaut / xato ─────┴─────────────────────────────────────┴──► failed  (UI: sabab + "qayta urinish")
```

| Taymaut | Qiymat | Natija |
|---|---|---|
| `media_start` → `media_ready` | **10 s** | `failed`, UI'da sabab + "qayta urinish" tugmasi |
| `whep_url` berildi → brauzer WHEP ulanishi | 10 s | `failed` |
| Transcode: FFmpeg start → `tc/...` publish | 15 s | `failed` (UI: "transcode ochilmadi") |
| Session TTL | 5 min, har 2 min'da uzaytiriladi | avtomatik `stopping → ended` |

Har bir o'tish WS orqali UI'ga (`session_state`) va audit log'ga yoziladi. `failed` sessiyalar limit hisoblagichidan darhol qaytariladi.

**MediaMTX auth-hook shartnomasi** (`authMethod: http`) — *tuzatilgan, protocol.md §7/§15*:
- MediaMTX har bir publish/read'da `POST http://127.0.0.1:8189/internal/auth` ga (vmsd ichida, **faqat localhost**) JSON yuboradi:
  `{action: publish|read|playback|api|metrics|pprof, path, protocol, user, password, token, ip, id, query, userAgent}`
  (`update` action'i **yo'q** — eski matndagi xato tuzatildi).
- Token uzatish protokol bo'yicha farq qiladi: **RTSP'da URL query `?token=…`**, **WHIP/WHEP'da `Authorization: Bearer <token>` header** (MediaMTX shu usulni qo'llab-quvvatlaydi). URL'larni vmsd o'zi token bilan quradi.
- Token: 256-bit tasodifiy (`base64url`, 43 belgi), DB'da **sha256 hash**. **"Bir martalik" emas**: mint→TTL oralig'ida **bitta session + bitta path + bitta action** uchun qayta ishlatilishi mumkin (WHIP/WHEP = POST SDP + PATCH ICE + DELETE), birinchi so'rovda session'ga bog'lanadi. Publish token TTL = `media_start.ttl_s` (60 s), read token TTL = 30 s.
- vmsd tekshiradi: token → sessiya → `path` mosligi (`cam/{client}/{camera}` yoki `tc/...`) → user huquqi. Javob: `200 {allow:true}` yoki 403. Tavsiya: javobda `"expireAt": "<RFC3339>"` berilsa — MediaMTX sessiyani o'zi yopadi (TTL ikki tomonlama kafolatlanadi).
- `tc/...` path'iga FFmpeg publish'i ham vmsd beradigan **ichki token** orqali kiradi (transcode jarayoni boshlanishida bir marta, agent tokenidan alohida).
- Hook natijalari sessiya id + publish id bilan bog'lanadi — audit zanjiri yopiladi (kim, qachon, qaysi path).

---

## 8. Media oqimi (kamera ochish ketma-ketligi)

1. UI katakda kamerani tanlaydi → `POST /api/cameras/{id}/play` (default `sub`).
2. Server tekshiradi: kamera + client online, **limitlar** (user ≤16, global ≤48, client ≤`max_uploads`).
3. Server MediaMTX'da path yaratadi: `cam/{client_id}/{camera_id}`, publish token + read token (TTL 30 s, bir marta). Kamera H.265 bo'lsa qo'shimcha `tc/{client_id}/{camera_id}` path va FFmpeg transcode jarayoni ishga tushadi — **kamera bo'yicha bitta, barcha tomoshobilarga umumiy** (8.1).
4. Server agent'ga `media_start` yuboradi: `publish:{token, urls:{whip, rtsp, rtsp_tc}}` + `ingest: whip|rtsp_tcp` + `ttl_s` + `transcode:{mode, by, backend, params}` (to'liq sxema §7-A / protocol.md §5.2). Token uzatish: RTSP'da `?token=`, WHIP'da `Authorization: Bearer`. `media_ready` 10 s ichida kelmasa sessiya `failed` (7.1).
5. Agent kameradan RTSP oladi. Profil **8.1** qoidaga ko'ra tanlanadi: H.264 bo'lsa **remux (transcode yo'q)** → **WHIP** upload; H.265 bo'lsa **transcode** (client yoki server) → natija H.264 WHIP bilan; H.265 to'g'ridan-to'g'ri (brauzer HEVC'li) yuborilsa → **RTSP/TCP publish** (H.265 WHIP ingest o'rniga — barqarorroq yo'l).
6. Server UI'ga `{whep_url, token}` qaytaradi (`cam/...` yoki transcode bo'lsa `tc/...`) → `RTCPeerConnection` boshlanadi. Transcode holatida UI `publishing` holatini ko'rsatadi: **"transcode boshlandi…"** (spinner + badge) — FFmpeg jarayonining ochilishi ~1–3 s qo'shimcha kechikish beradi (8.1).
7. Tomoshobi qolmasa (MediaMTX readers = 0, 10 s kuzatish) → `media_stop` → upload to'xtaydi, path o'chadi, transcode bo'lsa FFmpeg jarayoni ham o'ldiriladi.
8. UI sahifa o'zgarsa / katak bo'sasa / grid yopilsa → `stop` chaqiriladi (page unload'da ham).
9. **Fallback:** UDP ishlamasa agent RTSP-over-TCP publish qiladi, UI LL-HLS (`hls.js`) o'ynaydi.
10. **Agent tomonida refcount va TTL watchdog:** `camera+profile+ingest` bo'yicha **bitta** publish —
    2-user yoki `play`+`preview` bo'lsa ham upload bitta (`publish_id` hisobga olinadi);
    publish TTL 60 s, `media_refresh` bilan siljitiladi, **WS uzilgan bo'lsa ham** watchdog oqimni yopadi
    (joy kanali band qolmaydi). SIGTERM'da barcha publish'lar `media_stopped {reason:"agent_restart"}` bilan yopiladi.

Katak holatlari: bo'sh → "+" ; offline → qora fon + nom + ⚠ belgi ; yuklanmoqda → spinner ; xato → sabab + "qayta urinish".

### 8.1 Kodek siyosati — H.264 **va** H.265 (tasdiqlangan: aniq emas, aralash bo'ladi)

Brauzer WebRTC'da **H.265'ni dekodlamaydi**. Shuning uchun **kamera ulanganoq** kodek aniqlanadi va kerakli amal avtomatik bajariladi — operator hech narsa sozlamaydi.

**0-qadam — quvvatni aniqlash (agent ishga tushganda, bir marta).**
Agent FFmpeg imkoniyatlarini tekshiradi: `h264_nvenc` (NVIDIA), `h264_vaapi` (Intel/AMD), `h264_amf` (AMD), `libx264` (CPU) va HEVC dekodlash (`hevc_nvenc` / `hevc_vaapi`). Natija serverga `caps` xabari bilan yuboriladi: `caps {gpu: nvenc|vaapi|amf|none, cpu_cores, ram_mb, ffmpeg_version}`. Server `clients.caps_json` da saqlaydi va admin panelida ko'rsatiladi. Server tomoni ham o'z quvvatini (`vmsd --probe`) bir marta hisobga oladi.

1. **Aniqlash (kamera ulanganda).** Kamera qo'shilganda/ulanilganda va keyin har soatda RTSP DESCRIBE (SDP) orqali profil kodekini biladi (`codec_main / codec_sub` — uchinchi profil yo'q) va serverga yuboradi. Natija UI'da badge bo'lib ko'rinadi.
2. **Profil tanlash.** O'ynatish uchun **H.264 bo'lgan eng past bitrate'li profil** tanlanadi. Amaliyotda ko'p Hikvision/Dahua kameralarda `main = H.265`, `sub = H.264` bo'ladi — bu holatda transcode **umuman kerak bo'lmaydi** (eng ko'p uchraydigan yo'l shu).
2.5. **Brauzer imkoniyatini hisobga olish (katta tejash).** UI birinchi ochilishda brauzerning H.265 (HEVC) qo'llab-quvvatini aniqlab serverga yuboradi: `RTCRtpReceiver.getCapabilities('video')` ichida h265 payload qidiriladi (yordamchi: `navigator.mediaCapabilities.decodingInfo`). Chrome **136+** WebRTC'da HEVC'ni standart holda yoqdi (HW decode sharti bilan; Windows — D3D11, Linux — VAAPI, Chrome 108+). **Agar operator brauzeri HEVC'ni qo'llab-quvvatlasa — H.265 oqimi to'g'ridan-to'g'ri beriladi: transcode yo'q, NVENC band emas, kanal ~30–50% kam.** Qo'llab-quvvatlamasa 4-qadamga o'tiladi.
2.6. **B-frame tekshiruvi.** WebRTC H.264 B-frame'larni qo'llab-quvvatlamaydi — kamera `High` profile + B-frame yuborsa oqim brauzerda o'ynalmaydi. Agent SDP/kadr tahlili bilan B-frame'ni aniqlaydi va kamerani `Main`/`Baseline` (B-frame'siz) profiliga o'tkazadi (ONVIF), bo'lmasa re-encode qiladi.
3. **ONVIF bilan tuzatish (ixtiyoriy qadam).** Agar kerakli profil H.265 bo'lsa, agent `SetVideoEncoderConfiguration` orqali sub-stream'ni H.264'ga o'tkazishga urinadi (client UI'da yoqiladi/o'chiriladi, default yoqilgan). **Eslatma (tasdiqlangan): kameralar asosan Dahua va RTSP orqali ulangan** — ONVIF umuman bo'lmasa yoki switch'ni qo'llab-quvvatlamasa bu qadam jimgina o'tkazib yuboriladi. Kamera qo'llab-quvvatlamasa 4-qadamga o'tiladi.
4. **Transcode H.265 → H.264.** Serverda **RTX 3070 (NVENC)** bor — shuning uchun default **server** tomonda:
   - Server oqimi: upload H.265 (kichikroq bitrate, **RTSP/TCP publish** orqali) → MediaMTX `cam/...` path → **FFmpeg** jarayoni (HEVC dekod + `h264_nvenc` encode, 720p) → yangi `tc/...` path → brauzerga WHEP. MediaMTX o'zi transcode qilmaydi — jarayonni `vmsd` boshqaradi (zaxira usul: MediaMTX `runOnDemand` hook'i).
   - **Deduplikatsiya (majburiy):** har kamera uchun **bitta** FFmpeg jarayoni — nechta user / katak ko'rsa ham bitta process, barcha tomoshobilar shundan oladi. NVENC sessiyalari soni = transcode qilinayotgan **kamera** soni, tomoshobi soni emas. `tc/...` reader=0 bo'lsa 10 s keyin jarayon o'ldiriladi.
   - **Ochilish kechikishi ~1–3 s** (process start + pipeline iliq bo'lishi): UI `publishing` holatida **"transcode boshlandi…"** spinner/badge ko'rsatadi; taymaut 15 s (7.1).
   - **NVENC sessiya cheklovi:** GeForce kartalarda drayver/avlodga qarab **3–8** parallel sessiya (rasmiy hujjat 8 ko'rsatsa-da, amalda 3 ga cheklangan holatlar qayd etilgan). Shuning uchun `server_max_transcodes` **o'lchanadi**: `make probe` (yoki `vmsd --probe`) bir necha test encode ochib haqiqiy limitni topadi va `settings` ga yozadi (boshlang'ich qiymat konservativ **5**). Cheklovdan chiqqan H.265 kamera zaxira zanjirga o'tadi.
   - **Zaxira zanjiri:** server GPU → joy GPU (`caps` da nvenc/vaapi/amf bo'lsa client'da) → client CPU (`libx264 -preset veryfast -tune zerolatency`, max 4) → aniq xato xabari.
   - Cheklovlar: `server_max_transcodes` (8), per-client `max_transcodes` (GPU bo'lsa 8, CPU-only 4); chegaradan oshsa navbat + UI'da "navbatda" belgisi.
5. **Fallback.** Transcode mumkin bo'lmasa → LL-HLS (`hls.js`) **H.264** uchun. Eslatma: H.265 uchun HLS ham Chrome'da ishonchsiz (HEVC MSE qo'llab-quvvati platformaga bog'liq), shuning uchun **H.265 holatda transcode deyarli majburiy** hisoblanadi; transcode yo'q bo'lsa UI aniq xato ko'rsatadi: *"Bu kamera H.265 — H.264 profilini sozlang yoki transcode'ni yoqing"*.
6. **UI badge:** `H.264` · `H.265→GPU(S)` (server GPU) · `H.265→GPU(J)` (joy GPU) · `H.265→CPU(J)` · `H.265⚠` · `preview`. Admin kamera yoki client darajasida `transcode_mode` ni qo'lda o'zgartira oladi (`auto|server|client|off`, default **auto → server GPU**).
7. **Kengaytiriluvchanlik:** `codec` maydoni erkin satr (`av1`, `mjpeg`, `unknown`) — kelajakda ulanadigan har qanday kamera uchun sxema o'zgarmaydi.

### 8.2 Preview siyosati (tasdiqlangan: past sifatli live; og'ir bo'lsa snapshot'ga almashtiriladi)

- **Preview manba — `sub` profili** (tasdiqlangan: kameralarda uchinchi/preview profil **yo'q**). Qo'shimcha siqish yoki transcode **bajarilmaydi**: mavjud `sub` oqimi kichik oynada ko'rsatiladi.
- Ro'yxatda kamera ustiga kursor kelganda (yoki bitta bosish) preview session ochiladi; kursor chiqsa ~1 s ichida yopiladi.
- **Hover debounce (majburiy):** preview ochilishi uchun kursor **~600 ms** ushlab turiladi (dwell); ochilgach kamida **2 s** ushlab turiladi (qo'shni qatorga o'tishda churn bo'lmasin). Har preview to'liq RTSP→ingest→WHEP zanjirini ochadi (~0.5–1 s sozlanish), shuning uchun debounce `preview_max` limitini himoya qiladi.
- **Deduplikat (muhim):** preview va play **bitta path'ni** ishlatadi — `cam/{client}/{camera}`. Kamera allaqachon grid'da ochiq bo'lsa, preview **qo'shimcha upload ochmaydi**, mavjud oqimni ko'rsatadi. Upload bandligi **kamera bo'yicha** sanaladi (session bo'yicha emas).
- Bir vaqtda **faqat 1 preview** (user uchun), global `preview_max = 8`. Preview sessionlari `sessions.kind = preview` bilan auditlanadi va play limitidan (16) alohida hisoblanadi.
- `sub` H.265 bo'lsa va transcode zanjiri band bo'lsa → preview o'rniga faqat status belgisi ko'rsatiladi.
- **Kelajak almasinuvi uchun abstraksiya:** UI'da `PreviewProvider` interfeysi — `getPreview(cameraId) → {kind: "stream" | "image", url}`. Snapshot rejimiga o'tishda **faqat server** tomon o'zgaradi, UI komponenti o'zgarmaydi.

---

## 9. Auth, rollar, enrollment

- Parol: **argon2id**, login rate-limit (5 urinish / 5 min).
- Access JWT 15 min, refresh cookie 7 kun (httpOnly, SameSite=Lax).
- **admin**: user CRUD, joy/client boshqarish, enrollment kod, limitlar, audit, barcha sessionlar.
- **viewer**: faqat ko'rish + **o'z** favorites/layout. Boshqa user ma'lumotini ko'rmaydi.
- Hozircha location bo'yicha user cheklash **yo'q** (barcha viewerlar barcha joylarni ko'radi). Kerak bo'lsa keyin `user_site_grants` jadvali qo'shiladi.
- Enrollment: admin kod yaratadi → client lokal UI'sida server adresi + kod kiritiladi → agentga uzoq muddatli `agent_secret` beriladi (client faylida shifrlangan) → admin client nomini/site'ni belgilaydi.

---

## 10. Limitlar va resurs siyosati

| Parametr | Default | Izoh |
|---|---|---|
| Grid uchun profile | `sub` — **720p / 10 fps / 600–800 kbps CBR / GOP 2 s / B-frame'siz**, H.264 bo'lgan eng past profil tanlanadi (8.1, 16.1) | 4x4 = 16 oqim ≈ 10 Mbps (1080p/25fps holatiga qarab 3 marta kam) |
| Double-click / fullscreen | `main` | Bitta katakda main stream |
| Preview profile | **`sub`** (uchinchi profil yo'q) — qo'shimcha siqish/transcode **yo'q** | Ro'yxatdagi "yengil preview", bir vaqtda **1 ta**, play bilan bitta path |
| User concurrent (play) | 16 | Grid sig'imi bilan bir xil |
| User concurrent (preview) | 1 | Play limitiga kirmaydi |
| Global concurrent | 48 | Server CPU/kanalidan sozlanadi |
| Global preview | 8 | Preview yukini cheklar |
| Per-client upload | 8 | Joy internetini himoya qiladi, client UI'da o'zgartiriladi |
| Per-client transcode | GPU bo'lsa **8**, faqat CPU bo'lsa **4** | H.265→H.264 parallel soni, oshsa navbat |
| Server transcode (NVENC) | **5** boshlang'ich, keyin **o'lchanadi (3–8)** | GeForce drayver/kartasiga qarab parallel NVENC sessiya cheklovi |
| Idle stop | 10 s | Reader 0 bo'lsa (preview: ~1 s) |
| Session TTL | 5 min, har 2 min'da uzaytiriladi | UI yopilsa oqim o'zi o'ladi |

**Operator PC minimal konfiguratsiyasi (belgilangan talab):** 4 yadro CPU (Intel 8-avlod+ / Ryzen 3+), **8 GB RAM**, apparat dekoderi bor GPU (Intel UHD 620+ / NVIDIA / AMD — WebRTC HW decode uchun shart), Chrome yoki Edge **136+** (Linux'da VAAPI HW decode bilan), monitor 1080p. 4x4 grid doimiy ishlatiladigan bo'lsa tavsiya: 6+ yadro, 16 GB RAM, mustaqil GPU. HEVC'to'g'ridan-to'g'ri yo'li (8.1, 2.5-qadam) shu minimal konfiguratsiyada ishlaydi; undan past PC'da brauzer HEVC aniqlagi "yo'q" chiqadi va transcode zanjiri ishlatiladi.

Hisob: 30 concurrent × 1.5 Mbps ≈ **45 Mbps** joy→server upload va server→browser download. Transcoding bo'lmagani uchun server CPU ~4–8 vCPU yetadi.
**Transcode yuk:** serverda **RTX 3070** bor — H.265→H.264 default shu yerda, CPU deyarli band bo'lmaydi. Cheklov: GeForce'da parallel NVENC sessiyasi drayver/kartaga qarab **3–8** (boshlang'ich sozlamamiz **5**, `make probe` bilan o'lchanadi). Limitdan chiqqan H.265 kamera zaxira zanjirga o'tadi: **brauzer HEVC'ni o'zi esa o'ynatadi (transcode yo'q)** → joy GPU (bo'lsa) → joy CPU (`libx264 ultrafast`, 720p ≈ 1 vCPU/oqim, max 4) → aniq xato. Kameralarning aksariyati `sub` profilni H.264 beradi — transcode umuman kerak bo'lmasligi kutiladi.
**Copy (transcodesiz) holat:** 48 sessiyada server CPU ≈ 4–8 vCPU, kanal ≈ 70 Mbps download; VRAM band emas.

---

## 11. Server UI ekranlari va o'zaro ta'sir (o'zbek tilida)

**Ekran 1 — Login:** username/parol, xato sanab o'tish, refresh cookie bilan avtomatik kirish.

**Ekran 2 — Asosiy konsol** (uch zonali):
- **Chap panel:** joy sarlavhasi (masalan "Toshkent") ostida **client bloklari** — nom, status nuqtasi (yashil/kul), `online/total` kamera soni. Bitta shaharda bir nechta client bo'lsa, sarlavha ostida bir nechta blok ko'rinadi (tasdiqlangan). **Joy va client nomlari unikal** — UI'da chalkashlik bo'lmaydi.
- **Blok bosilganda:** ichidagi kameralar ro'yxati — qatorlarda nom, status nuqtasi, kodek badge; tanlangan kamera uchun **live preview** (`sub` oqimi kichik oynada, 8.2).
- **Asosiy maydon:** grid (1x1 / 2x2 / 3x3 / 4x4 tugmalari), pagination, layout boshqaruvi, "Favoritelar" tab.

**Grid o'zaro ta'siri (tasdiqlangan):**
- Joy/client tanlanganda grid **avtomatik** o'sha joy kameralari bilan to'ldiriladi (tartib: `cameras.position`).
- Kamera soni katakdan ko'p bo'lsa → **pagination** (4x4 = 16; 150 kamera = 10 sahifa). Sahifa o'zgarganda eski sahifadagi barcha oqimlar **darhol to'xtatiladi**.
- Katakka kamera biriktirish: ro'yxatdan bosish (bo'sh katakka) yoki drag&drop. Ikki katakni **suring** bilan o'zaro almashtirish.
- Double-click → katak to'liq ekran + `main` profile.
- Grid o'lchami kichraytirilsa (4x4 → 2x2) ortda qolgan kameralar navbatga o'tadi va oqimlari to'xtatiladi.
- Klaviatura: `1/2/3/4` — grid o'lchami, `←/→` — sahifa, `Esc` — to'liq ekrandan chiqish.

**Favorites:** har kamera yonida ⭐; "Favoritelar" tab'ida faqat o'zlari; serverda user bo'yicha saqlanadi; kataklarga ham favorites'dan qo'yish mumkin.

**Layout (moslash, tasdiqlangan):** "Saqlash" (nom bilan) → `layouts`; "Yuklash" → grid o'lchami + kataklar tartibi tiklanadi; "Standart qilish" → login'da avtomatik ochiladi. Layout **aralash joy** kameralarini saqlay oladi (slotlarda `camera_id` saqlanadi). Har user faqat o'z layoutlarini ko'radi.

**Status va xabarlar:** client offline → blok kul + "oxirgi ko'rilgan: 12:03"; kamera offline → katak qora + ⚠; limit tugasa → toast *"Oqimlar chegarasiga yetdingiz (16). Birortasini yoping."*; global limit band → *"Server band, navbatda turibsiz"* + avtomatik qayta urinish.

---

## 12. Xavfsizlik (TLS'siz holat uchun minimum)

- TLS hozircha yo'q — parollar va video och tarmoqda. Buni **xavf** sifatida qayd qilaman; tashqi himoya (VPN/IP allowlist/firewall) sizning tomonda.
- Server tomonda qiladiganlar: qisqa TTL tokenlar, WHEP token bir marta, login rate-limit, agent_secret rotatsiyasi, admin UI'ga IP allowlist imkoniyati, audit log, kamera parollarini serverda saqlamaslik.
- Keyingi bosqichda: Caddy avtomatik TLS (domain olinganda).

---

## 13. Kuzatish, zaxira

- `vmsd` va `vmsagent` systemd + journald, log darajasi config orqali.
- Har 5 min: client heartbeat; 30 s javobsiz → offline belgi.
- SQLite kundalik `VACUUM INTO` zaxira (`backup.sh`), `/var/lib/vmsd/`.
- O'lchash vositalari: MediaMTX `pprof: yes` + `/metrics` (Prometheus), `vmsd` `/metrics`, brauzer `getStats()` (bitrate, packet loss) — yuqorida 16.3.
- Admin panel: faol sessionlar, client offline ro'yxati, versiya, **transcode yuklamasi** (FFmpeg jarayonlari soni, NVENC sessiyalari `nvidia-smi` orqali, navbatda turganlar).

---

## 14. Bosqichma-bosqich reja

**Ish tartibi (tasdiqlangan, medium effort rejimi — batafsil `client/TASKS.md §0` va `client/REJA.md §9`):**
har bosqich **mayda qadamlarga** bo'lingan (bitta qadam = bitta modul, ≤120 qator kod + testi), har
qadamda kontrakt § ko'rsatilgan, test/fixture oldin yoziladi, taymautlar **fake clock** bilan,
murakkab konkurensiya o'rniga **single-writer goroutine** andozasi ishlatiladi. Bosqichlar orasida
**gate'lar** (G1–G5) bor: oldingi bosqichning `make fmt vet lint race` + `make itest` tekshiruvi
o'tmasa keyingisi boshlanmaydi. Maqsad — modelning ko'proq o'ylashiga emas, xato imkoniyatini
tuzilma darajasida kamaytirishga tayanish.

| Bosqich | Ishlar | Qabul mezoni | Taxmin |
|---|---|---|---|
| **P0** | `docs/protocol.md` (7.1 holat mashinasi + auth-hook shartnomasi bilan) + `api.md` + MediaMTX config loyihasi + **versiyani qulflash (`MEDIAMTX_VERSION`)**; `server/` va `client/` alohida skelet + Makefile (`start`/`build`) + `build/` chiqishi | `cd server && make build` va `cd client && make build` alohida ishlaydi; `make start` qurib ishga tushiradi | 2 kun |
| **P1** | Server: auth (admin/viewer), sites/clients/cameras registry, enrollment, agent WS hub, cams_sync | Agent ulanadi, 100 kamera serverda ko'rinadi, status yangilanadi | 3–4 kun |
| **P2** | Client: systemd, lokal UI (kamera CRUD + **bulk/CSV** + ONVIF, dizayn talabi client REJA §8), **kamera `id` generatsiyasi (UUID, o'zgarmas) + `cams_epoch`**, RTSP probe, **quvvat probe (GPU/CPU `caps`)**, **kodek probe (SDP: main/sub + profile + B-frame)**, **kashf etish (ONVIF WS-Discovery + RTSP port-scan) va ommaviy qo'shish**, ONVIF orqali profil almashtirish (**ixtiyoriy — kameralar asosan Dahua, RTSP orqali, 8.1-3-qadam**; sub'ni 720p / 10 fps / 600–800 kbps CBR / GOP 2 s / B-frame'siz qilib sozlash, 16.1; `codec_fix.apply_all` bulk), parol store, WS/backoff, `secret_rotate`/`config_push` | Joyda 100 kamera qo'shilib (≥10 tasi kashf etish orqali bir amaliyotda), serverda ro'yxat + status + **kodek** + **caps** to'g'ri ko'rinadi; sub profil parametrlari o'lchanadi; kamera o'chirilsa serverda ham o'chadi, nom o'zgarsa `id` saqlanadi | 6–8 kun |
| **P3** | Media: MediaMTX + **ingest: H.264 → WHIP (`pion/webrtc` v4), H.265 → RTSP/TCP publish (`gortsplib` v4 ANNOUNCE/RECORD; pion'da H.265 packetizer yo'q)** + WHEP play, session manager + **holat mashinasi/taymautlar (7.1)** + **auth-hook (`/internal/auth`)**, idle stop, limitlar, LL-HLS fallback, **kodek siyosati (8.1): H.264 remux / brauzer HEVC aniqlash (transcodesiz H.265 yo'li) / H.265 transcode — server NVENC default (kamera bo'yicha bitta FFmpeg), zaxira joy GPU→CPU / fallback**, **agent tomonida publish refcount + TTL watchdog + graceful shutdown**, NVENC real limitini o'lchash (`make probe`) | Brauzerda H.264 kamera **≤1.5s** kechikishda ochiladi (target ~0.5–1.0s, eng yaxshi ~0.3s); **HEVC'li brauzerda H.265 transcodesiz (RTSP/TCP ingest bilan) ochiladi**; HEVC'siz brauzerda server GPU transcode bilan ochiladi — **"transcode boshlandi…"** holati ko'rinadi, 2 user bitta H.265 kamerada **bitta** FFmpeg jarayoni ishlaydi; **2 user + preview bo'lsa agentda bitta publish**; `media_ready` 10 s taymauti va `failed` + qayta urinish ishlashi; yopilsa upload to'xtaydi; WS uzilsa TTL watchdog publish'ni yopadi | 7–10 kun |
| **P4** | Server UI: login, client bloklari + **live preview (8.2)**, grid 1x1–4x4 (**default 3x3**), **avtomatik to'ldirish + pagination**, favorites, layout saqlash, drag&drop, status/kodek belgilari, **IntersectionObserver + Page Visibility bilan avtomatik to'xtatish (16.2)** | 2 user parallel 15 tadan kamera ko'radi, layout saqlanadi, preview og'irlik yaratmaydi, tab yashirinsa barcha oqim to'xtaydi | 6–8 kun |
| **P5** | Qotirish: limitlar sozlamasi, audit, backup, offline holatlar, **MediaMTX tuning (`udpReadBufferSize`, `writeQueueSize`, faqat RTSP+WebRTC protokollari) + `pprof`/`metrics`**, load test (48 copy session + NVENC transcode limiti), deploy skriptlari | 48 concurrent session 30 daq uzluksiz, memory o'smaydi; NVENC chegarasi va zaxira zanjir ishlashi o'lchangan; metrikalar ko'rinadi | 3–4 kun |
| **P6** | Keyin (talab bo'lsa): TLS, location bo'yicha ruxsat, Telegram alert, **preview'ni snapshot rejimiga almashtirish** (agar live preview og'ir bo'lsa), arxiv playback | — | — |

Jami MVP: **~4–5 hafta** (bitta dasturchi, taktik tekshiruvlar bilan).

---

## 15. MVP "bajarildi" ro'yxati

- [ ] Admin yangi user va yangi joy/client yaratadi, client kod bilan ulanadi
- [ ] Joyda 100+ kamera client UI orqali qo'shiladi, serverda ko'rinadi
- [ ] **Kamera kashf etish:** tarmoqdagi kameralar (ONVIF WS-Discovery / RTSP scan) topiladi va **checkbox bilan bir amaliyotda** ommaviy qo'shiladi; qayta skanda mavjud kameralar takrorlanmaydi
- [ ] **Agent lokal UI'si zamonaviy/ixcham** (dark, zich jadval, inline edit, hotkeylar, self-test paneli) — client REJA §8
- [ ] UI'da client bloki bosiladi → kameralar ro'yxati + **past sifatli live preview** (bir vaqtda 1 ta, kursor chiqsa yopiladi)
- [ ] Location tanlanganda grid **avtomatik** to'ldiriladi va faqat o'sha joy kameralari ko'rinadi
- [ ] 1x1/2x2/3x3/4x4 grid ishlaydi, 16+ kamera pagination bilan; sahifa o'zgarganda eski oqimlar to'xtaydi
- [ ] H.264 kamera WebRTC'da **≤1.5s** kechikishda ochiladi (eng yaxshi ~0.3s); **H.265 kamera avtomatik transcode bilan ochiladi** (kamera bo'yicha **bitta** FFmpeg; UI'da `H.265→GPU(S)` / `H.265→GPU(J)` / `H.265→CPU(J)` badge va "transcode boshlandi…" holati)
- [ ] Preview `sub` orqali ishlaydi; kamera grid'da allaqachon ochiq bo'lsa preview **qo'shimcha upload ochmaydi**
- [ ] Faqat ochiq kataklarda oqim bor, yopilsa to'xtaydi
- [ ] Favorites ⭐ saqlanadi, user'ga mos
- [ ] Layout saqlanadi/yuklanadi, kataklar drag&drop almashtiriladi
- [ ] Offline kamera belgilangan, client offline bo'lsa blok belgilangan
- [ ] UI o'zbek tilida
- [ ] `cd server && make build` → `build/` ichida binary + `web/` + migrations; `cd client && make build` → `build/` ichida binary + config + systemd unit
- [ ] `make start` (server) va `make start` (client) alohida papkalarda ishlaydi; `make install` bilan systemd autostart sozlanadi

---

## 16. Yukni kamaytirish tadbirlari (kanal / CPU / brauzer)

### 16.1 Kamera tomonida (eng arzon — server resursi sarflanmaydi)

| Tadbir | Ta'sir | Izoh |
|---|---|---|
| FPS 25 → **10** (grid uchun) | bitrate ~50–60% kam | kuzatuv uchun 10 fps yetarli; fullscreen'da 25 fps |
| Ruxsat 720p → **640×360 / D1** | 40–60% kam | katak kichik, farq ko'zga tashlanmaydi |
| **CBR 600–800 kbps** (sub) | kanal barqaror | VBR dan ko'ra rejalashtirish oson, pike yo'q |
| **GOP 2 s** (I-frame intervali) | 20–40% kam | WebRTC'da birinchi kadr kutiladi — 2 s optimal, 4+ s ochilishni sekinlashtiradi |
| **H.265+ / Smart H.264** (Hikvision), **Smart H.265+ / AI Coding** (Dahua) | statik fonda 30–70% | ko'pincha faqat kamera web UI'sidan yoqiladi — P2 ro'yxatiga |
| Region of Interest / masklash | 10–30% | keraksiz hududni (osmon, devor) qoraytirish |
| Audio o'chirilgan | 64–128 kbps | allaqachon rejalashtirilgan |

**Kanal hisobi — 4x4 grid (16 kamera, bitta user):**

| Sifat | 1 kamera | 16 kamera |
|---|---|---|
| 1080p / 25fps / 2 Mbps | 2 Mbps | **32 Mbps** |
| 720p / 15fps / 1 Mbps | 1 Mbps | 16 Mbps |
| **720p / 10fps / 600 kbps (default)** | 0.6 Mbps | **9.6 Mbps** |
| 360p / 8fps / 350 kbps (tejamkor rejim) | 0.35 Mbps | 5.6 Mbps |

### 16.2 Brauzer tomonida

- **HEVC'ni to'g'ridan-to'g'ri o'ynatish** (8.1, 2.5-qadam) — transcode va qo'shimcha kanal umuman yo'q.
- **IntersectionObserver:** ko'rinmayotgan katak oqimini darhol to'xtatish (scroll / sahifa almashinuvi).
- **Page Visibility:** tab yashirinsa yoki deraza fokusda bo'lmasa **barcha** oqimlar to'xtaydi (amaliyotda 40–70% tejash — operator ekranni doim kara olmaydi).
- Default grid **3x3** (9 dekoder), 4x4 ixtiyoriy — dekoder soni 44% kam.
- `<video srcObject>` bilan GPU decode; canvas/WebCodecs orqali qo'lda render qilish **ishlatmaslik**.
- `RTCPeerConnection.getStats()` — inbound bitrate/packet loss yomon bo'lsa avtomatik past profilga tushish.

### 16.3 Server / media tomonida

- **Session deduplikatsiyasi:** play va preview bitta path — nechta user ko'rsa ham upload bitta (allaqachon rejalashtirilgan).
- **Transcode deduplikatsiyasi:** `tc/...` uchun har kamera **bitta** FFmpeg jarayoni — barcha tomoshobilar undan oladi; NVENC sessiyalari soni = H.265 kamera soni, tomoshobi soni emas (8.1). Sessiya bo'yicha transcoder qilish **taqiqlangan** — NVENC limiti bir zumda portlanadi.
- Transcode FFmpeg parametrlari: NVENC `-c:v h264_nvenc -preset p1 -tune ll -rc cbr -b:v 800k -maxrate 1M -bufsize 1M -g 50 -profile:v baseline -pix_fmt yuv420p`; CPU fallback `-c:v libx264 -preset ultrafast -b:v 600k`.
- MediaMTX hook varianti (`runOnAvailable` + FFmpeg → boshqa path) hujjatlashtirilgan zaxira; asosiy yo'l — `vmsd` boshqaradi (limitlar sababli).
- **MediaMTX tuning:** `udpReadBufferSize: 1000000` (+ `sysctl net.core.rmem_max=100000000`), `writeQueueSize: 1024`, packet loss bo'lsa `rtspTransports: [tcp]` / path uchun `rtspTransport: tcp`.
- **Faqat kerakli protokollar:** RTSP (publish) + WebRTC (play) + HLS (faqat fallback). RTMP / SRT / MPEG-TS o'chiriladi — har bir modul CPU/RAM sarflaydi.
- **O'lchash:** `pprof: yes` + `/metrics` (Prometheus) — yukni bilmasdan tejab bo'lmaydi.
- **Snapshot preview (keyin):** `runOnAvailable` + FFmpeg JPEG (har 10 s) — 16 kamera preview uchun ~0.5–1 MB bir marta, live preview (≈10 Mbps uzluksiz) o'rniga. `PreviewProvider` abstraksiyasi tufayli UI o'zgarmaydi.
- Kelajakda kuzatiladi: MediaMTX'da **Media-over-QUIC** qo'llab-quvvati.

### 16.4 Tarmoq va status yuk

- `webrtcAdditionalHosts` ga server static IP'sini to'g'ri yozish — aks holda bog'lanish TURN relay orqali bo'lib qo'shimcha yuk beradi.
- UDP afzal; UDP bloklangan joy uchun `webrtcLocalTCPAddress: :8189`.
- WS **delta sync + gzip**, heartbeat 30 s, status event-driven (har soatda to'liq `cams_sync` emas).
- Kamera status probe: RTSP `GET_PARAMETER` (keepalive) — DESCRIBE'dan arzon; to'liq kodek probe kuniga 1 marta.

### 16.5 MVP'ga kiritiladiganlar (ustuvorlik)

1. Kamera profil siyosati: 720p / 10 fps / 600–800 kbps CBR, GOP 2 s, B-frame'siz → **P2**
2. Page Visibility + IntersectionObserver → **P4**
3. Brauzer HEVC aniqlash va transcodesiz yo'l → **P3/P4**
4. MediaMTX tuning + `pprof`/`metrics` → **P5**
5. Snapshot preview almasinuvi → **P6** (tayyor abstraksiya bilan)

---

## 17. Ochiq savollar / risklar

Yuqoridagi 1-bo'limdagi barcha bandlar **tasdiqlangan**. Qolgan noaniq nuqtalar va risklar:

**Ochiq (aniqlash kerak)**
1. **Serverda NVIDIA drayveri va `ffmpeg` NVENC/CUVID bilan o'rnatilganmi** (Ubuntu versiyasi qaysi)? Transcode shunga bog'liq.
2. **Bir vaqtda 8 tadan ko'p H.265 kamera ochilishi kutiladimi?** NVENC sessiya cheklovi 8 — ko'proq bo'lsa qo'shimcha GPU yoki joy transcode'iga o'tamiz.
3. **Joy mashinasi quvvati/GPU** — agent `caps` orqali o'zi aniqlaydi, lekin xarid rejalashtiruvida GPU'li mini-PC zaxira transcode uchun qulayroq.

**Qabul qilingan (e'tiroz bo'lmasa shu qoladi)**
- Preview manba — **`sub`** (kameralarda uchinchi profil yo'q), qo'shimcha siqish yo'q; ochiq kamera uchun preview qo'shimcha upload ochmaydi.
- Transcode default — **server, RTX 3070 NVENC**; zaxira zanjir: joy GPU → joy CPU → xato xabari.
- Client offline bo'lsa kamera ro'yxati serverda oxirgi sinxron holaticha ko'rinadi, status "offline".
- Kamera o'sishi ~1000/yil deb hisoblanadi: `cams_sync` chunk'lab uzatiladi, indekslar (`cameras.client_id`, `sessions.camera_id`), SQLite WAL.
- Frontend binary'ga embed qilinmaydi, `build/web/` diskdan serve qilinadi.
- Kameralar asosan **Dahua, RTSP orqali** (tasdiqlangan): ONVIF ixtiyoriy; Dahua RTSP URL namunasi `/cam/realmonitor?channel=1&subtype=0|1` (subtype 0=main, 1=sub) — `docs/api.md` ga yoziladi. Dahua sub-stream odatda H.264 bo'ladi → transcode kamdan-kam kutiladi.
- Server offline bo'lsa joyda **lokal ko'rish talab qilinmaydi** (tasdiqlangan) — agent lokal UI faqat kamera CRUD/sozlamalar/status uchun.
- Haqiqiy kanal tezliklarini rejada ko'rsatish shart emas (tasdiqlangan) — 10-bo'lim limitlari yetarli, amaliyotda `make probe` + metrikalar bilan tekshiriladi.
- **H.265 ingest — RTSP/TCP publish** (WHIP emas); H.264 ingest — WHIP. Sabab: MediaMTX'da H.265 WHIP ingest'i nisbatan yangi, RTSP esa barqaror — P3 riski kamayadi. **Qo'shimcha sabab (tasdiqlangan):** `pion/webrtc` v4'da **H.265 packetizer yo'q** (faqat Opus, PCM, H264, VP8, VP9) — H.265'ni WHIP orqali yuborish texnik jihatdan ham imkonsiz.
- **Agent ingest kutubxonalari (tasdiqlangan):** `bluenviron/gortsplib` v4 (RTSP pull + H.265 RTSP/TCP publish — MediaMTX bilan bir xil RTSP implementatsiyasi, sof Go, qo'shimcha process yo'q) + `pion/webrtc` v4 (H.264 WHIP). FFmpeg faqat remux / B-frame re-encode / transcode / interop zaxirasi (`-f rtsp`, `-f whip`, FFmpeg ≥ 7.1).
- **Kontrakt manbasi (tasdiqlangan):** `client/docs/protocol.md` (1.0-draft.4) — client tomonida yozilgan va muzlatilgan; server S0'da `docs/protocol.md` ga ko'chiradi. Ziddiyat bo'lsa kontrakt kuchli.
- **Enrollment endpoint (tasdiqlangan):** `POST /api/agent/enroll` (ochiq, rate-limit 5/5 min) → `{agent_id, agent_secret}`; WS'da `auth {agent_id, secret}` → `welcome` handshake; `agent_id` ni server beradi.
- **Token semantikasi (tuzatilgan):** RTSP'da `?token=`, WHIP/WHEP'da `Authorization: Bearer`; token bir martalik emas — TTL davomida bitta session/path/action uchun qayta ishlatiladi (WHIP POST+PATCH+DELETE). Auth-hook `action` ro'yxatida `update` yo'q.
- **Kamera `id` (tasdiqlangan):** UUIDv4 ni agent yaratadi va o'zgarmaydi; sync `cams_epoch` + `op: upsert|delete` orqali (o'chirish `cams_sync` ichida).
- **Publish refcount + TTL watchdog (tasdiqlangan):** camera+profile+ingest bo'yicha bitta publish; publish TTL 60 s (`media_refresh`), WS uzilsa ham watchdog yopadi.
- **Kamera kashf etish + ommaviy qo'shish (tasdiqlangan, C2):** ONVIF WS-Discovery (`239.255.255.250:3702`) + RTSP port-scan (`discovery.cidrs`, ≤1024 host); checkbox + naming pattern bilan bir amaliyotda qo'shish; qayta skanda mavjud `id` saqlanadi; skan natijalari serverga yuborilmaydi.
- **Lokal UI dizayni (tasdiqlangan):** `go:embed`, qorong'i, ixcham, zich jadval, hotkeylar, 5 ekran (Kameralar / Kashf etish / Sozlamalar / Holat-Self-test / Jurnal) — client REJA §8, protocol.md §17.
- **Qo'shimcha mexanizmlar (tasdiqlangan):** `secret_rotate`, `config_push`, `media_stats` (10 s), `hb`, `media_refresh`, `codec_fix.apply_all` (bulk), `selftest_done`, graceful shutdown, log redaksiyasi, defensive limitlar, `probe_cycle_s` o'lchovi, Dahua `GET_PARAMETER` ishlamasa `OPTIONS`/`DESCRIBE` fallback.
- **MediaMTX versiyasi qulflangan** (`MEDIAMTX_VERSION`, P0'da tanlanadi) — prod takrorlanmasligi uchun; agent tomonida ham minimum MediaMTX versiyasi kontraktga yoziladi (WHIP/WHEP/H.265 xatti-harakati versiyaga bog'liq).
- Transcode — **kamera bo'yicha bitta FFmpeg**, barcha tomoshobilarga umumiy; UI'da "transcode boshlandi…" holati (~1–3 s qo'shimcha kechikish).
- Sessiya holatlari va taymautlar **7.1** da belgilangan; to'liq shartnoma `docs/protocol.md` (MediaMTX auth-hook shartnomasi bilan).
- Preview hover debounce: ochilish ~600 ms dwell, ochilgach kamida 2 s, yopilish ~1 s (8.2).
- Operator PC minimal konfiguratsiyasi **10-bo'limda** belgilangan.

**Risklar (reja ichida yechilgan)**
- **H.265 + WebRTC:** brauzer dekodlamaydi → 8.1 bosqichma-bosqich siyosat (H.264 profil → ONVIF almashtirish → GPU/CPU transcode → fallback). Eng ko'p uchraydigan holat (`main=H.265, sub=H.264`) transcodesiz yechiladi.
- **Transcode yuk:** server GPU (RTX 3070, NVENC) default; NVENC sessiya cheklovi **3–8** (o'lchanadi) → limitdan chiqsa zaxira zanjir: brauzer HEVC (transcodesiz) → joy GPU → joy CPU. Chegaraga yetilsa UI'da "navbatda" belgisi.
- **Brauzer HEVC holati operator PC'siga bog'liq:** Chrome 136+ WebRTC'da HEVC'ni yoqdi, lekin HW decode sharti bilan (Windows D3D11, Linux VAAPI Chrome 108+); eski brauzer/GPU'da ishlamaydi. Shuning uchun imkoniyat **har bir brauzer uchun alohida aniqlanadi** va natija `sessions` da ko'rinadi.
- **H.264 profile/level:** kamera `High` profile yuborsa WebRTC negotiation'da muammo bo'lishi mumkin → agent `annexb → avc` remux qiladi, P3'da maxsus test.
- **TLS yo'q:** parollar va video och tarmoqda (tashqi himoya sizning tomonda). Qisqa TTL tokenlar + agent_secret rotatsiyasi bilan yumshatiladi; domain olinganda Caddy orqali TLS qo'shiladi.
- **16 parallel WebRTC** brauzerda xotira ~1.5–2 GB'gacha bo'lishi mumkin → katak ko'rinmayotgan bo'lsa oqim avtomatik to'xtatiladi (viewport-based).
- **Vaqt:** serverda UTC saqlanadi, UI `Asia/Toshkent (+05)` bilan ko'rsatadi.
