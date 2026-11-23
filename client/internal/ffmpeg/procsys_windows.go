//go:build windows

// procsys_windows.go — Windows uchun jarayonni to'xtatish (make buildWin,
// 2026-11-23). Windows'da POSIX jarayon guruhlari/sinyallari yo'q:
// graceful yo'q — Kill() bitta jarayonni o'ldiradi. FFmpeg'ning bolalari
// odatda yo'q (statik exe); muxer yopilishi kafolatlanmaydi — shuning
// uchun bizning yo'llarimizda SIGINT o'rniga avval process.Exit(0)...
// oddiyroq: graceful = TerminateProcess (Windows'da boshqa yo'q).
// IZOH: Windows agent'da transcode/fallback kamdan-kam ishlatiladi;
// asosiy yo'l (Go RTSP/WHIP) platformaga bog'liq emas.
package ffmpeg

import (
	"os/exec"
	"syscall"
)

func sysProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{} }

func killGraceful(cmd *exec.Cmd) error { return cmd.Process.Kill() }

func killForce(cmd *exec.Cmd) error { return cmd.Process.Kill() }
