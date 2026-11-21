package cams

import "sync"

// gate.go — bitta kameraga (qurilmaga) RTSP murojaatlarini hisobga olish.
//
// §12.1/§8.3b: ko'plab kameralar (masalan Hikvision) parallel RTSP
// sessiyalarini rad etadi yoki eski sessiyani chiqarib tashlaydi.
// 2026-11-21 auditidagi topilma: probe sikli faol oqim paytida kameraga
// ikkinchi sessiya ochgani uchun (a) asossiz "offline" holat yuborardi,
// (b) ba'zan publish sessiyasini uzib qo'yardi. Endi probe/preview
// qurilma band bo'lsa o'tkazib yuboriladi, oqim esa o'z navbatini biladi.

// Gate — kameraning "band" holatini sanaydi. Bloklovchi emas: probe
// uchun TryProbe (band bo'lsa o'tkazamiz), tortish (pull/publish) uchun
// AcquirePublish/ReleasePublish hisobi.
type Gate struct {
	mu sync.Mutex
	m  map[string]*gateState
}

type gateState struct {
	publishers int // faol RTSP tortishlar (main/sub alohida)
	probing    bool
}

// NewGate — bo'sh gate.
func NewGate() *Gate { return &Gate{m: map[string]*gateState{}} }

func (g *Gate) state(id string) *gateState {
	st, ok := g.m[id]
	if !ok {
		st = &gateState{}
		g.m[id] = st
	}
	return st
}

// AcquirePublish — kamera oqimini tortish boshlandi (§9.1).
func (g *Gate) AcquirePublish(id string) {
	if id == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.state(id).publishers++
}

// ReleasePublish — tortish tugadi.
func (g *Gate) ReleasePublish(id string) {
	if id == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st, ok := g.m[id]
	if !ok {
		return
	}
	if st.publishers > 0 {
		st.publishers--
	}
	if st.publishers == 0 && !st.probing {
		delete(g.m, id) // xotira o'smasligi uchun
	}
}

// PublishCount — kamerada faol tortishlar soni (0 → probe mumkin).
func (g *Gate) PublishCount(id string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if st, ok := g.m[id]; ok {
		return st.publishers
	}
	return 0
}

// TryProbe — probe/preview uchun urinish. Kamera oqimi faol yoki boshqa
// probe islayotgan bo'lsa false qaytadi — chaqiruvchi o'tkazib yuboradi.
func (g *Gate) TryProbe(id string) bool {
	if id == "" {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st := g.state(id)
	if st.publishers > 0 || st.probing {
		return false
	}
	st.probing = true
	return true
}

// ReleaseProbe — probe tugadi.
func (g *Gate) ReleaseProbe(id string) {
	if id == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	st, ok := g.m[id]
	if !ok {
		return
	}
	st.probing = false
	if st.publishers == 0 {
		delete(g.m, id)
	}
}
