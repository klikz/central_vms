package selftest

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRun_AllChecks(t *testing.T) {
	r := New(Deps{
		WSRTT:   func(context.Context) (time.Duration, error) { return 12 * time.Millisecond, nil },
		WHIP:    func(context.Context) (string, error) { return "udp=ok tcp=ok", nil },
		RTSPPub: func(context.Context) (string, error) { return "", errors.New("8554 yopiq") },
		Cameras: func() []CamSpec {
			return []CamSpec{{ID: "cam-b", URL: "rtsp://b"}, {ID: "cam-a", URL: "rtsp://a"}}
		},
		ProbeCam: func(_ context.Context, url string) (string, error) {
			return "h264 2000kbps " + url, nil
		},
		Publishes: func() []string { return []string{"p1", "p2"} },
		FFmpeg:    func() []FFProc { return []FFProc{{PID: 1, Role: "transcode"}} },
	})
	res := r.Run(context.Background())
	names := map[string]Result{}
	for _, x := range res {
		names[x.Name] = x
	}
	if len(res) != 7 {
		t.Fatalf("natijalar = %d, 7 kutilardi: %+v", len(res), res)
	}
	if !names["server WS RTT"].OK || names["server WS RTT"].Detail != "12 ms" {
		t.Errorf("WS RTT: %+v", names["server WS RTT"])
	}
	if names["RTSP publish"].OK || names["RTSP publish"].Detail != "xato: 8554 yopiq" {
		t.Errorf("RTSP xato ko'rinishi: %+v", names["RTSP publish"])
	}
	if names["faol publish'lar"].Detail != "2 ta" || names["FFmpeg processlari"].Detail != "1 ta" {
		t.Errorf("ro'yxatlar: %+v", res[5:])
	}
	// Kameralar ID bo'yicha saralanadi.
	if names["kamera cam-a"].Detail == "" || names["kamera cam-b"].Detail == "" {
		t.Error("kamera testlari yo'q")
	}
	// Last() saqlanadi.
	if len(r.Last()) != 7 {
		t.Error("Last() saqlanmadi")
	}
}

func TestRun_Taymaut(t *testing.T) {
	r := New(Deps{WSRTT: func(ctx context.Context) (time.Duration, error) {
		<-ctx.Done() // hech qachon javob bermaydi
		return 0, ctx.Err()
	}})
	r.perCheck = 50 * time.Millisecond
	res := r.Run(context.Background())
	if len(res) != 1 || res[0].OK {
		t.Fatalf("taymaut natijasi: %+v", res)
	}
}

func TestRun_EmptyDeps(t *testing.T) {
	r := New(Deps{})
	if res := r.Run(context.Background()); len(res) != 0 {
		t.Errorf("bo'sh deps: %+v", res)
	}
}
