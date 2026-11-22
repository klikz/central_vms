// GET /metrics — Prometheus matni (ichki bind; api.md §"metrics").
// Avval hujjatda bor deb yozilgan, lekin endpoint umulqon mavjud emas edi
// (2026-11-21 server auditi).
package main

import (
	"fmt"
	"io"
	"time"

	"central-vms/server/internal/httpapi"
)

var startedAt = time.Now()

func writeMetrics(w io.Writer, deps *httpapi.Deps) {
	fmt.Fprintf(w, "# vmsd metrikalari\n")
	fmt.Fprintf(w, "vmsd_uptime_seconds %d\n", int(time.Since(startedAt).Seconds()))
	if deps.Agent != nil {
		fmt.Fprintf(w, "vmsd_agents_connected %d\n", deps.Agent.Count())
		ok, fail := deps.Agent.ConnStats()
		fmt.Fprintf(w, "vmsd_agent_handshakes_total{result=\"ok\"} %d\n"+
			"vmsd_agent_handshakes_total{result=\"fail\"} %d\n", ok, fail)
	}
	if deps.UI != nil {
		fmt.Fprintf(w, "vmsd_ui_connections %d\n", deps.UI.Count())
		ok, fail := deps.UI.ConnStats()
		fmt.Fprintf(w, "vmsd_ui_handshakes_total{result=\"ok\"} %d\n"+
			"vmsd_ui_handshakes_total{result=\"fail\"} %d\n", ok, fail)
	}
	if deps.HTTP != nil {
		deps.HTTP.Write(w) // so'rovlar soni, status klasslari, davomiylik
	}
	if total, online, err := deps.Reg.CountCameras(); err == nil {
		fmt.Fprintf(w, "vmsd_cameras_total %d\nvmsd_cameras_online %d\n", total, online)
	}
	if deps.Media != nil {
		st := deps.Media.StateSnapshot()
		if sess, ok := st["sessions"].(map[string]int); ok {
			for state, n := range sess {
				fmt.Fprintf(w, "vmsd_sessions{state=%q} %d\n", state, n)
			}
		}
		if n, ok := st["publishes"].([]map[string]any); ok {
			fmt.Fprintf(w, "vmsd_publishes %d\n", len(n))
		}
		for _, k := range []string{"transcodes", "transcode_max", "tc_restarts"} {
			if v, ok := st[k]; ok {
				fmt.Fprintf(w, "vmsd_%s %v\n", k, v)
			}
		}
		if b, ok := st["backend"].(string); ok {
			fmt.Fprintf(w, "vmsd_transcode_backend_info{backend=%q} 1\n", b)
		}
	}
}
