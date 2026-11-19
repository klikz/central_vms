# CLIENT vazifalar (C0 → C4) — **medium effort rejimiga moslashtirilgan**

Belgilar: `[ ]` bajarilmagan, `[x]` bajarilgan. **Dependence** — qaysi vazifa oldin/tayyor bo'lishi kerak.
Kontrakt: **`docs/protocol.md`** (shu papkada, 1.0-draft.4 — **yozilgan va muzlatilgan**). Server uni
S0'da o'z `docs/` ga ko'chiradi; shu sababli client serverni kutmaydi. `devserver` mock bilan
C1→C3 to'liq parallel yuradi.

---

## 0. Medium effort rejimida ishlash qoidalari (majburiy)

Maqsad: **model ko'proq o'ylasin deb umid qilish emas** — xatoni imkoniyat darajasiga tushirish.
Shu sababli har bir qadam kichik, oldindan belgilangan, avtomatik tekshiriladigan bo'ladi.

### 0.1 Bo'laklash qoidasi
- Bitta qadam = **bitta modul yoki bitta fayl guruhi**, odatda ≤ 120 qator kod + uning testi.
- Bitta qadamda **ikkita mustaqil modulni** o'zgartirish taqiqlanadi (masalan `publish` + `localui`).
- Har qadam oxirida commit: `feat(client): C3.4 refcount registry` (commit-messages skilla bo'yicha).
- Qadam tekshiruvi (`Tekshiruv:` qatori) o'tmasa — **keyingi qadamga o'tilmaydi**.

### 0.2 Test-first (fixture oldin)
- Har modul uchun **avval `testdata/` fixture va test**, keyin kod. Test yozib bo'lmaydigan qadam
  qayta bo'linadi (odatda "modul noto'g'ri dizayn qilingan" degani).
- **Golden testlar** — `docs/protocol.md` dagi JSON misollar `testdata/protocol/*.json` ga ko'chiriladi
  va encode/decode round-trip qilinadi. Kontrakt bilan kod shu yerda bog'lanadi (qo'lda JSON yozish yo'q).
- **Taymaut/watchdog/retry** testlarida haqiqiy vaqt ishlatilmaydi — **fake clock** (`internal/clock`
  interfeysi: `Now()`, `After()`, `NewTicker()`), test 10 ms'da 60 s'lik siklni o'tadi.
- **Tarmoq/RTP/SDP** testlari faqat fixture fayllar ustida (`testdata/sdp/*.sdp`, `testdata/rtp/*.rtp`),
  tarmoq talab qilinmaydi.

### 0.3 "Aqlli kod" taqiqlangan andozalar (medium uchun asosiy himoya)
- **Yagona yozuvchi goroutine** (single-writer): publish registry, session map, state store —
  har birining **bitta egasi** bor, o'zgarishlar channel orqali keladi. `sync.Mutex` + murakkab
  lock tartibi **ishlatilmaydi** (race xatosi tug'iladigan joy shu).
- Har blokllovchi funksiyada **birinchi parametr `ctx context.Context`**; `contextcheck` linteri bilan tekshiriladi.
- `time.Sleep` testlarda **taqiqlangan**; `panic` boshqaruv oqimida **taqiqlangan**; `init()` da ish **taqiqlangan**.
- URL qurishda string concat **taqiqlangan** — `url.URL` / `url.JoinPath` (`?token=` qo'shish testi bilan).
- SDP/RTP parse qo'lda **taqiqlangan** — faqat `bluenviron/mediacommon` API si; bizning kodimiz
  faqat natijani o'qiydi (`codecs.H265`, `sdp.Media`).
- Xar xatoda **aniq `reason`** (protocol.md §13 jadvali) — `fmt.Errorf("...: %w", err)` zanjiri,
  "noma'lum xato" taqiqlangan.
- Global o'zgaruvchi yo'q; konfiguratsiya struktura orqali uzatiladi.

### 0.4 Statik tekshiruv (har qadamda)
```
make fmt vet lint test race   # gofumpt + go vet + golangci-lint + go test -race
```
- `C0` da `.golangci.yml` qo'shiladi: `errcheck, govet, staticcheck, ineffassign, contextcheck,
  goleak, copyloopvar, nilerr, errorlint, revive`. `goleak` — har testda goroutine sizib chiqmasligi.
- Qoplama (coverage) bo'sag'asi: `internal/proto`, `internal/cams`, `internal/publish` ≥ **70 %**,
  qolgan paketlar ≥ 50 %. `make test` shu bo'sagadan o'tmasa fail qiladi.

### 0.5 Qadam formati (shu faylda)
```
- [ ] **Nomi** — [Kontrakt §x.y] — [Fayl: internal/…] — [Test: …] — [Tekshiruv: komanda]
```
`Kontrakt §` bo'lsa, kod yozuvchi o'ylash o'rniga **shu bo'limni ko'chiradi**. Shu sababli
har qadamda § ko'rsatilgan; yo'q bo'lsa — qadam yozilmagan (avval kontraktga § qo'shiladi).

### 0.6 DoD (har bosqich uchun umumiy)
1. `make fmt vet lint test race` toza; 2. coverage bo'sag'asi o'tilgan; 3. qadam `Tekshiruv:` komandasi
o'tilgan; 4. yangi xabar/maydon bo'lsa `testdata/protocol/` golden yangilangan; 5. loglarda parol/token
yo'q (`make secretscan`); 6. hujjat yangilangan (REJA/protocol changelog).

### 0.7 Xato chiqsa (escalation)
- Bir qadam **2 urinishda** tuzatilsa → qadam ikkiga bo'linadi (effort ko'tarilmaydi, bo'lak kichraytiriladi).
- Xato tarmoq/media kutubxonasida bo'lsa → **minimal reproduksiya** (`cmd/repro/` ichida 30 qatorlik
  mustaqil dastur) yoziladi, keyin tuzatiladi. Reproduksiya `scripts/` da qoladi.
- Kutubxona interop xatosi → darhol **FFmpeg zaxira yo'liga** o'tiladi (protocol.md §8.1) va alohida
  task ochiladi; asosiy yo'l keyin tuzatiladi.

---

## C0 — Skelet, build va **test infratuzilmasi** (≈1.5–2 kun)

> Bu bosqichda **hech qanday biznes mantiq yo'q** — faqat skelet va tekshiruv poydevori.
> Medium rejimning asosiy kafolati shu yerda: keyingi barcha qadamlar shu quvur ustida yuradi.

- [x] **C0.1** Kontrakt fayli — `docs/protocol.md` (1.0-draft.4, yozilgan) — barcha qadamlar shu § larga bog'lanadi — [Test: yo'q] — [Tekshiruv: `grep -c "^## " docs/protocol.md` ≥ 20]
- [x] **C0.2** `go.mod` + `cmd/vmsagent/main.go` (flag `-config`, `-version`, SIGHUP reload stub, ldflags) — [§1] — [Test: `main_test.go` versiya chop etish] — [Tekshiruv: `go build ./... && ./vmsagent -version`]
- [x] **C0.3** Bog'liqliklarni qulflash (2026-11-19: tarmoq ochildi, `go mod verify` OK): `pion/webrtc v4`, `pion/interceptor`, `bluenviron/gortsplib v4`, `bluenviron/mediacommon`, `pion/rtp`, `google/uuid`; Go 1.26 — [§8] — [Test: yo'q] — [Tekshiruv: versiyalar `docs/deps.md` + `tools/deps.go` da qulflangan; tarmoq bo'lsa `make deps-fetch && go mod verify`]
- [x] **C0.4** `.golangci.yml` (§0.4 ro'yxati) + `gofumpt` + `make fmt vet lint` — [§0.4] — [Test: yo'q] — [Tekshiruv: `make lint` bo'sh chiqadi]
- [x] **C0.5** `internal/clock` — fake clock interfeysi + real impl — [§9.2] — [Test: `clock_test.go` — qo'lda siljitish] — [Tekshiruv: `go test ./internal/clock -race`]
- [x] **C0.6** `internal/redact` — parol/token/secret yashirish (bitta funksiya, barcha chiqish shu orqali) — [§10.2] — [Test: table-driven: `rtsp://u:pass@h`, `?token=`, `agent_secret`, Bearer header] — [Tekshiruv: `go test ./internal/redact -race -cover` ≥ 90 %]
- [x] **C0.7** `internal/proto` — barcha WS xabarlari uchun Go tiplari (faqat strukturalar, mantiq yo'q) — [§4, §5] — [Test: **golden** — `testdata/protocol/*.json` (protocol.md misollaridan) round-trip] — [Tekshiruv: `go test ./internal/proto -run Golden -race`]
- [x] **C0.8** `testdata/` fixture bazasi: `sdp/h264-baseline.sdp`, `h264-high-bframes.sdp`, `h265.sdp`, `mediamtx-auth/*.json` — [§8, §15] — FFmpeg bilan generatsiya skripti `testdata/gen.sh` — [Test: `testdata_test.go` — fixture'lar parse bo'lishi] — [Tekshiruv: `./testdata/gen.sh && go test ./internal/cams -run Fixture`]
- [x] **C0.9** `Makefile`: `start, dev, build, install, uninstall, logs, status, test, lint, fmt, vet, race, deps, clean` + **`devserver`**, **`package`**, **`remote-update`**, **`secretscan`**, **`itest`**, **`run`**, **`stop`**, **`stop-service`** — [§5 (client REJA)] — [Tekshiruv: `make -n` barcha targetlar; `make secretscan` bo'sh]
- [x] **C0.10** `config/client.yaml.example` — to'liq sxema — [§11] — [Test: `config` paketi — load + default + validatsiya (table-driven)] — [Tekshiruv: `go test ./internal/config -race`]
- [x] **C0.11** `systemd/vmsagent.service` + `install.sh` (`/opt/vmsagent`, `agent.key` 0600, `/var/lib/vmsagent`) — [§10.1] — [Test: `shellcheck install.sh`] — [Tekshiruv: `make build && ls build/` tarkibi §4.1 bo'yicha]
- [x] **C0.12** `make build` → `build/` (CGO_ENABLED=0, lokal UI `go:embed`), `ffmpeg` va `-f whip` borligini tekshiruvchi `make deps` — [§4.1 root] — [Tekshiruv: `cd client && make build && ./build/vmsagent -config config/client.yaml --version`]
- [x] **C0.13** `.gitignore` (build/, *.local.yaml, parol store, `agent.key`) — [Tekshiruv: `git status` toza]

- [x] **C0.14** Lokal UI API to'liqlash: `/api/ui/log` (logbuf buferi, yashirilgan qatorlar), `/favicon.ico` (embed SVG), noma'lum `/api/*` → JSON 404 — [§17.1, §10.2] — [Fayl: `internal/logbuf/`, `internal/localui/server.go`, `web/favicon.svg`] — [Test: `logbuf_test.go` (ring, daraja filtri, redaktasiya tartibi, musobaqa) + `server_test.go` (log limit clamp, favicon SVG, JSON 404, token yo'qolishi)] — [Tekshiruv: `go test ./internal/logbuf -race` (≥90 %) va `curl -s -o /dev/null -w "%{http_code}" localhost:8082/{favicon.ico,api/ui/log}` = 200]
      *Sabab:* brauzer konsolidagi `404 (Not Found)` — favicon va jurnal endpoint'i yo'q edi.

**Qabul:** `cd client && make build` mustaqil ishlaydi; `make start` lokal UI `http://localhost:8082` ochiladi;
`make devserver` ishga tushadi; `make fmt vet lint race` toza; `internal/proto` golden testlari o'tadi.

### C0 natijasi (2026-11-19) — **G1 gate o'tdi**
- Build: `make build` → `build/{vmsagent, config/client.yaml, systemd/vmsagent.service, install.sh, VERSION}`;
  versiya/commit/sana ldflags orqali kiritiladi (`-version` tekshirildi).
- UI: `go:embed` orqali binary ichida — `http://127.0.0.1:8082/healthz`, `/api/ui/status`, index (5 ekran) tekshirildi.
- Mock: `make devserver` — enroll 200/401/409 + rate-limit + `/api/agent/ws` 501 (C1.4 gacha) amalda ko'rildi.
- Testlar: `clock` 96%, `redact` 95%, `proto` 92% (25 golden fixture), `config` 84%, `localui` 80%,
  `cams` fixturelari (4 SDP + 4 Annex-B + 2 WSDD XML + 2 CSV) o'qildi. `make gate` (fmt+vet+lint+race+coverage) **yashil**.
- `install.sh` staging sinovi (`DESTDIR=`) — kalit 0600 yaratildi, takroriy ishga tushirishda kalit/config saqlandi.
- **C0.3 to'siq:** bu muhitda Go proxy'siga chiqish blokklangan (TLS handshake timeout).
  Qulflangan versiyalar `docs/deps.md` + `tools/deps.go` da; tarmoq ochilgach `make deps-fetch`.
  Hozircha kod **stdlib** da (YAML uchun `internal/config/yamlsub.go` — §11 ga yetarli qismiy parser).
- **Ochiq masala:** `client/` hali git reposi emas (`git describe` ishlamayapti, commit qoidalari uchta
  repo kerak: root emas, `client/` va `server/` alohida — root REJA §4.2).

---

## C0.5 — Spike: brauzer H.265 yo'lini tekshirish (≈0.5–1 kun, C0 bilan parallel)

- [ ] Maqsad operator PC konfiguratsiyasida (Ubuntu + Chrome/Edge 136+ + HW decode GPU) **H.265 → WHEP**
      oqimini amalda sinash: MediaMTX hujjati Chrome HEVC WebRTC uchun "only on Windows and only when a
      capable GPU is present" deydi — root §10 dagi Linux/VAAPI taxmini **tasdiq yoki inkor**
- [ ] Natija: `docs/hevc-browser-spike.md` — ishlaydi/ishlamaydi, versiyalar, kechikish o'lchovi
- [ ] Inkor bo'lsa: H.265 da transcode doimiy → NVENC limiti (5) va zaxira zanjir qayta baholanadi (server REJA §4)
- [ ] MediaMTX minimum versiyasini kontraktga yozish (WHIP/WHEP/H.265 xatti-harakati versiyaga bog'liq)

**Qabul:** bitta aniq javob + hujjat; natija C3.11 (client transcode) hajmiga ta'sir qiladi.

---

## C1 — Enrollment, WS, caps, lokal UI skelet (≈2.5–3.5 kun) — *Dependence: C0; real server uchun S1, lekin `devserver` mock bilan yuradi*

- [x] **C1.1** `internal/devserver` mock skelet: `POST /api/agent/enroll` + `/api/agent/ws` + `auth/welcome` — [§3.1, §3.3] — [Test: `devserver_test.go` — enroll + auth muvaffaqiyati] — [Tekshiruv: `go test ./internal/devserver -race`]
- [x] **C1.2** `internal/keystore` — AES-256-GCM (`agent.key` 0600, `secrets.bin`, `secret_ref`) — [§10.1] — [Test: round-trip, kalit noto'g'ri, fayl 0600 tekshiruvi] — [Tekshiruv: `go test ./internal/keystore -race -cover` ≥ 85 %]
- [x] **C1.3** Enrollment mijozi: `POST /api/agent/enroll` → `{agent_id, agent_secret}` → keystore'ga saqlash — [§3.1] — [Test: 200/401/409/420 holatlari (table-driven, `httptest`)] — [Tekshiruv: `go test ./internal/agent -run Enroll -race`]
- [x] **C1.4** WS ulanish + envelope `{type, seq, ts, data}` + `auth`→`welcome` (≤5 s) — [§2, §3.3] — [Test: devserver bilan to'liq handshake; `auth` kechiksa 4002] — [Tekshiruv: `go test ./internal/agent -run Handshake -race`]
- [x] **C1.5** Backoff + jitter + uzilishdan qayta ulanish (fake clock) — [§2] — [Test: ketma-ketlik 1,2,4,8…60 s ± jitter; 4001/4003'da 60→300 s] — [Tekshiruv: `go test ./internal/agent -run Backoff -race`]
- [x] **C1.6** `hello` yuborish + `welcome` qayta ishlash (`limits`, `settings`, `needs_full_sync`) — [§4.1, §5.7] — [Test: golden JSON orqali] — [Tekshiruv: `go test ./internal/agent -run Hello -race`]
- [x] **C1.7** `protocol_version` mosligi: `compat.ok=false` → aniq xato + backoff oshirish — [§1] — [Test: 4001 kodi bilan yopilish] — [Tekshiruv: `go test ./internal/agent -run Compat -race`]
- [x] **C1.8** `internal/caps` probe: `ffmpeg -encoders` parse (`h264_nvenc/vaapi/amf`, `libx264`, HEVC decode), `cpu_cores`, `ram_mb`, `ffmpeg_version` — [§4.1] — [Test: `testdata/ffmpeg-encoders*.txt` parse (table-driven, GPU bor/yo'q variantlar)] — [Tekshiruv: `go test ./internal/caps -race`]
- [x] **C1.9** `secret_rotate` + `config_push` + `media_refresh` handler'lari (handler = kichik funksiya, mantiq keyingi modullarda) — [§5.4, §5.6, §5.7] — [Test: atomik saqlash (crash-safe: `*.tmp`+rename), `ok:true` javob] — [Tekshiruv: `go test ./internal/agent -run Rotate -race`]
- [x] **C1.10** `internal/statestore` — `state.json` atomik yozish/oadarish + `cams_epoch` o'sishi + `schema` versiyasi — [§3.2, §6] — [Test: yozish/oadarish, korrupt fayl tiklash (`*.bak`), epoch o'sishi] — [Tekshiruv: `go test ./internal/statestore -race -cover` ≥ 80 %]
- [x] **C1.11** UI dizayn tizimi: `localui/web/{index.html,app.css,app.js}` — dark palitra, 56 px panel, status chizig'i, 28 px jadval, toast/modal/badge, hash routing (5 ekran), `i18n` obyekt, hotkeylar — [§17] — [Test: `app.js` uchun `node --test` **yo'q** (node yo'q) → statik tekshiruv: `make ui-lint` (HTML validator + CSS token ro'yxati) + qo'lda checklist] — [Tekshiruv: `make build && curl -s localhost:8082 | grep -c "data-screen"` ≥ 5]
- [x] **C1.12** Lokal UI HTTP API skeleti (`/api/ui/*`) + `ui_pin` (`X-Agent-Pin`, cookie 12 soat) + bind `127.0.0.1` — [§10.3] — [Test: PIN bo'lmasa 401; LAN bind faqat `--allow-lan` + PIN] — [Tekshiruv: `go test ./internal/localui -run Pin -race`]
- [ ] **C1.13** *(qisman)* Holat chizig'i: server holati, oxirgi heartbeat, oxirgi xato (redaktor orqali) — [§17] — [Test: handler snapshot testi] — [Tekshiruv: qo'lda: devserver'ni o'ldir → banner ko'rinadi]
- [ ] **C1.14** *(qisman — cams/media simulyatsiyasi C2/C3 da)* `devserver` to'liq: `cams_request/cams_ack`, `media_start/stop/refresh`, `codec_fix`, `config_push`, `secret_rotate`, taymaut simulyatsiyasi — [§18] — [Test: `make itest C1` ssenariysi] — [Tekshiruv: `make itest SCENARIO=c1`]

### C1 natijasi (2026-11-19) — **G2 oldidan**
- **End-to-end sinov (haqiqiy jarayonlar, `make devserver` + `build/vmsagent`):**
  `-enroll 7F3K-92QX-BD8T` → `agent ro'yxatdan o'tdi agent_id=ag_7bdc8df3213c53b5` →
  `joy imkoniyatlari gpu=nvenc cores=32 ram_mb=62830 ffmpeg=n9.0.2 hevc_decode=true` →
  `welcome qabul qilindi` → UI `/api/ui/status` **server_online:true** 22 s davomida barqaror.
  Server o'ldirildi → status **false**, backoff jurnali: `1s → 2s → 4s → 8s` (§2/§14).
  Server qayta boshlandi (`-state` fayldan 1 agent yuklandi) → agent **qayta enroll'siz** ulandi.
- **Fayllar:** `var/etc/agent.key` 0600, `secrets.bin` (`gcm1:` prefiks, 0600), `state.json` (0600, `secret_ref`).
- **Coverage:** keystore 86 %, statestore 80 %, agent 81 %, devserver 84 %, caps 94 %, cams 100 %, proto 100 %.
- **Topilgan va tuzatilgan:** devserver handshake'dan keyin read deadline'ni tiklamas edi (ulanish 5 s'da uzilar edi);
  endi 90 s + har 30 s server pingi (§2). Mock agentlarni yo'qotmaslik uchun `-state` qo'shildi.
- **Qolgan (C1.13/C1.14):** UI'da "oxirgi xato" banneri; devserver'da `cams_request/cams_ack`, `media_*`, `codec_fix`,
  taymaut simulyatsiyasi va `make itest SCENARIO=c1` ssenariysi (C2 bilan birga yoziladi).


**Qabul:** agent mock'da "online", `caps` to'g'ri; kod bir marta ishlaydi; uzilishdan keyin avtomatik qayta
ulanadi; `secret_rotate` dan keyin uzilmaydi; `make itest SCENARIO=c1` o'tadi; UI dizayn tizimi ishlaydi.

---

## C2 — Kameralar, probe, sync, kashf etish (≈5–6.5 kun) — *Dependence: C1* [root P2]

> SDP/B-frame tahlili — medium rejimda **eng xatoga moyil** joy. Shu sababu parse **faqat
> `mediacommon`** orqali, testlar esa **golden SDP fixture'lari** ustida. Qo'lda regex bilan
> SDP o'qish **taqiqlangan**.

- [x] **C2.1** Kamera modeli + **ID generatsiyasi**: UUIDv4 (agent), o'zgarmas, `state.json` da; o'chirilsa `id` qayta ishlatilmaydi — [§6] — [Test: nom/host o'zgarsa `id` bir xil; 1000 marta yaratishda takrorlanmaslik] — [Tekshiruv: `go test ./internal/cams -run ID -race`]
- [x] **C2.2** RTSP URL qurish/yorliqlash (`url.URL`, mask'lash `rtsp_masked`) — [§4.2, §10.2] — [Test: Dahua shablon `subtype=0|1`, parol mask'langan, `?` escape] — [Tekshiruv: `go test ./internal/cams -run URL -race`]
- [x] **C2.3** Kamera CRUD lokal UI (jadval + inline edit + `position`) — [§17] — [Test: handler CRUD (httptest)] — [Tekshiruv: `go test ./internal/localui -run CamCRUD -race`]
- [x] **C2.4** Bulk amaliyotlar + CSV import/eksport (`--with-secrets` bayrog'i) — [§16.3] — [Test: 50 qatorli CSV import, xato qatorlar hisoboti; eksportda parol `***`] — [Tekshiruv: `go test ./internal/cams -run CSV -race`]
- [x] **C2.5** `internal/rtspclient` — RTSP mijoz: `Describe`, `GetParameter`, `Options`, timeout, retry (bitta joyda). **Qaror (2026-11-19):** tarmoq to'liq intercept — proxy ham, `git clone` ham soxta stub qaytardi; variant (b) bo'yicha **o'z minimal mijoz (stdlib)**: `msg.go` (golden fixture `testdata/rtsp/*`), `client.go` (transport, ctx timeout, CSeq, StatusError), `auth.go` (401 Basic + Digest MD5/qop=auth), `retry.go` (`WithRetry`+`backoffDelay`). Qoplama 89 % — [§8, §12] — [Test: lokal TCP sun'iy server; timeout/401/refused/uzilish; Digest RFC 2617 mustaqil orakol] — [Tekshiruv: `go test ./internal/rtspclient -race`]
- [x] **C2.6** **SDP tahlili**: kodek (`h264|h265|unknown`) + profile (Baseline/Main/High) + B-frame belgisi — [§8.1, §12] — [Test: **golden** `testdata/sdp/*` — 4 fixture: h264-baseline, h264-main-nob, h264-high-bframes, h265; kutilgan natija JSON bilan solishtiriladi] — [Tekshiruv: `go test ./internal/cams -run SDP -race`]
- [x] **C2.7** Status watcher: `GET_PARAMETER` → `OPTIONS` → `DESCRIBE` fallback (C2.5 ga bog'liq); pacing **5 parallel**; event-driven `cam_status` — [§12] — [Test: fake clock + fake rtspclient: 100 kamera sikli, fallback ketma-ketligi, `probe_cycle_s` hisobi] — [Tekshiruv: `go test ./internal/cams -run Watcher -race`] *(2026-11-19: `internal/cams/watcher.go` — `Prober` interfeysi (rtspclient'ga bog'liq emas, fake bilan testlanadi), `ProbeOnce` zanjiri, `ProbeCycle` semaphore≤5 + `probe_cycle_s` logi, `Run` ticker; hodisalar faqat holat o'zgarsa `Emit`. Testlar: zanjir/fallback, 100 kamera pacing (maks 5), 60s sikl + log, emit-once-per-change. `-race -count=3` toza)*
- [x] **C2.8** Kodek probe jadvali: ulanganda + kuniga 1 + publish fail'da darhol — [§12.3] — [Test: 3 trigger holati, takrorlanish blokirovkasi] — [Tekshiruv: `go test ./internal/cams -run CodecProbe -race`] *(2026-11-19: `internal/cams/codecprobe.go` — `CodecScheduler.TryStart/Finish`, 3 trigger (`connect`/`daily`/`publish_fail`), per-kamera running bloki, daily 24h oynasi; §12.4 `IsBusy` hook'i — publish band bo'lsa probe kechiktiriladi (rad), to'xtatilmaydi. Fake clock bilan deterministik. Konstruktordagi `last` nil-map panic'i tuzatildi.)*
- [ ] **C2.9** `codec_probe` / `cam_status` yuborish (`internal/agent` orqali) — [§4.4, §4.5] — [Test: golden JSON chiqishi] — [Tekshiruv: `go test ./internal/agent -run Status -race`]
- [x] **C2.10** `cams_sync`: chunk'lash (≤100 kamera, ≤900 KiB), `op: upsert|delete`, `epoch/seq_no/chunk_count/done`, `cams_ack` (10 s retry ×3) — [§4.2, §4.3] — [Test: 250 kamera → 3 chunk; ack yo'qolsa retry; stale epoch rad etiladi] — [Tekshiruv: `go test ./internal/agent -run Sync -race`] *(2026-11-19: `internal/agent/sync.go` + unit + integratsiya (`tests/integration/c2_test.go`) yashil; testdagi ack-hisoblagich va `CamsRequest` ulanuvi tuzatildi; `Session.trySend/closeSendCh` race'ini yopdi)*
- [x] **C2.11** `cams_epoch` integratsiyasi: `hello`/`welcome` solishtiruvi → to'liq sync — [§6.5] — [Test: epoch farq → full sync; bir xil → delta yo'q] — [Tekshiruv: `go test ./internal/agent -run Epoch -race`] *(2026-11-19: `SyncIfRequired` + `cmd/vmsagent` OnWelcome ulanuvi; `go test ./internal/agent -run "Sync|Epoch"` o'tdi)*
- [ ] **C2.12** ONVIF moduli (ixtiyoriy): `GetProfiles/GetStreamUri`, `SetVideoEncoderConfiguration` (sub → 720p/10fps/700kbps CBR/GOP 2s/B-frame'siz) — [§16.1, §12] — [Test: SOAP fixture javoblar (httptest server); ONVIF bo'lmasa jimgina o'tkazib yuborish] — [Tekshiruv: `go test ./internal/cams -run ONVIF -race`]
- [ ] **C2.13** `codec_fix` bajarish + `apply_all` bulk + `codec_fix_done` javob — [§5.5] — [Test: 1 va 12 kamera, qisman muvaffaqiyat hisoboti] — [Tekshiruv: `go test ./internal/cams -run CodecFix -race`]
- [ ] **C2.14** B-frame siyosati: ONVIF bo'lmasa → C3 re-encode belgisi (`needs_reencode`) — [§8.1] — [Test: fixture `h264-high-bframes.sdp` → flag true] — [Tekshiruv: `go test ./internal/cams -run BFrame -race`]
- [ ] **C2.15** **Discovery: WS-Discovery** — multicast `239.255.255.250:3702` (IPv4), `Probe/ProbeMatch` parse, `XAddrs`/`Scopes`/MAC — [§16.1] — [Test: `testdata/wsdd/probe-match-*.xml` parse; tarmoq talab qilmaydi] — [Tekshiruv: `go test ./internal/discovery -run WSDD -race`]
- [ ] **C2.16** **Discovery: RTSP port-scan** (C2.5 ga bog'liq) — `discovery.cidrs` (≤/22, ≤1024 host), 25 parallel, 500 ms timeout, anonim `DESCRIBE` — [§16.1] — [Test: `127.0.0.1/32` + lokal gortsplib server; keng CIDR rad etilishi] — [Tekshiruv: `go test ./internal/discovery -run Scan -race`]
- [ ] **C2.17** Discovery birlashtirish: MAC/serial, bo'lmasa `ip+port`; `already_known` → mavjud `id` saqlanadi — [§16.2] — [Test: 10 soxta kamera, 3 tasini qayta skanda takrorlanmasligi] — [Tekshiruv: `go test ./internal/discovery -run Merge -race`]
- [ ] **C2.18** "Kashf etish" ekrani: natija jadvali, checkbox, `name_pattern` (`{model}-{index:02d}`), umumiy/alohida parol, progress, `d` hotkey — [§16.3, §17] — [Test: handler + snapshot HTML] — [Tekshiruv: qo'lda checklist (10 kamera → 1 amaliyot) + `go test ./internal/localui -run Discovery`]
- [ ] **C2.19** Bulk add oqimi: UUID yaratish + parol keystore'ga + `cams_epoch++` + delta `cams_sync` — [§16.3] — [Test: 50 kamera bir amaliyotda, qisman xato (parol yato) hisoboti] — [Tekshiruv: `go test ./internal/discovery -run BulkAdd -race`]
- [ ] **C2.20** `deploy/agent-state-backup.md` — `agent.key` + `secrets.bin` + `state.json` birga, tiklash tartibi — [§10.1] — [Test: yo'q (hujjat)] — [Tekshiruv: hujjat bo'yicha tiklash sinovi (lokal papkada)]

### C2 holati (2026-11-19) — qisman bajarildi
- Bajarilgan: **C2.1** (UUIDv4, o'zgarmas `id`), **C2.2** (`internal/cams/rtspurl.go`: qurish, sub profil `subtype=1`, mask'lash — parol hech qayerda ko'rinmaydi),
  **C2.3** (`/api/ui/cams*` CRUD + UI jadvali va qo'shish formasi; `go test ./internal/localui -run CamCRUD`), **C2.4** (`internal/camsvc`: CSV import/eksport,
  `with_secrets=1`), **C2.6** — **G3 gate yashil**: SDP + H.264 SPS parse (VUI `max_num_reorder_frames`), 4 golden fixture va `ffprobe` mustaqil orakoli bilan solishtirish.
- Yangi paketlar/fayllar: `internal/camsvc`, `internal/cams/{sdp,h264sps,bits,rtspurl}.go`, `testdata/sdp/expected-*.json`.
- **G2 gate yashil**: `make itest SCENARIO=c1` — enroll → ulanish → hb → `secret_rotate` → uzilish → backoff → enroll'siz qayta ulanish (race toza).
- **Bloklangan (tashqi sabab)**: `gortsplib` va `mediacommon` modullari proxy orqali **bo'sh stub** bo'lib keladi (fayl ichida faqat `package gortsplib`),
  `GOPROXY=direct` ham ishlamadi. Shu sababli C2.5/C2.7/C2.16 dagi RTSP amaliyotlari vaqtincha to'xtadi. Variantlar:
  (a) tarmoq ruxsati (haqiqiy proxy yoki GitHub), (b) o'z minimal RTSP mijozimiz (stdlib, ~500 qator), (c) boshqa mashinadan vendor qilish.
  Kodek/SDP tahlili bog'liq emas — o'z parserimiz yozildi va testlandi.
- **2026-11-19 (2-tekshiruv) — blok ochildi, variant (b) tanlandi:** tarmoq "ochiq" (curl 200), ammo proxy ham,
  GitHub'ga `git clone` ham **soxta 5 faylli stub** qaytardi (tarmoq to'liq intercept). C2.5 o'z minimal
  RTSP mijozimiz sifatida yozildi (C2.5a xabar modeli → C2.5b transport → C2.5c auth+retry; coverage 89 %).
  C2.7/C2.16 endi `internal/rtspclient` orqali davom etadi. RTP pull (C3.5) bu mijozda yo'q — C3 FFmpeg
  zaxira yo'li orqali (yoki tarmoq tuzalgach gortsplib).
- **Tuzatilgan (C2.10/C2.11 yashil bo'ldi):** `tests/integration/c2_test.go` da `cams_ack` hisoblagichi
  sync'dan KEYIN o'rnatilgan edi (ack'lar allaqachon iste'mol qilingan) va `CamsRequest` hook'i ulanmagan
  edi. Race detektori `Session`da haqiqiy uzilni topdi: `serve()` `sendCh`ni yopayotganda parallel `Send()`
  "send on closed channel"ga tushishi mumkin edi — `trySend`/`closeSendCh` (bitta mutex ostida) yopildi.
  Endi `go test ./... -race -count=3` toza.
- Qo'shimcha himoya: `scripts/jscheck.py` (UI JS'ini node'siz tekshiradi — o'zbek apostrofi JS satrini uzib butun UI bo'sh qolgan xatoni ushlash uchun),
  `make ui-shot` har ekran uchun **DOM tekshiruvi** ham qiladi (JS o'lsa oq sahifa skrinshoti "yashil" ko'rinardi — endi ushlanadi).


**Qabul:** 100 kamera qo'shiladi (≥10 tasi kashf etish orqali bir amaliyotda); serverda ro'yxat + status +
kodek + caps to'g'ri; kamera o'chirilsa serverda ham o'chadi; nom o'zgarsa `id` saqlanadi;
`probe_cycle_s` log'da (100 kamera ≤ 25 daq); `make itest SCENARIO=c2` o'tadi.

---

## C3 — Media publish / ingest (≈6–8 kun) — *Dependence: C2; integratsiya server S2 bilan* [root P3 client qismi]

> Eng xavfli bosqich. Medium rejimda **eng mayda qadamlar** shu yerda: har bir transport,
> har bir holat alohida qadam + alohida fixture. Har qadamda **bitta** yangi tushuncha.

- [ ] **C3.1** `internal/publish/registry` — **single-writer goroutine**: `publish_key = camera_id+profile+ingest`, session set, buyruqlar channel orqali — [§9.1] — [Test: 2 user + preview → bitta publish; `media_ready` bir marta; `goleak`] — [Tekshiruv: `go test ./internal/publish -run Registry -race`]
- [ ] **C3.2** TTL watchdog (fake clock): `ttl_s`, `media_refresh` deadline siljitadi, har 5 s tekshiruv, muddat o'tsa `media_stopped{ttl_expired}`; **WS uzilganda ham ishlaydi** — [§9.2] — [Test: 60 s sikl 10 ms'da; uzilish holati] — [Tekshiruv: `go test ./internal/publish -run Watchdog -race`]
- [ ] **C3.3** `media_start` parse + URL/token tanlash: `ingest` bo'yicha `urls.whip` yoki `urls.rtsp`; RTSP → `?token=`, WHIP → `Authorization: Bearer` — [§5.2, §7.2] — [Test: table-driven (ingest × token transport), `url.URL` natijasi golden] — [Tekshiruv: `go test ./internal/publish -run StartParse -race`]
- [ ] **C3.4** `internal/ffmpeg` — subprocess boshqaruvi: `exec.CommandContext`, process group kill, `-progress pipe:1` parse, `Wait()` goroutine, zombie yo'q — [§8.2] — [Test: `sleep 60` jarayonini kill; progress parse (`out_bytes`, `fps`, `bitrate`); `goleak`] — [Tekshiruv: `go test ./internal/ffmpeg -race -cover` ≥ 75 %]
- [ ] **C3.5** Kamera RTSP pull (gortsplib, TCP, past buffer) → RTP paketlarni o'qish; uzilishda 3 retry (2 s) — [§8, §9.3] — [Test: lokal gortsplib server (fixture H.264/H.265), uzilish/retry] — [Tekshiruv: `go test ./internal/publish -run Pull -race`]
- [ ] **C3.6** **H.264 → WHIP publish** (pion/webrtc v4): `POST {urls.whip}` + Bearer, SDP offer/answer, ICE/UDP, `media_ready{ingest:"whip", transport:"udp", ready_ms}` — [§5.2, §8.1] — [Test: lokal MediaMTX (yoki WHEP mijoz) bilan; 401 token holati] — [Tekshiruv: `make itest SCENARIO=c3_h264_whip`]
- [ ] **C3.7** WHIP zaxira: UDP ishlamasa → **RTSP/TCP publish** (Go) — [§8.1] — [Test: UDP bloklangan muhit (`iptables` yoki `netns`) yoki fake ICE fail] — [Tekshiruv: `make itest SCENARIO=c3_udp_blocked`]
- [ ] **C3.8** **H.265 → RTSP/TCP publish** (gortsplib `ANNOUNCE → SETUP(interleaved) → RECORD`, `mediacommon` H.265 packetizer) — [§8, §8.1] — [Test: lokal MediaMTX'da path paydo bo'ladi + `ffprobe` H.265 deb ko'rsatadi; **H.265 WHIP'ga urinilmasligi** (kod testi: `ingest=="whip" && codec=="h265"` → darhol xato)] — [Tekshiruv: `make itest SCENARIO=c3_h265_rtsp`]
- [ ] **C3.9** Retry siyosati: publish ochilishida 1/2/4 s (jami ~7 s < 10 s taymaut) → `publish_failed` — [§9.3] — [Test: fake clock, server javob bermaydi] — [Tekshiruv: `go test ./internal/publish -run Retry -race`]
- [ ] **C3.10** `media_stop` + refcount 0 → 1 s kutish → yopish → `media_stopped{no_viewers}` — [§9.1] — [Test: tezkor qayta ochish (1 s ichida) publish'ni uzmaydi] — [Tekshiruv: `go test ./internal/publish -run Stop -race`]
- [ ] **C3.11** **Client transcode** (`transcode.by=client`): FFmpeg `hevc_cuvid/vaapi → h264_nvenc/vaapi/libx264`, `params` bo'yicha flaglar, max 4 parallel navbat — [§5.2, §8.1, §10.4] — [Test: flag qatorlari golden; navbat (5-so request kutadi); GPU yo'q muhitda CPU yo'liga tushish] — [Tekshiruv: `make itest SCENARIO=c3_client_transcode` + `go test ./internal/transcode -race`]
- [ ] **C3.12** B-frame re-encode: `High`+B-frame → `-profile:v baseline -bf 0` → WHIP; bo'lmasa `bframe_unsupported` — [§8.1] — [Test: fixture `h264-high-bframes` → flag; FFmpeg flag golden] — [Tekshiruv: `make itest SCENARIO=c3_bframes`]
- [ ] **C3.13** `-c copy` remux yo'li (profile/level muammosi bo'lsa) — [§8.1] — [Test: fixture H.264 High → copy arxivi MediaMTX'da qabul bo'lmasa re-encode'ga o'tadi] — [Tekshiruv: `make itest SCENARIO=c3_remux`]
- [ ] **C3.14** FFmpeg zaxira publish: Go yo'li interop xatosida `-f rtsp` / `-f whip` (FFmpeg ≥ 7.1 tekshiruvi) — [§8.1, §8.2] — [Test: Go yo'li sun'iy o'ldirilgan holat; FFmpeg versiyasi past bo'lsa zaxira o'chirilgan] — [Tekshiruv: `make itest SCENARIO=c3_ffmpeg_fallback`]
- [ ] **C3.15** `media_stats` (10 s) va `media_ready` maydonlari (`bytes_sent`, `fps`, `bitrate_kbps`, `rtt_ms`, `packet_loss_pct`) — [§4.6] — [Test: golden JSON; manba uzilganda stats to'xtaydi] — [Tekshiruv: `go test ./internal/publish -run Stats -race`]
- [ ] **C3.16** Reason kodlari matritsasi: `camera_offline, camera_rejected, token_rejected, publish_failed, codec_mismatch, bframe_unsupported, transcode_failed, limit_exceeded, ttl_expired, network, agent_restart, shutdown, no_viewers, server_stop, user_close, page_hidden, idle_ttl` — [§13] — [Test: **table-driven** — har reason uchun trigger → kutilgan xabar; "noma'lum" holat yo'q] — [Tekshiruv: `go test ./internal/publish -run Reason -race`]
- [ ] **C3.17** Kodek o'zgarishi: publish fail → darhol to'liq probe → `codec_probe` + `codec_mismatch` — [§12.3] — [Test: manba kodekini o'rtada almashtirish (fixture almashinuvi)] — [Tekshiruv: `make itest SCENARIO=c3_codec_change`]
- [ ] **C3.18** Defensive limitlar: `max_uploads` band → `limit_exceeded`; FFmpeg ≤ `max_local_transcodes+2` — [§10.4] — [Test: 9-so `media_start` rad etiladi] — [Tekshiruv: `go test ./internal/publish -run Limits -race`]
- [ ] **C3.19** `preview` vs `play`: bir path, qo'shimcha upload yo'q (refcount orqali) — [root §8.2] — [Test: play + preview → 1 publish, 2 session] — [Tekshiruv: `go test ./internal/publish -run Preview -race`]
- [ ] **C3.20** Integratsiya (real server S2): H.264 ≤1.5 s ochilish o'lchovi, H.265 server transcode, 2 user = 1 FFmpeg — [root §14 P3] — [Test: `scripts/itest.sh` to'liq matritsa] — [Tekshiruv: `make itest SCENARIO=all`]

**Qabul:** H.264 ≤1.5 s; H.265 → server yoki joy transcode; 2 user + preview = **bitta** publish;
`media_stop` dan keyin upload yo'q; WS uzilsa watchdog yopadi; bo'sh holatda ~30 MB RAM;
`make itest SCENARIO=all` yashil; `goleak` barcha testlarda toza.

---

## C4 — Qotirish va deploy (≈2–3 kun) — *Dependence: C3* [root P5 client qismi]

- [ ] **C4.1** journald log darajasi config orqali; strukturali xatolar (`camera_id`, `publish_id`, `reason`) — [§10.2] — [Test: log satrlarida parol/token yo'q (`make secretscan`)] — [Tekshiruv: `make secretscan && journalctl -u vmsagent | grep -c pass` = 0]
- [ ] **C4.2** Graceful shutdown: SIGTERM → barcha publish `media_stopped{agent_restart}` → WS 1001 → state flush; SIGHUP → reload; FFmpeg yetimlari yo'q — [§9.4] — [Test: fake clock + `exec` sinov: `pgrep -P` bo'sh] — [Tekshiruv: `make itest SCENARIO=shutdown`]
- [ ] **C4.3** systemd hardening: `Restart=always`, `RestartSec=5`, `NoNewPrivileges`, `ProtectSystem=strict`, `ReadWritePaths=/var/lib/vmsagent`, `LimitNOFILE=4096`, `TimeoutStopSec=10` — [§5] — [Test: `systemd-analyze verify`] — [Tekshiruv: `systemd-analyze verify build/systemd/vmsagent.service`]
- [ ] **C4.4** `make install / uninstall / logs / status` yangi Ubuntu'da sinov (10 daq); `make package` + `SSH_HOST=… make remote-update` — [§5] — [Test: yo'q (qo'lda protokoll)] — [Tekshiruv: `docs/install-checklist.md` bo'yicha 10 band]
- [ ] **C4.5** Agent state backup + tiklash skripti (`agent.key` + `secrets.bin` + `state.json`) — [§10.1] — [Test: zaxiradan tiklash → `id` lar saqlanadi, `cams_epoch` oshadi] — [Tekshiruv: `make itest SCENARIO=restore`]
- [ ] **C4.6** Self-test paneli: server WS RTT, WHIP UDP/TCP, RTSP publish, har kamera "test" (ms, kodek, bitrate), faol publish ro'yxati, FFmpeg processlari — [§4.7, §17] — [Test: natija struktura snapshot] — [Tekshiruv: `go test ./internal/selftest -race`]
- [ ] **C4.7** Versiya: `--version`, `hello` da; `protocol_version` mos kelmasa UI banner — [§1] — [Test: banner ko'rinishi] — [Tekshiruv: `make itest SCENARIO=version_mismatch`]
- [ ] **C4.8** Stress: 2000 mock kamera — `probe_cycle_s` ≤ 4 soat, RAM ≤ 150 MB (100 kamera + 8 publish), FD `LimitNOFILE` ichida — [§12] — [Test: `make stress CAMS=2000`] — [Tekshiruv: `make stress` hisoboti `docs/stress-2000.md`]
- [ ] **C4.9** `deploy/client-firewall.md` — chiqish portlari (8081 WS, 8889 WHIP, **8189 UDP/TCP ICE**, 8554 RTSP) va joy ichida (554, 80, UDP 3702) — [§10.5] — [Test: yo'q (hujjat)] — [Tekshiruv: hujjat bo'yicha real joyda sinov]
- [ ] **C4.10** Load/monitoring: `hb` da `mem_mb`, `publishes`; `docs/runbook-client.md` (xato holatlari + reason jadvali) — [§4.8, §13] — [Test: yo'q] — [Tekshiruv: runbook bo'yicha 3 xato ssenariysini qo'lda qaytarish]

**Qabul:** root §15 MVP ro'yxatidagi client bandlari yashil; yangi mashinaga o'rnatish hujjatlanganidek
ishlaydi; SIGTERM'dan keyin serverda "yetim" sessiyalar qolmaydi.

---

## Qadam tartibi va parallelizatsiya

| Ketma-ketlik | Qadamlar | Parallel yuradigan |
|---|---|---|
| 1 | C0.1 → C0.13 (poydevor) | C0.5 spike |
| 2 | C1.1 → C1.14 | server S1 |
| 3 | C2.1 → C2.14 (kameralar/probe) | C2.15 → C2.19 (discovery) — alohida odam/orientir |
| 4 | C3.1 → C3.5 (transport poydevori) | C1.11/C1.18 UI ishlari |
| 5 | C3.6 → C3.10 (H.264/H.265 yo'llari) | server S2/S3 |
| 6 | C3.11 → C3.20 (transcode, fallback, integratsiya) | server S3 |
| 7 | C4.1 → C4.10 | server S4 |

**Majburiy to'xtash nuqtalari (gate):**
- **G1** (C0 oxiri): `make fmt vet lint race` + golden proto testlari — o'tmasa C1 boshlanmaydi.
- **G2** (C1 oxiri): `make itest SCENARIO=c1` — o'tmasa C2 boshlanmaydi.
- **G3** (C2.6 oxiri): SDP golden testlari — o'tmasa C3.5/C3.8 boshlanmaydi (barcha media mantiqi shu tahlilga tayanadi).
- **G4** (C3.10 oxiri): refcount + watchdog + stop testlari — o'tmasa transcode/fallback qadamlari boshlanmaydi.
- **G5** (C3.20): `make itest SCENARIO=all` — o'tmasa C4 boshlanmaydi.

Kalit nuqta: **kontrakt yozilgan va muzlatilgan** (`client/docs/protocol.md`, 1.0-draft.4) — client
serverni kutmaydi; medium effort rejimida har qadamda `Kontrakt §` ko'rsatilgan, shu sababli
"qanday bo'lishi kerak" degan savol kod yozish paytida tug'ilmaydi.
