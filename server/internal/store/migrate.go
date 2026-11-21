package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Migratsiya fayllari: NNNN_name.sql (up) va NNNN_name.down.sql (down).
var upRe = regexp.MustCompile(`^(\d{4})_[A-Za-z0-9_-]+\.sql$`)

const maxStmtSplit = 200 // himoyaviy limit (TASKS §0.3 defensive limitlar)

// MigrateUp — papkadagi barcha yangi migratsiyalarni tartib bilan bajaradi.
func MigrateUp(db *sql.DB, dir string) ([]int, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return nil, err
	}
	var ups []struct {
		ver  int
		name string
	}
	for _, f := range files {
		b := filepath.Base(f)
		if strings.HasSuffix(b, ".down.sql") {
			continue
		}
		m := upRe.FindStringSubmatch(b)
		if m == nil {
			continue
		}
		v, _ := strconv.Atoi(m[1])
		ups = append(ups, struct {
			ver  int
			name string
		}{v, f})
	}
	sort.Slice(ups, func(i, j int) bool { return ups[i].ver < ups[j].ver })

	applied, err := appliedVersions(db)
	if err != nil {
		return nil, err
	}
	var done []int
	for _, u := range ups {
		if applied[u.ver] {
			continue
		}
		b, err := os.ReadFile(u.name)
		if err != nil {
			return done, fmt.Errorf("migrate %04d: o'qish: %w", u.ver, err)
		}
		// Journal yozuvi skript BILAN BIR tranzaksiyada: avval ular
		// alohida edi — orasida crash bo'lsa migratsiya qayta bajarilib
		// "table already exists"/"duplicate column" bilan server umuman
		// boshlanmas edi (2026-11-21 auditi).
		ver := u.ver
		if err := execTxWith(db, string(b), func(tx *sql.Tx) error {
			_, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", ver)
			return err
		}); err != nil {
			return done, fmt.Errorf("migrate %04d (%s): %w", u.ver, filepath.Base(u.name), err)
		}
		done = append(done, u.ver)
	}
	return done, nil
}

// MigrateDown — oxirgi qo'llanilgan migratsiyani bitta qadam orqaga qaytaradi.
func MigrateDown(db *sql.DB, dir string) (int, error) {
	vers, err := appliedVersions(db)
	if err != nil {
		return 0, err
	}
	last := 0
	for v := range vers {
		if v > last {
			last = v
		}
	}
	if last == 0 {
		return 0, nil
	}
	pat := filepath.Join(dir, fmt.Sprintf("%04d_*.down.sql", last))
	files, err := filepath.Glob(pat)
	if err != nil || len(files) == 0 {
		return last, fmt.Errorf("migrate down %04d: %s topilmadi", last, pat)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		return last, fmt.Errorf("migrate down %04d: o'qish: %w", last, err)
	}
	ver := last
	if err := execTxWith(db, string(b), func(tx *sql.Tx) error {
		_, err := tx.Exec("DELETE FROM schema_migrations WHERE version = ?", ver)
		return err
	}); err != nil {
		return last, fmt.Errorf("migrate down %04d: %w", last, err)
	}
	return last, nil
}

// Status — qo'llanilgan versiyalar (tartibli) va papkadagi jami versiyalar.
func Status(db *sql.DB, dir string) (applied, available []int, err error) {
	appliedSet, err := appliedVersions(db)
	if err != nil {
		return nil, nil, err
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return nil, nil, err
	}
	availSet := map[int]bool{}
	for _, f := range files {
		b := filepath.Base(f)
		if strings.HasSuffix(b, ".down.sql") {
			continue
		}
		if m := upRe.FindStringSubmatch(b); m != nil {
			v, _ := strconv.Atoi(m[1])
			availSet[v] = true
		}
	}
	for v := range appliedSet {
		applied = append(applied, v)
	}
	for v := range availSet {
		available = append(available, v)
	}
	sort.Ints(applied)
	sort.Ints(available)
	return applied, available, nil
}

func appliedVersions(db *sql.DB) (map[int]bool, error) {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version   INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
	)`); err != nil {
		return nil, fmt.Errorf("schema_migrations: %w", err)
	}
	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

// execTx — bitta migratsiya faylini bitta tranzaksiyada bajaradi
// (SQLite DDL tranzaksiyada qaytarilishi mumkin — atomik migratsiya).
func execTx(db *sql.DB, script string) error { return execTxWith(db, script, nil) }

// execTxWith — skript + (ixtiyoriy) qo'shimcha yozuv BITTA tranzaksiyada.
func execTxWith(db *sql.DB, script string, inTx func(*sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmts, err := splitStmts(script)
	if err != nil {
		return err
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("%w (stmt: %.80q)", err, stmt)
		}
	}
	if inTx != nil {
		if err := inTx(tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// splitStmts — `;` bo'yicha ajratish; string/comment/trigger ichidagi `;`
// hisobga olinmaydi. Oddiy, "aqlli" parser yo'q (TASKS §0.3).
func splitStmts(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inStr, inLineC, inBlockC := false, false, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		var next byte
		if i+1 < len(s) {
			next = s[i+1]
		}
		switch {
		case inLineC:
			cur.WriteByte(ch)
			if ch == '\n' {
				inLineC = false
			}
			continue
		case inBlockC:
			cur.WriteByte(ch)
			if ch == '*' && next == '/' {
				cur.WriteByte(next)
				i++
				inBlockC = false
			}
			continue
		case inStr:
			cur.WriteByte(ch)
			if ch == '\'' {
				if next == '\'' { // SQL'da qo'shaloq ' — escape
					cur.WriteByte(next)
					i++
					continue
				}
				inStr = false
			}
			continue
		case ch == '\'':
			inStr = true
			cur.WriteByte(ch)
		case ch == '-' && next == '-':
			inLineC = true
			cur.WriteByte(ch)
		case ch == '/' && next == '*':
			inBlockC = true
			cur.WriteByte(ch)
		case ch == ';':
			if t := strings.TrimSpace(cur.String()); t != "" {
				out = append(out, t)
			}
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
		if len(out) > maxStmtSplit {
			// ESKISI: `return out` — qolgan operatorlar JIM tashlab
			// yuborilardi, migratsiya baribir "muvaffaqiyatli" hisoblanar
			// edi (qisman sxema!). Endi xato qaytaramiz.
			return nil, fmt.Errorf("migratsiya fayli %d+ operatorli — fayl buzilgan yoki juda katta", maxStmtSplit)
		}
	}
	if t := strings.TrimSpace(cur.String()); t != "" {
		out = append(out, t)
	}
	return out, nil
}
