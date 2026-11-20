package registry

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.uber.org/goleak"

	"central-vms/server/internal/store"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func testReg(t *testing.T) *Registry {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "reg.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
	if _, err := store.MigrateUp(db, dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(db)
}

func mustUser(t *testing.T, r *Registry, name, role string) int64 {
	t.Helper()
	id, err := r.CreateUser(name, "hash", role, "")
	if err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
	return id
}

func mustClient(t *testing.T, r *Registry, name string) int64 {
	t.Helper()
	id, err := r.CreateClient(name, 0, "ag_"+name, "hash")
	if err != nil {
		t.Fatalf("create client %s: %v", name, err)
	}
	return id
}

func TestUsers(t *testing.T) {
	r := testReg(t)
	id := mustUser(t, r, "admin1", "admin")
	if _, err := r.CreateUser("admin1", "h", "admin", ""); err == nil {
		t.Fatal("takror username qabul qilindi")
	}
	if _, err := r.CreateUser("x", "h", "superuser", ""); err == nil {
		t.Fatal("noma'lum rol qabul qilindi")
	}
	u, hash, err := r.GetUserByName("admin1")
	if err != nil || u.ID != id || hash != "hash" || u.Role != "admin" {
		t.Fatalf("get: %+v hash=%q err=%v", u, hash, err)
	}
	if _, _, err := r.GetUserByName("yoq"); err == nil {
		t.Fatal("yo'q user topildi")
	}
	role := "viewer"
	dis := true
	if err := r.UpdateUser(id, &role, nil, &dis, nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	u, err = r.GetUserByID(id)
	if err != nil || u.Role != "viewer" || !u.Disabled {
		t.Fatalf("update natija: %+v err=%v", u, err)
	}
	if err := r.TouchLogin(id); err != nil {
		t.Fatalf("touch: %v", err)
	}
	u, _ = r.GetUserByID(id)
	if u.LastLoginAt == "" {
		t.Fatal("last_login_at yangilanmadi")
	}
	list, err := r.ListUsers()
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
}

func TestSitesClients(t *testing.T) {
	r := testReg(t)
	sid, err := r.CreateSite("Oxangaron", "OX", 1)
	if err != nil {
		t.Fatalf("site: %v", err)
	}
	if _, err := r.CreateSite("Oxangaron", "", 0); err == nil {
		t.Fatal("takror joy nomi qabul qilindi")
	}
	cid, err := r.CreateClient("ox-1", sid, "ag_ox1", "h1")
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if _, err := r.CreateClient("ox-1", 0, "ag_ox2", "h2"); err == nil {
		t.Fatal("takror client nomi qabul qilindi")
	}
	c, err := r.GetClientByAgentID("ag_ox1")
	if err != nil || c.ID != cid || c.SiteID != sid {
		t.Fatalf("by agent: %+v err=%v", c, err)
	}
	if err := r.UpdateClientHello(cid, "1.2.3", "ubuntu", "10.0.5.9", `{"gpu":"nvenc"}`); err != nil {
		t.Fatalf("hello: %v", err)
	}
	c, _ = r.GetClientByAgentID("ag_ox1")
	if c.Status != "online" || c.Version != "1.2.3" || c.CapsJSON == "" {
		t.Fatalf("hello keyin: %+v", c)
	}
	// offline: kameralar ham offline bo'ladi
	mustCamera(t, r, cid, "cam-a")
	if err := r.MarkClientOffline(cid); err != nil {
		t.Fatalf("offline: %v", err)
	}
	cam, err := r.GetCamera("cam-a")
	if err != nil || cam.Status != "offline" {
		t.Fatalf("kamera offline bo'lmadi: %+v err=%v", cam, err)
	}
	// patch + hisoblar
	ups := 4
	if err := r.PatchClient(cid, nil, nil, &ups, nil, nil); err != nil {
		t.Fatalf("patch: %v", err)
	}
	list, err := r.ListClients()
	if err != nil || len(list) != 1 || list[0].MaxUploads != 4 || list[0].CamerasCount != 1 {
		t.Fatalf("list clients: %+v err=%v", list, err)
	}
	// delete cascade
	if err := r.DeleteClient(cid); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := r.GetCamera("cam-a"); err == nil {
		t.Fatal("kamera cascade o'chirilmadi")
	}
}

func mustCamera(t *testing.T, r *Registry, clientID int64, id string) {
	t.Helper()
	if err := r.UpsertCamera(clientID, SyncCamera{ID: id, Name: id, Status: "online"}); err != nil {
		t.Fatalf("upsert %s: %v", id, err)
	}
}

func TestCameras(t *testing.T) {
	r := testReg(t)
	c1 := mustClient(t, r, "c1")
	c2 := mustClient(t, r, "c2")

	if err := r.UpsertCamera(c1, SyncCamera{ID: "uuid-1", Name: "Kirish", RTSPMasked: "rtsp://***:***@10.0.5.21/x", CodecMain: "h265", CodecSub: "h264", Status: "online", Position: 3}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Boshqa client xuddi shu id'ni yuborsa — rad etiladi (protocol.md §6).
	if err := r.UpsertCamera(c2, SyncCamera{ID: "uuid-1", Name: "O'g'ir"}); err == nil {
		t.Fatal("o'zaro client id qabul qilindi")
	}
	// Qayta sync: policy maydonlari saqlanadi.
	pp, tm, en := "main", "off", false
	if err := r.PatchCameraPolicy("uuid-1", &pp, &tm, &en); err != nil {
		t.Fatalf("patch: %v", err)
	}
	if err := r.UpsertCamera(c1, SyncCamera{ID: "uuid-1", Name: "Kirish2", Status: "offline"}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	cam, err := r.GetCamera("uuid-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if cam.Name != "Kirish2" || cam.Status != "offline" {
		t.Fatalf("sync yangilanmadi: %+v", cam)
	}
	if cam.PlayProfile != "main" || cam.TranscodeMode != "off" || cam.Enabled {
		t.Fatalf("policy sync'da buzildi: %+v", cam)
	}
	if err := r.SetCameraStatus("uuid-1", "online", "", 12); err != nil {
		t.Fatalf("status: %v", err)
	}
	if err := r.SaveCodecProbe("uuid-1", "h265", "h264", false, true); err != nil {
		t.Fatalf("probe: %v", err)
	}
	cam, _ = r.GetCamera("uuid-1")
	if cam.CodecMain != "h265" || !cam.BFrameSub || cam.CodecProbeAt == "" {
		t.Fatalf("probe saqlanmadi: %+v", cam)
	}
	if err := r.PatchCameraPolicy("uuid-1", strp("bad"), nil, nil); err == nil {
		t.Fatal("noma'lum play_profile qabul qilindi")
	}
	list, err := r.ListCamerasByClient(c1)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
}

func strp(s string) *string { return &s }

func TestFavoritesLayouts(t *testing.T) {
	r := testReg(t)
	u := mustUser(t, r, "v1", "viewer")
	c := mustClient(t, r, "c1")
	mustCamera(t, r, c, "k1")
	mustCamera(t, r, c, "k2")

	if err := r.AddFavorite(u, "k1", 0); err != nil {
		t.Fatalf("fav: %v", err)
	}
	if err := r.AddFavorite(u, "k1", 0); err != nil { // idempotent
		t.Fatalf("takror fav xato berdi: %v", err)
	}
	if err := r.AddFavorite(u, "yoq-id", 1); err == nil {
		t.Fatal("yo'q kameraga favorite qo'yildi (FK)")
	}
	if err := r.ReorderFavorites(u, []string{"k2", "k1"}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	ids, _ := r.ListFavoriteCameraIDs(u)
	if len(ids) != 2 || ids[0] != "k2" {
		t.Fatalf("tartib: %v", ids)
	}

	lid, err := r.CreateLayout(u, "Asosiy", 3, `["k1","k2",null]`)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	if _, err := r.CreateLayout(u, "Asosiy", 2, "[]"); err == nil {
		t.Fatal("takror layout nomi qabul qilindi")
	}
	if _, err := r.CreateLayout(u, "Yomon", 5, "[]"); err == nil {
		t.Fatal("grid=5 qabul qilindi")
	}
	if err := r.SetDefaultLayout(lid, u); err != nil {
		t.Fatalf("default: %v", err)
	}
	ls, _ := r.ListLayouts(u)
	if len(ls) != 1 || !ls[0].IsDefault {
		t.Fatalf("layouts: %+v", ls)
	}
	// Boshqa user o'z layoutlarini ko'radi (viewer izolatsiyasi).
	u2 := mustUser(t, r, "v2", "viewer")
	ls2, err := r.ListLayouts(u2)
	if err != nil || len(ls2) != 0 {
		t.Fatalf("v2 layoutlari: %+v %v", ls2, err)
	}

	// Kamera o'chsa: favorites FK cascade + layout sloti bo'shaydi.
	if err := r.DeleteCamera("k1"); err != nil {
		t.Fatalf("delete cam: %v", err)
	}
	if err := r.ClearCameraSlots("k1"); err != nil {
		t.Fatalf("clear slots: %v", err)
	}
	ids, _ = r.ListFavoriteCameraIDs(u)
	if len(ids) != 1 || ids[0] != "k2" {
		t.Fatalf("favorites cascade: %v", ids)
	}
	ls, _ = r.ListLayouts(u)
	if ls[0].SlotsJSON != `[null,"k2",null]` {
		t.Fatalf("layout sloti: %s", ls[0].SlotsJSON)
	}
}

// Server qayta boshlanganda DB'da qolgan "online" belgilari xato ko'rsatiladi:
// birorta WS sessiya yo'q. Restart paytida ular tozalanishi kerak
// (2026-11-20 kuzatildi: UI'da "online" ko'rinib turardi).
func TestResetOnlineStates(t *testing.T) {
	r := testReg(t)
	id, err := r.CreateClient("Qayta-boshlangan", 0, "ag_reset00000001", "hash")
	if err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if err := r.UpsertCamera(id, SyncCamera{ID: "cam-reset", Name: "A"}); err != nil {
		t.Fatalf("UpsertCamera: %v", err)
	}
	if err := r.MarkClientSeen(id); err != nil {
		t.Fatalf("MarkClientSeen: %v", err)
	}
	if err := r.SetCameraStatus("cam-reset", "online", "", 10); err != nil {
		t.Fatalf("SetCameraStatus: %v", err)
	}
	n, err := r.ResetOnlineStates()
	if err != nil {
		t.Fatalf("ResetOnlineStates: %v", err)
	}
	if n != 1 {
		t.Fatalf("offline qilingan mijozlar: %d, 1 kutilgan", n)
	}
	c, err := r.GetClientByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != "offline" {
		t.Fatalf("mijoz holati: %s", c.Status)
	}
	cams, err := r.ListCamerasByClient(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(cams) != 1 || cams[0].Status != "offline" {
		t.Fatalf("kameralar holati: %+v", cams)
	}
}

// 1.0-draft.12: kamera xatosi sababi saqlanadi va online bo'lganda
// tozalanadi (MediaMTX'ning keepalive_unsupported kabi eslatmalari
// "xato" bo'lib qolmasligi uchun).
func TestCameraLastError(t *testing.T) {
	r := testReg(t)
	c1 := mustClient(t, r, "err-client")
	mustCamera(t, r, c1, "err-cam")

	if err := r.SetCameraStatus("err-cam", "offline", "dial: connection refused", 0); err != nil {
		t.Fatalf("status: %v", err)
	}
	c, err := r.GetCamera("err-cam")
	if err != nil {
		t.Fatal(err)
	}
	if c.LastError != "dial: connection refused" || c.LastErrorAt == "" {
		t.Fatalf("sabab saqlanmadi: %+v", c)
	}

	// Online → sabab tozalanadi (keepalive_unsupported kabi eslatmalar
	// operatorga xato bo'lib ko'rinmasin).
	if err := r.SetCameraStatus("err-cam", "online", "keepalive_unsupported", 8); err != nil {
		t.Fatalf("status online: %v", err)
	}
	c, _ = r.GetCamera("err-cam")
	if c.LastError != "" || c.LastErrorAt != "" {
		t.Fatalf("online holda sabab qoldi: %+v", c)
	}

	// Oqim xatosi holatni o'zgartirmaydi, faqat sababni yozadi.
	if err := r.SetCameraError("err-cam", "oqim to'xtadi: publish_failed"); err != nil {
		t.Fatalf("SetCameraError: %v", err)
	}
	c, _ = r.GetCamera("err-cam")
	if c.Status != "online" || !strings.Contains(c.LastError, "publish_failed") {
		t.Fatalf("oqim xatosi noto'g'ri saqlandi: %+v", c)
	}
	// Bo'sh sabab — yozilmaydi (eski sabab saqlanadi).
	if err := r.SetCameraError("err-cam", "   "); err != nil {
		t.Fatal(err)
	}
	c, _ = r.GetCamera("err-cam")
	if !strings.Contains(c.LastError, "publish_failed") {
		t.Fatalf("bo'sh sabab eskisini o'chirdi: %+v", c)
	}
}
