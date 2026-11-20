-- 0002 down: ulanish kodlari jadvali qaytariladi (1.0-draft.7 holati).
DROP TABLE IF EXISTS preauth_ips;
DROP TABLE IF EXISTS join_requests;

CREATE TABLE enrollment_codes (
    code_hash  TEXT PRIMARY KEY,               -- sha256; xom kod saqlanmaydi
    site_id    INTEGER REFERENCES sites(id) ON DELETE SET NULL,
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    expires_at TEXT NOT NULL,
    used_at    TEXT,
    client_id  INTEGER REFERENCES clients(id) ON DELETE SET NULL
);
