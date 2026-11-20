package store

import (
	"database/sql"
	"path/filepath"
	"runtime"
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// migDir — replikadagi migrations/ papkasi (test faylidan hisoblanadi,
// `go test` qaysi katalogdan ishlatilishidan qat'iyoq topiladi).
var migDir = findMigrations()

func findMigrations() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "migrations"
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
}

func mustExec(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func TestMigrateUp_Down_Idempotent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "vmsd.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	done, err := MigrateUp(db, migDir)
	if err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	if len(done) == 0 {
		t.Fatal("hech migratsiya qo'llanmadi")
	}
	// Takror: hech narsa qayta bajarilmasligi kerak.
	done2, err := MigrateUp(db, migDir)
	if err != nil {
		t.Fatalf("MigrateUp (takror): %v", err)
	}
	if len(done2) != 0 {
		t.Fatalf("takrorda %d migratsiya qayta bajarildi", len(done2))
	}

	applied, available, err := Status(db, migDir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(applied) != len(available) || len(applied) == 0 {
		t.Fatalf("status: applied=%v available=%v", applied, available)
	}

	// Har bir migratsiya o‘z .down.sql‘i bilan qaytariladi: ro‘yxat
	// to‘languncha pasaytamiz (2026-11-20: migratsiyalar soni o‘sdi).
	for range 10 {
		v, err := MigrateDown(db, migDir)
		if err != nil {
			t.Fatalf("MigrateDown: %v", err)
		}
		if v == 0 {
			break
		}
	}
	applied, _, err = Status(db, migDir)
	if err != nil {
		t.Fatalf("Status (down keyin): %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("down keyin journal bo'sh bo'lishi kerak: %v", applied)
	}
}

func TestSchema_TablesAndIndexes(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "vmsd.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if _, err := MigrateUp(db, migDir); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	want := []string{"users", "sites", "clients", "cameras", "favorites", "layouts",
		"sessions", "audit_log", "settings", "join_requests", "preauth_ips", "media_tokens"}
	for _, tbl := range want {
		var n int
		q := "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?"
		if err := db.QueryRow(q, tbl).Scan(&n); err != nil {
			t.Fatalf("query %s: %v", tbl, err)
		}
		if n != 1 {
			t.Fatalf("jadval %s topilmadi", tbl)
		}
	}
	// S0 TASKS: majburiy indekslar
	for _, idx := range []string{"idx_cameras_client_id", "idx_sessions_camera_id"} {
		var n int
		q := "SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?"
		if err := db.QueryRow(q, idx).Scan(&n); err != nil {
			t.Fatalf("query %s: %v", idx, err)
		}
		if n != 1 {
			t.Fatalf("indeks %s topilmadi", idx)
		}
	}
}

func TestCascade_CameraDeleteFreesSlots(t *testing.T) {
	// S1 qabul mezonining bazasi: kamera o'chsa favorites/layouts'dagi
	// bog'lamalar ham tozalanadi (cascade).
	db, err := Open(filepath.Join(t.TempDir(), "vmsd.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if _, err := MigrateUp(db, migDir); err != nil {
		t.Fatalf("MigrateUp: %v", err)
	}
	mustExec(t, db, `INSERT INTO users (username, pass_hash, role) VALUES ('u1','x','viewer')`)
	mustExec(t, db, `INSERT INTO clients (name, agent_id, agent_secret_hash) VALUES ('c1','ag_1','h')`)
	mustExec(t, db, `INSERT INTO cameras (id, client_id, name) VALUES ('11111111-1111-4111-8111-111111111111',1,'kam1')`)
	mustExec(t, db, `INSERT INTO favorites (user_id, camera_id) VALUES (1,'11111111-1111-4111-8111-111111111111')`)

	if _, err := db.Exec(`DELETE FROM cameras WHERE id='11111111-1111-4111-8111-111111111111'`); err != nil {
		t.Fatalf("camera o'chirish: %v", err)
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM favorites").Scan(&n); err != nil || n != 0 {
		t.Fatalf("favorites cascade tozalanmadi: n=%d err=%v", n, err)
	}
}

func TestSplitStmts(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"A; B;", 2},
		{"INSERT INTO t VALUES ('x;y');", 1},                // string ichidagi ;
		{"INSERT INTO t VALUES ('it''s;ok'); SELECT 1;", 2}, // qo'shaloq ' escape
		{"-- izoh; bu hisobga olinmaydi\nSELECT 1;", 1},     // line comment
		{"/* blok; izoh */ SELECT 1; SELECT 2;", 2},         // block comment
		{"SELECT 1 ;; ;", 1},
		{"", 0},
		{"   ;;  ;", 0},
	}
	for i, c := range cases {
		got := splitStmts(c.in)
		if len(got) != c.want {
			t.Errorf("case %d %q: %d stmt, kerak %d (%q)", i, c.in, len(got), c.want, got)
		}
	}
}
