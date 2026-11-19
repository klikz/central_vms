package publish

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"testing"

	"central-vms/client/internal/rtspclient"
)

// §13: reason matritsasi — xato turi + tomon (kamera/server) → reason + UI xabari.

func TestReasons_Table(t *testing.T) {
	tests := []struct {
		name string
		err  error
		side Side
		want string
	}{
		{"kamera_401", &rtspclient.StatusError{Method: "DESCRIBE", Code: 401}, SideCamera, ReasonCameraRejected},
		{"kamera_404", &rtspclient.StatusError{Method: "DESCRIBE", Code: 404}, SideCamera, ReasonCameraRejected},
		{"kamera_refused", fmt.Errorf("dial: %w", syscall.ECONNREFUSED), SideCamera, ReasonCameraOffline},
		{"kamera_timeout", context.DeadlineExceeded, SideCamera, ReasonCameraOffline},
		{"server_401", &rtspclient.StatusError{Method: "ANNOUNCE", Code: 401}, SideServer, ReasonTokenRejected},
		{"server_403", &rtspclient.StatusError{Method: "POST", Code: 403}, SideServer, ReasonTokenRejected},
		{"server_500", &rtspclient.StatusError{Method: "POST", Code: 500}, SideServer, ReasonNetwork},
		{"server_refused", fmt.Errorf("dial whip: %w", syscall.ECONNREFUSED), SideServer, ReasonNetwork},
		{"limit", ErrLimitExceeded, SideServer, ReasonLimitExceeded},
		{"codec", ErrCodecMismatch, SideServer, ReasonCodecMismatch},
		{"bframe", ErrBFrameUnsupported, SideCamera, ReasonBFrameUnsupported},
		{"transcode", ErrTranscodeFailed, SideServer, ReasonTranscodeFailed},
		{"publish_failed_net", &PublishError{Cause: syscall.ECONNREFUSED}, SideServer, ReasonNetwork},
		{"publish_failed_yogdi", &PublishError{Cause: errors.New("shg")}, SideServer, ReasonPublishFailed},
		{"publish_failed_token", &PublishError{Cause: &rtspclient.StatusError{Method: "POST", Code: 401}}, SideServer, ReasonTokenRejected},
		{"nil", nil, SideServer, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReasonFor(tc.err, tc.side); got != tc.want {
				t.Errorf("ReasonFor(%v, %v) = %q, %q kutilardi", tc.err, tc.side, got, tc.want)
			}
		})
	}
}

// §13: barcha reason kodlari UI xabari bilan (yashirinlar bo'sh).
func TestReasons_AllCodesHaveText(t *testing.T) {
	hidden := map[string]bool{
		ReasonTTLExpired: true, ReasonAgentRestart: true, ReasonShutdown: true,
		ReasonNoViewers: true, ReasonServerStop: true, ReasonUserClose: true,
		ReasonPageHidden: true, ReasonIdleTTL: true,
	}
	visible := []string{
		ReasonCameraOffline, ReasonCameraRejected, ReasonTokenRejected,
		ReasonPublishFailed, ReasonCodecMismatch, ReasonBFrameUnsupported,
		ReasonTranscodeFailed, ReasonLimitExceeded, ReasonNetwork,
	}
	for _, r := range visible {
		if txt := ReasonText(r); txt == "" {
			t.Errorf("%s: UI xabari yo'q", r)
		}
	}
	for r := range hidden {
		if txt := ReasonText(r); txt != "" {
			t.Errorf("%s: yashirin bo'lishi kerak, topildi %q", r, txt)
		}
	}
	// Noma'lum reason → bo'sh emas (fallback), panic yo'q.
	if ReasonText("nima_dir") == "" {
		t.Error("noma'lum reason fallback yo'q")
	}
}

// §13: watchdog/normal yopilishlar to'g'ridan-to'g'ri kodlar bilan.
func TestReasons_DirectCodes(t *testing.T) {
	for _, r := range []string{ReasonTTLExpired, ReasonNoViewers, ReasonServerStop,
		ReasonUserClose, ReasonPageHidden, ReasonIdleTTL, ReasonAgentRestart, ReasonShutdown} {
		if !ValidReason(r) {
			t.Errorf("%s valid emas deb belgilandi", r)
		}
	}
	if ValidReason("salom") {
		t.Error("noma'lum kod valid emas bo'lishi kerak")
	}
}
