-- 2026-11-21 server auditi: retention/janitor to'liq skan qilardi.
-- `DELETE FROM sessions WHERE started_at < ?` → SCAN sessions; har
-- o'chirilgan qator uchun FK CASCADE `SCAN media_tokens` (session_id
-- indekslanmagan). MaxOpenConns(1) bilan bu soatlik tozalash davomida
-- barcha yozuvchilarni qotirardi ("database is locked").
CREATE INDEX IF NOT EXISTS idx_sessions_started_at ON sessions(started_at);
CREATE INDEX IF NOT EXISTS idx_media_tokens_session ON media_tokens(session_id);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_join_requests_client ON join_requests(client_id);
CREATE INDEX IF NOT EXISTS idx_preauth_created ON preauth_ips(created_at);
