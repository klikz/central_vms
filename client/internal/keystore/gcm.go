package keystore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

// seal — AES-256-GCM: "gcm1:" + b64(nonce||ciphertext+tag).
func seal(plain, key []byte) (string, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("keystore: nonce: %w", err)
	}
	ct := gcm.Seal(nil, nonce, plain, []byte(aadText))
	buf := make([]byte, 0, nonceLen+len(ct))
	buf = append(buf, nonce...)
	buf = append(buf, ct...)
	return magic + base64.StdEncoding.EncodeToString(buf), nil
}

// open — seal'ning teskarisi. Kalit noto'g'ri yoki matn buzilgan bo'lsa
// ErrBadStore qaytadi (GCM tag tekshiruvi).
func open(sealed string, key []byte) ([]byte, error) {
	// Avval kalitni tekshiramiz: xato aniq bo'lsin (kalit qisqa bo'lsa
	// "fayl buzilgan" deb chalg'itmasin).
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(sealed, magic) {
		return nil, fmt.Errorf("%w: magic kutilmagan", ErrBadStore)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimSpace(sealed), "\n")[len(magic):])
	if err != nil {
		return nil, fmt.Errorf("%w: base64: %v", ErrBadStore, err)
	}
	if len(raw) < nonceLen+16 { // nonce + GCM tag
		return nil, fmt.Errorf("%w: qator juda qisqa (%d bait)", ErrBadStore, len(raw))
	}
	plain, err := gcm.Open(nil, raw[:nonceLen], raw[nonceLen:], []byte(aadText))
	if err != nil {
		return nil, ErrBadStore
	}
	return plain, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != keyBytes {
		return nil, fmt.Errorf("%w: %d bait", ErrKeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("keystore: AES: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("keystore: GCM: %w", err)
	}
	if gcm.NonceSize() != nonceLen {
		return nil, fmt.Errorf("keystore: nonce o'lchami %d, kutilgan %d", gcm.NonceSize(), nonceLen)
	}
	return gcm, nil
}
