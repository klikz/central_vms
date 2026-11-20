package publish

import (
	"errors"
	"testing"

	"central-vms/client/internal/proto"
)

// §5.2/§7.2: ingest whip → urls.whip + Bearer header;
// ingest rtsp_tcp → urls.rtsp + ?token=;
// transcode.by=client → urls.whip (default) yoki urls.rtsp_tc.

func baseStart() proto.MediaStart {
	return proto.MediaStart{
		SessionID: "se_1", CameraID: "cam1", Profile: "main", Kind: "play",
		TTLS: 60, Ingest: "whip",
		Publish: proto.Publish{
			Token: "T0KEN-xyz",
			URLs: proto.PublishURLs{
				WHIP:   "http://10.0.0.5:8889/cam/ag_1/cam1/whip",
				RTSP:   "rtsp://10.0.0.5:8554/cam/ag_1/cam1",
				RTSPTC: "rtsp://10.0.0.5:8554/tc/ag_1/cam1",
			},
			HeaderName: "Authorization", QueryName: "token",
		},
	}
}

func TestStartParse_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*proto.MediaStart)
		wantURL  string
		wantHdrN string
		wantHdrV string
		wantIng  string
		wantErr  error
	}{
		{
			name:     "whip_bearer",
			mutate:   func(m *proto.MediaStart) {},
			wantURL:  "http://10.0.0.5:8889/cam/ag_1/cam1/whip",
			wantHdrN: "Authorization", wantHdrV: "Bearer T0KEN-xyz", wantIng: "whip",
		},
		{
			name:     "rtsp_tcp_query",
			mutate:   func(m *proto.MediaStart) { m.Ingest = "rtsp_tcp" },
			wantURL:  "rtsp://10.0.0.5:8554/cam/ag_1/cam1?token=T0KEN-xyz",
			wantHdrN: "", wantIng: "rtsp_tcp",
		},
		{
			name: "client_transcode_default_whip",
			mutate: func(m *proto.MediaStart) {
				m.Ingest = "rtsp_tcp"
				m.Transcode = proto.Transcode{Mode: "client", By: "client", Backend: "gpu"}
			},
			wantURL:  "http://10.0.0.5:8889/cam/ag_1/cam1/whip",
			wantHdrN: "Authorization", wantHdrV: "Bearer T0KEN-xyz", wantIng: "whip",
		},
		{
			name: "client_transcode_only_rtsp_tc",
			mutate: func(m *proto.MediaStart) {
				m.Ingest = "rtsp_tcp"
				m.Transcode = proto.Transcode{Mode: "client", By: "client"}
				m.Publish.URLs.WHIP = ""
			},
			wantURL: "rtsp://10.0.0.5:8554/tc/ag_1/cam1?token=T0KEN-xyz",
			wantIng: "rtsp_tcp",
		},
		{
			name: "custom_names",
			mutate: func(m *proto.MediaStart) {
				m.Ingest = "rtsp_tcp"
				m.Publish.QueryName = "authtok"
			},
			wantURL: "rtsp://10.0.0.5:8554/cam/ag_1/cam1?authtok=T0KEN-xyz",
			wantIng: "rtsp_tcp",
		},
		{
			name: "existing_query_preserved",
			mutate: func(m *proto.MediaStart) {
				m.Ingest = "rtsp_tcp"
				m.Publish.URLs.RTSP = "rtsp://10.0.0.5:8554/cam/ag_1/cam1?x=1"
			},
			wantURL: "rtsp://10.0.0.5:8554/cam/ag_1/cam1?token=T0KEN-xyz&x=1",
			wantIng: "rtsp_tcp",
		},
		{
			name:    "noma'lum_ingest",
			mutate:  func(m *proto.MediaStart) { m.Ingest = "hls" },
			wantErr: ErrUnknownIngest,
		},
		{
			name:    "token_yoq",
			mutate:  func(m *proto.MediaStart) { m.Publish.Token = "" },
			wantErr: ErrNoToken,
		},
		{
			name:    "url_yoq",
			mutate:  func(m *proto.MediaStart) { m.Publish.URLs.WHIP = "" },
			wantErr: ErrNoURL,
		},
		{
			name:    "url_noto'g'ri",
			mutate:  func(m *proto.MediaStart) { m.Publish.URLs.WHIP = "http://%zz" },
			wantErr: errParse,
		},
		{
			name:    "camera_id_yoq",
			mutate:  func(m *proto.MediaStart) { m.CameraID = "" },
			wantErr: ErrNoCamera,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ms := baseStart()
			tc.mutate(&ms)
			p, err := ParseStart(ms)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, %v kutilardi", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("kutilmagan xato: %v", err)
			}
			if p.URL != tc.wantURL {
				t.Errorf("URL = %q, %q kutilardi", p.URL, tc.wantURL)
			}
			if p.HeaderName != tc.wantHdrN || p.HeaderValue != tc.wantHdrV {
				t.Errorf("header = %q: %q, kutilardi %q: %q",
					p.HeaderName, p.HeaderValue, tc.wantHdrN, tc.wantHdrV)
			}
			if p.Ingest != tc.wantIng {
				t.Errorf("ingest = %q, %q kutilardi", p.Ingest, tc.wantIng)
			}
			if p.Key.CameraID != ms.CameraID || p.Key.Profile != ms.Profile {
				t.Errorf("key = %+v", p.Key)
			}
		})
	}
}

// §9.1: publish_key ingest bilan — client_transcode whip'ga o'tsa, key whip bo'ladi.
func TestStartParse_KeyMatchesChosenIngest(t *testing.T) {
	ms := baseStart()
	ms.Ingest = "rtsp_tcp"
	ms.Transcode = proto.Transcode{By: "client"}
	p, err := ParseStart(ms)
	if err != nil {
		t.Fatal(err)
	}
	if p.Key.Ingest != "whip" {
		t.Errorf("Key.Ingest = %q, whip kutilardi (tanlangan ingest)", p.Key.Ingest)
	}
}
