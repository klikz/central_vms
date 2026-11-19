// devserver — mock vmsd (make devserver). Server tayyor bo'lmaganda
// agentning C1–C3 qismlarini tekshirish uchun (docs/protocol.md §18).
// Barcha mantiq internal/devserver paketi ichida (testlar o'sha yerda).
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"central-vms/client/internal/devserver"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8081", "tinglash manzili")
	code := flag.String("code", "7F3K-92QX-BD8T", "bir martalik enrollment kodi")
	state := flag.String("state", "build/devserver.state.json", "agentlar saqlanadigan fayl (dev; bo'sh → xotira)")
	flag.Parse()

	srv := devserver.New(devserver.Options{
		EnrollCode:  *code,
		AuthTimeout: 5 * time.Second,
		MinVersion:  1,
		MaxVersion:  1,
		StateFile:   *state,
	})

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("devserver (mock vmsd) http://%s  enroll kodi: %s\n", *addr, *code)
	fmt.Println("WS: /api/agent/ws?agent_id=… — auth → hello → welcome (§3.3, §4.1)")
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
