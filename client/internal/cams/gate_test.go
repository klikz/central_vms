package cams

import "testing"

// Gate — kamera oqimi faol paytida probe o'tkazib yuboriladi (§12.1/§8.3b).
func TestGatePublishBlocksProbe(t *testing.T) {
	g := NewGate()
	if !g.TryProbe("c1") {
		t.Fatal("bo'sh kameraga probe berilmadi")
	}
	if g.TryProbe("c1") {
		t.Fatal("ikkita parallel probe o'tdi")
	}
	g.ReleaseProbe("c1")

	g.AcquirePublish("c1")
	if g.PublishCount("c1") != 1 {
		t.Fatalf("publish hisobi: %d", g.PublishCount("c1"))
	}
	if g.TryProbe("c1") {
		t.Fatal("oqim faol paytida probe o'tdi — kamera sessiyasi uzilishi mumkin")
	}
	g.ReleasePublish("c1")
	if !g.TryProbe("c1") {
		t.Fatal("oqim tugagach probe o'tmadi")
	}
	g.ReleaseProbe("c1")
	if g.PublishCount("c1") != 0 {
		t.Fatal("hisob tozalanmadi")
	}
}

// main+sub bir kamerada ikkita tortish bo'lishi mumkin, lekin probe
// baribir o'tkazib yuboriladi; hisoblar to'g'ri sanaladi.
func TestGateMultiplePublishes(t *testing.T) {
	g := NewGate()
	g.AcquirePublish("c1")
	g.AcquirePublish("c1")
	if g.PublishCount("c1") != 2 {
		t.Fatalf("hisob: %d, 2 kutilardi", g.PublishCount("c1"))
	}
	g.ReleasePublish("c1")
	if g.TryProbe("c1") {
		t.Fatal("hali bitta tortish bor — probe o'tmasligi kerak")
	}
	g.ReleasePublish("c1")
	g.ReleasePublish("c1") // ortiqcha release panic/xato bermasin
	if g.PublishCount("c1") != 0 {
		t.Fatal("manfiy hisob")
	}
	// Bo'sh kamera xotirada qolmaydi.
	g.mu.Lock()
	n := len(g.m)
	g.mu.Unlock()
	if n != 0 {
		t.Fatalf("gate xotirasi tozalanmadi: %d", n)
	}
}
