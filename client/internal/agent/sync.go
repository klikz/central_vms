package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/clock"
	"central-vms/client/internal/proto"
	"central-vms/client/internal/statestore"
)

// cams_sync chunk'lash va cams_ack kutish (docs/protocol.md §4.2, §4.3, §6.5).
//
// Chegaralar: ≤100 kamera/xabar va ≤900 KiB/xabar (§2). Ack yo'qolsa 10 s'dan
// keyin qayta yuborish (3 marta), bo'lmasa xato — lokal UI'da ko'rsatiladi.

const (
	MaxCamsPerChunk = 100
	MaxChunkBytes   = 900 * 1024
	ackTimeout      = 10 * time.Second
	ackRetries      = 3
)

var (
	ErrSyncNoAck    = errors.New("cams_ack kelmadi (3 urinish)")
	ErrSyncStale    = errors.New("server eski epoch'ni rad etdi (stale_epoch)")
	ErrSyncRejected = errors.New("cams_sync rad etildi")
)

// SyncSender — xabar yuborish (Session shu interfeysni qanoatlantiradi).
type SyncSender interface {
	Send(typ string, data any) error
}

// Syncer — kamera ro'yxatini serverga yetkazadi.
type Syncer struct {
	sess SyncSender
	st   *statestore.State
	clk  clock.Clock
	log  *slog.Logger

	ackCh chan proto.CamsAck // OnCamsAck orqali to'ldiriladi

	mu     sync.Mutex
	pushMu sync.Mutex // chunk'lar ketma-ketligi uchun
	gen    int64      // "restart" hisoblagichi: eski sync natijasi e'tiborga olinmaydi
}

// NewSyncer — syncer quradi (clk nil → Real, log nil → Discard).
func NewSyncer(sess SyncSender, st *statestore.State, clk clock.Clock, log *slog.Logger) *Syncer {
	if clk == nil {
		clk = clock.Real{}
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Syncer{sess: sess, st: st, clk: clk, log: log,
		ackCh: make(chan proto.CamsAck, 8)}
}

// OnCamsAck — dispatch'dan chaqiriladi (§4.3 javobi).
func (s *Syncer) OnCamsAck(a proto.CamsAck) {
	select {
	case s.ackCh <- a:
	default: // katta kechikgan ack'lar tashlab yuboriladi
	}
}

// SyncIfRequired — §6.5: epoch farq qilsa yoki server `needs_full_sync` desa
// to'liq sync yuboradi; bo'lmasa jimgina qaytadi. Joriy sync bo'lsa uni
// bekor qilib qaytadan boshlaydi (chunk'lar idempotent, §4.2).
func (s *Syncer) SyncIfRequired(w proto.Welcome) error {
	epoch := s.st.CamsEpoch
	if !w.NeedsFullSync && w.CamsEpoch == epoch && len(s.st.PendingDeletes) == 0 {
		return nil // drift yo'q — to'liq sync kerak emas (§6.5.5)
	}
	return s.Push(epoch)
}

// Restart — joriy sync'ning natijasini bekor qiladi (chunk'lar serverda
// idempotent, shuning uchun yangi sync xavfsiz qaytadan boshlanadi).
func (s *Syncer) Restart() {
	s.mu.Lock()
	s.gen++
	s.mu.Unlock()
	s.drainAcks()
}

// currentGen — Push boshlangan paytdagi generatsiya.
func (s *Syncer) currentGen() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}

// Push — to'liq sync yuboradi (full=true): server `done: true` dan keyin
// ro'yxatda bo'lmagan kameralarni o'chiradi (§4.2).
func (s *Syncer) Push(epoch int64) error {
	camsList := s.st.SnapshotCameras()
	pending := s.st.SnapshotPending()
	entries := make([]proto.CameraEntry, 0, len(camsList)+len(pending))
	for _, c := range camsList {
		entries = append(entries, entryFromCamera(c))
	}
	for _, id := range pending {
		entries = append(entries, proto.CameraEntry{Op: "delete", ID: id})
	}
	return s.push(epoch, true, entries)
}

// PushDelta — faqat berilgan id'lar bo'yicha upsert + navbatdagi delete'lar.
func (s *Syncer) PushDelta(epoch int64, ids []string) error {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	camsList := s.st.SnapshotCameras()
	pending := s.st.SnapshotPending()
	entries := make([]proto.CameraEntry, 0, len(ids)+len(pending))
	for _, c := range camsList {
		if want[c.ID] {
			entries = append(entries, entryFromCamera(c))
		}
	}
	for _, id := range pending {
		entries = append(entries, proto.CameraEntry{Op: "delete", ID: id})
	}
	return s.push(epoch, false, entries)
}

// pushMu — chunk'lar ketma-ket yuborilishi uchun (audit, 2026-11-21):
// ikki parallel sync o'zaro bir-birining ack'ini "yeyishi", ack kutish
// keshi (drainAcks) aralashishi va pending_deletes erta tozalanishi
// mumkin edi.
func (s *Syncer) pushMuLock() { s.pushMu.Lock() }

