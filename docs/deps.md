# Bog'liqliklar (qulflangan)

## Go modullari — versiyalar qulflangan (docs/protocol.md §8)

| Modul | Versiya | Nima uchun | Qachon kerak |
|---|---|---|---|
| github.com/pion/webrtc/v4 | v4.2.27 | H.264 → WHIP publish (ICE/DTLS/SRTP) | C3.6 |
| github.com/pion/interceptor | v0.1.51 | NACK/RTCP hisobotlari (media_stats) | C3.15 |
| github.com/pion/rtp | v1.10.6 | RTP paketlari | C3.5 |
| github.com/bluenviron/gortsplib/v4 | v4.16.3 | Kamera RTSP pull + **H.265 RTSP/TCP publish** (ANNOUNCE/RECORD) | C2.5, C3.8 — **STUB** (pastga qarang) |
| github.com/bluenviron/mediacommon | v1.14.1 | H.264/H.265 packetizer + SPS/PPS tahlili (B-frame) | C2.6 — **STUB**; o'rniga o'z parserimiz (`internal/cams/h264sps.go`) |
| github.com/google/uuid | v1.6.0 | Kamera `id` (UUIDv4, o'zgarmas) — §6 | C2.1 |
| github.com/gorilla/websocket | v1.5.3 | WS transport (agent ↔ server) — §2 | C1.4 |
| gopkg.in/yaml.v3 | v3.0.1 | client.yaml (hozircha ichki `yamlsub` ishlatilmoqda) | almashtirish imkoniyati |

**Majburiy eslatma:** `pion/webrtc` v4'da **H.265 packetizer yo'q** — H.265 hech qachon
WHIP orqali yuborilmaydi (protocol.md §8). Shu sababli H.265 ingest — RTSP/TCP (`gortsplib`).

`tools/deps.go` (`//go:build tools`) bu versiyalarni `go mod tidy` o'chirib yubormasligi
uchun ushlab turadi.

## Holat: yuklandi (2026-11-19)

`make deps-fetch` muvaffaqiyatli o'tdi: `go mod verify` → **all modules verified**,
`go build ./...` → OK. `go.mod` da versiyalar qulflangan holda turibdi
(hozircha `// indirect`, chunki kod ularni hali import qilmayapti — C1/C2 da
`go mod tidy` bilan direct holatga o'tadi).

**Qoida:** yangi modul qo'shish faqat `tools/deps.go` + shu jadval orqali.
`yamlsub.go` (ichki qismiy YAML parser) hozircha ishlatilmoqda — `yaml.v3`ga
o'tish imkoniyati saqlanib, C2 da qaror qilinadi (kontrakt §11 sxemasi ikkisida ham
bir xil tekshiriladi).

## Tashqi (system) bog'liqliklar

| Bog'liq | Minimal | Tekshiruv |
|---|---|---|
| ffmpeg | **>= 7.1** (`-f whip` muxer bo'lishi shart) | `make deps`, `make install` ogohlantiradi |
| NVIDIA drayveri + `h264_nvenc` (ixtiyoriy) | — | zaxira transcode (joy GPU) |
| `h264_vaapi` / `libx264` | — | CPU/VAAPI zaxira |
| golangci-lint (ixtiyoriy) | v1.x | `make lint` bo'lmasa `go vet` + gofmt bilan cheklanadi |

### Holat: qisman yuklandi (2026-11-19)

Haqiqiy va ishlatilmoqda: `gorilla/websocket`, `pion/webrtc v4`, `pion/rtp`, `google/uuid`, `yaml.v3`.
**Stub bo'lib kelgan** (fayl ichida faqat `package <nom>`): `bluenviron/gortsplib/v4`,
`bluenviron/mediacommon`. Proxy ularni bo'sh arxiv sifatida qaytaradi; `GOPROXY=direct` ishlamadi.
Rejalashtirilgan qaror `REJA.md §9` "Bloklov" bandida (tarmoq ruxsati / o'z RTSP mijozimiz / vendor).
