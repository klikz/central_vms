// S1.10: cams_sync — chunk'lab qabul, epoch tekshiruvi, op: upsert|delete,
// cascade (favorites FK + layout slotlari) va cams_ack (protocol.md §4.2-4.3, §6).
package wsagent

import (
	"errors"

	"central-vms/server/internal/proto"
	"central-vms/server/internal/registry"
)

// handleCamsSync — bitta chunk: epoch eski bo'lsa stale_epoch; aks holda
// amallar bajariladi, chunk tugaganda (done) cams_ack yuboriladi.
func (c *Conn) handleCamsSync(cs proto.CamsSync) error {
	h := c.hub
	h.mu.Lock()
	stored := h.epochs[c.agentID]
	if cs.SeqNo <= 1 || cs.Epoch != stored {
		h.syncApplied[c.agentID] = 0 // yangi sinxron boshlandi
		h.syncSeen[c.agentID] = map[string]bool{}
	}
	h.mu.Unlock()

	if cs.Epoch < stored {
		reason := "stale_epoch"
		return c.sendEnvelope(proto.MsgCamsAck, proto.CamsAck{
			Epoch: cs.Epoch, OK: false, Reason: &reason})
	}

	applied := 0
	var firstErr error
	seen := make(map[string]bool, len(cs.Cameras))
	if h.syncSeen[c.agentID] == nil {
		h.mu.Lock()
		h.syncSeen[c.agentID] = map[string]bool{}
		h.mu.Unlock()
	}
	for _, cam := range cs.Cameras {
		var err error
		switch cam.Op {
		case "delete":
			err = c.deleteCamera(cam.ID)
		default: // upsert
			err = h.registry.UpsertCamera(c.clientID, registry.SyncCamera{
				ID: cam.ID, Name: cam.Name, RTSPMasked: cam.RTSPMasked,
				CodecMain: cam.Codecs.Main, CodecSub: cam.Codecs.Sub,
				BFrameMain: cam.BFrame.Main, BFrameSub: cam.BFrame.Sub,
				// 1.0-draft.12: `position` (§4.2) va ONVIF manzili sync'da
				// kelardi, lekin bu yerga yetkazilmasdi — serverda
				// kameralar doim 0-tartibda qolardi.
				Position: cam.Position,
				// §4.2: holat sync'da ham keladi. Bo'sh kelsa
				// UpsertCamera mavjud holatni saqlab qoladi — avval
				// to'liq sync `cam_status`dagi "online"ni bosib
				// "offline" qo'yardi (2026-11-21 kuzatuvi).
				Status: cam.Status,
			})
		}
		if err == nil && cam.Op != "delete" {
			seen[cam.ID] = true
			h.mu.Lock()
			h.syncSeen[c.agentID][cam.ID] = true
			h.mu.Unlock()
		}
		// ESKISI: `err != nil && firstErr == nil` — birinchi xatodan
		// keyingi muvaffaqiyatsiz amallar `applied++` bilan sanalib,
		// cams_ack'dagi `applied` shishirilgan holda `ok:true` ketardi (§4.3).
		if err != nil && !isNotFound(err) {
			if firstErr == nil {
				firstErr = err
			}
			h.lg.Warn("cams_sync amali", "agent_id", c.agentID, "cam", cam.ID, "op", cam.Op, "err", err)
			continue
		}
		applied++
	}

	h.mu.Lock()
	h.syncApplied[c.agentID] += applied
	total := h.syncApplied[c.agentID]
	if cs.Epoch > stored {
		h.epochs[c.agentID] = cs.Epoch
	}
	if cs.Done {
		h.syncApplied[c.agentID] = 0
	}
	if cs.Done {
		defer delete(h.syncSeen, c.agentID)
	}
	h.mu.Unlock()

	if !cs.Done {
		return nil // orasidagi chunk — ack tugashida
	}
	// §4.2: `full:true` to'liq ro'yxat degani — bu sync'da ko'rinmagan
	// kameralar agentda yo'q, serverda esa qolgan. Bu pruning umuman
	// bajarilmagani uchun agentda o'chirilgan kamera serverda abadiy
	// "o'lik kamera" bo'lib turardi (UI ro'yxati, layout slotlari band).
	if cs.Full && firstErr == nil {
		if n := c.pruneMissingCams(c.accumulatedSeen()); n > 0 {
			h.lg.Info("cams_sync: ko'rinmagan kameralar o'chirildi",
				"agent_id", c.agentID, "soni", n)
		}
	}
	// Ro'yxat o'zgardi — UI'ga signal (Monitor sahifasi qayta o'qiydi):
	// avval qo'shilgan kamera refresh qilingandagina ko'rinardi (2026-11-20).
	c.hub.uiEvent(EvCamsChanged, map[string]any{
		"client_id": c.clientID, "agent_id": c.agentID, "epoch": cs.Epoch,
	})
	ack := proto.CamsAck{Epoch: cs.Epoch, OK: firstErr == nil, Applied: total}
	if firstErr != nil {
		r := "apply_failed"
		ack.Reason = &r
	}
	return c.sendEnvelope(proto.MsgCamsAck, ack)
}

// accumulatedSeen — chunk'lar bo'yicha to'plangan ko'rish to'plami.
// ESKISI: pruning har chunk'ning o'z ID'lariga qarab edi — oxirgi
// chunk'da faqat shu chunk kameralari "ko'rilgan" bo'lib, qolganlari
// o'chib ketardi (test: 3 kamera 1 ga qisqardi).
func (c *Conn) accumulatedSeen() map[string]bool {
	c.hub.mu.Lock()
	defer c.hub.mu.Unlock()
	out := make(map[string]bool, len(c.hub.syncSeen[c.agentID]))
	for id, v := range c.hub.syncSeen[c.agentID] {
		out[id] = v
	}
	return out
}

// pruneMissingCams — to'liq sync'da kelmay qolgan kameralarni o'chirish.
func (c *Conn) pruneMissingCams(seen map[string]bool) int {
	list, err := c.hub.registry.ListCamerasByClient(c.clientID)
	if err != nil {
		c.hub.lg.Warn("cams_sync pruning ro'yxat", "agent_id", c.agentID, "err", err)
		return 0
	}
	n := 0
	for _, cam := range list {
		if seen[cam.ID] {
			continue
		}
		if err := c.deleteCamera(cam.ID); err != nil {
			c.hub.lg.Warn("cams_sync pruning", "cam", cam.ID, "err", err)
			continue
		}
		n++
	}
	return n
}

// deleteCamera — kamera o'chirish + layout slotlarini bo'shatish.
// (favorites — FK cascade, avtomatik; kamera yo'q bo'lsa OK hisoblaymiz:
// idempotent sync.)
func (c *Conn) deleteCamera(cameraID string) error {
	err := c.hub.registry.DeleteCameraForClient(c.clientID, cameraID)
	if err != nil && isNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return c.hub.registry.ClearCameraSlots(cameraID)
}

func isNotFound(err error) bool {
	return err != nil && errors.Is(err, registry.ErrNotFound)
}
