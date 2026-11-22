package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Bekor qilish (revocation) — to'liq oqim: eski refresh cookie rad
// etiladi, hisobni o'chirish esa mavjud access tokenni darhol yopadi
// (2026-11-21 server auditi).
func TestTokenRevocationHTTP(t *testing.T) {
	srv, reg, _, _, _, _ := newServer(t)

	call := func(method, path, body, tok, refresh string) (int, map[string]any, []*http.Cookie) {
		t.Helper()
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, srv.URL+path, rd)
		if err != nil {
			t.Fatal(err)
		}
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		if refresh != "" {
			req.AddCookie(&http.Cookie{Name: "vms_refresh", Value: refresh})
		}
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		return resp.StatusCode, m, resp.Cookies()
	}
	cookieOf := func(cs []*http.Cookie) string {
		for _, c := range cs {
			if c.Name == "vms_refresh" && c.Value != "" {
				return c.Value
			}
		}
		return ""
	}

	code, m, cookies := call(http.MethodPost, "/api/auth/login", `{"username":"view1","password":"Parol-Salom-123"}`, "", "")
	if code != http.StatusOK {
		t.Fatalf("login: %d %v", code, m)
	}
	access, _ := m["access"].(string)
	refresh := cookieOf(cookies)
	if access == "" || refresh == "" {
		t.Fatalf("login javobi: access=%q refresh=%q", access, refresh)
	}
	if code, _, _ := call(http.MethodGet, "/api/me", "", access, ""); code != http.StatusOK {
		t.Fatalf("me: %d", code)
	}
	// Refresh rotatsiyasi: yangi cookie chiqadi.
	code, _, cookies2 := call(http.MethodPost, "/api/auth/refresh", "", "", refresh)
	if code != http.StatusOK {
		t.Fatalf("refresh: %d", code)
	}
	newRefresh := cookieOf(cookies2)
	if newRefresh == "" || newRefresh == refresh {
		t.Fatalf("rotatsiya bo'lmadi: %q", newRefresh)
	}
	// ESKI cookie endi rad etilishi kerak.
	if code, _, _ := call(http.MethodPost, "/api/auth/refresh", "", "", refresh); code != http.StatusUnauthorized {
		t.Fatal("eski refresh cookie qabul qilindi")
	}
	// Hisobni o'chirish (disable) — mavjud access token darhol yaroqsiz.
	u, _, err := reg.GetUserByName("view1")
	if err != nil {
		t.Fatal(err)
	}
	_, am, _ := call(http.MethodPost, "/api/auth/login", `{"username":"admin1","password":"Parol-Salom-123"}`, "", "")
	adminTok, _ := am["access"].(string)
	if adminTok == "" {
		t.Fatal("admin access yo'q")
	}
	body, _ := json.Marshal(map[string]any{"disabled": true})
	if code, m, _ = call(http.MethodPatch, "/api/admin/users/"+itoa64(u.ID), string(body), adminTok, ""); code != http.StatusOK {
		t.Fatalf("patch: %d %v", code, m)
	}
	time.Sleep(30 * time.Millisecond) // avlod keshi (testda 1 ms)
	if code, _, _ := call(http.MethodGet, "/api/me", "", access, ""); code != http.StatusUnauthorized {
		t.Fatalf("o'chirilgan foydalanuvchi access tokeni ishlayapti: %d", code)
	}
	// Yangi refresh ham endi yaroqsiz (revoke).
	if code, _, _ := call(http.MethodPost, "/api/auth/refresh", "", "", newRefresh); code != http.StatusUnauthorized {
		t.Fatalf("o'chirilgan hisob refresh'i: %d", code)
	}
}
