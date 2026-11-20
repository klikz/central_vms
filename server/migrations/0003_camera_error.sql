-- 0003: kamera xatosi sababini saqlash (1.0-draft.12).
-- Operator "nima uchun offline/publish_failed" degan savolga javobni
-- server UI'da ko'radi — avval sabab faqat server log'ida qolardi.
ALTER TABLE cameras ADD COLUMN last_error TEXT NOT NULL DEFAULT '';
ALTER TABLE cameras ADD COLUMN last_error_at TEXT;
