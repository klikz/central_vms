-- 0001_init — boshlang'ich sxema (root REJA §5; server TASKS S0).
-- Vaqt maydonlari: UTC RFC3339 (root §17 "Vaqt" bandi), UI +05 ko'rsatadi.
-- Kamera paroli SAQLANMAYDI — faqat rtsp_masked (root §2).

CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    pass_hash     TEXT NOT NULL,               -- argon2id (S1)
    role          TEXT NOT NULL CHECK (role IN ('admin','viewer')),
    display_name  TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    last_login_at TEXT,
    disabled      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE sites (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    name     TEXT NOT NULL UNIQUE,             -- nom unikal (root §5 izoh)
    code     TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE clients (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    site_id           INTEGER REFERENCES sites(id) ON DELETE SET NULL,
    name              TEXT NOT NULL UNIQUE,
    agent_id          TEXT NOT NULL UNIQUE,    -- server bergan `ag_…` (protocol.md §3)
    agent_secret_hash TEXT NOT NULL,           -- sha256 (S1)
    enrolled_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    last_seen_at      TEXT,
    status            TEXT NOT NULL DEFAULT 'offline' CHECK (status IN ('online','offline')),
    version           TEXT NOT NULL DEFAULT '',
    os                TEXT NOT NULL DEFAULT '',
    lan_ip            TEXT NOT NULL DEFAULT '',
    caps_json         TEXT NOT NULL DEFAULT '{}',
    max_uploads       INTEGER NOT NULL DEFAULT 8,
    max_transcodes    INTEGER NOT NULL DEFAULT 8,
    transcode_mode    TEXT NOT NULL DEFAULT 'auto' CHECK (transcode_mode IN ('auto','server','client','off'))
);

CREATE TABLE cameras (
    id             TEXT PRIMARY KEY,           -- UUIDv4 — agent yaratadi, o'zgarmas (protocol.md §6)
    client_id      INTEGER NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    onvif_xaddr    TEXT NOT NULL DEFAULT '',
    rtsp_masked    TEXT NOT NULL DEFAULT '',   -- parol mask'langan URL
    profile_main   TEXT NOT NULL DEFAULT '',
    profile_sub    TEXT NOT NULL DEFAULT '',
    codec_main     TEXT NOT NULL DEFAULT 'unknown',
    codec_sub      TEXT NOT NULL DEFAULT 'unknown',
    bframe_main    INTEGER NOT NULL DEFAULT 0,
    bframe_sub     INTEGER NOT NULL DEFAULT 0,
    play_profile   TEXT NOT NULL DEFAULT 'sub' CHECK (play_profile IN ('main','sub')),
    transcode_mode TEXT NOT NULL DEFAULT 'auto' CHECK (transcode_mode IN ('auto','server','client','off')),
    enabled        INTEGER NOT NULL DEFAULT 1,
    position       INTEGER NOT NULL DEFAULT 0,
    status         TEXT NOT NULL DEFAULT 'offline' CHECK (status IN ('online','offline','error')),
    last_check_at  TEXT,
    codec_probe_at TEXT
);

CREATE TABLE favorites (
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    camera_id TEXT NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    position  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, camera_id)
);

CREATE TABLE layouts (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    grid       INTEGER NOT NULL CHECK (grid IN (1,2,3,4)),
    slots_json TEXT NOT NULL DEFAULT '[]',     -- slotlarda camera_id (aralash joy mumkin)
    is_default INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    UNIQUE (user_id, name)
);

CREATE TABLE sessions (
    id                TEXT PRIMARY KEY,
    camera_id         TEXT NOT NULL REFERENCES cameras(id) ON DELETE CASCADE,
    user_id           INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    started_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    ended_at          TEXT,
    profile           TEXT NOT NULL CHECK (profile IN ('main','sub')),
    kind              TEXT NOT NULL CHECK (kind IN ('play','preview')),
    transcoded        INTEGER NOT NULL DEFAULT 0,
    transcode_backend TEXT NOT NULL DEFAULT 'none' CHECK (transcode_backend IN ('nvenc','vaapi','amf','cpu','none')),
    transcode_by      TEXT NOT NULL DEFAULT 'none' CHECK (transcode_by IN ('server','client','none')),
    -- Holat mashinasi §7.1 / protocol.md §9
    state             TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','publishing','playing','stopping','ended','failed')),
    expires_at        TEXT                        -- TTL 5 min, uzatish 2 min (§7.1)
);

CREATE TABLE audit_log (
    id     INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    target TEXT NOT NULL DEFAULT '',
    at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Enrollment: bir martalik kod (24 soat) — protocol.md §3.1
CREATE TABLE enrollment_codes (
    code_hash  TEXT PRIMARY KEY,               -- sha256; xom kod saqlanmaydi
    site_id    INTEGER REFERENCES sites(id) ON DELETE SET NULL,
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    expires_at TEXT NOT NULL,
    used_at    TEXT,
    client_id  INTEGER REFERENCES clients(id) ON DELETE SET NULL
);

-- Media tokenlari: sha256 hash, TTL, session/path/action bog'lamasi
-- (protocol.md §7 — token bir martalik EMAS, shu uchun used_at YO'Q).
CREATE TABLE media_tokens (
    token_hash TEXT PRIMARY KEY,
    session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE,
    path       TEXT NOT NULL,                  -- cam/{client}/{camera} | tc/{client}/{camera}
    action     TEXT NOT NULL CHECK (action IN ('publish','read')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    expires_at TEXT NOT NULL,
    bound_ip   TEXT NOT NULL DEFAULT ''
);

-- Majburiy indekslar (server TASKS S0) + amaliy so'rov indekslari
CREATE INDEX idx_cameras_client_id ON cameras(client_id);
CREATE INDEX idx_sessions_camera_id ON sessions(camera_id);
CREATE INDEX idx_sessions_state ON sessions(state);
CREATE INDEX idx_audit_log_at ON audit_log(at);
CREATE INDEX idx_media_tokens_expires ON media_tokens(expires_at);
