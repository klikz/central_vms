//go:build !windows

package caps

// totalRAMMB — Unix: /proc/meminfo (Linux). Boshqa unix tizimlarida
// 0 qaytishi mumkin — caps nil-bilan boshlanadi, xato emas.
func totalRAMMB() int { return ParseMemMB(ReadFileString("/proc/meminfo")) }
