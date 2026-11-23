//go:build windows

package caps

import (
	"syscall"
	"unsafe"
)

// memoryStatusEx — kernel32 MEMORYSTATUSEX (to'liq layout; x/sys/windows
// va sof syscall bu API'ni eksport QILMAYDI — 2026-11-23 auditi shuni
// ko'rsatdi). Struktura Win32 to'liq ko'rsatmasiga mos.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// totalRAMMB — Windows: /proc/meminfo yo'q → kernel32!GlobalMemoryStatusEx.
func totalRAMMB() int {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
	var ms memoryStatusEx
	ms.Length = uint32(unsafe.Sizeof(ms))
	if r, _, _ := proc.Call(uintptr(unsafe.Pointer(&ms))); r == 0 {
		return 0
	}
	return int(ms.TotalPhys >> 20)
}
