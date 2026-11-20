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
	}
	h.mu.Unlock()

	if cs.Epoch < stored {
		reason := "stale_epoch"
		return c.sendEnvelope(proto.MsgCamsAck, proto.CamsAck{
			Epoch: cs.Epoch, OK: false, Reason: &reason})
	}

	applied := 0
	var firstErr error
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
			})
		}
		if err != nil && firstErr == nil && !isNotFound(err) {
			firstErr = err
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
	h.mu.Unlock()

	if !cs.Done {
		return nil // orasidagi chunk — ack tugashida
	}
	ack := proto.CamsAck{Epoch: cs.Epoch, OK: firstErr == nil, Applied: total}
	if firstErr != nil {
		r := "apply_failed"
		ack.Reason = &r
	}
	return c.sendEnvelope(proto.MsgCamsAck, ack)
}

// deleteCamera — kamera o'chirish + layout slotlarini bo'shatish.
// (favorites — FK cascade, avtomatik; kamera yo'q bo'lsa OK hisoblaymiz:
// idempotent sync.)
func (c *Conn) deleteCamera(cameraID string) error {
	err := c.hub.registry.DeleteCamera(cameraID)
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
