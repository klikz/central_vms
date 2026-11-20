package publish

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"central-vms/client/internal/proto"
	"central-vms/client/internal/rtspclient"
)

// §5.2/§8.1: H.264 → WHIP publish (pion v4): POST urls.whip + Bearer,
// SDP offer/answer. Test sunucusi — ikkinchi pion PC (tarmoq talab qilmaydi).

// whipTestServer — offer'ni tekshiradi, pion bilan javob (answer) qaytaradi.
func whipTestServer(t *testing.T, authHeader string, status int) (*httptest.Server, *string) {
	t.Helper()
	gotOffer := new(string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, POST kutilardi", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/sdp") {
			t.Errorf("Content-Type = %q", ct)
		}
		if auth := r.Header.Get("Authorization"); auth != authHeader {
			t.Errorf("Authorization = %q, %q kutilardi", auth, authHeader)
		}
		body, _ := io.ReadAll(r.Body)
		offer := string(body)
		*gotOffer = offer

		if status != 0 && status != http.StatusCreated {
			w.WriteHeader(status)
			return
		}
		// Javob: ikkinchi pion PC offer'ni qabul qilib answer yaratadi.
		pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		defer pc.Close()
		if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offer}); err != nil {
			t.Errorf("server SetRemoteDescription: %v", err)
			w.WriteHeader(500)
			return
		}
		ans, err := pc.CreateAnswer(nil)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		if err := pc.SetLocalDescription(ans); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/sdp")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, ans.SDP)
	}))
	t.Cleanup(srv.Close)
	return srv, gotOffer
}

func whipPlan(t *testing.T, srvURL string) StartPlan {
	t.Helper()
	ms := baseStart()
	ms.Publish.URLs.WHIP = srvURL + "/cam/ag_1/cam1/whip"
	p, err := ParseStart(ms)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWhip_OfferAnswerHandshake(t *testing.T) {
	srv, gotOffer := whipTestServer(t, "Bearer T0KEN-xyz", 0)
	plan := whipPlan(t, srv.URL)

	w, err := NewWhip(context.Background(), plan)
	if err != nil {
		t.Fatalf("NewWhip: %v", err)
	}
	defer w.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.Publish(ctx); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Offer H.264 video m-line'ni o'z ichiga olgan.
	if gotOffer == nil || !strings.Contains(*gotOffer, "m=video") ||
		!strings.Contains(*gotOffer, "H264") {
		t.Errorf("offer'da H264 video yo'q: %v", gotOffer)
	}
	// Signalization to'liq: remote description qo'llangan.
	if st := w.SignalingState(); st != webrtc.SignalingStateStable {
		t.Errorf("SignalingState = %v, Stable kutilardi", st)
	}
	// §4.6: transport udp (ICE/UDP yo'li).
	if tr := w.Transport(); tr != "udp" {
		t.Errorf("Transport = %q, udp kutilardi", tr)
	}
}

// 401 → token_rejected (§13) — StatusError orqali.
func TestWhip_401TokenRejected(t *testing.T) {
	srv, _ := whipTestServer(t, "Bearer T0KEN-xyz", http.StatusUnauthorized)
	plan := whipPlan(t, srv.URL)

	w, err := NewWhip(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	err = w.Publish(context.Background())
	if err == nil {
		t.Fatal("401 kutilardi")
	}
	if got := ReasonFor(err, SideServer); got != ReasonTokenRejected {
		t.Errorf("reason = %q, token_rejected kutilardi (err=%v)", got, err)
	}
	var se *rtspclient.StatusError
	if !errors.As(err, &se) || se.Code != 401 {
		t.Errorf("StatusError kutilardi, topildi: %v", err)
	}
}

// Buzilgan answer SDP → aniq xato (panic yo'q).
func TestWhip_BuzilganAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/sdp")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, "bu SDP emas")
	}))
	defer srv.Close()
	plan := whipPlan(t, srv.URL)

	w, err := NewWhip(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Publish(context.Background()); err == nil {
		t.Fatal("buzilgan answer uchun xato kutilardi")
	}
}

// media_start fixture'i bilan to'liq reja (golden emas — integratsion tutashuv).
func TestWhip_FromFixturePlan(t *testing.T) {
	ms := proto.MediaStart{}
	b, err := os.ReadFile("../../testdata/protocol/media_start.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &ms); err != nil {
		t.Fatal(err)
	}
	p, err := ParseStart(ms)
	if err != nil {
		t.Fatal(err)
	}
	if p.Ingest != "whip" || p.HeaderValue != "Bearer Zm9vYmFyYmF6cXV1eTEyMzQ1Njc4OTA" {
		t.Errorf("plan: %+v", p)
	}
}
