package publish

import "context"

// §12.3: publish muvaffaqiyatsiz bo'lsa — darhol to'liq kodek probe
// (trigger publish_fail) va codec_probe yuborish; natijada
// media_stopped{codec_mismatch}.

// CodecProbePayload — §4.5 codec_probe maydonlari (kichik to'plam;
// to'liq payload agent tomonidan quriladi).
type CodecProbePayload map[string]any

// ProbeGate — cams.CodecScheduler bilan mos interfeys (§12.4: band
// paytda probe o'tkazilmaydi).
type ProbeGate interface {
	TryStart(id, trigger string) bool
	Finish(id string)
}

// HandleCodecFailure — publish fail'dan keyin chaqiriladi.
// Qaytaradigan reason §13 bo'yicha media_stopped uchun.
func HandleCodecFailure(ctx context.Context, gate ProbeGate,
	probe func(ctx context.Context, camID, url string) (CodecProbePayload, error),
	send func(camID string, p CodecProbePayload) error,
	camID, url string) string {

	if gate != nil && gate.TryStart(camID, "publish_fail") {
		payload, err := probe(ctx, camID, url)
		gate.Finish(camID) // lock qaytariladi — xato bo'lsa ham (§12.4)
		if err == nil && send != nil {
			_ = send(camID, payload)
		}
	}
	return ReasonCodecMismatch
}
