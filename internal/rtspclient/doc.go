// rtspclient — minimal RTSP/1.0 mijoz (faqat stdlib; RFC 2326).
//
// Sabab: `bluenviron/gortsplib` proxy orqali **bo'sh stub** bo'lib keladi
// (2026-11-19 qayta tekshirildi: proxy ham, GitHub'ga git orqali murojaat ham
// soxta 5 faylli paket qaytardi — tarmoq to'liq intercept qilingan).
// Shu sababli TASKS.md §C2 variant (b) tanlandi: o'z minimal mijozimiz.
//
// Qamrov (probe uchun yetarli, docs/protocol.md §12):
//   - OPTIONS, GET_PARAMETER, DESCRIBE; timeout (ctx), retry;
//   - 401 javobda Basic va Digest (MD5, qop=auth) avtorizatsiya;
//   - SDP body o'qiladi (tahlil internal/cams'da).
//
// RTP/interleaved ma'lumot kanallari bu paketda YO'Q — kamera pull (C3)
// FFmpeg zaxira yo'li orqali bajariladi (protocol.md §8.1 zaxira zanjiri).
package rtspclient
