//go:build tools

// Package tools — qulflangan bog'liqliklar ro'yxati (docs/protocol.md §8, TASKS C0.3).
//
// Bu fayl **hech qachon kompilyatsiya qilinmaydi** (`tools` build tag), lekin
// `go mod tidy` bog'liqliklarni o'chirib yubormasligi uchun blank import saqlaydi.
// Versiyalar `docs/deps.md` da izohlanadi va `make deps` tekshiradi.
//
//	Kuid: go build -tags tools ./tools   (tekshiruv uchun)
package tools

import (
	// 2026-11-19: bluenviron/gortsplib va mediacommon — tarmoq intercepti
	// tufayli faqat BO'SH taqilgan paketlarni qaytaradi (proxy ham, git clone
	// ham); haqiqiy manbaga e'rib bo'lmadi. RTSP yozuvi o'z `internal/rtspclient`
	//imizda, H.265/zaxira yo'llari FFmpeg orqali (TASKS.md "C2 holati").
	// Tarmoq ruxsat etilganda bu importlar qayta qo'shiladi.
	_ "github.com/google/uuid"
	_ "github.com/gorilla/websocket"
	_ "github.com/pion/interceptor"
	_ "github.com/pion/rtp"
	_ "github.com/pion/webrtc/v4"
	_ "gopkg.in/yaml.v3"
)
