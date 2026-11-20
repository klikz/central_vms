package localui

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/camsvc"
	"central-vms/client/internal/config"
	"central-vms/client/internal/keystore"
	"central-vms/client/internal/statestore"
)

type fakeSnap struct {
	captured []string
	dir      string
}

func (f *fakeSnap) File(id string) (string, bool) {
	p := filepath.Join(f.dir, id+".jpg")
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	return p, true
}
func (f *fakeSnap) At(id string) *time.Time {
	p := filepath.Join(f.dir, id+".jpg")
	st, err := os.Stat(p)
	if err != nil {
		return nil
	}
	mt := st.ModTime()
	return &mt
}
func (f *fakeSnap) Capture(ctx context.Context, id string) error {
	f.captured = append(f.captured, id)
	return os.WriteFile(filepath.Join(f.dir, id+".jpg"), []byte("JPEG"), 0o600)
}
func (f *fakeSnap) Remove(id string) { os.Remove(filepath.Join(f.dir, id+".jpg")) }

func newSnapServer(t *testing.T, pin string) (*httptest.Server, *camsvc.Service) {
	t.Helper()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "agent.key")
	if _, err := keystore.NewKeyFile(keyPath); err != nil {
		t.Fatal(err)
	}
	ks, err := keystore.Open(keyPath, filepath.Join(dir, "secrets.bin"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := statestore.Load(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc := camsvc.New(st, ks)
	cfg := config.Default()
	s, err := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), "test", Deps{
		Cams: svc, Snapshots: &fakeSnap{dir: t.TempDir()},
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.mux)
	t.Cleanup(ts.Close)
	return ts, svc
}

func TestSnapshotEndpoints(t *testing.T) {
	ts, camsSvc := newSnapServer(t, "1234")
	created, err := camsSvc.Create(cams.Camera{Name: "k", Host: "1.2.3.4", Port: 554, Enabled: true}, "pw")
	if err != nil {
		t.Fatal(err)
	}

	// Yo'q snapshot → 404
	res, _ := doJSON(t, ts, http.MethodGet, "/api/ui/cams/"+created.ID+"/snapshot", "")
	if res.StatusCode != 404 {
		t.Fatalf("404 kutilardi: %d", res.StatusCode)
	}
	// POST (PIN bilan) → oladi
	res, body := doJSON(t, ts, http.MethodPost, "/api/ui/cams/"+created.ID+"/snapshot", "")
	if res.StatusCode != 200 {
		t.Fatalf("POST snapshot: %d %s", res.StatusCode, body)
	}
	// GET → JPEG fayl
	res, _ = doJSON(t, ts, http.MethodGet, "/api/ui/cams/"+created.ID+"/snapshot", "")
	if res.StatusCode != 200 {
		t.Fatalf("GET snapshot: %d", res.StatusCode)
	}
	// Ro'yxatda snapshot_at bor
	res, body = doJSON(t, ts, http.MethodGet, "/api/ui/cams", "")
	var lr struct {
		Cams []struct {
			ID         string `json:"id"`
			SnapshotAt string `json:"snapshot_at"`
		} `json:"cams"`
	}
	if err := json.Unmarshal([]byte(body), &lr); err != nil {
		t.Fatal(err)
	}
	if len(lr.Cams) != 1 || lr.Cams[0].SnapshotAt == "" {
		t.Fatalf("snapshot_at kutilardi: %s", body)
	}
	// O'chirish snapshotni ham olib tashlaydi
	res, body = doJSON(t, ts, http.MethodDelete, "/api/ui/cams", `{"ids":["`+created.ID+`"]}`)
	if res.StatusCode != 200 {
		t.Fatalf("delete: %d %s", res.StatusCode, body)
	}
	res, _ = doJSON(t, ts, http.MethodGet, "/api/ui/cams/"+created.ID+"/snapshot", "")
	if res.StatusCode != 404 {
		t.Errorf("o'chirilgandan keyin 404 kutilardi: %d", res.StatusCode)
	}
}
