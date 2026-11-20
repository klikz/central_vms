package main

import (
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"central-vms/client/internal/agent"
	"central-vms/client/internal/cams"
	"central-vms/client/internal/keystore"
	"central-vms/client/internal/statestore"
)

// "Clear connection" (1.0-draft.11): eski identity o'chiriladi, kameralar
// va sozlamalar saqlanadi. Server mijozni o'chirganda agent cheksiz 4003
// oladi (§3.6) va shu amaldan keyin qayta so'rov yuboriladi.
func TestResetIdentity(t *testing.T) {
	dir := t.TempDir()
	st, err := statestore.Load(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.AgentID, st.JoinKey, st.JoinStatus = "ag_eskikey000001", "ab12cd34", "approved"
	st.Cameras = []cams.Camera{{ID: "cam-1", Host: "10.0.0.5", Enabled: true}}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "agent.key")
	if _, err := keystore.NewKeyFile(keyPath); err != nil {
		t.Fatal(err)
	}
	ks, err := keystore.Open(keyPath, filepath.Join(dir, "secrets.bin"))
	if err != nil {
		t.Fatal(err)
	}
	ks.Put(keystore.RefAgentSecret, "sir-eski-32-belgilik-uzunlik!!")
	if err := ks.Save(); err != nil {
		t.Fatal(err)
	}

	r := &runtime{log: slog.New(slog.NewTextHandler(discardW{}, nil)), st: st, ks: ks}
	if err := r.resetIdentity(); err != nil {
		t.Fatalf("resetIdentity: %v", err)
	}
	if st.AgentID != "" || st.JoinKey != "" || st.JoinStatus != "" {
		t.Fatalf("identity tozalanmadi: %+v", st)
	}
	if _, ok := ks.Get(keystore.RefAgentSecret); ok {
		t.Fatal("agent_secret keystore'da qoldi")
	}
	if len(st.Cameras) != 1 || st.Cameras[0].ID != "cam-1" {
		t.Fatalf("kameralar yo'qoldi: %+v", st.Cameras)
	}
	// Diskda ham saqlanganligi tekshiriladi.
	st2, err := statestore.Load(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if st2.AgentID != "" || len(st2.Cameras) != 1 {
		t.Fatalf("state fayli: %+v", st2)
	}
	ks2, err := keystore.Open(keyPath, filepath.Join(dir, "secrets.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ks2.Get(keystore.RefAgentSecret); ok {
		t.Fatal("secrets.bin'da sir qoldi")
	}
	// Idempotent: qayta bosilsa xato bermaydi.
	if err := r.resetIdentity(); err != nil {
		t.Fatalf("takroriy resetIdentity: %v", err)
	}
}

// §3.6: qaysi yopinish kodlari "identity eskirgan" degani.
func TestIsAuthReject(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{&agent.ConnError{Code: 4003, Reason: "secret rad etildi"}, true},
		{&agent.ConnError{Code: 4004, Reason: "agent revoked"}, true},
		{&agent.ConnError{Code: 4001, Reason: "versiya mos emas"}, false},
		{&agent.ConnError{Code: 1006, Reason: "tarmoq"}, false},
		{errors.New("ulanib bo'lmadi"), false},
		{nil, false},
	} {
		if got := isAuthReject(tc.err); got != tc.want {
			t.Errorf("isAuthReject(%v) = %v, %v kutilgan", tc.err, got, tc.want)
		}
	}
}
