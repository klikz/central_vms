package cams

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func readFixtureBytes(t *testing.T, parts ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(fixturePath(t, parts...))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return b
}

// onvifFixtureServer — so'rov Body'sidagi Action'ga qarab fixture javob.
func onvifFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/soap+xml")
		switch {
		case strings.Contains(string(body), "GetProfiles"):
			_, _ = w.Write(readFixtureBytes(t, "onvif", "getprofiles.xml"))
		case strings.Contains(string(body), "GetStreamUri"):
			_, _ = w.Write(readFixtureBytes(t, "onvif", "getstreamuri.xml"))
		case strings.Contains(string(body), "SetVideoEncoderConfiguration"):
			_, _ = w.Write(readFixtureBytes(t, "onvif", "setvideoencoder.xml"))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
}

func TestONVIF_GetProfiles(t *testing.T) {
	srv := onvifFixtureServer(t)
	defer srv.Close()
	c := NewOnvifClient(srv.URL, "admin", "pass")
	profs, err := c.GetProfiles(context.Background())
	if err != nil {
		t.Fatalf("GetProfiles: %v", err)
	}
	if len(profs) != 2 {
		t.Fatalf("profillar = %d, 2 kutilardi", len(profs))
	}
	if profs[0].Token != "Profile_0000" || profs[1].Token != "Profile_0001" {
		t.Errorf("tokenlar: %+v", profs)
	}
	if profs[1].VideoEncoderToken != "VideoEncoderConfig_0001" {
		t.Errorf("encoder token = %q", profs[1].VideoEncoderToken)
	}
}

func TestONVIF_GetStreamUri(t *testing.T) {
	srv := onvifFixtureServer(t)
	defer srv.Close()
	c := NewOnvifClient(srv.URL, "admin", "pass")
	uri, err := c.GetStreamUri(context.Background(), "Profile_0001")
	if err != nil {
		t.Fatalf("GetStreamUri: %v", err)
	}
	if !strings.HasPrefix(uri, "rtsp://192.168.10.31:554/cam/realmonitor") || !strings.Contains(uri, "subtype=1") {
		t.Errorf("uri = %q (sub profil subtype=1 bo'lishi kerak)", uri)
	}
}

func TestONVIF_SetVideoEncoderConfig(t *testing.T) {
	srv := onvifFixtureServer(t)
	defer srv.Close()
	c := NewOnvifClient(srv.URL, "admin", "pass")
	err := c.SetVideoEncoderConfiguration(context.Background(), "VideoEncoderConfig_0001", EncoderParams{
		Encoding: "H264", Width: 1280, Height: 720, FPS: 10,
		BitrateKbps: 700, GovLengthSec: 2, RateControl: "CBR",
	})
	if err != nil {
		t.Fatalf("SetVideoEncoderConfiguration: %v", err)
	}
}

// SOAP Fault (auth xatosi) → aniq xato, jimgina o'tkazib yuborilmaydi.
func TestONVIF_FaultAniqXato(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(readFixtureBytes(t, "onvif", "fault-auth.xml"))
	}))
	defer srv.Close()
	c := NewOnvifClient(srv.URL, "admin", "yoto")
	_, err := c.GetProfiles(context.Background())
	if err == nil {
		t.Fatal("SOAP Fault'da xato kutilardi")
	}
	if !strings.Contains(err.Error(), "NotAuthorized") && !strings.Contains(err.Error(), "Invalid") {
		t.Errorf("xato matni foydali emas: %v", err)
	}
}

// Server javob bermasa (transport xatosi) → xato qaytadi.
func TestONVIF_TransportXato(t *testing.T) {
	c := NewOnvifClient("http://127.0.0.1:1/nope", "admin", "pass")
	if _, err := c.GetProfiles(context.Background()); err == nil {
		t.Fatal("transport xatosi kutilardi")
	}
}

// WS-Security UsernameToken so'rovga qo'shiladi; parol Digest (matn emas).
func TestONVIF_KredsSoRovda(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotAuth = string(body)
		_, _ = w.Write(readFixtureBytes(t, "onvif", "getprofiles.xml"))
	}))
	defer srv.Close()
	c := NewOnvifClient(srv.URL, "admin", "sirli")
	if _, err := c.GetProfiles(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotAuth, "UsernameToken") || !strings.Contains(gotAuth, "admin") {
		t.Errorf("so'rovda UsernameToken/admin yo'q: %s", firstN(gotAuth, 400))
	}
	if strings.Contains(gotAuth, ">sirli<") {
		t.Error("parol PasswordText ko'rinishida yuborilgan (Digest kutilardi)")
	}
}

func firstN(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}
