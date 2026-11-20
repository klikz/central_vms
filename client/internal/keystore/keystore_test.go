package keystore

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// helper: kalit + store yo'llari (vaqtinchalik papkada)
func tempKey(t *testing.T) (keyPath, storePath string) {
	t.Helper()
	dir := t.TempDir()
	keyPath = filepath.Join(dir, "agent.key")
	storePath = filepath.Join(dir, "secrets.bin")
	if _, err := NewKeyFile(keyPath); err != nil {
		t.Fatalf("NewKeyFile: %v", err)
	}
	return keyPath, storePath
}

func TestNewKeyFile_Perms(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "agent.key")
	b, err := NewKeyFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 32 {
		t.Fatalf("kalit %d bait, kutilgan 32", len(b))
	}
	st, _ := os.Stat(keyPath)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("rejim %04o, kutilgan 0600", st.Mode().Perm())
	}
	// Takroriy: mavjud kalit o'zgarmaydi
	b2, err := NewKeyFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, b2) {
		t.Fatal("mavjud kalit qayta yozildi")
	}
}

func TestLoadKey_HimoyaTekshiruvlari(t *testing.T) {
	t.Run("rejim 0644 rad etiladi", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "agent.key")
		_ = os.WriteFile(p, bytes.Repeat([]byte("k"), 32), 0o644)
		if _, err := LoadKey(p); !errors.Is(err, ErrKeyPerms) {
			t.Fatalf("xato: %v, kutilgan ErrKeyPerms", err)
		}
	})
	t.Run("o'lcham 16 bait rad etiladi", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "agent.key")
		_ = os.WriteFile(p, bytes.Repeat([]byte("k"), 16), 0o600)
		if _, err := LoadKey(p); !errors.Is(err, ErrKeySize) {
			t.Fatalf("xato: %v, kutilgan ErrKeySize", err)
		}
	})
	t.Run("fayl yo'q", func(t *testing.T) {
		if _, err := LoadKey(filepath.Join(t.TempDir(), "yoq.key")); err == nil {
			t.Fatal("xato kutilardi")
		}
	})
}

func TestStore_RoundTrip(t *testing.T) {
	keyPath, storePath := tempKey(t)

	s, err := Open(keyPath, storePath)
	if err != nil {
		t.Fatal(err)
	}
	s.Put(RefAgentSecret, "S3cret-Agent")
	s.Put("cam:11111111-2222-3333-4444-555555555555:main", "kamera-paroli")
	if !s.Dirty() {
		t.Fatal("Dirty=true bo'lishi kerak edi")
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if s.Dirty() {
		t.Fatal("Save dan keyin Dirty=false bo'lishi kerak")
	}

	raw, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), magic) {
		t.Fatalf("fayl formati: %q", string(raw)[:12])
	}
	for _, leak := range []string{"S3cret-Agent", "kamera-paroli"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("faylda ochiq parol qoldi: %s", leak)
		}
	}
	if st, _ := os.Stat(storePath); st.Mode().Perm() != 0o600 {
		t.Fatalf("secrets.bin rejimi %04o, kutilgan 0600", st.Mode().Perm())
	}

	// Qayta ochib o'qish
	s2, err := Open(keyPath, storePath)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := s2.Get(RefAgentSecret); !ok || v != "S3cret-Agent" {
		t.Fatalf("agent_secret: %q ok=%v", v, ok)
	}
	if v, ok := s2.Get("cam:11111111-2222-3333-4444-555555555555:main"); !ok || v != "kamera-paroli" {
		t.Fatalf("kamera paroli: %q", v)
	}
	refs := s2.Refs()
	if len(refs) != 2 || refs[0] > refs[1] {
		t.Fatalf("Refs: %v", refs)
	}
	if !s2.Delete(RefAgentSecret) || s2.Delete(RefAgentSecret) {
		t.Fatal("Delete ishlamadi")
	}
	if len(s2.Refs()) != 1 {
		t.Fatalf("Delete dan keyin: %v", s2.Refs())
	}
}

func TestStore_KalitNotoGri(t *testing.T) {
	keyPath, storePath := tempKey(t)
	s, _ := Open(keyPath, storePath)
	s.Put("cam:x:main", "parol")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	// Boshqa kalit bilan ochish
	otherKey := filepath.Join(filepath.Dir(keyPath), "other.key")
	if _, err := NewKeyFile(otherKey); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(otherKey, storePath); !errors.Is(err, ErrBadStore) {
		t.Fatalf("xato: %v, kutilgan ErrBadStore", err)
	}
}

func TestStore_BuzilganFayllar(t *testing.T) {
	keyPath, storePath := tempKey(t)
	s, _ := Open(keyPath, storePath)
	s.Put("cam:x:main", "parol")
	_ = s.Save()

	good, _ := os.ReadFile(storePath)

	bad := map[string]string{
		"magic yo'q":      "xyz:" + string(good[5:]),
		"base64 buzilgan": magic + "!!!!",
		"juda qisqa":      magic + base64.StdEncoding.EncodeToString([]byte("qisqa")),
		"bo'sh":           "",
	}
	for name, content := range bad {
		t.Run(name, func(t *testing.T) {
			_ = os.WriteFile(storePath, []byte(content), 0o600)
			if _, err := Open(keyPath, storePath); !errors.Is(err, ErrBadStore) {
				t.Fatalf("%s: xato %v, kutilgan ErrBadStore", name, err)
			}
		})
	}

	// Bittgina baitni o'zgartirish (tag tekshiruvi ushlashi kerak)
	tampered := []byte(good)
	i := len(tampered) - 5
	if tampered[i] == 'A' {
		tampered[i] = 'B'
	} else {
		tampered[i] = 'A'
	}
	_ = os.WriteFile(storePath, tampered, 0o600)
	if _, err := Open(keyPath, storePath); !errors.Is(err, ErrBadStore) {
		t.Fatalf("tamper: xato %v", err)
	}
}

