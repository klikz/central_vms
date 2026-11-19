package publish

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"central-vms/client/internal/clock"
)

// Publish registry (docs/protocol.md §9.1).
// Yagona yozuvchi goroutine (TASKS §0.3): entries/xaritasi faqat writer'da
// o'zgaradi — murakkab lock'lar yo'q. Barcha amallar buyruq kanali orqali.

// gracePeriod — refcount 0 bo'lganda kutish (§9.1: tezkor qayta ochilishlarga
// chidamli).
const gracePeriod = time.Second

// defaultTTL — media_start ttl_s bo'lmasa (§9.2 default 60 s).
const defaultTTL = 60 * time.Second

// ErrClosed — registry yopilgan (Shutdown), amallar bajarilmaydi.
var ErrClosed = errors.New("publish: registry yopilgan")

// Key — publish_key = camera_id + profile + ingest (§9.1).
// kind (play/preview) kiritilmagan: ular BITTA publish'ni bo'lishadi.
type Key struct {
	CameraID string
	Profile  string // main | sub
	Ingest   string // whip | rtsp_tcp
}

// ReadyEvent — media_ready (§4.6), har publish uchun BIR marta.
type ReadyEvent struct {
	PublishID string
	Key       Key
}

// StoppedEvent — media_stopped (§4.6) aniq reason bilan (§13).
type StoppedEvent struct {
	PublishID string
	Key       Key
	Reason    string
}

// StartResult — media_start natijasi.
type StartResult struct {
	PublishID string
	New       bool // true → transport ochish kerak (yagona media_ready)
	Refcount  int
}

type cmdKind int

const (
	cmdStart cmdKind = iota
	cmdStop
	cmdReady
	cmdCount
	cmdRefresh
	cmdList
)

// PublishRef — faol publish'ning ko'rsatkichi (stats/status uchun).
type PublishRef struct {
	PublishID string
	Key       Key
	Sessions  int
}

type cmd struct {
	kind  cmdKind
	key   Key
	sess  string
	ttl   time.Duration // start/refresh uchun (§9.2)
	reply chan any      // buffered(1): StartResult|error | int | nil
}

type entry struct {
	publishID   string
	sessions    map[string]struct{}
	closeAt     time.Time // zero → grace yo'q; dol bo'lsa yopish (§9.1)
	ttlDeadline time.Time // §9.2: muddat o'tsa → media_stopped{ttl_expired}
	readySent   bool
}

// Registry — refcount jadvali + grace yopish.
type Registry struct {
	cmds chan cmd
	done chan struct{}
	once sync.Once

	clk       clock.Clock
	log       *slog.Logger
	onReady   func(ReadyEvent)
	onStopped func(StoppedEvent)
	grace     time.Duration
}

