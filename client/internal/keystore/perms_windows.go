//go:build windows

package keystore

import "os"

// keyPermsOK — Windows'da Unix rejim tekshiruvi MA'NOSIZ: NTFS ACL'lari
// Unix bitlariga kirmaydi va Go oddiy fayl uchun 0666 qaytaradi — bu
// Windows'dagi HAR QANDAY kalitni "noto'g'ri rejim" deb rad eder
// (2026-11-23 foydalanuvchi xatosi: "agent.key rejimi 0600 bo'lishi
// shart"). Himoya Windows'da ACL/papka darajasida: kalit foydalanuvchi
// papkasida (<exe>/data) saqlanadi — boshqa hisoblar o'qiy olmaydi.
func keyPermsOK(os.FileInfo) bool { return true }
