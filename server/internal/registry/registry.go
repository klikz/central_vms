// Package registry — sites, clients, cameras, favorites, layouts va users
// CRUD (root §5/§6; S1). Barcha yozuvlar bitta SQLite (MaxOpenConns=1),
// shuning uchun qo'shimcha lock yo'q — yagona yozuvchi DB o'zi (TASKS §0.3).
package registry

import (
	"database/sql"
	"errors"
	"fmt"

	"modernc.org/sqlite"
)

// Kontrakt xatolari — handler'lar HTTP statusga shu yerda map qiladi.
var (
	ErrNotFound = errors.New("registry: topilmadi")
	ErrExists   = errors.New("registry: nom/identifikator band")
	// ErrNameTaken — mijoz nomi band (clients.name UNIQUE). Kod hali
	// yaroqli: operator boshqa nom tanlashi kerak.
	ErrNameTaken = errors.New("registry: bunday nom band")
	ErrBadInput  = errors.New("registry: ma'lumot noto'g'ri")
)

// isConstraint — SQLite CONSTRAINT xatolarini aniqlash (UNIQUE/FK).
func isConstraint(err error) bool {
	var e *sqlite.Error
	if errors.As(err, &e) {
		code := e.Code() & 0xff
		return code == 19 // SQLITE_CONSTRAINT
	}
	return false
}

// wrap — xatoni kontrakt kodiga aylantiradi.
func wrap(err error, what string) error {
	switch {
	case err == nil:
		return nil
	case isConstraint(err):
		// Sabab ham qo‘shiladi: "band" xatolari qaysi ustun bo‘yicha
		// kelganini bilmaslik tuzatishni qiyinlashtiradi (2026-11-20).
		return fmt.Errorf("%s: %v: %w", what, err, ErrExists)
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	default:
		return fmt.Errorf("%s: %w", what, err)
	}
}

// Registry — DB ustida qalinlash (bitta db, lock yo'q).
type Registry struct {
	db *sql.DB
}

func New(db *sql.DB) *Registry { return &Registry{db: db} }

func (r *Registry) DB() *sql.DB { return r.db }