func TestStore_FaylYoqBolsaBosh(t *testing.T) {
	keyPath, storePath := tempKey(t)
	s, err := Open(keyPath, storePath) // fayl hali yo'q
	if err != nil {
		t.Fatalf("birinchi ishga tushishda xato berdi: %v", err)
	}
	if len(s.Refs()) != 0 {
		t.Fatalf("bo'sh bo'lishi kerak edi: %v", s.Refs())
	}
}

func TestSeal_NonceHarSafarYangi(t *testing.T) {
	key := bytes.Repeat([]byte("K"), 32)
	a, err := seal([]byte("bir xil matn"), key)
	if err != nil {
		t.Fatal(err)
	}
	b, err := seal([]byte("bir xil matn"), key)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("nonce takrorlandi — bir xil matn bir xil shifrlanmasligi kerak")
	}
	pa, _ := open(a, key)
	pb, _ := open(b, key)
	if string(pa) != "bir xil matn" || string(pb) != "bir xil matn" {
		t.Fatal("ochish xato")
	}
}

func TestStore_Musroboziy(t *testing.T) {
	keyPath, storePath := tempKey(t)
	s, _ := Open(keyPath, storePath)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(3)
		go func(i int) { defer wg.Done(); s.Put("cam:x:main", "v") }(i)
		go func() { defer wg.Done(); s.Get("cam:x:main"); _ = s.Refs() }()
		go func() { defer wg.Done(); _ = s.Save() }()
	}
	wg.Wait()
	if _, ok := s.Get("cam:x:main"); !ok {
		t.Fatal("musobaqadan keyin qiymat yo'qoldi")
	}
}

// --- xato yo'llari (coverage bo'sagasi uchun muhim) ---

func TestLoadKey_Direktoriya(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadKey(dir); err == nil {
		t.Fatal("direktoriya kalit bo'la olmaydi")
	}
}

func TestGCM_KalitOlchamiXato(t *testing.T) {
	short := bytes.Repeat([]byte("K"), 16)
	if _, err := seal([]byte("matn"), short); !errors.Is(err, ErrKeySize) {
		t.Fatalf("seal: %v", err)
	}
	if _, err := open(magic+"AAAA", short); !errors.Is(err, ErrKeySize) {
		t.Fatalf("open: %v", err)
	}
}

func TestSave_TmpYozibBoLMadi(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "agent.key")
	if _, err := NewKeyFile(keyPath); err != nil {
		t.Fatal(err)
	}
	roDir := filepath.Join(dir, "read-only")
	if err := os.Mkdir(roDir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := Open(keyPath, filepath.Join(roDir, "secrets.bin"))
	if err != nil {
		t.Fatal(err)
	}
	s.Put("cam:x:main", "v")
	if err := os.Chmod(roDir, 0o500); err != nil { // yozish uchun yopiq
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o755) })
	if err := s.Save(); err == nil {
		t.Fatal("yozish xatosi kutilardi")
	}
}

func TestSave_RenameYilmadi(t *testing.T) {
	keyPath, storePath := tempKey(t)
	if err := os.Mkdir(storePath, 0o700); err != nil { // store yo'liga katalog
		t.Fatal(err)
	}
	s, err := Open(keyPath, filepath.Join(filepath.Dir(storePath), "s2.bin"))
	if err != nil {
		t.Fatal(err)
	}
	s.Put("cam:x:main", "v")
	s.path = storePath // rename manzili katalog bilan to'qnashadi
	if err := s.Save(); err == nil {
		t.Fatal("rename xatosi kutilardi")
	}
}

func TestOpen_FaylOqilmadi(t *testing.T) {
	keyPath, storePath := tempKey(t)
	s, _ := Open(keyPath, storePath)
	s.Put("cam:x:main", "v")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(storePath, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(storePath, 0o600) })
	if _, err := Open(keyPath, storePath); err == nil {
		t.Fatal("o'qish xatosi kutilardi")
	}
}

func TestNewKeyFile_YozibBoLMadi(t *testing.T) {
	dir := t.TempDir()
	ro := filepath.Join(dir, "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	if _, err := NewKeyFile(filepath.Join(ro, "agent.key")); err == nil {
		t.Fatal("kalit yaratib bo'lmasligi kerak edi")
	}
}

func TestPapkaYaratibBoLMadi(t *testing.T) {
	// /proc ichida papka yaratib bo'lmaydi — MkdirAll xato yo'li
	if _, err := NewKeyFile("/proc/self/mem/agent.key"); err == nil {
		t.Fatal("kalit papkasi yaratilmasligi kerak edi")
	}
	keyPath, _ := tempKey(t)
	key, err := LoadKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	// Store'ni to'g'ridan-to'g'ri quramiz: Save'dagi MkdirAll xato yo'lini sinash uchun
	s := &Store{path: "/proc/self/mem/secrets.bin", key: key, values: map[string]string{}}
	s.Put("cam:x:main", "v")
	if err := s.Save(); err == nil {
		t.Fatal("Save papka xatosini qaytarmadi")
	}
}
