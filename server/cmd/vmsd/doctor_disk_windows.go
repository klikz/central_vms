//go:build windows

// doctor_disk_windows.go — Windows uchun `make buildWin` (2026-11-23).
// Go'ning sof `syscall` paketida disk bo'shlig'i API eksport qilinmagan —
// golang.org/x/sys/windows ishlatiladi (modul grafinda allaqachon bor,
// modernc.org/sqlite orqali; qo'shimcha tarmoq talab qilmaydi).
package main

import "golang.org/x/sys/windows"

func diskFreeBytes(dir string) (uint64, bool) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, false
	}
	var freeAvail, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeAvail, &total, &totalFree); err != nil {
		return 0, false
	}
	return freeAvail, true
}
