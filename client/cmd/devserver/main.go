// devserver — mock vmsd (make devserver). Server tayyor bo'lmaganda
// agentning C1–C3 qismlarini tekshirish uchun (docs/protocol.md §18).
// Barcha mantiq internal/devserver paketi ichida (testlar o'sha yerda).
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"central-vms/client/internal/devserver"
	"central-vms/client/internal/logx"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8081", "tinglash manzili")
	state := flag.String("state", "build/devserver.state.json", "agentlar saqlanadigan fayl (dev; bo'sh → xotira)")
	logPath := flag.String("log", "logs/devserver.log", "SERVER log fayli (client logidan ALLOHIIDA)")
	flag.Parse()

	// SERVER logi alohida faylda: std log stderr+BILAN faylga ham yozadi.
	fw, err := logx.New(*logPath, 4, 5)
	if err != nil {
		fmt.Fprintf(os.Stderr, "devserver: log fayli ochilmadi (%s): %v — faqat terminal\n", *logPath, err)
	} else {
		defer fw.Close()
		log.SetOutput(io.MultiWriter(os.Stderr, fw))
	}
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)

	srv := devserver.New(devserver.Options{
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
	log.Printf("devserver (mock vmsd) http://%s  join so‘rovlari avtomatik tasdiqlanadi  log: %s", *addr, *logPath)
	log.Println("WS: /api/agent/ws?agent_id=… — auth → hello → welcome (§3.3, §4.1)")
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("XATO: ListenAndServe: %v", err)
		os.Exit(1)
	}
}
