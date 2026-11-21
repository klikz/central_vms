package statestore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"central-vms/client/internal/cams"
)

func stPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "state.json")
}

func sample(path string) *State {
	s := New(path)
	s.ServerURL = "ws://192.168.1.10:8081"
	s.AgentID = "ag_9f2c1a7e"
	s.SecretRef = "agent_secret_enc"
	s.Cameras = []cams.Camera{
		{ID: cams.NewID(), Name: "Ong eshik", Host: "192.168.10.20", Port: 554,
			Enabled: true, Status: cams.StatusOnline, CodecMain: "h264"},
		{ID: cams.NewID(), Name: "Hovli", Host: "192.168.10.21", MainPath: "/live",
			Enabled: true, Status: cams.StatusOffline, CodecMain: "h265"},
	}
	return s
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	path := stPath(t)
	s := sample(path)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(".tmp fayli qolib ketdi — atomik yozish buzilgan")
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("state.json rejimi %04o, kutilgan 0600", st.Mode().Perm())
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Schema != Schema || got.AgentID != s.AgentID || got.ServerURL != s.ServerURL {
		t.Fatalf("maydonlar: %+v", got)
	}
	if len(got.Cameras) != 2 || got.Cameras[0].Name != "Ong eshik" {
		t.Fatalf("kameralar: %+v", got.Cameras)
	}
	if got.Cameras[1].CodecMain != "h265" {
		t.Fatalf("kodek: %+v", got.Cameras[1])
	}
}

func TestSave_IkkinchiYozish_BakQoladi(t *testing.T) {
	path := stPath(t)
	s := sample(path)
	s.CamsEpoch = 5
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)

	s.ServerURL = "ws://10.0.0.9:8081"
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf(".bak yaratilmadi: %v", err)
	}
	if string(bak) != string(first) {
		t.Fatal(".bak eski mazmuni saqlamadi")
	}
	now, _ := os.ReadFile(path)
	if !strings.Contains(string(now), "10.0.0.9") {
		t.Fatal("yangi mazmun yozilmadi")
	}
}

