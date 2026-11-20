// Package auth — parol hash (argon2id), JWT (access/refresh), rol middleware,
// login rate-limit. S1 qadamlari: S1.1 (hash), S1.2 (JWT), S1.3 (limit+rol).
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parametrlari — OWASP tavsiyasi (m=64MB, t=3, p=4).
// Hash ~0.5 s davom etadi: login kam (2 user), brute-force esa qimmat bo'lishi kerak.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB = 64 MB
	argonThreads = 4
	keyLen       = 32
	saltLen      = 16
)

// HashPassword — PHC format: $argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: salt: %w", err)
	}
	dk := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, keyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(dk)), nil
}

// VerifyPassword — PHC satrini qayta hisoblab, vaqtiga bog'liq bo'lmagan
// taqqoslash (subtle.ConstantTimeCompare) qiladi. Noto'g'ri format → error
// (xato emas) — chaqiruvchi logga yozadi.
func VerifyPassword(password, phc string) (bool, error) {
	parts := strings.Split(phc, "$")
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, fmt.Errorf("auth: parol hash formati noto'g'ri")
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, fmt.Errorf("auth: parol hash parametrlari: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("auth: parol hash salt: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("auth: parol hash dk: %w", err)
	}
	dk := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(dk, want) == 1, nil
}
