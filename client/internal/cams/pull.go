package cams

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"central-vms/client/internal/clock"
	"central-vms/client/internal/redact"
	"central-vms/client/internal/rtspclient"
)

// C3.5 (§8, §9.3): kamera RTSP pull — DESCRIBE → SETUP(interleaved TCP)
// → PLAY → RTP o'qish → AU'lar. Uzilishda 3 marta 2 s interval bilan
// qayta urinish, bo'lmasa camera_offline (§9.3).

// ErrCameraOffline — barcha retrylar tugadi (§13 camera_offline).
var ErrCameraOffline = errors.New("cams: kamera RTSP ulanib bo'lmadi")

// AU — bitta H.264 access unit (NAL'lar) + RTP timestampi.
type AU struct {
	Nals      [][]byte
	Timestamp uint32
}

// PullOptions — pull sozlamalari.
type PullOptions struct {
	URL        string
	User, Pass string        // kamera RTSP hisobi (§10.1: parol keystore'dan)
	Clock      clock.Clock   // nil → Real (retry kutishlari uchun)
	Retries    int           // ≤0 → 3 (§9.3)
	RetryDelay time.Duration // ≤0 → 2 s (§9.3)
	Log        *slog.Logger  // nil → Discard
}

// Pull — oqimni ochadi va sink'ga AU'larni beradi. ctx bekor qilinsa
// context.Canceled (mayin to'xtatish). Uzilishlar retry bilan qoplanadi.
func Pull(ctx context.Context, o PullOptions, sink func(AU) error) error {
	if o.Clock == nil {
		o.Clock = clock.Real{}
	}
	if o.Retries <= 0 {
		o.Retries = 3
	}
	if o.RetryDelay <= 0 {
		o.RetryDelay = 2 * time.Second
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}

	var last error
	for attempt := 0; attempt <= o.Retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt > 0 {
			o.Log.Warn("rtsp_pull_qayta_ulanish", "urinish", attempt, "url", redact.String(o.URL), "err", fmt.Sprint(last))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-o.Clock.After(o.RetryDelay):
			}
		}
		err := pullOnce(ctx, o, sink)
		if err == nil || errors.Is(err, context.Canceled) {
			return err
		}
		last = err
	}
	return ErrCameraOffline
}

func pullOnce(ctx context.Context, o PullOptions, sink func(AU) error) error {
	conn, err := rtspclient.Dial(ctx, o.URL, rtspclient.Options{User: o.User, Pass: o.Pass})
	if err != nil {
		return err
	}
	defer conn.Close()

	sdpBytes, err := conn.Describe(ctx)
	if err != nil {
		return err
	}
	sdp, err := ParseSDP(string(sdpBytes))
	if err != nil {
		return err
	}
	streams := sdp.VideoStreams()
	if len(streams) == 0 {
		return errors.New("cams: SDP'da video oqim yo'q")
	}
	v := streams[0]
	// Media bandi (xulosa emas) kerak: sprop parameter set'lari shu yerda.
	vmedia := Media{}
	for _, mm := range sdp.Medias {
		if mm.Kind == "video" {
			vmedia = mm
			break
		}
	}

	rtpCh, _, err := conn.SetupTrack(ctx, v.Control, 0, 1)
	if err != nil {
		return err
	}
	if err := conn.Play(ctx, "npt=0.0-"); err != nil {
		return err
	}

	// Parameter set'larni SDP'dan olamiz (sprop) — manba ularni oqimda
	// takrorlamasa, quyidagi sikl qo'shadi (sprop.go, 2026-11-20 xatosi).
	h265 := h265Stream(vmedia)
	param := spropNALs(vmedia)

	var dep interface {
		Add(rtspclient.RTPPacket) ([][]byte, bool)
	} = &rtspclient.H264Depacketizer{}
	if h265 {
		dep = rtspclient.NewH265Depacketizer()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ch, raw, err := conn.ReadInterleaved()
		if err != nil {
			return err
		}
		if int(ch) != rtpCh { // RTCP yoki boshqa kanal — o'tkazamiz
			continue
		}
		p, err := rtspclient.ParseRTP(raw)
		if err != nil {
			continue // buzilgan paket — tashlab ketamiz (+discardcorrupt ruhi)
		}
		if nals, ok := dep.Add(p); ok {
			if len(param) > 0 && keyKadr(nals, h265) && !paramSetBor(nals, h265) {
				nals = append(append([][]byte(nil), param...), nals...)
			}
			if err := sink(AU{Nals: nals, Timestamp: p.Timestamp}); err != nil {
				return err
			}
		}
	}
}
