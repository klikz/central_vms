package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"central-vms/client/internal/cams"
	"central-vms/client/internal/media"
	"central-vms/client/internal/rtspclient"
	"central-vms/client/internal/selftest"
)

// C4.6 (§4.7): self-test paneli deps'lari — haqiqiy tarmoq/protsseslar.

// mediaHost — server_url'dan MediaMTX joylashgan deb olinadigan host
// (dev/oddiy deployda vmsd va MediaMTX bitta mashinada; §10.5 portlar).
func mediaHost(serverURL string) string {
	u, err := url.Parse(serverURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (r *runtime) newSelftest() *selftest.Runner {
	deps := selftest.Deps{
		WSRTT: func(ctx context.Context) (time.Duration, error) {
			r.mu.Lock()
			sess := r.sess
			r.mu.Unlock()
			if sess == nil {
				return 0, fmt.Errorf("sessiya yo'q")
			}
			return sess.PingRTT(ctx)
		},
		WHIP: func(ctx context.Context) (string, error) {
			host := mediaHost(r.st.ServerURL)
			if host == "" {
				return "", fmt.Errorf("server_url o'qilmadi")
			}
			tcpAddr := net.JoinHostPort(host, "8889")
			d := net.Dialer{Timeout: 3 * time.Second}
			c, err := d.DialContext(ctx, "tcp", tcpAddr)
			if err != nil {
				return "", fmt.Errorf("WHIP TCP 8889: %w", err)
			}
			_ = c.Close()
			udpDetail := stunProbe(ctx, net.JoinHostPort(host, "8189"))
			return "tcp=ok " + udpDetail, nil
		},
		RTSPPub: func(ctx context.Context) (string, error) {
			host := mediaHost(r.st.ServerURL)
			if host == "" {
				return "", fmt.Errorf("server_url o'qilmadi")
			}
			c, err := rtspclient.Dial(ctx, "rtsp://"+net.JoinHostPort(host, "8554")+"/_selftest", rtspclient.Options{})
			if err != nil {
				return "", err
			}
			defer c.Close()
			resp, err := c.Do(ctx, "OPTIONS", rtspclient.Headers{}, nil)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("RTSP 8554 javob: %d", resp.Status), nil
		},
		Cameras: func() []selftest.CamSpec {
			r.mu.Lock()
			defer r.mu.Unlock()
			out := make([]selftest.CamSpec, 0, len(r.st.Cameras))
			for _, c := range r.st.Cameras {
				out = append(out, selftest.CamSpec{ID: c.ID, URL: media.CamRTSPURL(c)})
			}
			return out
		},
		ProbeCam: func(ctx context.Context, u string) (string, error) {
			c, err := rtspclient.Dial(ctx, u, rtspclient.Options{})
			if err != nil {
				return "", err
			}
			defer c.Close()
			sdpBytes, err := c.Describe(ctx)
			if err != nil {
				return "", err
			}
			sdp, err := cams.ParseSDP(string(sdpBytes))
			if err != nil {
				return "", err
			}
			vs := sdp.VideoStreams()
			if len(vs) == 0 {
				return "", fmt.Errorf("video oqim yo'q")
			}
			return fmt.Sprintf("kodek=%s profil=%s", vs[0].Codec, vs[0].Profile), nil
		},
		Publishes: func() []string {
			r.mu.Lock()
			reg := r.publishes
			r.mu.Unlock()
			if reg == nil {
				return nil
			}
			refs := reg.List()
			out := make([]string, 0, len(refs))
			for _, p := range refs {
				out = append(out, p.PublishID)
			}
			return out
		},
		FFmpeg: ffProcs,
	}
	return selftest.New(deps)
}

// stunProbe — UDP 8189 STUN Binding so'rovi (WHIP ICE UDP yo'l-echar).
func stunProbe(ctx context.Context, addr string) string {
	req := make([]byte, 20)
	req[0], req[1] = 0x00, 0x01 // Binding Request
	req[4], req[5], req[6], req[7] = 0x21, 0x12, 0xa4, 0x42
	pc, err := net.DialTimeout("udp", addr, 2*time.Second)
	if err != nil {
		return "udp=xato"
	}
	defer pc.Close()
	_ = pc.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := pc.Write(req); err != nil {
		return "udp=xato"
	}
	buf := make([]byte, 512)
	n, err := pc.Read(buf)
	if err != nil || n < 20 {
		return "udp=javob-yo'q"
	}
	return "udp=ok"
}

// ffProcs — boshqariladigan FFmpeg jarayonlari ro'yxati.
func ffProcs() []selftest.FFProc {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "pgrep", "-a", "-f", "ffmpeg").Output()
	if err != nil {
		return nil
	}
	var procs []selftest.FFProc
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pid, cmdLine, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		id, err := strconv.Atoi(pid)
		if err != nil {
			continue
		}
		role := "transcode"
		if bytes.Contains([]byte(cmdLine), []byte("whip")) {
			role = "whip"
		}
		procs = append(procs, selftest.FFProc{PID: id, Role: role})
	}
	return procs
}