// push — chunk'larga bo'lib, har biridan keyin ack kutadi.
func (s *Syncer) push(epoch int64, full bool, entries []proto.CameraEntry) error {
	s.pushMu.Lock()
	defer s.pushMu.Unlock()
	gen := s.currentGen()
	chunks := chunkEntries(entries)
	if len(chunks) == 0 && !full {
		return nil // yuboradigan hech narsa yo'q
	}
	for i, ch := range chunks {
		env := proto.CamsSync{
			Epoch: epoch, Full: full,
			SeqNo: i + 1, ChunkCount: len(chunks),
			Done: i == len(chunks)-1, Cameras: ch,
		}
		if err := s.sendWithAck(env); err != nil {
			if s.currentGen() != gen {
				return context.Canceled // Restart() chaqirildi — natija e'tiborga olinmaydi
			}
			return err
		}
	}
	// Barcha chunk'lar tasdiqlandi → delete navbati tozalanadi (§4.3)
	s.st.Mutate(func() { s.st.ClearPendingDeletes() })
	if err := s.st.Save(); err != nil {
		s.log.Warn("pending_deletes tozalash saqlanmadi", "err", err.Error())
	}
	s.log.Info("cams_sync yetkazildi", "epoch", epoch, "chunklar", len(chunks), "full", full)
	return nil
}

// sendWithAck — bitta chunk'ni yuborib, ack kutadi; 10 s'da qayta yuboradi (×3).
//
// Deterministik bo'lishi uchun taymaut **bitta** obyektda: fake clock bilan
// Advance() shu obyektga signal beradi, keyingi urinishda esa yangi taymaut
// ochiladi (eski timer'lar yig'ilmaydi — aks holda test tugaganini kutish kerak).
func (s *Syncer) sendWithAck(env proto.CamsSync) error {
	s.drainAcks()
	// Taymaut oldindan ochiladi: `timer` o'zgaruvchisi select'dan KEYIN
	// yangilanadi, shuning uchun undan oldin chaqiriladigan Advance() eski
	// taymautga tegmaydi (fake clock uchun muhim).
	timer := s.clk.After(ackTimeout)
	for attempt := 0; ; attempt++ {
		if err := s.sess.Send(proto.MsgCamsSync, env); err != nil {
			if attempt == 0 {
				return fmt.Errorf("cams_sync yuborish: %w", err)
			}
			return fmt.Errorf("cams_sync qayta yuborish: %w", err)
		}
		select {
		case a := <-s.ackCh:
			if a.Epoch != env.Epoch {
				// begona/eski epoch javobi — davom etamiz, taymaut saqlanadi
				s.log.Debug("cams_sync: epoch mos emas", "kutildi", env.Epoch, "keldi", a.Epoch)
				continue
			}
			if a.OK {
				return nil
			}
			if a.Reason != nil && *a.Reason == "stale_epoch" {
				return ErrSyncStale
			}
			return fmt.Errorf("%w: %v", ErrSyncRejected, a.Reason)
		case <-timer:
			if attempt >= ackRetries {
				return ErrSyncNoAck
			}
			s.log.Warn("cams_sync: ack kelmadi, qayta yuboriladi",
				"seq_no", env.SeqNo, "urinish", attempt+1)
			timer = s.clk.After(ackTimeout)
		}
	}
}

// drainAcks — oldingi sync'dan qolgan kechikkan ack'larni tozalaydi.
func (s *Syncer) drainAcks() {
	for {
		select {
		case <-s.ackCh:
		default:
			return
		}
	}
}

// chunkEntries — ≤100 kamera va ≤900 KiB chegarasi bilan bo'ladi (§2, §4.2).
func chunkEntries(in []proto.CameraEntry) [][]proto.CameraEntry {
	var out [][]proto.CameraEntry
	cur := []proto.CameraEntry{}
	for _, e := range in {
		// O'lchamni taxminiy emas, aniq hisoblaymiz: qo'shib ko'ramiz.
		trial := append(cur, e)
		if len(trial) > MaxCamsPerChunk {
			out = append(out, cur)
			cur = []proto.CameraEntry{e}
			continue
		}
		if n, err := json.Marshal(proto.CamsSync{Cameras: trial}); err == nil && n != nil && len(n) > MaxChunkBytes {
			if len(cur) == 0 {
				// bitta kamera ham katta — yagona chunk sifatida yuboriladi
				out = append(out, trial)
				cur = []proto.CameraEntry{}
				continue
			}
			out = append(out, cur)
			cur = []proto.CameraEntry{e}
			continue
		}
		cur = trial
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

// entryFromCamera — §4.2 sxemasi. Parol hech qachon kirmaydi: `rtsp_masked` (§10.2).
func entryFromCamera(c cams.Camera) proto.CameraEntry {
	st := string(c.Status)
	if st == "" {
		st = string(cams.StatusUnknown)
	}
	return proto.CameraEntry{
		Op:         "upsert",
		ID:         c.ID,
		Name:       c.Name,
		RTSPMasked: cams.NewRTSPURL(c.Host, c.Port, c.MainPath).Masked(),
		Profiles:   proto.CameraProfilePair{Main: c.MainPath, Sub: c.SubPath},
		Codecs:     proto.CameraProfilePair{Main: c.CodecMain, Sub: c.CodecSub},
		BFrame:     proto.CameraBoolPair{Main: c.BframeMain, Sub: c.BframeSub},
		OnvifOK:    c.OnvifOK,
		Status:     st,
		Position:   c.Position,
		Enabled:    c.Enabled,
	}
}
