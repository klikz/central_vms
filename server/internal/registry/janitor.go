package registry

import (
	"time"
)

// janitor.go — S4.3 retention: `sessions` va `audit_log` yozuvlari
// belgilangan kundan (config.retention_days, standart 90) eski bo'lsa
// o'chiriladi. Vaqt SQLite'da UTC RFC3339Z satr sifatida saqlanadi —
// shu formatdagi chegara bilan solishtirish leksikografik tartibda
// to'g'ri ishlaydi.

// PurgeOlderThan — retention chegarasidan eski sessiyalar va audit
// yozuvlarini o'chiradi; nechta o'chirilgani qaytadi.
func (r *Registry) PurgeOlderThan(days int) (sessions, audit int64, err error) {
	if days <= 0 {
		return 0, 0, nil // o'chirish o'chirilgan sozlama — hech narsa qilmaymiz
	}
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)

	// Faol sessiyalar TTL bilan yashaydi (5 min) — 90 kunlik "faol" yozuv
	// allaqachon qoldiq, shuning uchun ended_at ga qaramay o'chiramiz.
	res, err := r.db.Exec(`DELETE FROM sessions WHERE started_at < ?`, cutoff)
	if err != nil {
		return 0, 0, wrap(err, "sessions tozalash")
	}
	sessions, _ = res.RowsAffected()

	res, err = r.db.Exec(`DELETE FROM audit_log WHERE at < ?`, cutoff)
	if err != nil {
		return sessions, 0, wrap(err, "audit_log tozalash")
	}
	audit, _ = res.RowsAffected()
	return sessions, audit, nil
}
