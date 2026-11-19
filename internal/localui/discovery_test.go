package localui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/config"
	"central-vms/client/internal/discovery"
	"log/slog"
)

// fakeDiscovery — DiscoveryService soxtasi.
type fakeDiscovery struct {
	scanErr   error
	scanned   int
	results   []discovery.MergeResult
	addReport discovery.BulkAddReport
	addErr    error
	gotKeys   []string
	gotInput  discovery.BulkAddInput
}

func (f *fakeDiscovery) StartScan() (int, error)          { return f.scanned, f.scanErr }
func (f *fakeDiscovery) Results() []discovery.MergeResult { return f.results }
func (f *fakeDiscovery) AddSelected(keys []string, in discovery.BulkAddInput) (discovery.BulkAddReport, error) {
	f.gotKeys = keys
	f.gotInput = in
	return f.addReport, f.addErr
}

func newDiscServer(t *testing.T, svc DiscoveryService, pin string) *Server {
	t.Helper()
	cfg := config.Config{UIBind: "127.0.0.1:0", UIPin: pin}
	s, err := NewServer(cfg, slog.New(slog.DiscardHandler), "test", Deps{Discovery: svc})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func doReq(s *Server, method, path, body, pin string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if pin != "" {
		req.Header.Set("X-Agent-Pin", pin)
	}
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	return rec
}

func TestDiscovery_ScanStart(t *testing.T) {
	svc := &fakeDiscovery{scanned: 1024}
	s := newDiscServer(t, svc, "1234")
	rec := doReq(s, "POST", "/api/ui/discovery/scan", "", "1234")
	if rec.Code != http.StatusOK {
		t.Fatalf("kod = %d, 200 kutilardi: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if int(out["scanned"].(float64)) != 1024 {
		t.Errorf("scanned = %v", out["scanned"])
	}
}

func TestDiscovery_ResultsShape(t *testing.T) {
	svc := &fakeDiscovery{results: []discovery.MergeResult{
		{Device: discovery.Device{IP: "10.0.0.5", Port: 554, Model: "Dahua", MAC: "aa:bb"}, AlreadyKnown: true, ExistingID: "id1"},
		{Device: discovery.Device{IP: "10.0.0.6", Port: 554, Model: "Acme"}},
	}}
	s := newDiscServer(t, svc, "1234")
	rec := doReq(s, "GET", "/api/ui/discovery/results", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("kod = %d", rec.Code)
	}
	var out struct {
		Devices []map[string]any `json:"devices"`
		Count   int              `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 2 || len(out.Devices) != 2 {
		t.Fatalf("count = %d", out.Count)
	}
	// Birinchasi already_known + existing_id ko'rinishi kerak.
	if out.Devices[0]["already_known"] != true || out.Devices[0]["existing_id"] != "id1" {
		t.Errorf("known maydonlari: %v", out.Devices[0])
	}
	if out.Devices[0]["ip"] != "10.0.0.5" || out.Devices[0]["model"] != "Dahua" {
		t.Errorf("device maydonlari: %v", out.Devices[0])
	}
}

// Add — PIN talab qiladi, tanlangan kalitlarni o'tkazadi, hisobotni qaytaradi.
func TestDiscovery_Add(t *testing.T) {
	svc := &fakeDiscovery{addReport: discovery.BulkAddReport{
		Added:  []cams.Camera{{ID: "n1", Name: "Dahua-01"}},
		Failed: []discovery.BulkAddFailure{{Err: "host bo'sh"}},
	}}
	s := newDiscServer(t, svc, "1234")
	body := `{"keys":["mac:aa:bb"],"name_pattern":"{model}-{index:02d}","password":"pw","enabled":true}`
	rec := doReq(s, "POST", "/api/ui/discovery/add", body, "1234")
	if rec.Code != http.StatusOK {
		t.Fatalf("kod = %d: %s", rec.Code, rec.Body.String())
	}
	if len(svc.gotKeys) != 1 || svc.gotKeys[0] != "mac:aa:bb" {
		t.Errorf("keys = %v", svc.gotKeys)
	}
	if svc.gotInput.NamePattern != "{model}-{index:02d}" || svc.gotInput.Password != "pw" || !svc.gotInput.Enabled {
		t.Errorf("input = %+v", svc.gotInput)
	}
	var out struct {
		Added  int `json:"added"`
		Failed int `json:"failed"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Added != 1 || out.Failed != 1 {
		t.Errorf("hisobot = %+v", out)
	}
}

func TestDiscovery_AddPINKerak(t *testing.T) {
	svc := &fakeDiscovery{}
	s := newDiscServer(t, svc, "1234")
	rec := doReq(s, "POST", "/api/ui/discovery/add", `{"keys":["ip:1.2.3.4:554"]}`, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("PIN'siz add = %d, 401 kutilardi", rec.Code)
	}
}

// Discovery xizmati bo'lmasa — 501.
func TestDiscovery_NotReady(t *testing.T) {
	s := newDiscServer(t, nil, "1234")
	rec := doReq(s, "GET", "/api/ui/discovery/results", "", "")
	if rec.Code != http.StatusNotImplemented {
		t.Errorf("kod = %d, 501 kutilardi", rec.Code)
	}
}
