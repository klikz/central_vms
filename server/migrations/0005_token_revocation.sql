-- 2026-11-21 server auditi: token bekor qilish umuman yo'q edi.
-- 1) `users.token_epoch` — parol almashtirish, hisobni o'chirish yoki
--    rol pasaytirishdan keyin mavjud access token darhol yaroqsiz
--    bo'ladi (avval 15 daqiqagacha ishlayverardi).
-- 2) `refresh_tokens` — refresh cookie rotatsiyasi endi haqiqiy:
--    eski (ko'chirilgan) refresh JTI'si DB'da bo'lmasa rad etiladi
--    (avval eski cookie 7 kun davomida yangi access chiqarardi).
ALTER TABLE users ADD COLUMN token_epoch INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS refresh_tokens (
	jti        TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL,
	expires_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user ON refresh_tokens(user_id);
