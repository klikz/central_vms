package publish

import (
	"errors"
	"net/url"

	"central-vms/client/internal/proto"
)

// media_start → transport rejasi (§5.2, §7.2).
// URL qurishda string konkatensiya YO'Q — url.URL/url.Values (§0.2).

// Rejim xatolari — media_stopped reason'ga §13 bo'yicha bog'lanadi.
var (
	ErrNoCamera      = errors.New("publish: camera_id bo'sh")
	ErrNoToken       = errors.New("publish: publish.token bo'sh")
	ErrNoURL         = errors.New("publish: ingest uchun URL bo'sh")
	ErrUnknownIngest = errors.New("publish: noma'lum ingest")
	errParse         = errors.New("publish: URL parse")
)

// StartPlan — tanlangan ingest, URL va token tashish usuli.
type StartPlan struct {
	Key         Key    // publish_key (§9.1) — tanlangan ingest bilan
	Ingest      string // "whip" | "rtsp_tcp"
	URL         string // token kiritilgan yakuniy URL
	HeaderName  string // bo'sh → header ishlatilmaydi
	HeaderValue string // "Bearer <token>"
}

// ParseStart — §5.2 qoidalari:
//   - ingest whip     → urls.whip + Bearer header;
//   - ingest rtsp_tcp → urls.rtsp + ?<query_name>=;
//   - transcode.by=client → urls.whip (default) yoki urls.rtsp_tc.
func ParseStart(ms proto.MediaStart) (StartPlan, error) {
	if ms.CameraID == "" {
		return StartPlan{}, ErrNoCamera
	}
	if ms.Publish.Token == "" {
		return StartPlan{}, ErrNoToken
	}

	ingest, raw := ms.Ingest, ""
	switch {
	case ms.Transcode.By == "client":
		// Manba H.265, natija H.264 — qaysi URL kelgani agent tanlaydi.
		if ms.Publish.URLs.WHIP != "" {
			ingest, raw = "whip", ms.Publish.URLs.WHIP
		} else {
			ingest, raw = "rtsp_tcp", ms.Publish.URLs.RTSPTC
		}
	case ingest == "whip":
		raw = ms.Publish.URLs.WHIP
	case ingest == "rtsp_tcp":
		raw = ms.Publish.URLs.RTSP
	default:
		return StartPlan{}, ErrUnknownIngest
	}
	if raw == "" {
		return StartPlan{}, ErrNoURL
	}

	u, err := url.Parse(raw)
	if err != nil {
		return StartPlan{}, errParse
	}

	p := StartPlan{
		Key:    Key{CameraID: ms.CameraID, Profile: ms.Profile, Ingest: ingest},
		Ingest: ingest,
	}
	if ingest == "whip" {
		// HTTP(S) → Authorization: Bearer (§7.2); URL o'zgarmaydi.
		hn := ms.Publish.HeaderName
		if hn == "" {
			hn = "Authorization"
		}
		p.URL = u.String()
		p.HeaderName = hn
		p.HeaderValue = "Bearer " + ms.Publish.Token
		return p, nil
	}

	// RTSP → URL query orqali (§7.2). Mavjud query saqlanadi, token qo'shiladi.
	q := u.Query()
	qn := ms.Publish.QueryName
	if qn == "" {
		qn = "token"
	}
	q.Set(qn, ms.Publish.Token)
	u.RawQuery = q.Encode()
	p.URL = u.String()
	return p, nil
}
