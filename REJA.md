# Central VMS — CLIENT qismi reyasi (`vmsagent` — joydagi agent)

Mustaqil loyiha: `client/` — alohida go module, alohida `Makefile`, alohida `build/` chiqishi.
Server bilan **bitta chiqish WebSocket** orqali bog'lanadi (joylarda port ochish shart emas).
Ubuntu, Docker yo'q, **systemd autostart**, lokal brauzer UI `:8082`.
Bu fayl root `../REJA.md` ga taylanadi (§ raqamlari o'sha yerdan).

Vazifalar ro'yxati: `TASKS.md` (shu papkada). Implementatsiya bosqichlari: **C0 → C4**.
**Kontrakt manbasi: `docs/protocol.md`** (shu papkada, versiya 1.0-draft.4 — muzlatilgan).
Server S0'da bu fayl `docs/protocol.md` ga ko'chiriladi; ziddiyat bo'lsa `client/docs/protocol.md` kuchli.

---

## 1. Doira (nima clientda, nima emas)

| Clientda | Serverda (qarang `../server/REJA.md`) |
|---|---|
| Kamera CRUD + **RTSP login/parollar (shifrlangan fayl)** — faqat shu yerda | registry, auth, limitlar, audit |
| RTSP probe (kodek/B-frame aniqlash), status watcher | session manager, WHEP, brauzer pleyeri |
| ONVIF (ixtiyoriy): discovery + profil sozlash | transcode default (RTX 3070 NVENC) |
| **Tarmoqdagi kameralarni topish va ommaviy qo'shish** (§7) | kamera ro'yxatini saqlash (`id` ni agent beradi, protocol.md §6) |
| **Ingest/publish:** H.264 → WHIP, H.265 → RTSP/TCP | MediaMTX, token mint, auth-hook |
| Zaxira transcode: joy GPU → joy CPU (server GPU band bo'lsa) | zaxira zanjir boshqaruvi (`transcode.by`) |
| Lokal UI `:8082` — kamera CRUD, server sozlamalari, holat | operator UI (React, serverda) |
| caps probe (GPU/CPU imkoniyatlari) → `hello` da yuboradi | |

**Tasdiqlangan (root §1/§17):** kameralar asosan **Dahua, RTSP orqali**; ONVIF ixtiyoriy. Server offline bo'lsa joyda **lokal ko'rish kerak emas** — lokal UI faqat CRUD/sozlamalar/status uchun. Kamera paroli serverga hech qachon uzatilmaydi (faqat mask'langan URL).

## 2. Chegara kontraktlari (yagona manba: **`docs/protocol.md`** — bu papkada, 1.0-draft.4, muzlatilgan)

To'liq xabar sxemalari, maydon nomlari, reason kodlari va misollar `docs/protocol.md` da. Bu yerda faqat qisqacha:

1. **Enrollment (protocol.md §3):** `POST /api/agent/enroll` (ochiq endpoint, rate-limit 5/5 min)
   `{code, hostname, os, lan_ip, agent_version, caps}` → `{agent_id, agent_secret, protocol_version}`.
   Kod bir marta; `401 bad_code`, `409 already_enrolled`, `429 rate_limited`. `agent_id` ni **server**
   beradi (`ag_…`), `agent_secret` DB'da sha256 hash, faqat bir marta chiqadi.
2. **Agent WS:** `ws://SERVER/api/agent/ws?agent_id=…` — ulanishdan keyin ≤5 s ichida
   `auth {agent_id, secret}` → server `welcome {server_version, compat, last_seq, cams_epoch,
   limits, settings, needs_full_sync}` (yoki 4002/4003/4004 bilan yopiladi). Envelope:
   `{type, seq, ts, data}`; noma'lum `type` e'tiborga olinmaydi.
   - yuboriladi: `hello {protocol_version, agent_version, os, hostname, lan_ip, boot_id, cams_epoch, caps}`,
     `cams_sync` (chunk'lab, `op: upsert|delete`, `epoch`), `cam_status`, `codec_probe`,
     `media_ready {publish_id, path, ingest, transport, transcoded, backend, fps, ready_ms}`,
     `media_stopped {publish_id, reason}`, `media_stats` (10 s), `hb`, `pong`, `discovery_done`, `selftest_done`
   - qabul qilinadi: `cams_request`, `cams_ack`, `media_start` (§2.3), `media_stop`, `media_refresh`,
     `codec_fix {apply_all}`, `secret_rotate {new_secret, grace_s}`, `config_push`, `ping`, `error`
3. **`media_start` to'liq maydonlari (endi tugallangan):** `session_id, camera_id, profile, kind,
   ttl_s, ingest, publish:{token, urls:{whip, rtsp, rtsp_tc}, header_name, query_name},
   transcode:{mode, by, backend, params:{width,height,fps,bitrate_kbps,maxrate_kbps,gop_s,profile}}, reason`.
   `whip_url`/`publish_token` alohida maydonlari **bekor qilindi** — `publish` bloki ishlatiladi.
4. **Token uzatish (protokol bo'yicha farq qiladi):** RTSP'da URL query `?token=`;
   **WHIP/WHEP'da `Authorization: Bearer <token>` header**. Token "bir martalik" emas —
   mint→TTL oralig'ida **bitta session/path/action** uchun qayta ishlatiladi
   (WHIP: POST SDP + PATCH ICE + DELETE). MediaMTX auth-hook `action` ro'yxati:
   `publish|read|playback|api|metrics|pprof` (`update` yo'q); javobda `expireAt` berilsa
   MediaMTX sessiyani o'zi yopadi.
5. **Ingest rejimi (majburiy):** H.264 → **WHIP (UDP)**; H.265 → **RTSP/TCP publish**.
   **Sabab (hujjatlashtirilgan):** `pion/webrtc` v4 media qatorida **H.265 packetizer YO'Q**
   (faqat Opus, PCM, H264, VP8, VP9) — H.265 WHIP orqali yuborib bo'lmaydi; MediaMTX'da esa
   H.265 WHIP ingest'i nisbatan yangi, RTSP barqaror.
6. **Publisher kutubxonalari (tanlov — eng yengil va stabil):**
   - kamera RTSP pull + **H.265 RTSP/TCP publish (ANNOUNCE/SETUP-interleaved/RECORD)** → **`bluenviron/gortsplib` v4**
     (+ `bluenviron/mediacommon` H.265 depacketizer/packetizer) — MediaMTX bilan bir xil RTSP implementatsiyasi,
     sof Go, qo'shimcha process yo'q;
   - **H.264 WHIP publish** → **`pion/webrtc` v4** (+ `pion/interceptor`);
   - **FFmpeg subprocess** faqat: remux `-c copy`, B-frame re-encode, client transcode,
     va Go yo'li interop sababli ishlamasa zaxira (`-f rtsp`, `-f whip` — FFmpeg ≥ 7.1).
7. **Kamera `id` ni agent yaratadi:** UUIDv4, o'zgarmas (protocol.md §6). Nom/host o'zgarsa ham
   `id` qoladi → server `favorites`/`layouts` buzilmaydi. Sync drift'i `cams_epoch` orqali.
8. Dahua RTSP URL: `rtsp://HOST:554/cam/realmonitor?channel=1&subtype=0|1` (0=main, 1=sub).
9. `media_start` olgach **10 s ichida** `media_ready` (server taymauti) — agent retry 1/2/4 s,
   bo'lmasa aniq `media_stopped {reason}` (§13 reason kodlari).

## 3. Komponentlar

> Paket ro'yxati `TASKS.md` qadamlari bilan bir xil — har qadam **bitta** paketga tegishli bo'lib
> belgilangan (medium rejim: bir qadamda ikki paket o'zgartirilmaydi).

```
cmd/vmsagent/main.go      # bitta binary (CGO_ENABLED=0), ldflags bilan versiya;
                          #   flaglar: -config, -version; SIGHUP → config reload
cmd/devserver/            # mock vmsd (make devserver) — C1.1, C1.14
cmd/repro/                # minimal reproduksiya dasturlari (xato tuzatishda yoziladi, §9.9)
testdata/                 # golden/fixture bazasi: protocol/*.json, sdp/*, rtp/*, wsdd/*.xml,
                          #   ffmpeg-encoders*.txt, csv/* — generatsiya: testdata/gen.sh

internal/proto/           # WS xabarlarining Go tiplari (faqat strukturalar, mantiq YO'Q);
                          #   golden round-trip testlari — kontrakt ↔ kod bog'lanishi shu yerda
internal/config/          # client.yaml load + default + validatsiya (§11 sxemasi)
internal/clock/           # vaqt interfeysi: Now/After/NewTicker + fake impl (testlar uchun)
internal/redact/          # parol/token/secret redaksiyasi: BARCHA log va UI xabarlari shu orqali
internal/keystore/        # AES-256-GCM: agent.key (0600) + secrets.bin + secret_ref
internal/statestore/      # state.json atomik yozish (*.tmp + rename), schema versiya, cams_epoch
internal/caps/            # ffmpeg -encoders parse (nvenc/vaapi/amf/libx264/hevc decode) + CPU/RAM
internal/agent/           # WS ulanish, envelope {type,seq,ts,data}, auth/welcome, backoff+jitter,
                          #   hello, cams sync (epoch/chunk/ack/op), secret_rotate, config_push, hb
internal/rtspclient/      # RTSP mijoz: Describe/Options/GetParameter, timeout, retry
                          #   (kamera pull va probe shu bitta joydan o'tadi).
                          #   ⚠ gortsplib bu muhitda stub bo'lib keldi — o'z minimal mijozimiz
                          #   yozilmoqda (variantlar REJA §9 "Bloklov" bandida)
internal/cams/            # SDP tahlili + H.264 SPS parse (o'z parserimiz: bits.go/h264sps.go;
                          #   B-frame = VUI max_num_reorder_frames; golden + ffprobe orakoli),
                          #   RTSP URL qurish/mask'lash (rtspurl.go), status watcher
                          #   (GET_PARAMETER → OPTIONS → DESCRIBE, pacing 5),
                          #   ONVIF (GetProfiles/SetVideoEncoderConfiguration)
internal/camsvc/          # kamera CRUD xizmati: id UUIDv4, position, parol keystore'ga,
                          #   har amalda cams_epoch++; CSV import/eksport (with-secrets)
internal/discovery/       # ONVIF WS-Discovery (239.255.255.250:3702) + RTSP port-scan (cidrs),
                          #   birlashtirish (MAC/serial/ip), bulk add (UUID yaratish)
internal/publish/         # registry (single-writer goroutine, refcount), TTL watchdog, retry 1/2/4 s,
                          #   H.264 → WHIP (pion/webrtc v4), H.265 → RTSP/TCP (gortsplib v4
                          #   ANNOUNCE/SETUP/RECORD), media_stats, zaxira: FFmpeg -f rtsp / -f whip
internal/ffmpeg/          # subprocess boshqaruvi: CommandContext, process group kill,
                          #   -progress pipe:1 parse, Wait(), zombie yo'q
internal/transcode/       # zaxira joy transcode: hevc_cuvid/vaapi → h264_nvenc/vaapi/amf/libx264
                          #   (max 4, navbat); `-c copy` remux; B-frame re-encode (baseline, -bf 0)
internal/localui/         # :8082 — kamera CRUD + bulk, kashf etish, self-test, jurnal, sozlamalar;
internal/logbuf/          # oxirgi N log qatori buferi (Jurnal ekrani, §17.1); redact'dan KEYIN yozadi;
                          #   UI binary'ga go:embed (server UI'dan farqli); dizayn §8
internal/selftest/        # server WS, WHIP UDP/TCP, RTSP publish, kamera connectivity — o'lchov
systemd/vmsagent.service  # unit (build'ga ko'chiriladi), install.sh /opt/vmsagent
config/client.yaml.example  # to'liq sxema: protocol.md §11
scripts/itest.sh          # make itest SCENARIO=… — integratsiya matritsasi (§6)
scripts/jscheck.py        # UI JS sintaksisi (node yo'q): qavs muvozanati + o'zbek apostrofi
                          #   JS satrini uzib qo'yishini ushlash (make ui-lint chaqiradi)
scripts/ui-shot.sh        # Chrome headless skrinshot + har ekran uchun DOM tekshiruvi
```

## 4. Clientga xos qarorlar (tasdiqlangan)

- **Lokal holat:** oddiy fayl (JSON + shifrlangan parol bloki) — DB kerak emas; backup/recovery `deploy` hujjatida (parol store yo'qolsa 100+ parolni qayta kiritishga to'g'ri keladi — zaxira shart).
- **Probe siyosati (root §16.4):** status — event-driven + arzon `GET_PARAMETER`; to'liq kodek probe kuniga 1 marta + kamera ulanganda; probe'lar **5 ta parallel** bilan tartibga solinadi (2000 kamerada LAN/firmware bombardimon bo'lmasin).
- **Sub profil maqsad parametrlari (root §16.1):** 720p / 10 fps / 600–800 kbps CBR / GOP 2 s / B-frame'siz. ONVIF bo'lsa agent sozlaydi; bo'lmasa kamera web UI orqali qo'lda (P2 hujjatiga).
- **B-frame (root §8.1-2.6):** H.264 `High`+B-frame kelsa — WebRTC'da o'ynalmaydi: agent aniqlaydi, imkon bo'lsa ONVIF orqali `Main/Baseline`'ga o'tkazadi, bo'lmasa re-encode.
- **caps:** `ffmpeg -encoders` tekshiruvi (`h264_nvenc/vaapi/amf`, `libx264`, HEVC decode) + CPU/RAM → `hello` da; server `clients.caps_json` da saqlaydi.
- **Tashqi bog'liqlik:** `ffmpeg` (`make install` tekshiradi, bo'lmasa ogohlantiradi).
- **Xavfsizlik:** parol store — machine-local kalit bilan shifrlangan; lokal UI `127.0.0.1` bind; agent_secret faylda shifrlangan.
- **Kamera `id` (tasdiqlangan):** UUIDv4 ni **agent** yaratadi, o'zgarmas; server PK shu. Nom/host/port
  o'zgartirilsa `id` saqlanadi → favorites/layout buzilmaydi. Sync drift `cams_epoch` orqali (protocol.md §6).
- **Publisher tanlovi (tasdiqlangan):** `gortsplib` v4 (RTSP pull + H.265 RTSP/TCP publish) +
  `pion/webrtc` v4 (H.264 WHIP); FFmpeg faqat remux/re-encode/transcode va zaxira. **pion'da H.265
  packetizer yo'q** — H.265 WHIP orqali yuborilmaydi (protocol.md §8).
- **Token semantikasi (tasdiqlangan):** RTSP'da `?token=`, WHIP/WHEP'da `Authorization: Bearer`;
  token bir martalik emas, TTL davomida bitta session/path/action uchun (protocol.md §7).
- **Publish refcount + TTL watchdog (tasdiqlangan):** `camera+profile+ingest` bo'yicha bitta publish,
  session'lar refcount; `ttl_s` (default 60 s) `media_refresh` bilan siljitiladi, WS uzilsa ham
  watchdog publish'ni yopadi (protocol.md §9).
- **Graceful shutdown (tasdiqlangan):** SIGTERM → barcha publish `media_stopped{agent_restart}` →
  WS 1001 → state flush; FFmpeg yetim processlar qoldirilmaydi (process group kill).
- **`secret_rotate` (tasdiqlangan):** server yangi secret yuboradi → agent atomik saqlaydi,
  keyingi ulanishdan boshlab ishlatadi; eski secret `grace_s` dan keyin bekor.
- **Log redaksiyasi (tasdiqlangan):** `internal/redact` — parol/token/secret barcha log, xato va
  UI xabarlarida `****` (journald'da RTSP paroli ko'rinmasligi shart).
- **Defensive limitlar (tasdiqlangan):** `max_uploads` va `max_local_transcodes` agent tomonida ham
  tekshiriladi (server limitidan mustaqil ikkinchi himoya), FFmpeg processlari ≤ limit+2.
- **Kutubxona versiyalari qulflanadi:** `go.mod` da `pion/webrtc v4`, `pion/interceptor`,
  `bluenviron/gortsplib v4`, `bluenviron/mediacommon`, `pion/rtp`; Go **1.26**; FFmpeg ≥ 7.1
  (`-f whip` muxer uchun). `make deps` ro'yxat chiqaradi.
- **Dahua keepalive ehtiyori (tasdiqlangan):** `GET_PARAMETER` ishlamasa → `OPTIONS` → qisqa `DESCRIBE`;
  tanlangan usol `cam_status` da ko'rinadi.
- **Probe sikli o'lchanadi:** 100 kamera ÷ 5 parallel ≈ 20 daq; `probe_cycle_s` log'da,
  qabul mezoni: 100 kamera sikli ≤ 25 daq, 2000 kamera sikli ≤ 4 soat (kunlik kodek probe oynasiga sig'adi).

## 5. Make targetlari

Root §4.2 client jadvali bilan aynan bir xil: `start, dev, build, install, uninstall, logs, status, test, lint, fmt, clean`.
Qo'shimcha (clientga xos, tasdiqlangan): **`run`** (qayta qurmasdan ishga tushirish), **`stop`** (dev/start jarayonlarini PID + aniq pattern orqali mayin to'xtatish), **`ui-shot`** (Chrome headless bilan 5 ekran skrinshoti → `build/shots/` — dizaynni ko'z bilan tekshirish), **`stop-service`** (`sudo systemctl stop vmsagent`), **`vet`, `race`** (`go test -race -cover` + coverage bo'sagasi),
**`secretscan`** (log/fayllarda parol/token qidiruvi — §9.7), **`itest`** (`SCENARIO=…` integratsiya
matritsasi, §6), **`stress`** (`CAMS=2000`), **`ui-lint`** (HTML/CSS tekshiruvi, node talab qilmaydi),
**`devserver`** (mock vmsd — §6), **`package`**
(`vmsagent-<version>-linux-amd64.tar.gz`), **`remote-update`** (`SSH_HOST=… make remote-update` —
`rsync build/` + `systemctl restart vmsagent`), **`deps`** (tashqi bog'liqliklar ro'yxati).
`build/` tarkibi root §4.1: `vmsagent`, `config/client.yaml`, `systemd/vmsagent.service`, `install.sh`, `VERSION`.
**Lokal UI binary'ga `go:embed` qilinadi** — `build/` ichida alohida `web/` papka YO'Q
(server UI'dan farqli: u diskdan serve qilinadi, client UI esa bitta binary).

## 6. Test strategiyasi (server tayyor bo'lmaganda ham)

- **`devserver` mock** (`make devserver`, C1): `POST /api/agent/enroll` + `/api/agent/ws` +
  `auth/hello/welcome` + `cams_request/cams_ack` + `media_start/stop/refresh` + `codec_fix` +
  `config_push` + `secret_rotate` + `media_ready` taymaut simulyatsiyasi — agent C2/C3 ishlarini
  server kutmasdan tekshirish mumkin (protocol.md §18).
- Mock MediaMTX o'rniga lokal `mediamtx` yoki `gortsplib` sun'iy serveri (token soxta tekshiruvi).
- Kamera o'rniga: FFmpeg `testsrc` RTSP sun'iy oqimi — **H.264 baseline (B-frame'siz)**,
  **H.264 High + B-frame**, **H.265**, parol bilan himoyalangan variant + imkon bo'lsa bitta real Dahua.
- **Test matritsasi (C3 qabul mezoni, `make itest SCENARIO=…`):** H.264 WHIP · UDP bloklangan → RTSP/TCP ·
  H.265 → RTSP/TCP · H.265 + `transcode.by=client` (GPU/CPU) · B-frame re-encode · token muddati o'tishi ·
  `media_ready` 10 s · refcount (2 user + preview) · WS uzilishi (watchdog) · SIGTERM (graceful) ·
  parol xato (`camera_rejected`) · discovery bulk add (10 soxta kamera) · zaxiradan tiklash · versiya nomosligi.
- **Fixture bazasi (`testdata/`, medium rejimning asosiy kafolati — §9.3):**
  `protocol/*.json` (kontrakt misollari, round-trip), `sdp/{h264-baseline, h264-main-nob,
  h264-high-bframes, h265}.sdp`, `rtp/*.rtp`, `wsdd/probe-match-*.xml`, `ffmpeg-encoders*.txt`,
  `csv/cameras-*.csv` — `testdata/gen.sh` FFmpeg orqali generatsiya qiladi, repo'ga commit qilinadi.
- **Fake clock** (`internal/clock`) — watchdog/retry/backoff/debounce testlari deterministik, ~10 ms'da.
- **`goleak`** — har testda goroutine sizib chiqmasligi; **`-race`** `make race` da majburiy.
- `make dev` — hot reload, `config/client.local.yaml`.

## 7. Tarmoqdagi kameralarni topish va ommaviy qo'shish (tasdiqlangan, C2)

**Mumkin — ikki usul bilan (protocol.md §16):**

1. **ONVIF WS-Discovery (afzal):** UDP multicast `239.255.255.250:3702`, 10 s timeout,
   `Probe`/`ProbeMatch` → `XAddrs` (ONVIF manzili), `Scopes` (vendor/model), MAC/serial.
   ONVIF auth so'ralsa — UI'dan beriladigan umumiy user/parol bilan `GetDeviceInformation` +
   `GetProfiles/GetStreamUri` orqali tasdiqlanadi.
2. **RTSP port-scan (zaxira, ONVIFsiz Dahua uchun):** `discovery.cidrs` bo'yicha TCP 554
   (25 parallel, 500 ms timeout) + anonim `DESCRIBE` (`/cam/realmonitor?channel=1&subtype=0|1`)
   → javob kelsa kamera topilgan, kodeklar darhol aniqlanadi.

**Ommaviy qo'shish oqimi (lokal UI):** skan → natijalar jadvali (IP, model, vendor, ONVIF bor/yo'q,
RTSP javobi, kodeklar, "allaqachon qo'shilgan") → checkbox bilan tanlash → `name_pattern`
(`{model}-{index:02d}`), umumiy yoki har biriga alohida user/parol, `position` avtomatik →
bitta amaliyotda qo'shish (progress chizig'i) → `cams_epoch++` → delta `cams_sync`.

**Qoidalar:**
- Unikal kalit **MAC** (yoki serial), bo'lmasa `ip+port` — qayta skanda mavjud kamera `id` si
  saqlanadi (`already_known: true`), layout/favorites buzilmaydi.
- Skan faqat `discovery.cidrs`; `/22` dan keng to'r rad etiladi (`max_hosts: 1024`);
  probe pacing navbati bilan birga ishlaydi (LAN bombardmoni bo'lmasin).
- Skan natijalari **faqat lokal UI'da** — serverga yuborilmaydi, server faqat qo'shilgan kameralarni biladi.
- **Bulk tahrirlash:** tanlangan kameralarga bir vaqtda `enabled`, `play_profile`, parol almashtirish,
  ONVIF `codec_fix {apply_all:true}` (sub → 720p/10fps/700kbps CBR/GOP 2s/B-frame'siz).
- **Import/eksport CSV** (`name,host,port,main_path,sub_path,user,pass`) — parol eksportda faqat
  `--with-secrets` bilan.

## 8. Lokal UI dizayni (tasdiqlangan talab: chiroyli, modern, qulay, ixcham)

To'liq spesifikasiya: **protocol.md §17**. Qisqacha:
- **Texnologiya:** `go:embed` — bitta HTML + CSS + vanilla JS (framework yo'q, `npm` talab qilinmaydi),
  binary'ga embed qilinadi; endpoint'lar JSON (`/api/ui/*`).
- **Uslub:** qorong'i tema (`--bg #0f1115`, `--panel #171a21`, `--accent #3b82f6`,
  `--ok #22c55e`, `--warn #f59e0b`, `--err #ef4444`), radius 8 px, chiziq chegaralar, soya yo'q;
  system-ui 13 px, IP/bitrate/ID lar uchun monospace.
- **Ekranlar (SPA, hash routing):** Kameralar · Kashf etish · Sozlamalar · Holat/Self-test · Jurnal.
- **Layout:** chapda 56 px ikonka paneli, yuqorida status chizig'i (server holati, publish soni,
  upload Mbps, versiya), asosiy maydon — zich jadval (28 px qator, inline edit, ko'p tanlash).
- **Komponentlar:** toast (4 s), modal (skan/parol), progress (bulk add), status nuqtasi,
  kodek badge (H.264 / H.265 / ⚠), filtr maydoni, sort, ustun tanlovi.
- **Klaviatura:** `/` filtr, `n` yangi, `d` kashf etish, `Space` tanlash, `Enter` tahrirlash,
  `Del` o'chirish, `?` yordam, `Esc` modal yopish.
- **Til:** o'zbek (bitta `i18n` obyekti); vaqt `Asia/Tashkent (+05)`.
- **Responsiv:** ≥1280 px optimal, 1024 px'da ustunlar kamayadi (mobil mo'ljallanmagan).
- **Holatlar:** server offline banner, skan bo'sh, parol xato, FFmpeg yo'q banner, GPU topilmadi badge,
  disk to'lgan (state yozilmadi) ogohlantirishi.
- **Himoya:** bind `127.0.0.1:8082`; LAN'ga chiqarish faqat `--allow-lan` + `ui_pin` majburiy
  (yozuv endpoint'lari `X-Agent-Pin`, sessiya cookie 12 soat).

## 9. Medium effort rejimida ishlash (xatosizlik kafolati)

Kod yozishda modelning "ko'proq o'ylashi"ga tayanmaymiz — **xato qilish imkoniyatini kamaytiradigan
tuzilma** ishlatamiz. Tasklar shu qoidalarga bo'ysunadi (`TASKS.md §0`).

1. **Kontrakt — yagona javob manbai.** Har task qatorida `[Kontrakt §x.y]` ko'rsatilgan; kod yozuvchi
   qaror qabul qilmaydi, `docs/protocol.md` dagi § ni ko'chiradi. § bo'lmasa — avval kontrakt to'ldiriladi.
2. **Mayda qadamlar.** Bitta qadam = bitta modul, ≤120 qator kod + testi. Katta bo'lak 2 urinishda
   tuzatilmasa ikkiga bo'linadi (effort ko'tarilmaydi).
3. **Fixture / golden testlar.** `docs/protocol.md` JSON misollari → `testdata/protocol/*.json`
   (round-trip); SDP/RTP/WSDD javoblari → `testdata/sdp/`, `testdata/rtp/`, `testdata/wsdd/`
   (`testdata/gen.sh` FFmpeg bilan generatsiya qiladi). Qo'lda SDP **regex** parse taqiqlangan —
   SDP/H.264 SPS parse strukturali (`internal/cams/sdp.go`, `h264sps.go`) va `ffprobe` bilan solishtirib tekshiriladi.
4. **Fake clock.** Taymaut, retry, watchdog, debounce testlarida haqiqiy uyqu taqiqlangan —
   `internal/clock` interfeysi orqali (60 s sikl ~10 ms'da o'tadi, test deterministik).
5. **Single-writer goroutine.** Publish registry, session map, state store — har birining bitta egasi,
   o'zgarishlar channel orqali. Murakkab `Mutex` tartibi **taqiqlangan** (race xatolarining manbai shu).
6. **Kontekst intizomi.** Har blokllovchi funksiyada `ctx` birinchi parametr; `contextcheck` + `goleak`
   linterlari majburiy.
7. **Statik tekshiruv quvuri.** `make fmt vet lint race` (gofumpt, go vet, golangci-lint: errcheck,
   govet, staticcheck, ineffassign, contextcheck, goleak, copyloopvar, nilerr, errorlint, revive) +
   coverage bo'sagasi: `proto/cams/publish` ≥ 70 %, qolganlar ≥ 50 %.
8. **Aniq xatolar.** Har failda protocol.md §13 reason kodi; `fmt.Errorf("…: %w", err)` zanjiri;
   "noma'lum xato" taqiqlangan. UI xabarlari reason jadvalidan olinadi (matritsa test bilan qoplangan).
9. **Kutubxona interop xatosida** → minimal reproduksiya (`cmd/repro/`) + darhol FFmpeg zaxira yo'liga
   o'tish (protocol.md §8.1), asosiy yo'l keyin tuzatiladi.
10. **Gate'lar.** `TASKS.md` da G1–G5: oldingi bosqich tekshiruvi o'tmasa keyingisi boshlanmaydi.

**Natija:** medium effortda ham har qadam "kontrakt → fixture → test → kod → tekshiruv" zanjiri bilan
yopiladi; murakkablik testlar orqali oldindan yozib qo'yiladi, o'ylashga qoldirilmaydi.

### Bloklov: tashqi RTSP kutubxonasi (2026-11-19, C2.5)

`go get` muvaffaqiyatli ko'rinadi, lekin `gortsplib` va `mediacommon` modullari **bo'sh stub** bo'lib
keladi (ichida faqat `package gortsplib` / `package mediacommon`; `go mod verify` ham "verified" deydi,
chunki go.sum shu proxydan olingan). `GOPROXY=direct` ishlamadi (GitHub'ga chiqish yo'q).
`pion/webrtc`, `pion/rtp`, `gorilla/websocket`, `google/uuid`, `yaml.v3` — haqiqiy va ishlayapti.

Ta'siri: C2.5 (RTSP mijoz), C2.7 (status watcher), C2.16 (RTSP port-scan), C3.8 (H.265 RTSP publish).
Kodek/SDP/B-frame tahlili ta'sirlanmadi — o'z parserimiz yozildi (G3 gate yashil).

Yechim variantlari (tasdiq uchun):
- **(a) Tarmoq ruxsati** — haqiqiy proxy yoki GitHub'ga chiqish; eng arzon, reja o'zgarmaydi.
- **(b) O'z minimal RTSP mijozimiz** — `internal/rtspclient` stdlib ustida (OPTIONS/DESCRIBE/SETUP/PLAY/
  ANNOUNCE/RECORD + interleaved RTP, TCP). ~500 qator + o'z mock RTSP serverimiz bilan testlanadi.
  Uzoq muddatda ham foydali (bog'liqlik kamayadi), lekin C2.5/C3.8 kechikadi.
- **(c) Vendor** — boshqa mashinadan `go mod vendor` qilib keltirish (muhit siyosati talabiga bog'liq).

## 10. Qabul mezoni (client MVP)

- Yangi Ubuntu mashinaga `make install` bilan 10 daqiqada o'rnatiladi, systemd avtomatik ko'tariladi.
- 100+ kamera lokal UI orqali qo'shiladi; serverda ro'yxat + status + **kodek** + **caps** to'g'ri ko'rinadi.
- **Kashf etish:** 10 soxta/real kamera topiladi, checkbox bilan **bir amaliyotda** qo'shiladi,
  qayta skanda mavjud `id` lar saqlanadi.
- `media_start` → 10 s ichida `media_ready`; H.264 WHIP, H.265 RTSP/TCP; `media_stop` → upload to'xtaydi.
- **Refcount:** bir kameraga 2 user + preview → **bitta** publish; TTL watchdog WS uzilsa ham yopadi.
- **Loglarda parol/token/secret yo'q** (redaktor testi); SIGTERM'da yetim FFmpeg process qolmaydi.
- Offline kamera/agent belgilanadi; resurs ~30 MB (bo'sh holatda), 100 kamera + 8 publish ≤ 150 MB.
