// C2 qabul mezoni: kamera ro'yxati serverga cams_sync orqali yetadi —
// chunk'lash, epoch drift, cams_request va o'chirish (op:"delete").
//
// Ishga tushirish: make itest SCENARIO=c2
package integration

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"central-vms/client/internal/agent"
	"central-vms/client/internal/cams"
	"central-vms/client/internal/camsvc"
	"central-vms/client/internal/clock"
	"central-vms/client/internal/devserver"
	"central-vms/client/internal/keystore"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/statestore"
)

func TestC2_CamsSyncChunksAndEpoch(t *testing.T) {
	// AutoCamsAck: server har chunk'ga cams_ack qaytaradi (§4.3)
	h := newHarness(t, devserver.Options{AutoCamsAck: true, AutoCamsEpoch: true})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	res, err := agent.Enroll(ctx, h.srv.URL, agent.EnrollInfo{
		Code: "KOD-1", Hostname: "joy-02", OS: "Ubuntu 24.04", LanIP: "10.0.0.5", AgentVersion: "0.4.0",
	})
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}

	// --- keystore + state + camsvc (haqiqiy CRUD yo'li bilan) ---
	keyPath := filepath.Join(h.tempDir, "agent.key")
	if _, err := keystore.NewKeyFile(keyPath); err != nil {
		t.Fatal(err)
	}
	ks, err := keystore.Open(keyPath, filepath.Join(h.tempDir, "secrets.bin"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := statestore.Load(filepath.Join(h.tempDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.AgentID, st.SecretRef = res.AgentID, keystore.RefAgentSecret
	st.ServerURL = h.wsURL
	ks.Put(keystore.RefAgentSecret, res.AgentSecret)
	if err := ks.Save(); err != nil {
		t.Fatal(err)
	}
	svc := camsvc.New(st, ks)

	// 250 kamera — chunk chegarasi (≤100/xabar) sinovdan o'tadi (§4.2)
	for i := 0; i < 250; i++ {
		if _, err := svc.Create(cams.Camera{Name: "kamera-" + itoa(i), Host: "192.168.10." + itoa(i%200)}, "parol"); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	if st.CamsEpoch != 250 {
		t.Fatalf("cams_epoch=%d, kutilgan 250", st.CamsEpoch)
	}

	// --- sessiya + syncer ---
	secret, _ := ks.Get(keystore.RefAgentSecret)
	sess := agent.NewSession(agent.SessionConfig{
		ServerURL: h.wsURL, AgentID: res.AgentID, Secret: secret,
		AgentVersion: "0.4.0", CamsEpoch: st.CamsEpoch, Caps: proto.Caps{GPU: "nvenc"},
		Clock: clock.Real{}, Log: testLog(t),
	})
	sy := agent.NewSyncer(sess, st, clock.Real{}, discardLog())
	hnd := &agent.Handlers{KS: ks, St: st, Log: discardLog(), Sess: sess,
		CamsAck: sy.OnCamsAck,
		// §5.1: server `cams_request` qilsa agent sync yuboradi
		// (cmd/vmsagent/agent.go'dagi asl ulanuv takrorlanadi).
		CamsRequest: func(req proto.CamsRequest) {
			if req.Full {
				go func() { _ = sy.Push(st.CamsEpoch) }()
				return
			}
			go func() { _ = sy.PushDelta(st.CamsEpoch, req.CameraIDs) }()
		}}
	// cams_ack hisoblagichi sync'dan OLDIN o'rnatiladi: SyncIfRequired
	// ack'larni ichida iste'mol qiladi, kech o'rnatilsa ular sanalmaydi.
	var acks atomic.Int64
	sess.SetOnMessage(func(e proto.Envelope) {
		hnd.Handle(e)
		if e.Type == proto.MsgCamsAck {
			acks.Add(1)
		}
	})

	// §14: uzluksiz sessiya (Run) — sync yuborish uchun ulanish ochiq turishi kerak
	done := make(chan struct{}, 1)
	go func() { sess.Run(ctx); close(done) }()
	if !h.dev.WaitConnected(res.AgentID, 5*time.Second) {
		t.Fatalf("agent ulanmadi; faollar: %v", h.dev.Connected())
	}

	// Server welcome'da epoch 0 qaytardi → drift bor → to'liq sync
	if err := sy.SyncIfRequired(proto.Welcome{
		Compat:        proto.Compat{OK: true},
		CamsEpoch:     0,
		NeedsFullSync: true,
	}); err != nil {
		t.Fatalf("SyncIfRequired: %v", err)
	}

	seen := h.dev.CamsSyncSeen(res.AgentID)
	if len(seen) != 3 {
		t.Fatalf("chunk'lar=%d, kutilgan 3 (250 kamera / ≤100)", len(seen))
	}
	var total int
	for i, c := range seen {
		if c.SeqNo != i+1 || c.ChunkCount != 3 || !c.Full {
			t.Fatalf("chunk %d: seq=%d count=%d full=%v", i, c.SeqNo, c.ChunkCount, c.Full)
		}
		if c.Done != (i == 2) {
			t.Fatalf("chunk %d done=%v", i, c.Done)
		}
		if c.Epoch != 250 {
			t.Fatalf("chunk %d epoch=%d", i, c.Epoch)
		}
		total += len(c.Cameras)
	}
	if total != 250 {
		t.Fatalf("jami kamera=%d", total)
	}
	if n := h.waitFor(func() bool { return acks.Load() >= 3 }, 5*time.Second); !n {
		t.Fatalf("cams_ack kelmadi: %d", acks.Load())
	}

	// --- epoch bir xil bo'lsa: drift yo'q → sync bo'lmaydi (§6.5.5) ---
	before := len(h.dev.CamsSyncSeen(res.AgentID))
	if err := sy.SyncIfRequired(proto.Welcome{
		Compat: proto.Compat{OK: true}, CamsEpoch: st.CamsEpoch, NeedsFullSync: false,
	}); err != nil {
		t.Fatal(err)
	}
	if after := len(h.dev.CamsSyncSeen(res.AgentID)); after != before {
		t.Fatalf("epoch bir xil bo'lsa sync yuborilmasligi kerak edi: %d → %d", before, after)
	}

	// --- o'chirish: op:"delete" + epoch o'sadi (§6.5.3) ---
	list := svc.List()
	if n := svc.Delete([]string{list[0].ID, list[1].ID}); n != 2 {
		t.Fatalf("o'chirilgan=%d", n)
	}
	if err := sy.SyncIfRequired(proto.Welcome{
		Compat: proto.Compat{OK: true}, CamsEpoch: 0, NeedsFullSync: true,
	}); err != nil {
		t.Fatalf("delete'dan keyin sync: %v", err)
	}
	seen = h.dev.CamsSyncSeen(res.AgentID)
	var delIDs []string
	for _, c := range seen[before:] {
		for _, e := range c.Cameras {
			if e.Op == "delete" {
				delIDs = append(delIDs, e.ID)
			}
		}
	}
	if len(delIDs) != 2 {
		t.Fatalf("op:delete entry=%v, kutilgan 2 ta", delIDs)
	}
	// Muvaffaqiyatdan keyin navbat tozalanadi (§4.3)
	if len(st.PendingDeletes) != 0 {
		t.Fatalf("pending_deletes tozalanmadi: %v", st.PendingDeletes)
	}

	// --- cams_request (§5.1): server so'roqi → agent sync yuboradi ---
	reqBefore := len(h.dev.CamsSyncSeen(res.AgentID))
	if err := h.dev.Send(res.AgentID, proto.MsgCamsRequest, proto.CamsRequest{Full: true}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(h.dev.CamsSyncSeen(res.AgentID)) > reqBefore {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(h.dev.CamsSyncSeen(res.AgentID)) == reqBefore {
		t.Fatal("cams_request javob berilmadi — agent sync yubormadi")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("sessiya yopilmadi")
	}
}

// stale_epoch: server eski epoch'ni rad etsa → agent xato oladi (§4.2).
func TestC2_StaleEpochRejected(t *testing.T) {
	h := newHarness(t, devserver.Options{AutoCamsAck: true, AutoCamsEpoch: true})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	res, err := agent.Enroll(ctx, h.srv.URL, agent.EnrollInfo{Code: "KOD-1", Hostname: "joy-03"})
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(h.tempDir, "agent.key")
	if _, err := keystore.NewKeyFile(keyPath); err != nil {
		t.Fatal(err)
	}
	ks, err := keystore.Open(keyPath, filepath.Join(h.tempDir, "secrets.bin"))
	if err != nil {
		t.Fatal(err)
	}
	// §10.1: agent secret'i keystore'da saqlanadi — auth shu orqali o'tadi
	ks.Put(keystore.RefAgentSecret, res.AgentSecret)
	if err := ks.Save(); err != nil {
		t.Fatal(err)
	}
	st, err := statestore.Load(filepath.Join(h.tempDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	st.Cameras = []cams.Camera{{ID: cams.NewID(), Name: "A", Host: "10.0.0.1", Port: 554,
		Enabled: true, Position: 1, Status: cams.StatusUnknown}}
	// state.json ga yozib olamiz: keyin o'chirish (op:"delete") sinovi shu
	// fayldan qayta o'qiydi (§6.5.3 — restart'ga chidamli navbat)
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	secret, _ := ks.Get(keystore.RefAgentSecret)
	sess := agent.NewSession(agent.SessionConfig{
		ServerURL: h.wsURL, AgentID: res.AgentID, Secret: secret,
		CamsEpoch: 1, Clock: clock.Real{}, Log: testLog(t),
	})
	sy := agent.NewSyncer(sess, st, clock.Real{}, discardLog())
	hnd := &agent.Handlers{KS: ks, St: st, Log: discardLog(), Sess: sess, CamsAck: sy.OnCamsAck}
	sess.SetOnMessage(hnd.Handle)

	// §14: sessiya uzluksiz ishlaydi (Run — backoff bilan qayta ulanish);
	// RunOnce bir marta ulanib, welcome'dan keyin yopadi — sync uchun Run kerak.
	go sess.Run(ctx)
	if !h.dev.WaitConnected(res.AgentID, 5*time.Second) {
		t.Fatalf("agent ulanmadi; devserverdagi faollar: %v", h.dev.Connected())
	}
	// Serverda biznikidan KATTA epoch saqlangan → kelganimiz stale (§4.2)
	h.dev.SetCamsEpoch(res.AgentID, 99)

	if err := sy.Push(1); err == nil {
		t.Fatal("stale_epoch rad etilishi kerak edi")
	} else if !isStale(err) {
		t.Fatalf("stale_epoch kutilmadi: %v", err)
	}
	cancel()
}

func isStale(err error) bool {
	return err != nil && (err == agent.ErrSyncStale ||
		err.Error() == agent.ErrSyncStale.Error())
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// testLog — sinovda serverga nima yuborilayotganini ko'rsatadi (nosozlikni
// topish oson bo'lsin; muvaffaqiyatli o'tsa chiqindi bo'sh bo'ladi).
func testLog(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(testWriter{t}, nil))
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Logf("%s", strings.TrimSpace(string(p)))
	return len(p), nil
}
