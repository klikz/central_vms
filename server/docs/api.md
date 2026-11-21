# vmsd HTTP API (server UI ↔ server)

Versiya: S0 (root §6 asosi; to'liq WS sxemalar: `docs/protocol.md`).
Xato formati (barcha endpoint'lar): `{"error":"<kod>","detail":"<matn>"}` + to'g'ri HTTP status.

## Auth (S1)

| Method | Yo'l | Izoh |
|---|---|---|
| POST | `/api/auth/login` | `{username,password}` → `{access, expires_in}` + refresh cookie (httpOnly, SameSite=Lax) |
| POST | `/api/auth/refresh` | cookie bilan yangi access |
| POST | `/api/auth/logout` | cookie bekor |
| GET | `/api/me` | user, role, limitlar |

Login rate-limit: 5 urinish / 5 min → `429 rate_limited`. Noto'g'ri parol: `401 bad_credentials`.

## Ro'yxatlar (S1)

| Method | Yo'l | Izoh |
|---|---|---|
| GET | `/api/sites` | joylar: name, clients_count, cameras online/total |
| GET | `/api/clients` | bloklar: name, site, status, cameras_count, online_count |
| GET | `/api/clients/{id}/cameras` | kamera ro'yxati + status + codec/preview imkoniyati; 1.0-draft.12: `last_error`, `last_error_at` (probe yoki oqim muvaffaqiyatsizligi sababi) |
| GET | `/api/cameras/{id}` | bitta kamera detali |

## Realtime: brauzer WS (`GET /api/ui/ws`, 1.0-draft.12)

Server UI'ga (`/monitor`, `/admin`) quyidagi voqealar uzatiladi — brauzer
so'rov yubormaydi, sahifa yangilanmaydi:

| Voqea | Maydonlar | Qachon |
|---|---|---|
| `hello` | `{clients, cameras}` | ulashganda boshlang'ich holat |
| `client_status` | `{client_id, online, ...}` | mijoz sessiyasi ochilganda/yopilganda |
| `cam_status` | `{camera_id, client_id, status, error, rtt_ms}` | agent `cam_status` yuborganda (§4.4) |
| `cams_changed` | `{client_id, agent_id, epoch}` | agent `cams_sync`ni **darhol** yuborganda (kamera qo'shildi/tahrirlandi/o'chirildi) — UI o'sha mijozning kamera ro'yxatini qayta oladi |
| `session_state` / `session_count` | sessiya/son | oqim holati o'zgarganda |
| `session_revoked` | `{session_id}` | sessiya bekor qilinganda |

## Oqim boshqaruvi (S2)

| Method | Yo'l | Izoh |
|---|---|---|
| POST | `/api/cameras/{id}/play` | `{profile:"sub"|"main", browser:{hevc:bool}}` → `{session_id, whep_url, token, ttl, codec, transcoded}` |
| POST | `/api/cameras/{id}/preview` | `{on:true|false}` → preview sessiya (kind=preview, profile=sub) |
| DELETE | `/api/sessions/{id}` | sessiyani mayin to'xtatish (idempotent). Faqat sessiya egasi — begga ID `403 session_not_yours` |
| POST | `/api/sessions/refresh` | `{session_id, extend_s}` → TTL uzatish. `extend_s` 0..120 s (0 — default); katta qiymat `400 bad_extend`. Faqat sessiya egasi: begga ID `403 session_not_yours` (sessiya ID'lari WS'da tarqaladi — 2026-11-21 auditi) |
| GET | `/api/sessions` | faol oqimlar (admin: barchasi, viewer: o'ziniki) |

  Javob (`GET /api/sessions`): massivi, har biri
  `{id, camera_id, user_id, profile, kind, state, expires_at, publish_id?}`
  — maydon nomlari **snake_case** (2026-11-20: Go strukturasida json tag
  bo'lmagani uchun `ID/CameraID/...` chiqib, UI jadvali bo'sh qolgan edi;
  tuzatildi + UI tipi moslashtirildi).

Xato kodlari: `404 not_found`, `403 disabled`, `409 client_offline`,
`429 user_limit` / `global_limit` / `client_uploads` / `preview_limit`
(`detail`da limit holati; navbat — S2.5 qoldig'i), `503 media_offline`.

**S2'da tekshirilgan (2026-11-20, `make e2e-s2`):** `play` javobida `token`
MediaMTX o'quvchi tokenidir — RTSP'da `?token=`, WHEP'da `Authorization: Bearer`
bilan beriladi; token TTL davomida bitta session/path/action uchun qayta
ishlatiladi, boshqa path yoki action'da `403`. Sessiya yopilgach MediaMTX
readers=0 bo'ladi va `idle_stop_s` keyin agentga `media_stop` yuboriladi
(har sessiya ID'si uchun alohida).

## Favorites va Layouts (S1/S3)

`GET/POST/DELETE /api/favorites[/{cameraId}]`, `PUT /api/favorites/reorder`;
`GET/POST/PUT/DELETE /api/layouts[/{id}]`, `PUT /api/layouts/{id}/default`.

## Admin (S1/S4)

`GET/POST/PATCH/DELETE /api/admin/users`;
`GET /api/admin/joins?status=pending` — ulanish so‘rovlari;
`POST /api/admin/joins/{id}/approve` `{name}` → `{agent_id}` (nom band bo‘lsa `409 name_taken`);
`POST /api/admin/joins/{id}/reject` `{note}`; `DELETE /api/admin/joins/{id}`;
`GET/POST/DELETE /api/admin/preauth` `{ip, name, note}` — IP bo‘yicha oldindan
ruxsat (1.0-draft.10; bir martalik kod oqimi bekor qilindi);
`GET/POST/PATCH /api/admin/clients` (nom, site, max_uploads, max_transcodes, transcode_mode, o'chirish);
`PATCH /api/admin/cameras/{id}` `{transcode_mode, play_profile, enabled}` (policy);
`GET /api/admin/media/state` (faol sessiyalar, FFmpeg jarayonlari, backend, navbat — S2/S4);
`GET /api/admin/audit`.

## Agent (ochiq endpoint — protocol.md §3)

```
POST /api/agent/join
  {join_key, agent_secret, hostname, name, os, lan_ip, agent_version}
  → 200 {status: "pending"|"approved", request_id, agent_id?}
  → 403 rejected (admin izohi bilan) | 400 bad_key | 429 rate_limited (5/5 min/IP)
  Agent so‘rov yuboradi va tasdiqni polling orqali kutadi. `agent_secret`ni
  agent o‘zi yaratadi — server uni faqat sha256 hash saqlaydi va hech
  qachon qaytarmaydi; tasdiqdan keyin faqat `agent_id` qaytadi.
  `name` — operator bergan ko‘rinadigan nom (masalan "Oxangaron").
  Mijoz faqat admin tasdig'idan yoki IP preauth mosligidan keyin yaratiladi
  (protocol.md §3.1–§3.3).
WS   /api/agent/ws?agent_id=…   (auth/welcome handshake — protocol.md §4)
```

## Ichki (faqat localhost)

```
POST /internal/auth     MediaMTX auth-hook (protocol.md §15) — S2'da to'liq
GET  /healthz           {ok, version, protocol, protocol_label}
GET  /metrics           Prometheus (S4)
```

## Kamera URL namunasi (Dahua, agent tomonida — serverga mask'langan holda keladi)

```
rtsp://USER:PASS@HOST:554/cam/realmonitor?channel=1&subtype=0   # main
rtsp://USER:PASS@HOST:554/cam/realmonitor?channel=1&subtype=1   # sub (preview ham shu)
Serverda saqlanadigan ko'rinish: rtsp://***:***@10.0.5.21:554/cam/realmonitor?channel=1&subtype=1
```