// New — yozuvchi goroutine'ni ishga tushiradi. clk nil → Real;
// onReady/onStopped nil → hodisalar tashlanadi.
func New(clk clock.Clock, log *slog.Logger, onReady func(ReadyEvent), onStopped func(StoppedEvent)) *Registry {
	if clk == nil {
		clk = clock.Real{}
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if onReady == nil {
		onReady = func(ReadyEvent) {}
	}
	if onStopped == nil {
		onStopped = func(StoppedEvent) {}
	}
	r := &Registry{
		cmds: make(chan cmd, 64), done: make(chan struct{}),
		clk: clk, log: log, onReady: onReady, onStopped: onStopped,
		grace: gracePeriod,
	}
	go r.writer(map[Key]*entry{})
	return r
}

// writer — yagona yozuvchi. entries faqat shu goroutine'ning ichida.
func (r *Registry) writer(entries map[Key]*entry) {
	var timer <-chan time.Time
	for {
		select {
		case <-r.done:
			return
		case c := <-r.cmds:
			c.reply <- r.handle(entries, c)
			timer = r.arm(entries)
		case <-timer:
			r.expire(entries)
			timer = r.arm(entries)
		}
	}
}

func (r *Registry) handle(entries map[Key]*entry, c cmd) any {
	now := r.clk.Now()
	switch c.kind {
	case cmdStart:
		e, ok := entries[c.key]
		isNew := !ok
		if !ok {
			e = &entry{publishID: "pub_" + uuid.NewString()[:16], sessions: map[string]struct{}{}}
			entries[c.key] = e
		}
		e.closeAt = time.Time{} // grace bekor qilindi (§9.1)
		e.sessions[c.sess] = struct{}{}
		r.extendTTL(e, c.ttl) // yangi media_start deadline'ni uzaytiradi (§9.2)
		return StartResult{PublishID: e.publishID, New: isNew, Refcount: len(e.sessions)}
	case cmdStop:
		e, ok := entries[c.key]
		if !ok {
			return 0
		}
		delete(e.sessions, c.sess)
		if len(e.sessions) == 0 {
			e.closeAt = now.Add(r.grace)
		}
		return len(e.sessions)
	case cmdReady:
		e, ok := entries[c.key]
		if !ok || e.readySent {
			return nil
		}
		e.readySent = true
		r.onReady(ReadyEvent{PublishID: e.publishID, Key: c.key})
		return nil
	case cmdCount:
		e, ok := entries[c.key]
		if !ok {
			return 0
		}
		return len(e.sessions)
	case cmdRefresh:
		e, ok := entries[c.key]
		if !ok {
			return nil
		}
		r.extendTTL(e, c.ttl) // media_refresh deadline'ni siljitadi (§9.2)
		return nil
	case cmdList:
		refs := make([]PublishRef, 0, len(entries))
		for k, e := range entries {
			refs = append(refs, PublishRef{PublishID: e.publishID, Key: k, Sessions: len(e.sessions)})
		}
		return refs
	}
	return nil
}

// extendTTL — deadline = max(mavjud, now+ttl).
func (r *Registry) extendTTL(e *entry, ttl time.Duration) {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	want := r.clk.Now().Add(ttl)
	if e.ttlDeadline.IsZero() || want.After(e.ttlDeadline) {
		e.ttlDeadline = want
	}
}

// expire — muddati yetgan grace/TTL'larni yopadi.
// §9.1: refcount 0 + grace o'tsa → no_viewers.
// §9.2: TTL muddati o'tsa (tomoshibi bo'lsa ham) → ttl_expired.
func (r *Registry) expire(entries map[Key]*entry) {
	now := r.clk.Now()
	for k, e := range entries {
		switch {
		case len(e.sessions) == 0 && !e.closeAt.IsZero() && !now.Before(e.closeAt):
			delete(entries, k)
			r.onStopped(StoppedEvent{PublishID: e.publishID, Key: k, Reason: "no_viewers"})
		case !e.ttlDeadline.IsZero() && !now.Before(e.ttlDeadline):
			delete(entries, k)
			r.onStopped(StoppedEvent{PublishID: e.publishID, Key: k, Reason: "ttl_expired"})
		}
	}
}

// arm — keyingi yopish/TTL muddatigacha taymer (nil → kutilayotgan yo'q).
func (r *Registry) arm(entries map[Key]*entry) <-chan time.Time {
	now := r.clk.Now()
	var next time.Time
	consider := func(t time.Time) {
		if t.IsZero() {
			return
		}
		if next.IsZero() || t.Before(next) {
			next = t
		}
	}
	for _, e := range entries {
		consider(e.closeAt)
		consider(e.ttlDeadline)
	}
	if next.IsZero() {
		return nil
	}
	d := next.Sub(now)
	if d < 0 {
		d = 0
	}
	return r.clk.After(d)
}

// send — buyruqni yuborish va javobni kutish (Shutdown'dan keyin ErrClosed).
func (r *Registry) send(c cmd) (any, error) {
	c.reply = make(chan any, 1)
	select {
	case <-r.done:
		return nil, ErrClosed
	case r.cmds <- c:
	}
	select {
	case v := <-c.reply:
		return v, nil
	case <-r.done:
		return nil, ErrClosed
	}
}

// Start — media_start (default TTL, §9.2).
func (r *Registry) Start(sess string, k Key, kind string) (StartResult, error) {
	return r.StartWithTTL(sess, k, kind, 0)
}

// StartWithTTL — media_start: sessiyani qo'shadi. New=true bo'lsa chaqiruvchi
// transport ochadi va MarkReady chaqiradi (§9.1). ttlS — sekundlarda
// (media_start.ttl_s, §9.2); ≤0 → default 60 s.
func (r *Registry) StartWithTTL(sess string, k Key, kind string, ttlS int) (StartResult, error) {
	ttl := time.Duration(ttlS) * time.Second
	if ttl <= 0 {
		ttl = defaultTTL
	}
	v, err := r.send(cmd{kind: cmdStart, key: k, sess: sess, ttl: ttl})
	if err != nil {
		return StartResult{}, err
	}
	return v.(StartResult), nil
}

// Refresh — media_refresh: deadline'ni now+extendS'ga siljitadi (§9.2).
// extendS sekundlarda (media_refresh.extend_s). Watchdog WS'dan mustaqil:
// uzilishda ham o'z vaqtida yopadi.
func (r *Registry) Refresh(k Key, extendS int) {
	ttl := time.Duration(extendS) * time.Second
	if ttl <= 0 {
		ttl = defaultTTL
	}
	_, _ = r.send(cmd{kind: cmdRefresh, key: k, ttl: ttl})
}

// Stop — media_stop: sessiyani oladi. Qaytgan refcount 0 bo'lsa 1 s grace
// boshlanadi (§9.1).
func (r *Registry) Stop(sess string, k Key) int {
	v, err := r.send(cmd{kind: cmdStop, key: k, sess: sess})
	if err != nil {
		return -1
	}
	return v.(int)
}

// MarkReady — transport muvaffaqiyatli ochildi: media_ready BIR marta
// yuboriladi (§9.1).
func (r *Registry) MarkReady(k Key) {
	_, _ = r.send(cmd{kind: cmdReady, key: k})
}

// Refcount — joriy tomoshobilar soni (status/selftest uchun). O'qish ham
// yozuvchi orqali: izchil ko'rinish (§0.3).
func (r *Registry) Refcount(k Key) int {
	v, err := r.send(cmd{kind: cmdCount, key: k})
	if err != nil {
		return -1
	}
	return v.(int)
}

// List — faol publish'lar snapshot'i (media_stats, §4.6).
func (r *Registry) List() []PublishRef {
	v, err := r.send(cmd{kind: cmdList})
	if err != nil {
		return nil
	}
	return v.([]PublishRef)
}

// Shutdown — yozuvchi goroutine'ni to'xtatadi.
func (r *Registry) Shutdown() {
	r.once.Do(func() { close(r.done) })
}
