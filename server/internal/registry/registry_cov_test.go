// registry_cov_test.go — tenant variantlari, sessiya yordamchilari va
// favorites/layouts chekka holatlari (2026-11-22 barqarorlik ishlari:
// qamrov 49,6% — aynan shu funksiyalar prod'da kech ochiladigan xatolar
// sinfi: begona tenant amali, TTL uzaytirish, faol sessiyalarni
// sanash/ro'yxat).
package registry

import (
	"errors"
	"testing"
)

func TestCameraTenantVariants(t *testing.T) {
	r := testReg(t)
	c1 := mustClient(t, r, "c1")
	c2 := mustClient(t, r, "c2")
	mustCamera(t, r, c1, "cam-a")

	// Begona tenant: barcha uchala amalda ham ErrNotFound (mavjudlik
	// sizib chiqmaydi) va kamera holati buzilmaydi.
	if err := r.SetCameraStatusForClient(c2, "cam-a", "error", "begona", 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("status begona tenant: %v", err)
	}
	if err := r.SaveCodecProbeForClient(c2, "cam-a", "h265", "", false, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("codec begona tenant: %v", err)
	}
	if err := r.DeleteCameraForClient(c2, "cam-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete begona tenant: %v", err)
	}
	if err := r.cameraOwnedBy(c2, "yoq-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cameraOwnedBy yo'q id: %v", err)
	}

	// O'z tenant: amallar o'tadi.
	if err := r.SetCameraStatusForClient(c1, "cam-a", "online", "", 12); err != nil {
		t.Fatalf("status o'z tenant: %v", err)
	}
	if err := r.SaveCodecProbeForClient(c1, "cam-a", "h265", "h264", true, false); err != nil {
		t.Fatalf("codec o'z tenant: %v", err)
	}
	cams, err := r.ListCamerasByAgent("ag_c1")
	if err != nil || len(cams) != 1 {
		t.Fatalf("ListCamerasByAgent: %v (%v)", cams, err)
	}
	if cams[0].Status != "online" || cams[0].CodecMain != "h265" || !cams[0].BFrameMain {
		t.Fatalf("maydonlar qo'llanilmadi: %+v", cams[0])
	}

	// CountCameras: 1 total, 1 online.
	total, online, err := r.CountCameras()
	if err != nil || total != 1 || online != 1 {
		t.Fatalf("CountCameras: total=%d online=%d err=%v", total, online, err)
	}

	if err := r.DeleteCameraForClient(c1, "cam-a"); err != nil {
		t.Fatalf("delete o'z tenant: %v", err)
	}
	if _, err := r.ListCamerasByAgent("ag_c1"); err != nil {
		t.Fatalf("ListCamerasByAgent (bo'sh): %v", err)
	}
}