func TestLoad_KorruptFayl_BakdanTiklash(t *testing.T) {
	path := stPath(t)
	s := sample(path)
	s.CamsEpoch = 3
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil { // .bak hosil bo'lsin
		t.Fatal(err)
	}
	// state.json'ni buzamiz
	if err := os.WriteFile(path, []byte("{ bu json emas"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err == nil {
		t.Fatal("ogohlantirish xato bilan qaytishi kerak edi")
	}
	if !strings.Contains(err.Error(), ".bak tiklandi") {
		t.Fatalf("xato matni: %v", err)
	}
	if got == nil || got.CamsEpoch != 3 || len(got.Cameras) != 2 {
		t.Fatalf("tiklangan holat: %+v", got)
	}
}

func TestLoad_KorruptVaBakYoq(t *testing.T) {
	path := stPath(t)
	if err := os.WriteFile(path, []byte("buzilgan"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("xato kutilmadi")
	}
}

func TestLoad_FaylYoqBolsa(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("fayl bo'lmasa xato berdi: %v", err)
	}
	if s.Schema != Schema || len(s.Cameras) != 0 {
		t.Fatalf("bo'sh holat: %+v", s)
	}
}

func TestLoad_SxemaYangi(t *testing.T) {
	path := stPath(t)
	raw, _ := json.Marshal(map[string]any{"schema": 99, "cameras": []any{}})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); !errors.Is(err, ErrSchema) {
		t.Fatalf("xato: %v, kutilgan ErrSchema", err)
	}
}

func TestLoad_JarimoqFayl(t *testing.T) {
	// .tmp qolib ketgan (crash) — asosiy faylga ta'sir qilmasligi kerak
	path := stPath(t)
	s := sample(path)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".tmp", []byte("jarimoq"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cameras) != 2 {
		t.Fatalf("kameralar: %d", len(got.Cameras))
	}
}

func TestEpoch(t *testing.T) {
	s := New(stPath(t))
	if s.CamsEpoch != 0 {
		t.Fatalf("boshlang'ich epoch=%d", s.CamsEpoch)
	}
	if got := s.BumpEpoch(); got != 1 {
		t.Fatalf("BumpEpoch=%d", got)
	}
	if got := s.SetCameras([]cams.Camera{{ID: cams.NewID(), Host: "1.2.3.4"}}); got != 2 {
		t.Fatalf("SetCameras epoch=%d", got)
	}
	if len(s.Cameras) != 1 {
		t.Fatalf("kameralar: %d", len(s.Cameras))
	}
}

func TestSave_PathBoLsh(t *testing.T) {
	s := &State{}
	if err := s.Save(); err == nil {
		t.Fatal("bo'sh pathda xato kutilmadi")
	}
}

func TestSave_PapkaYaratilmaydi(t *testing.T) {
	s := New("/proc/self/mem/state.json")
	s.Cameras = []cams.Camera{{ID: "x", Host: "1.1.1.1"}}
	if err := s.Save(); err == nil {
		t.Fatal("papka xatosi kutilmadi")
	}
}

func TestCameras_Valid(t *testing.T) {
	c := cams.Camera{Host: "192.168.10.20"}
	if err := c.Valid(); err == nil {
		t.Fatal("id bo'lmasa xato berishi kerak")
	}
	c.ID = cams.NewID()
	if err := c.Valid(); err != nil {
		t.Fatalf("host bilan xato berdi: %v", err)
	}
	if c.Port != 554 || c.MainPath == "" {
		t.Fatalf("defaultlar qo'yilmadi: port=%d path=%q", c.Port, c.MainPath)
	}
	noHost := cams.Camera{ID: cams.NewID()}
	if err := noHost.Valid(); err == nil {
		t.Fatal("host bo'lmasa xato berishi kerak")
	}
	if ref := cams.SecretRefFor(c.ID, "main"); !strings.HasPrefix(ref, "cam:") || !strings.HasSuffix(ref, ":main") {
		t.Fatalf("secret_ref: %q", ref)
	}
}

// Eski state.json (status bo'sh, nol vaqt) → normalize qilinadi (§3.2).
func TestLoadNormalize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	raw := `{"schema":1,"cameras":[{"id":"x1","name":"A","host":"10.0.0.1","port":554,` +
		`"status":"","last_check_at":"0001-01-01T00:00:00Z","codec_probe_at":"0001-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c := st.Cameras[0]
	if c.Status != cams.StatusUnknown {
		t.Fatalf("status=%q", c.Status)
	}
	if c.LastCheckAt != nil || c.CodecProbeAt != nil {
		t.Fatalf("nol vaqt nil bo'lishi kerak: %v %v", c.LastCheckAt, c.CodecProbeAt)
	}
	// Haqiqiy vaqt saqlanadi
	now := time.Now().UTC().Truncate(time.Second)
	st.Cameras[0].LastCheckAt = &now
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	st2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Cameras[0].LastCheckAt == nil || !st2.Cameras[0].LastCheckAt.Equal(now) {
		t.Fatalf("vaqt tiklanmadi: %v", st2.Cameras[0].LastCheckAt)
	}
}

// Parallel Mutate/Snapshot/Save/BumpEpoch — qoida: maydonlarga faqat
// qulf ostida tegiladi (2026-11-21 auditida race detektori
// Syncer.Push ↔ ClearPendingDeletes juftligini ko'rsatgan edi).
func TestConcurrentMutateSnapshotSave(t *testing.T) {
	st := New(stPath(t))
	st.Cameras = []cams.Camera{{ID: "a", Name: "A", Host: "h"}, {ID: "b", Name: "B", Host: "h"}}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for k := 0; k < 40; k++ {
				st.Mutate(func() {
					st.AddPendingDelete([]string{"id-" + strconv.Itoa(i)})
					if len(st.Cameras) > 0 {
						st.Cameras[0].Status = cams.StatusOnline
					}
				})
				_ = st.SnapshotCameras()
				_ = st.SnapshotPending()
				st.MutateEpoch(func() { st.ClearPendingDeletes() })
				_ = st.Save()
			}
		}(i)
	}
	wg.Wait()
	if st.CamsEpoch < 6*40 {
		t.Fatalf("epoch hisobi: %d, kamida %d kutilardi", st.CamsEpoch, 6*40)
	}
}
