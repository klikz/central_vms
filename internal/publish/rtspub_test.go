package publish

import (
	"context"
	"testing"

	"central-vms/client/internal/rtspclient"
)

// C3.8 bog'lamasi: ANNOUNCE→SETUP→RECORD→AU yozish→TEARDOWN (mock
// transport + haqiqiy packetizerlar).

type mockTr struct {
	methods []string
	writes  [][]byte
	ch      int
}

func (m *mockTr) Announce(ctx context.Context, sdp string) error {
	m.methods = append(m.methods, "ANNOUNCE")
	return nil
}
func (m *mockTr) SetupInterleaved(ctx context.Context, ti, r, rc int) (int, int, error) {
	m.methods = append(m.methods, "SETUP")
	m.ch = r
	return r, rc, nil
}
func (m *mockTr) Record(ctx context.Context, rng string) error {
	m.methods = append(m.methods, "RECORD")
	return nil
}
func (m *mockTr) WriteInterleaved(ch int, payload []byte) error {
	m.writes = append(m.writes, payload)
	return nil
}
func (m *mockTr) Teardown(ctx context.Context) error {
	m.methods = append(m.methods, "TEARDOWN")
	return nil
}

func TestRtspPublisher_H265Flow(t *testing.T) {
	tr := &mockTr{}
	pub := NewRtspPublisher(tr, rtspclient.NewH265Packetizer(96, 1), 1200)

	if err := pub.Start(context.Background(), "v=0\r\nm=video 0 RTP/AVP 96\r\n"); err != nil {
		t.Fatal(err)
	}
	want := []string{"ANNOUNCE", "SETUP", "RECORD"}
	if len(tr.methods) != 3 || tr.methods[0] != want[0] || tr.methods[1] != want[1] || tr.methods[2] != want[2] {
		t.Fatalf("zanjir: %v", tr.methods)
	}

	// Katta IDR → bo'linadi; kichik SPS → singleton.
	idr := make([]byte, 2500)
	idr[0], idr[1] = 0x26, 0x01
	sps := []byte{0x40, 0x01, 0x02}
	if err := pub.PublishAU([][]byte{sps, idr}, 90000); err != nil {
		t.Fatal(err)
	}
	if len(tr.writes) < 4 {
		t.Fatalf("RTP paketlari = %d, kamida 4 kutilardi (1 SPS + ≥3 IDR)", len(tr.writes))
	}
	// Oxirgi paketda marker.
	last := tr.writes[len(tr.writes)-1]
	if last[1]&0x80 == 0 {
		t.Error("AU oxirida marker yo'q")
	}

	if err := pub.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tr.methods[len(tr.methods)-1] != "TEARDOWN" {
		t.Errorf("teardown yo'q: %v", tr.methods)
	}
}

func TestRtspPublisher_H264Flow(t *testing.T) {
	tr := &mockTr{}
	pub := NewRtspPublisher(tr, rtspclient.NewH264Packetizer(96, 2), 1200)
	if err := pub.Start(context.Background(), "v=0\r\n"); err != nil {
		t.Fatal(err)
	}
	nal := []byte{0x61, 0x09, 0xF8} // odd NAL header: 0x61 & 0x1f == 1
	if err := pub.PublishAU([][]byte{nal}, 1); err != nil {
		t.Fatal(err)
	}
	if len(tr.writes) != 1 {
		t.Fatalf("paketlar = %d", len(tr.writes))
	}
	if tr.writes[0][12] != 0x61 {
		t.Errorf("singleton payload buzilgan: % x", tr.writes[0][12:])
	}
}
