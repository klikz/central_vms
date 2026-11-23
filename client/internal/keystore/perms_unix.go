//go:build !windows

package keystore

import "os"

// keyPermsOK — Unix: agent.key 0600 bo'lishi shart (§10.1 — parollar
// bazasi shu kalit bilan shifrlangan).
func keyPermsOK(fi os.FileInfo) bool { return fi.Mode().Perm()&0o077 == 0 }
