package selftest

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// C4.6 (§4.7, §17): self-test paneli — server WS RTT, WHIP UDP/TCP,
// RTSP publish, har kamera "test", faol publish'lar, FFmpeg processlari.
// Barcha tekshiruvlar Deps orqali injektlanadi (fake bilan testlanadi).

// Result — bitta tekshiruv natijasi (UI jadvali: Tekshiruv/Natija/ms).
type Result struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	MS     int64  `json:"ms"`
	Detail string `json:"detail,omitempty"`
}

// Deps — tekshiruvlarning tashqi dunyoga bog'lanishlari (barchasi nil bo'lsa
// tegishli tekshiruv "skip" OK=false, detail="mavjud emas" bilan yoziladi).
type Deps struct {
	WSRTT     func(ctx context.Context) (time.Duration, error)
	WHIP      func(ctx context.Context) (detail string, err error) // UDP+TCP +00
	RTSPPub   func(ctx context.Context) (detail string, err error)
	Cameras   func() []CamSpec
	ProbeCam  func(ctx context.Context, url string) (detail string, err error)
	Publishes func() []string
	FFmpeg    func() []FFProc
}

// CamSpec — kamera test parametrlari (§4.7: ms, kodek, bitrate).
type CamSpec struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// FFProc — FFmpeg jarayoni (pid + qisqa izoh).
type FFProc struct {
	PID  int    `json:"pid"`
	Role string `json:"role"`
}

// Runner — oxirgi natijalarni saqlaydi; Run barcha tekshiruvlarni ketma-ket
// bajaradi (har biriga perCheck taymaut).
type Runner struct {
	deps     Deps
	perCheck time.Duration

	mu      sync.Mutex
	last    []Result
	running bool
}

func New(deps Deps) *Runner {
	return &Runner{deps: deps, perCheck: 5 * time.Second}
}

// Last — oxirgi marta bajarilgan natijalar (bo'sh bo'lsa "bajarilmagan").
func (r *Runner) Last() []Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Result(nil), r.last...)
}

// Run — barcha tekshiruvlar. Parallel chaqirilsa ikkinchisi darhol qaytadi
// (alishlangan holatni qaytaradi — UI spam qilmasin).
func (r *Runner) Run(ctx context.Context) []Result {
	r.mu.Lock()
	if r.running {
		out := append([]Result(nil), r.last...)
		r.mu.Unlock()
		return out
	}
	r.running = true
	r.mu.Unlock()

	var out []Result
	add := func(name string, fn func(context.Context) (string, error)) {
		cctx, cancel := context.WithTimeout(ctx, r.perCheck)
		defer cancel()
		start := time.Now()
		detail, err := fn(cctx)
		res := Result{Name: name, OK: err == nil, MS: time.Since(start).Milliseconds(), Detail: detail}
		if err != nil {
			res.Detail = fmt.Sprint("xato: ", err)
		}
		out = append(out, res)
	}

	if r.deps.WSRTT != nil {
		add("server WS RTT", func(c context.Context) (string, error) {
			d, err := r.deps.WSRTT(c)
			return fmt.Sprintf("%d ms", d.Milliseconds()), err
		})
	}
	if r.deps.WHIP != nil {
		add("WHIP UDP/TCP", func(c context.Context) (string, error) { return r.deps.WHIP(c) })
	}
	if r.deps.RTSPPub != nil {
		add("RTSP publish", func(c context.Context) (string, error) { return r.deps.RTSPPub(c) })
	}
	if r.deps.ProbeCam != nil && r.deps.Cameras != nil {
		cams := r.deps.Cameras()
		sort.Slice(cams, func(i, j int) bool { return cams[i].ID < cams[j].ID })
		for _, c := range cams {
			cam := c
			add("kamera "+cam.ID, func(cc context.Context) (string, error) {
				return r.deps.ProbeCam(cc, cam.URL)
			})
		}
	}
	if r.deps.Publishes != nil {
		start := time.Now()
		pub := r.deps.Publishes()
		out = append(out, Result{Name: "faol publish'lar", OK: true,
			MS: time.Since(start).Milliseconds(), Detail: fmt.Sprintf("%d ta", len(pub))})
	}
	if r.deps.FFmpeg != nil {
		start := time.Now()
		procs := r.deps.FFmpeg()
		out = append(out, Result{Name: "FFmpeg processlari", OK: true,
			MS: time.Since(start).Milliseconds(), Detail: fmt.Sprintf("%d ta", len(procs))})
	}

	r.mu.Lock()
	r.last = out
	r.running = false
	r.mu.Unlock()
	return append([]Result(nil), out...)
}
