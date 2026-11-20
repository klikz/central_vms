package publish

import (
	"context"
	"errors"
	"testing"
)

// §12.3: publish fail → darhol to'liq kodek probe (trigger=publish_fail)
// → codec_probe yuboriladi → media_stopped{codec_mismatch}.

type fakeTrigger struct {
	started []string
	allow   bool
	finish  []string
}

func (f *fakeTrigger) TryStart(id, trig string) bool {
	if !f.allow {
		return false
	}
	f.started = append(f.started, id+":"+trig)
	return true
}
func (f *fakeTrigger) Finish(id string) { f.finish = append(f.finish, id) }

func TestCodecFailure_ProbesAndSends(t *testing.T) {
	tr := &fakeTrigger{allow: true}
	var probed, sent []string
	run := func(ctx context.Context, id, url string) (CodecProbePayload, error) {
		probed = append(probed, id+"|"+url)
		return CodecProbePayload{"codec": "h265", "profile": "Main"}, nil
	}
	send := func(id string, p CodecProbePayload) error {
		sent = append(sent, id)
		return nil
	}

	reason := HandleCodecFailure(context.Background(), tr, run, send, "cam1", "rtsp://10.0.0.7/stream")
	if reason != ReasonCodecMismatch {
		t.Errorf("reason = %q, codec_mismatch kutilardi", reason)
	}
	if len(probed) != 1 || probed[0] != "cam1|rtsp://10.0.0.7/stream" {
		t.Errorf("probe chaqiruvi: %v", probed)
	}
	if len(sent) != 1 || sent[0] != "cam1" {
		t.Errorf("codec_probe yuborilmadi: %v", sent)
	}
	if len(tr.started) != 1 || tr.started[0] != "cam1:publish_fail" {
		t.Errorf("trigger: %v", tr.started)
	}
	if len(tr.finish) != 1 {
		t.Errorf("Finish chaqirilmadi: %v", tr.finish)
	}
}

// Band (C2.8 bloki) — probe o'tkazilmaydi, lekin reason baribir
// codec_mismatch (server kodek o'zgarishini bilishi kerak).
func TestCodecFailure_BusySkipsProbe(t *testing.T) {
	tr := &fakeTrigger{allow: false}
	probed := 0
	reason := HandleCodecFailure(context.Background(), tr,
		func(context.Context, string, string) (CodecProbePayload, error) {
			probed++
			return nil, nil
		},
		func(string, CodecProbePayload) error { return nil },
		"cam1", "rtsp://x")
	if probed != 0 {
		t.Error("band paytda probe o'tkazildi (§12.4)")
	}
	if reason != ReasonCodecMismatch {
		t.Errorf("reason = %q", reason)
	}
}

// Probe xatosi — yuborilmaydi, Finish baribir chaqiriladi (lock qolmaydi).
func TestCodecFailure_ProbeError(t *testing.T) {
	tr := &fakeTrigger{allow: true}
	sent := 0
	reason := HandleCodecFailure(context.Background(), tr,
		func(context.Context, string, string) (CodecProbePayload, error) {
			return nil, errors.New("kamera o'chib qoldi")
		},
		func(string, CodecProbePayload) error { sent++; return nil },
		"cam1", "rtsp://x")
	if sent != 0 {
		t.Error("probe xatosiga qaramay codec_probe yuborildi")
	}
	if len(tr.finish) != 1 {
		t.Errorf("Finish chaqirilmadi — scheduler qulflanadi: %v", tr.finish)
	}
	if reason != ReasonCodecMismatch {
		t.Errorf("reason = %q", reason)
	}
}
