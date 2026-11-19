// clock — vaqt interfeysi (Now/After/NewTicker) va fake impl.
// Taymaut/retry/watchdog testlari deterministik bo'lishi uchun (TASKS §0.2).
package clock

import (
	"sort"
	"sync"
	"time"
)

// maxTicksPerAdvance — Advance bir chaqiruvda nechta tick ochishi mumkinligi
// (cheksiz loop va deadlock'dan himoya).
const maxTicksPerAdvance = 64

// Clock — ish kodi foydalanadigan vaqt interfeysi. Testlarda Fake ishlatiladi:
// 60 s'lik watchdog sikli ~0 ms'da o'tadi va natija har doim bir xil bo'ladi.
type Clock interface {
	Now() time.Time
	// After — d o'tgach bir marta signal (Real: time.After, Fake: Advance bilan).
	After(d time.Duration) <-chan time.Time
	// NewTicker — muntazam signal; Stop() bilan yopiladi.
	NewTicker(d time.Duration) *Ticker
}

// Real — soat bo'yicha haqiqiy implementatsiya (prod).
type Real struct{}

func (Real) Now() time.Time { return time.Now() }

func (Real) After(d time.Duration) <-chan time.Time { return time.After(d) }

func (Real) NewTicker(d time.Duration) *Ticker {
	t := time.NewTicker(d)
	return &Ticker{ch: t.C, stop: t.Stop}
}

// Ticker — Real va Fake uchun umumli qoplama.
type Ticker struct {
	ch   <-chan time.Time
	stop func()
	once sync.Once
}

func (t *Ticker) C() <-chan time.Time { return t.ch }

func (t *Ticker) Stop() {
	t.once.Do(func() {
		if t.stop != nil {
			t.stop()
		}
	})
}

// Fake — qo'lda boshqariladigan vaqt (testlar uchun).
type Fake struct {
	mu     sync.Mutex
	now    time.Time
	nextID int
	timers map[int]*fakeTimer  // bir martalik
	ticks  map[int]*fakeTicker // muntazam
}

type fakeTimer struct {
	id    int
	at    time.Time
	ch    chan time.Time
	fired bool
}

type fakeTicker struct {
	id       int
	interval time.Duration
	next     time.Time
	ch       chan time.Time
	stopped  bool
}

// NewFake — boshlang'ich vaqt bilan (0001-01-01 emas — testlarda aniq sana yaxshi).
func NewFake(start time.Time) *Fake {
	return &Fake{
		now:    start,
		timers: map[int]*fakeTimer{},
		ticks:  map[int]*fakeTicker{},
	}
}

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	t := &fakeTimer{id: f.nextID, at: f.now.Add(d), ch: make(chan time.Time, 1)}
	f.timers[t.id] = t
	return t.ch
}

func (f *Fake) NewTicker(d time.Duration) *Ticker {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	// Buffer = maxTicksPerAdvance: Advance davomida tug'ilgan barcha tick'lar
	// yo'qolmaydi — testlar ularni keyin o'qishi mumkin (deterministik).
	ch := make(chan time.Time, maxTicksPerAdvance)
	ft := &fakeTicker{id: f.nextID, interval: d, next: f.now.Add(d), ch: ch}
	f.ticks[ft.id] = ft
	return &Ticker{ch: ch, stop: func() { f.stopTicker(ft.id) }}
}

func (f *Fake) stopTicker(id int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ft, ok := f.ticks[id]; ok {
		ft.stopped = true
		delete(f.ticks, id)
	}
}

// Advance — vaqtni oldinga suradi va muddati yetgan timer/ticker'larni
// tartib bilan ochadi. Bir nuqtada bir nechta timer bo'lsa — id tartibida.
func (f *Fake) Advance(d time.Duration) {
	if d <= 0 {
		return
	}
	f.mu.Lock()
	target := f.now.Add(d)

	type event struct {
		at time.Time
		id int
		f  func()
	}
	var ev []event

	for id, t := range f.timers {
		if !t.at.After(target) {
			ev = append(ev, event{at: t.at, id: id, f: func() {
				t.fired = true
				select {
				case t.ch <- t.at:
				default: // kanal to'la — signal allaqachon o'qilmagan, takrorlamaymiz
				}
				delete(f.timers, id)
			}})
		}
	}
	for id, ft := range f.ticks {
		if ft.interval <= 0 {
			continue
		}
		// Nechta tick targetgacha yetadi — cheklangan (cheksiz loop bo'lmasin).
		for n := 0; n < maxTicksPerAdvance && !ft.next.After(target); n++ {
			at := ft.next
			ev = append(ev, event{at: at, id: id, f: func() {
				select {
				case ft.ch <- at:
				default: // o'qilmagan tick tushirib yuboriladi (ticker semantikasi)
				}
			}})
			ft.next = ft.next.Add(ft.interval)
		}
	}

	sort.Slice(ev, func(i, j int) bool {
		if ev[i].at.Equal(ev[j].at) {
			return ev[i].id < ev[j].id
		}
		return ev[i].at.Before(ev[j].at)
	})

	for _, e := range ev {
		f.now = e.at
		e.f()
	}
	f.now = target
	f.mu.Unlock()
}

// Set — vaqtni to'g'ridan-to'g'ri o'rnatadi (Advance ochmaydi).
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}

// PendingTimers — ochilmagan timerlar soni (test tekshiruvi uchun).
func (f *Fake) PendingTimers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.timers)
}
