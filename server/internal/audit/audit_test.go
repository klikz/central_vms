package audit

import (
	"database/sql"
	"path/filepath"
	"runtime"
	"testing"

	"go.uber.org/goleak"

	"central-vms/server/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := store.MigrateUp(db, migrationsDir()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
}

func TestAddAndList(t *testing.T) {
	db := testDB(t)
	// FK ishlayapti: user_id mavjud user'ni ko'rsatishi kerak (yoki NULL).
	if _, err := db.Exec(`INSERT INTO users (username, pass_hash, role) VALUES ('admin1','x','admin')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// user_id NULL holati (0 → NULL).
	lg := New(db)
	if err := lg.Add(0, EnrollUse, "ag_test"); err != nil {
		t.Fatalf("add anonim: %v", err)
	}
	if err := lg.Add(1, Login, "admin1"); err != nil {
		t.Fatalf("add: %v", err)
	}
	items, err := lg.List(10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len=%d", len(items))
	}
	if items[0].Action != Login || items[0].UserID != 1 {
		t.Fatalf("oxirgi yozuv noto'g'ri: %+v", items[0])
	}
	if items[1].UserID != 0 { // NULL → 0
		t.Fatalf("anonim yozuv: %+v", items[1])
	}
}

func TestAdd_ErrorPropagates(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := New(db).Add(1, Login, "x"); err == nil {
		t.Fatal("jadval yo'q — xato kutilardi")
	}
}
