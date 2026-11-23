// Package keystore — lokal parol store (docs/protocol.md §10.1).
//
//	/etc/vmsagent/agent.key      32 bait, 0600 — master kalit
//	/var/lib/vmsagent/secrets.bin  format: gcm1:<b64(nonce||ct)>
//
// state.json da parol SAQLANMAYDI — faqat secret_ref ("cam:<uuid>:main").
// Kalit noto'g'ri yoki fayl buzilgan bo'lsa xato qaytadi (GCM auth fail).
package keystore

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const (
	magic    = "gcm1:"
	keyBytes = 32
	nonceLen = 12
	// aad — fayl nomiga bog'lash: bitta store'dagi shifrlangan matnni
	// boshqa faylga ko'chirib bo'lmasin.
	aadText = "central-vms/client/secrets.bin/v1"
	// RefAgentSecret — agent_secret shu nom bilan saqlanadi (§10.1).
	RefAgentSecret = "agent_secret_enc"
)

// KeyPermsOK — tashqi tekshiruvlar (doctor) uchun: platformaga mos
// rejim qoidasi (Unix: 0600; Windows: tekshiruv ma'nosi yo'q —
// perms_windows.go).
func KeyPermsOK(fi os.FileInfo) bool { return keyPermsOK(fi) }

var (
	ErrKeySize  = errors.New("keystore: agent.key 32 bait bo'lishi kerak")
	ErrKeyPerms = errors.New("keystore: agent.key rejimi 0600 bo'lishi shart")
	ErrBadStore = errors.New("keystore: secrets.bin o'qilmadi (kalit noto'g'ri yoki fayl buzilgan)")
)

// LoadKey — master kalitni o'qiydi va rejimini tekshiradi.
func LoadKey(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("keystore: kalit fayli: %w", err)
	}
	if !keyPermsOK(st) {
		return nil, fmt.Errorf("%w: %s rejimi %04o", ErrKeyPerms, filepath.Base(path), st.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("keystore: kalit o'qilmadi: %w", err)
	}
	if len(b) != keyBytes {
		return nil, fmt.Errorf("%w: %d bait topildi", ErrKeySize, len(b))
	}
	return b, nil
}

// NewKeyFile — kalit faylini yaratadi (mavjud bo'lsa TEGMaydi).
func NewKeyFile(path string) ([]byte, error) {
	if _, err := os.Stat(path); err == nil {
		return LoadKey(path)
	}
	b := make([]byte, keyBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("keystore: kalit generatsiyasi: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("keystore: papka: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("keystore: kalit yaratilmadi: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("keystore: kalit yozilmadi: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("keystore: kalit yopilmedi: %w", err)
	}
	return b, nil
}

// Store — xotiradagi ref→qiymat xaritasi; Save atomik yozadi.
type Store struct {
	mu     sync.Mutex
	path   string
	key    []byte
	values map[string]string
	dirty  bool
}

// Open — store'ni ochadi. Fayl bo'lmasa bo'sh store qaytadi (birinchi ishga tushish).
func Open(keyPath, storePath string) (*Store, error) {
	key, err := LoadKey(keyPath)
	if err != nil {
		return nil, err
	}
	s := &Store{path: storePath, key: key, values: map[string]string{}}
	b, err := os.ReadFile(storePath)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("keystore: %s o'qilmadi: %w", storePath, err)
	}
	plain, err := open(string(b), key)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(plain, &s.values); err != nil {
		return nil, fmt.Errorf("%w: json: %v", ErrBadStore, err)
	}
	return s, nil
}

// Get — ref bo'yicha qiymat.
func (s *Store) Get(ref string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[ref]
	return v, ok
}

// Put — qiymatni qo'yadi (Save qilinguncha diskka tushmaydi).
func (s *Store) Put(ref, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[ref] = value
	s.dirty = true
}

// Delete — ref'ni o'chiradi.
func (s *Store) Delete(ref string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.values[ref]; !ok {
		return false
	}
	delete(s.values, ref)
	s.dirty = true
	return true
}

// Refs — barcha ref'lar (saralangan, testlar uchun barqaror).
func (s *Store) Refs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.values))
	for k := range s.values {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Dirty — saqlanmagan o'zgarish bormi.
func (s *Store) Dirty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dirty
}

// Save — atomik yozish: *.tmp (0600) + rename. Bo'sh o'zgarish bo'lsa yozmaydi.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return nil
	}
	plain, err := json.Marshal(s.values)
	if err != nil {
		return fmt.Errorf("keystore: json: %w", err)
	}
	sealed, err := seal(plain, s.key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("keystore: papka: %w", err)
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("keystore: vaqtinchalik fayl: %w", err)
	}
	if _, err := io.WriteString(f, sealed); err != nil {
		_ = f.Close()
		return fmt.Errorf("keystore: yozish: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("keystore: sync: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("keystore: yopish: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("keystore: rename: %w", err)
	}
	s.dirty = false
	return nil
}
