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
| GET | `/api/clients/{id}/cameras` | kamera ro'yxati + status + codec/preview imkoniyati |
| GET | `/api/cameras/{id}` | bitta kamera detali |

## Oqim boshqaruvi (S2)

| Method | Yo'l | Izoh |
|---|---|---|
| POST | `/api/cameras/{id}/play` | `{profile:"sub"|"main", browser:{hevc:bool}}` → `{session_id, whep_url, token, ttl, codec, transcoded}` |
| POST | `/api/cameras/{id}/preview` | `{on:true|false}` → preview sessiya (kind=preview, profile=sub) |
| POST | `/api/sessions/{id}/stop` | sessiyani mayin to'xtatish |
| GET | `/api/sessions` | faol oqimlar (admin: barchasi, viewer: o'ziniki) |

Xato kodlari: `404 not_found`, `409 client_offline`, `429 user_limit` / `global_limit` / `client_uploads` / `preview_limit` (`detail`da limit va navbat holati), `503 media_offline`.

## Favorites va Layouts (S1/S3)

`GET/POST/DELETE /api/favorites[/{cameraId}]`, `PUT /api/favorites/reorder`;
`GET/POST/PUT/DELETE /api/layouts[/{id}]`, `PUT /api/layouts/{id}/default`.

## Admin (S1/S4)

`GET/POST/PATCH/DELETE /api/admin/users`;
`POST /api/admin/enrollments` `{site_name}` → bir martalik kod (24 soat);
`GET/POST/PATCH /api/admin/clients` (nom, site, max_uploads, max_transcodes, transcode_mode, o'chirish);
`PATCH /api/admin/cameras/{id}` `{transcode_mode, play_profile, enabled}` (policy);
`GET /api/admin/media/state` (faol sessiyalar, FFmpeg jarayonlari, backend, navbat — S2/S4);
`GET /api/admin/audit`.

## Agent (ochiq endpoint — protocol.md §3)

```
POST /api/agent/enroll
  {code, hostname, os, lan_ip, agent_version, caps}
  → 200 {agent_id, agent_secret, protocol_version}
  → 401 bad_code | 409 already_enrolled | 429 rate_limited (5/5 min)
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