func TestSessionHelpers(t *testing.T) {
	r := testReg(t)
	uid := mustUser(t, r, "op1", "viewer")
	c1 := mustClient(t, r, "c1")
	mustCamera(t, r, c1, "cam-s")

	if err := r.InsertSession("s1", "cam-s", uid, "main", "play", "2026-11-22T06:00:00Z"); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}
	if err := r.InsertSession("s2", "cam-s", uid, "sub", "preview", "2026-11-22T06:00:00Z"); err != nil {
		t.Fatalf("InsertSession s2: %v", err)
	}

	// Faol ro'yxat + sanov.
	rows, err := r.ListActiveSessions()
	if err != nil || len(rows) != 2 {
		t.Fatalf("ListActiveSessions: %v (%v)", rows, err)
	}
	if n, err := r.CountActiveSessions(uid); err != nil || n != 2 {
		t.Fatalf("CountActiveSessions(user): %d %v", n, err)
	}
	if n, err := r.CountActiveSessions(0); err != nil || n != 2 {
		t.Fatalf("CountActiveSessions(global): %d %v", n, err)
	}

	// TTL uzaytirish.
	if err := r.TouchSessionTTL("s1", "2026-11-22T06:05:00Z"); err != nil {
		t.Fatalf("TouchSessionTTL: %v", err)
	}
	rows, _ = r.ListActiveSessions()
	for _, s := range rows {
		if s.ID == "s1" && s.ExpiresAt != "2026-11-22T06:05:00Z" {
			t.Fatalf("expires_at yangilanmadi: %+v", s)
		}
	}

	// Holat o'tishi: playing → ended (ended_at to'ladi) va faol ro'yxatdan chiqadi.
	if err := r.SetSessionState("s1", "playing"); err != nil {
		t.Fatalf("SetSessionState playing: %v", err)
	}
	if err := r.SetSessionState("s2", "failed"); err != nil {
		t.Fatalf("SetSessionState failed: %v", err)
	}
	rows, _ = r.ListActiveSessions()
	if len(rows) != 1 || rows[0].ID != "s1" {
		t.Fatalf("failed sessiya faol ro'yxatda qoldi: %+v", rows)
	}
	if err := r.SetSessionState("s1", "ended"); err != nil {
		t.Fatalf("SetSessionState ended: %v", err)
	}
	if n, _ := r.CountActiveSessions(0); n != 0 {
		t.Fatalf("ended keyin sanov: %d", n)
	}
	// Mavjud bo'lmagan id — xato emas (idempotent tozalash oqimi);
	// lekin holat yozuvi qayerga ketganini bilish uchun qaytarmaymiz.
	if err := r.SetSessionState("yoq", "ended"); err != nil {
		t.Fatalf("SetSessionState yo'q id: %v", err)
	}
}

func TestFavoritesLayoutEdges(t *testing.T) {
	r := testReg(t)
	u := mustUser(t, r, "op2", "viewer")
	c1 := mustClient(t, r, "c1")
	mustCamera(t, r, c1, "cam-f")

	if err := r.AddFavorite(u, "cam-f", 1); err != nil {
		t.Fatalf("AddFavorite: %v", err)
	}
	if err := r.AddFavorite(u, "cam-f", 2); err != nil {
		t.Fatalf("AddFavorite takror (IGNORE bo'lishi kerak): %v", err)
	}
	if err := r.RemoveFavorite(u, "cam-f"); err != nil {
		t.Fatalf("RemoveFavorite: %v", err)
	}
	if err := r.RemoveFavorite(u, "cam-f"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RemoveFavorite ikkinchi marta: %v", err)
	}

	// Layout: qisman yangilash + noto'g'ri kiritishlar + begona foydalanuvchi.
	lid, err := r.CreateLayout(u, "bosh sahifa", 2, `{"0":"cam-f"}`)
	if err != nil {
		t.Fatalf("CreateLayout: %v", err)
	}
	bad := 7 // validGrid emas
	if err := r.UpdateLayout(lid, u, nil, &bad, nil); err == nil {
		t.Fatal("grid=7 qabul qilindi")
	}
	if err := r.UpdateLayout(lid, u, nil, nil, strPtr("bucak json")); err == nil {
		t.Fatal("yaroqsiz slots_json qabul qilindi")
	}
	if err := r.UpdateLayout(lid, u, strPtr("yangi nom"), nil, nil); err != nil {
		t.Fatalf("UpdateLayout nom: %v", err)
	}
	// Bo'sh patch — mavjudlik tekshiruvi (begona user → ErrNotFound).
	other := mustUser(t, r, "op3", "viewer")
	if err := r.UpdateLayout(lid, other, nil, nil, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateLayout begona user: %v", err)
	}
	if err := r.DeleteLayout(lid, other); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteLayout begona user: %v", err)
	}
	if err := r.DeleteLayout(lid, u); err != nil {
		t.Fatalf("DeleteLayout: %v", err)
	}
	if err := r.DeleteLayout(lid, u); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteLayout ikkinchi marta: %v", err)
	}
}

func strPtr(s string) *string { return &s }
