// run_emptyurl_test.go — server_url bo'sh bo'lganda sessiya shovqin
// chiqarmaydi (backoff/xato log yo'q) va ctx bekor qilinsa darhol
// qaytadi (2026-11-23: config avtomatik yaratilganda bo'sh bo'ladi).
package agent

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"central-vms/client/internal/clock"
)

func TestRun_EmptyServerURLIdles(t *testing.T) {
	s := NewSession(SessionConfig{
		AgentID:   "ag_x",
		Secret:    "s",
		ServerURL: "", // sozlanmagan
		Clock:     clock.Real{},
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	select {
	case err := <-done:
		t.Fatalf("tinim holatida Run darhol qaytmadi: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancel keyin nil kerak edi: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel'dan keyin Run qaytmadi")
	}
}
