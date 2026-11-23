//go:build !windows

// procsys_unix.go — FFmpeg jarayonini guruh sifatida boshqarish:
// Setpgid + kill(-pid) butun daraxtni o'ldiradi (zombie qolmaydi, §8.2).
package ffmpeg

import (
	"os/exec"
	"syscall"
)

func sysProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

// killGraceful — SIGINT: ffmpeg muxer'ni to'g'ri yopadi (fayl/HLS bo'shagi
// buzilmaydi).
func killGraceful(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
}

// killForce — SIGKILL butun guruhga (bolalar bilan).
func killForce(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
