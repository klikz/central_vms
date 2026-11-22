package registry

import (
	"database/sql"
	"time"
)

// janitor.go — S4.3 retention: `sessions` va `audit_log` yozuvlari
// belgilangan kundan (config.retention_days, standart 90) eski bo'lsa
// o'chiriladi. Vaqt SQLite'da UTC RFC3339Z satr sifatida saqlanadi —
// shu formatdagi chegara bilan solishtirish leksikografik tartibda
// to'g'ri ishlaydi.

// purgeChunked — `LIMIT` bilan bosqichma-bosqich o'chirish.
func purgeChunked(db *sql.DB, table, pk, col, cutoff string) (int64, error) {
	const chunk = 500
	var total int64
	for {
		res, err := db.Exec(`DELETE FROM `+table+` WHERE `+pk+` IN
			(SELECT `+pk+` FROM `+table+` WHERE `+col+` < ? LIMIT ?)`, cutoff, chunk)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < chunk {
			return total, nil
		}
	}
}

// PurgeOlderThan — retention chegarasidan eski sessiyalar va audit
// yozuvlarini o'chiradi; nechta o'chirilgani qaytadi.
func (r *Registry) PurgeOlderThan(days int) (sessions, audit int64, err error) {
	if days <= 0 {
		return 0, 0, nil // o'chirish o'chirilgan sozlama — hech narsa qilmaymiz
	}
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)

	// Faol sessiyalar TTL bilan yashaydi (5 min) — 90 kunlik "faol" yozuv
	// allaqachon qoldiq, shuning uchun ended_at ga qaramay o'chiramiz.
	// Bo'laklab o'chirish: bitta katta DELETE bitta tranzaksiya bo'lib,
	// MaxOpenConns(1) bilan barcha yozuvchilarni (cam_status, hello,
	// audit) navbatda qoldirardi — 5 s'dan keyin "database is locked"
	// (2026-11-21 server auditi).
	if sessions, err = purgeChunked(r.db, "sessions", "id", "started_at", cutoff); err != nil {
		return 0, 0, wrap(err, "sessions tozalash")
	}
	if audit, err = purgeChunked(r.db, "audit_log", "rowid", "at", cutoff); err != nil {
		return sessions, 0, wrap(err, "audit_log tozalash")
	}
	return sessions, audit, nil
}
