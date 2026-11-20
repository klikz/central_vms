package main

import (
	"log/slog"
	"path/filepath"
	"testing"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/statestore"
)

// §12.3: codec_fix → state kodek maydonlari yangilanadi va saqlanadi.
func TestApplyCodecFix(t *testing.T) {
	dir := t.TempDir()
	st, err := statestore.Load(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.Cameras = []cams.Camera{
		{ID: "cam-1", CodecMain: "h265", Enabled: true},
		{ID: "cam-2", CodecMain: "h265", Enabled: true},
	}
	r := &runtime{log: slog.New(slog.NewTextHandler(discardW{}, nil)), st: st}

	r.applyCodecFix(proto.CodecFix{CameraID: "cam-1",
		Target: proto.CodecFixTarget{Codec: "h264", NoBFrames: true}})

	if st.Cameras[0].CodecMain != "h264" || !st.Cameras[0].BframeMain {
		t.Errorf("cam-1: %+v", st.Cameras[0])
	}
	if st.Cameras[1].CodecMain != "h265" {
		t.Errorf("cam-2 o'zgarmasligi kerak edi: %+v", st.Cameras[1])
	}
	// Saqlanganligini tekshirish: qayta o'qib ko'ramiz.
	st2, err := statestore.Load(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(st2.Cameras) != 2 || st2.Cameras[0].CodecMain != "h264" {
		t.Errorf("state faylida codec_fix ko'rinmadi: %+v", st2.Cameras)
	}
}

type discardW struct{}

func (discardW) Write(p []byte) (int, error) { return len(p), nil }
