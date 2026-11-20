-- 0002: ulanish tartibi koddan "so'rov + admin tasdiqi" modeliga o'tdi
-- (protocol.md §3, 1.0-draft.10). Operator talabi: "ulanish kodini olib
-- tashla. ulanish ruhsati faqat server UI dan tasdiqlanadi".

-- Agent yuborgan ulanish so'rovi. `join_key_hash` — agentning doimiy
-- kalitining sha256'i (xom kalit saqlanmaydi): shu kalit orqali agent
-- o'z so'rovini izlaydi va tasdiqlangandan keyin hisob ma'lumotlarini
-- oladi.
CREATE TABLE join_requests (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    join_key_hash TEXT NOT NULL UNIQUE,
    -- Agent o'z sirini O'ZI yaratadi va yuboradi; server uni faqat sha256
    -- hash ko'rinishida saqlaydi. Tasdiqdan keyin agent polling orqali
    -- faqat `agent_id` ni oladi — sir hech qachon serverdan qaytmaydi.
    agent_secret_hash TEXT NOT NULL DEFAULT '',
    hostname      TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL DEFAULT '',   -- operator bergan nom (ixtiyoriy)
    os            TEXT NOT NULL DEFAULT '',
    lan_ip        TEXT NOT NULL DEFAULT '',   -- agent aytgan LAN manzili
    source_ip     TEXT NOT NULL DEFAULT '',   -- so'rov kelgan IP (server o'lchaydi)
    agent_version TEXT NOT NULL DEFAULT '',
    caps_json     TEXT NOT NULL DEFAULT '{}',
    status        TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending','approved','rejected')),
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
    decided_at    TEXT,
    decided_by    INTEGER REFERENCES users(id) ON DELETE SET NULL,
    client_id     INTEGER REFERENCES clients(id) ON DELETE SET NULL,
    note          TEXT NOT NULL DEFAULT ''    -- admin rad sababi / izohi
);

CREATE INDEX idx_join_requests_status ON join_requests(status, created_at);

-- IP bo'yicha oldindan ruxsat: operator server UI'dan mijoz IP'sini
-- yozib qo'yadi — shu manzildan kelgan so'rov avtomatik tasdiqlanadi
-- (protocol.md §3.3). `ip` — aniq manzil yoki CIDR (192.168.0.0/22).
CREATE TABLE preauth_ips (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    ip         TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL DEFAULT '',  -- berilsa mijoz shu nom bilan yaratiladi
    note       TEXT NOT NULL DEFAULT '',
    created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);

-- Bir martalik ulanish kodlari bekor qilindi.
DROP TABLE enrollment_codes;
