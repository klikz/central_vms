package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/camsvc"
	"central-vms/client/internal/keystore"
	"central-vms/client/internal/preview"
	"central-vms/client/internal/statestore"
)

func newPS(t *testing.T) (*previewSvc, *camsvc.Service) {
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
	cs := camsvc.New(st, ks)
	store, err := preview.NewStore(filepath.Join(dir, "previews"))
	if err != nil {
		t.Fatal(err)
	}
	sh := filepath.Join(dir, "ff")
	if err := os.WriteFile(sh, []byte("#!/bin/sh\nfor a in \"$@\"; do last=\"$a\"; done\nprintf J > \"$last\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	capt := &preview.Capturer{Store: store, Bin: sh, Log: slog.New(slog.DiscardHandler)}
	return newPreviewSvc(cs, ks, store, capt, slog.New(slog.DiscardHandler)), cs
}

func TestPreviewSvc_CaptureAndAuth(t *testing.T) {
	ps, cs := newPS(t)
	c, err := cs.Create(cams.Camera{Name: "k", Host: "1.2.3.4", Port: 554, MainPath: "/live", Enabled: true}, "parol")
	if err != nil {
		t.Fatal(err)
	}
	if u := ps.authURL(c); u != "rtsp://admin:parol@1.2.3.4:554/live" {
		t.Errorf("authURL: %s", u)
	}
	if err := ps.Capture(context.Background(), c.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := ps.File(c.ID); !ok {
		t.Error("snapshot fayli yo'q")
	}
	if ps.At(c.ID) == nil {
		t.Error("At nil")
	}
	if err := ps.Capture(context.Background(), "yoq-id"); err == nil {
		t.Error("noma'lum kamera xato berishi kerak")
	}
	ps.Remove(c.ID)
	if _, ok := ps.File(c.ID); ok {
		t.Error("Remove islamadi")
	}
}

func TestPreviewSvc_EmitCapturesOnOnline(t *testing.T) {
	ps, cs := newPS(t)
	c, err := cs.Create(cams.Camera{Name: "k", Host: "1.2.3.4", Port: 554, Enabled: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	ps.emit(cams.ProbeResult{ID: c.ID, Status: cams.StatusOnline, Method: "options"})
	if _, ok := ps.File(c.ID); !ok {
		t.Error("online'ta snapshot olinishi kerak")
	}
	if got, _ := cs.Get(c.ID); got.Status != cams.StatusOnline {
		t.Errorf("status saqlanmadi: %v", got.Status)
	}
}
