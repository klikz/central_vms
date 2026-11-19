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
	_ "github.com/bluenviron/gortsplib/v4"
	_ "github.com/bluenviron/mediacommon/pkg/codecs/h264"
	_ "github.com/bluenviron/mediacommon/pkg/codecs/h265"
	_ "github.com/google/uuid"
	_ "github.com/gorilla/websocket"
	_ "github.com/pion/interceptor"
	_ "github.com/pion/rtp"
	_ "github.com/pion/webrtc/v4"
	_ "gopkg.in/yaml.v3"
)
