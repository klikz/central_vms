package statestore

import (
	"runtime"
	"testing"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/clock"
	"central-vms/client/internal/publish"
)

// C4.8: 100 kamera + 8 publish ≤ 150 MB (§12/§10.4).
func camID(i int) string {
	const hexd = "0123456789abcdef"
	b := []byte("cam-0000")
	b[4] = hexd[(i>>12)&0xf]
	b[5] = hexd[(i>>8)&0xf]
	b[6] = hexd[(i>>4)&0xf]
	b[7] = hexd[i&0xf]
	return string(b)
}

// TestStress_MemoryBudget — 100 kamera (statestore) + 8 publish (registry)
// ≤ 150 MB (§12/§10.4).
func TestStress_MemoryBudget(t *testing.T) {
	var ms0 runtime.MemStats
	runtime.ReadMemStats(&ms0)

	st := &State{ServerURL: "ws://x", AgentID: "ag", Cameras: make([]cams.Camera, 0, 100)}
	for i := 0; i < 100; i++ {
		st.Cameras = append(st.Cameras, cams.Camera{
			ID: camID(i), Name: "kamera-" + camID(i), Host: "10.0.0.1", Port: 554,
			MainPath: "/Streaming/Channels/101", Enabled: true, Status: cams.StatusOnline,
			CodecMain: "h264",
		})
	}
	clk := clock.NewFake(time.Unix(0, 0))
	reg := publish.New(clk, nil, nil, nil)
	for i := 0; i < 8; i++ {
		reg.Start("sess"+camID(i), publish.Key{
			CameraID: camID(i), Profile: "main", Ingest: "whip"}, "play")
	}
	if len(reg.List()) != 8 {
		t.Fatalf("publish = %d, 8 kutilardi", len(reg.List()))
	}
	_ = st

	var ms1 runtime.MemStats
	runtime.ReadMemStats(&ms1)
	used := int64(ms1.HeapAlloc) - int64(ms0.HeapAlloc)
	if used < 0 {
		used = 0
	}
	const budget = 150 * 1024 * 1024
	if used > budget {
		t.Errorf("qo'shimcha HeapAlloc = %d MB, ≤150 MB (§12)", used/(1024*1024))
	}
	t.Logf("100 kamera + 8 publish: qo'shimcha heap = %d KB", used/1024)

	reg.CloseAll(publish.ReasonAgentRestart)
	reg.Shutdown()
}
