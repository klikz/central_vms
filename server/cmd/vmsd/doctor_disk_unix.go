//go:build !windows

// doctor_disk_unix.go — disk joyini Statfs orqali olchaydi (Linux/macOS).
package main

import "syscall"

func diskFreeBytes(dir string) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return st.Bavail * uint64(st.Bsize), true
}
