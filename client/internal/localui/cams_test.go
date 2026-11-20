package localui

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"central-vms/client/internal/camsvc"
	"central-vms/client/internal/config"
	"central-vms/client/internal/keystore"
	"central-vms/client/internal/statestore"
)

// newCamServer — CRUD sinovi uchun haqiqiy camsvc bilan UI serveri
func newCamServer(t *testing.T) (*httptest.Server, *camsvc.Service) {
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
	s, err := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), "test", Deps{Cams: svc})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.mux)
	t.Cleanup(ts.Close)
	return ts, svc
}

func doJSON(t *testing.T, ts *httptest.Server, method, path, body string) (*http.Response, string) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func TestCamCRUD(t *testing.T) {
	ts, svc := newCamServer(t)

	// Bo'sh ro'yxat
	res, body := doJSON(t, ts, http.MethodGet, "/api/ui/cams", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", res.StatusCode, body)
	}
	var lr struct {
		Cams  []map[string]any `json:"cams"`
		Count int              `json:"count"`
	}
	if err := json.Unmarshal([]byte(body), &lr); err != nil {
		t.Fatal(err)
	}
	if lr.Count != 0 {
		t.Fatalf("bo'sh kutilmadi: %s", body)
	}

	// Yaratish — PIN'siz (1.0-draft.10: PIN olib tashlandi)
	res, body = doJSON(t, ts, http.MethodPost, "/api/ui/cams", `{"name":"A","host":"10.0.0.5","secret":"p1"}`)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	var created struct {
		ID        string `json:"id"`
		SecretRef string `json:"secret_ref"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.SecretRef == "" {
		t.Fatalf("yaratilgan: %s", body)
	}

	// Ro'yxatda ko'rinadi
	res, body = doJSON(t, ts, http.MethodGet, "/api/ui/cams", "")
	if !strings.Contains(body, `"10.0.0.5"`) {
		t.Fatalf("ro'yxat: %s", body)
	}
	// Javobda parol yo'q (faqat secret_ref)
	if strings.Contains(body, "p1") {
		t.Fatalf("javobda parol sizib chiqdi: %s", body)
	}

	// Yangilash
	res, body = doJSON(t, ts, http.MethodPut, "/api/ui/cams/"+created.ID, `{"name":"B","host":"10.0.0.6"}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update: %d %s", res.StatusCode, body)
	}
	if !strings.Contains(body, `"10.0.0.6"`) {
		t.Fatalf("update natija: %s", body)
	}
	// id o'zgarmadi
	if !strings.Contains(body, created.ID) {
		t.Fatalf("id o'zgardi: %s", body)
	}

	// Noma'lum id → 404
	res, _ = doJSON(t, ts, http.MethodPut, "/api/ui/cams/yoq-id", `{"host":"1.1.1.1"}`)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("noma'lum id: %d", res.StatusCode)
	}

	// Yomon JSON → 400
	res, _ = doJSON(t, ts, http.MethodPost, "/api/ui/cams", `{"noma_lum":1}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("noma'lum maydon: %d", res.StatusCode)
	}

	// CSV import
	res, body = doJSON(t, ts, http.MethodPost, "/api/ui/cams/import",
		"name,host\nk1,10.0.0.7\nk2,10.0.0.8\nyomon,,\n")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("import: %d %s", res.StatusCode, body)
	}
	var rep camsvc.CSVReport
	if err := json.Unmarshal([]byte(body), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Created != 2 || len(rep.Errors) != 1 {
		t.Fatalf("import hisoboti: %+v", rep)
	}

	// CSV eksport — parollar mask'langan
	res, body = doJSON(t, ts, http.MethodGet, "/api/ui/cams/export", "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("export: %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("content-type: %q", ct)
	}
	if strings.Contains(body, "p1") || !strings.Contains(body, "***") {
		t.Fatalf("eksport parolni ochib yubordi: %s", body)
	}
	// with_secrets=1 — PIN'siz ham (operator talabi, 1.0-draft.10)
	res, body = doJSON(t, ts, http.MethodGet, "/api/ui/cams/export?with_secrets=1", "")
	if !strings.Contains(body, "p1") {
		t.Fatalf("with_secrets eksport: %s", body)
	}

	// O'chirish
	res, body = doJSON(t, ts, http.MethodDelete, "/api/ui/cams", `{"ids":["`+created.ID+`"]}`)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"deleted":1`) {
		t.Fatalf("delete: %d %s", res.StatusCode, body)
	}
	if len(svc.List()) != 2 {
		t.Fatalf("qolgan kameralar: %d", len(svc.List()))
	}
	// cams_epoch o'sgan bo'lishi kerak
	if svc.St.CamsEpoch < 5 {
		t.Fatalf("epoch=%d, CRUD amallari yetarlicha o'stirmadi", svc.St.CamsEpoch)
	}
}

func TestCamsNotReady501(t *testing.T) {
	ts := newTestServer(t, "") // Deps{Cams: nil}
	res, body := doJSON(t, ts, http.MethodGet, "/api/ui/cams", "")
	if res.StatusCode != http.StatusNotImplemented {
		t.Fatalf("kod=%d, kutilgan 501 (%s)", res.StatusCode, body)
	}
	if !strings.Contains(body, "cams_not_ready") {
		t.Fatalf("javob: %s", body)
	}
}
