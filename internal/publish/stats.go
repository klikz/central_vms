package publish

import (
	"context"
	"sort"
	"time"

	"central-vms/client/internal/clock"
	"central-vms/client/internal/proto"
)

// §4.6: media_stats har 10 s (faqat faol publish'lar bo'lsa) va
// media_ready maydonlari.

// statsInterval — §4.6: media_stats har 10 s.
const statsInterval = 10 * time.Second

// Stat — bitta publish'ning jonli ko'rsatkichlari (pion/ffmpeg manbalari
// keyinchalik shu strukturni to'ldiradi).
type Stat struct {
	BytesSent     int64
	FPS           float64
	BitrateKbps   int
	RTTMs         int64
	PacketLossPct float64
	Transport     string // udp | tcp
}

// StatsCollector — registry'dagi faol publish'lar uchun 10 s'da bir
// media_stats yig'adi. Manba (source) har publish uchun Stat beradi.
type StatsCollector struct {
	reg      *Registry
	clk      clock.Clock
	source   func(Key) Stat
	emit     func(proto.MediaStats)
	interval time.Duration
	ready    chan struct{} // Run boshlandi (ticker o'rnatildi)
}

// NewStats — emit nil bo'lsa Snapshot orqali o'qiladi (test).
func NewStats(reg *Registry, clk clock.Clock, source func(Key) Stat, emit func(proto.MediaStats)) *StatsCollector {
	if clk == nil {
		clk = clock.Real{}
	}
	if source == nil {
		source = func(Key) Stat { return Stat{} }
	}
	if emit == nil {
		emit = func(proto.MediaStats) {}
	}
	return &StatsCollector{reg: reg, clk: clk, source: source, emit: emit,
		interval: statsInterval, ready: make(chan struct{})}
}

// Started — Run boshlanganini bildiruvchi kanal (fake-clock testlari
// Advance'dan oldin sinxronlashi uchun).
func (s *StatsCollector) Started() <-chan struct{} { return s.ready }

// Snapshot — bitta marta yig'ish (test va selftest uchun).
func (s *StatsCollector) Snapshot() proto.MediaStats {
	refs := s.reg.List()
	sort.Slice(refs, func(i, j int) bool { return refs[i].Key.CameraID < refs[j].Key.CameraID })
	out := proto.MediaStats{Publishes: make([]proto.PublishStat, 0, len(refs))}
	for _, ref := range refs {
		st := s.source(ref.Key)
		out.Publishes = append(out.Publishes, proto.PublishStat{
			PublishID:     ref.PublishID,
			CameraID:      ref.Key.CameraID,
			BytesSent:     st.BytesSent,
			FPS:           st.FPS,
			BitrateKbps:   st.BitrateKbps,
			RTTMs:         st.RTTMs,
			PacketLossPct: st.PacketLossPct,
			Transport:     st.Transport,
		})
	}
	return out
}

// Run — ticker sikli; faol publish bo'lmasa uzatmaydi (§4.6: "faqat faol
// publish'lar bo'lsa"). ctx bekor qilinsa chiqadi.
func (s *StatsCollector) Run(ctx context.Context) {
	t := s.clk.NewTicker(s.interval)
	defer t.Stop()
	close(s.ready)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C():
			if len(s.reg.List()) == 0 {
				continue
			}
			s.emit(s.Snapshot())
		}
	}
}

// BuildReady — media_ready xabari (§4.6): plan + statistika + ochilish
// vaqtidan to'liq maydonlar.
func BuildReady(sessionID, kind, publishID, path string, plan StartPlan, st Stat, transcoded bool, backend string, readyMs int64) proto.MediaReady {
	return proto.MediaReady{
		SessionID:   sessionID,
		CameraID:    plan.Key.CameraID,
		Profile:     plan.Key.Profile,
		Kind:        kind,
		PublishID:   publishID,
		Path:        path,
		Ingest:      plan.Ingest,
		Transport:   st.Transport,
		Transcoded:  transcoded,
		Backend:     backend,
		FPS:         st.FPS,
		BitrateKbps: st.BitrateKbps,
		BytesSent:   st.BytesSent,
		ReadyMs:     readyMs,
	}
}
