package redact

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestString_TableDriven(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "rtsp userinfo paroli",
			in:   "ulanish: rtsp://admin:Secret123@192.168.10.31:554/cam/realmonitor?channel=1&subtype=0",
			want: "ulanish: rtsp://admin:****@192.168.10.31:554/cam/realmonitor?channel=1&subtype=0",
		},
		{
			name: "rtsp parolsiz URL o'zgarmaydi",
			in:   "rtsp://192.168.10.31:554/cam/realmonitor?channel=1&subtype=1",
			want: "rtsp://192.168.10.31:554/cam/realmonitor?channel=1&subtype=1",
		},
		{
			name: "publish URL token query",
			in:   "rtsp://srv:8554/cam/ag_9f2c1a7e/6f0f?token=Zm9vYmFyYmF6cXV1eA",
			want: "rtsp://srv:8554/cam/ag_9f2c1a7e/6f0f?token=****",
		},
		{
			name: "WHIP Bearer header",
			in:   "POST /whip Authorization: Bearer AbCdEf123456-_==",
			want: "POST /whip Authorization: Bearer ****",
		},
		{
			name: "alohida bearer so'zi",
			in:   "header bearer YmVhcmVyVG9rZW4h",
			want: "header bearer ****",
		},
		{
			name: "JSON agent_secret",
			in:   `{"agent_id":"ag_1","agent_secret":"c2VjcmV0dmFsdWU="}`,
			want: `{"agent_id":"ag_1","agent_secret":"****"}`,
		},
		{
			name: "JSON password",
			in:   `{"user":"admin","password":"hunter2"}`,
			want: `{"user":"admin","password":"****"}`,
		},
		{
			name: "JSON publish_token",
			in:   `{"publish_token":"abc.def.ghi"}`,
			want: `{"publish_token":"****"}`,
		},
		{
			name: "log kalit=qiymat",
			in:   "camera_id=6f0f password=hunter2 token=xyz",
			want: "camera_id=6f0f password=**** token=****",
		},
		{
			name: "maxfiy bo'lmagan satr o'zgarmaydi",
			in:   "session se_7c1f state=playing ingest=whip transport=udp",
			want: "session se_7c1f state=playing ingest=whip transport=udp",
		},
		{
			name: "bo'sh satr",
			in:   "",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := String(tc.in)
			if got != tc.want {
				t.Fatalf("natija:\n  %q\nkutilgan:\n  %q", got, tc.want)
			}
			// Idempotentlik: qayta qo'llash o'zgarmas bo'lishi kerak.
			if again := String(got); again != got {
				t.Fatalf("idempotent emas: %q -> %q", got, again)
			}
		})
	}
}

func TestString_NoSecretLeft(t *testing.T) {
	// Barcha qoidalar aralashgan haqiqiy xato satri.
	in := `publish failed url=rtsp://user:pass@10.0.0.5:8554/cam/x?token=T0tFTg== ` +
		`auth="Bearer T0tFTg==" body={"agent_secret":"c3VwZXJzZWNyZXQ="} err=password=parol2`
	out := String(in)
	for _, leak := range []string{"pass@", "T0tFTg", "c3VwZXJzZWNyZXQ", "parol2"} {
		if strings.Contains(out, leak) {
			t.Fatalf("sizib chiqdi %q: %s", leak, out)
		}
	}
}

func TestErr(t *testing.T) {
	if got := Err(nil); got != "" {
		t.Fatalf("nil uchun: %q", got)
	}
	err := errors.New("rtsp://u:p@host: DESCRIBE failed")
	if got := Err(err); strings.Contains(got, "u:p@") {
		t.Fatalf("Err redaktasiya qilmadi: %s", got)
	}
	if got := Wrap(err); got == nil || strings.Contains(got.Error(), "u:p@") {
		t.Fatalf("Wrap redaktasiya qilmadi: %v", got)
	}
}

func TestHandler_RedactsMessageAndAttrs(t *testing.T) {
	var buf bytes.Buffer
	lg := slog.New(NewHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	lg.Info("kamera ulanmadi",
		"camera_id", "6f0f",
		"url", "rtsp://admin:Secret123@192.168.10.31/cam",
		"token", "T0tFTg",
		"group", slog.Group("auth", slog.String("password", "hunter2")),
	)
	lg.Error("xato rtsp://admin:Secret123@host token=abc")

	out := buf.String()
	for _, leak := range []string{"Secret123", "T0tFTg", "hunter2"} {
		if strings.Contains(out, leak) {
			t.Fatalf("log'da maxfiy qiymat qoldi %q:\n%s", leak, out)
		}
	}
	for _, want := range []string{"camera_id=6f0f", "****"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log'da %q yo'q:\n%s", want, out)
		}
	}
}

func TestHandler_WithAttrsRedacted(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(NewHandler(&buf, nil)).With("pass", "topsecret")
	base.Info("holat")
	if strings.Contains(buf.String(), "topsecret") {
		t.Fatalf("WithAttrs redaktasiya qilmadi:\n%s", buf.String())
	}
}

func TestWrap_Nil(t *testing.T) {
	if Wrap(nil) != nil {
		t.Fatal("Wrap(nil) nil bo'lishi kerak")
	}
}

func TestHandler_GroupAndAnyAttrs(t *testing.T) {
	var buf bytes.Buffer
	lg := slog.New(NewHandler(&buf, nil))

	lg.Info("holat",
		slog.Group("net",
			slog.String("url", "rtsp://u:p@host"),
			slog.Int("rtt_ms", 42),
		),
		slog.Any("err", errors.New("token=abc123 rad etildi")),
		slog.Any("raw", 12345),
	)
	out := buf.String()
	for _, leak := range []string{"u:p@", "abc123"} {
		if strings.Contains(out, leak) {
			t.Fatalf("sizib chiqdi %q:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, "rtt_ms=42") || !strings.Contains(out, "raw=12345") {
		t.Fatalf("oddiy qiymatlar buzildi:\n%s", out)
	}
}

func TestHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	lg := slog.New(NewHandler(&buf, nil)).WithGroup("agent")
	lg.Info("sinov", "pass", "sirr")
	out := buf.String()
	if strings.Contains(out, "sirr") {
		t.Fatalf("WithGroup holatida redaktasiya ishlamadi:\n%s", out)
	}
	if !strings.Contains(out, "agent.pass=****") {
		t.Fatalf("kutilgan group formati:\n%s", out)
	}
}

func TestHandler_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	lg := slog.New(NewHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	lg.Debug("token=secretvalue")
	if buf.Len() != 0 {
		t.Fatalf("debug yozuvi o'tib ketdi:\n%s", buf.String())
	}
	lg.Warn("diqqat", "token", "secretvalue")
	if strings.Contains(buf.String(), "secretvalue") {
		t.Fatalf("warn satrida maxfiy qoldi:\n%s", buf.String())
	}
}

func TestHandler_NestedGroupAndAttrSlice(t *testing.T) {
	var buf bytes.Buffer
	lg := slog.New(NewHandler(&buf, nil))
	lg.Info("ichma-ich",
		slog.Group("a", slog.Group("b", slog.String("secret", "qiymat"))),
		slog.Any("attrs", []slog.Attr{slog.String("password", "sir"), slog.Bool("ok", true)}),
	)
	out := buf.String()
	for _, leak := range []string{"qiymat", "sir"} {
		if strings.Contains(out, leak) {
			t.Fatalf("sizib chiqdi %q:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, "ok=true") {
		t.Fatalf("bool buzildi:\n%s", out)
	}
}

func TestRedactAttr_Direct(t *testing.T) {
	got := redactAttr(slog.Any("attrs", []slog.Attr{slog.String("token", "abc")}))
	if strings.Contains(got.Value.String(), "abc") {
		t.Fatalf("[]slog.Attr redaktasiya qilinmadi: %v", got.Value)
	}

	single := redactAttr(slog.Any("nested", slog.String("pass", "sir")))
	if strings.Contains(single.Value.String(), "sir") {
		t.Fatalf("ichma-ich Attr redaktasiya qilinmadi: %v", single.Value)
	}

	if got := redactAttr(slog.Any("err", errors.New("pass=sir"))); !strings.Contains(got.Value.String(), "****") {
		t.Fatalf("error redaktasiya qilinmadi: %v", got.Value)
	}

	if got := redactAttr(slog.Int64("bytes_sent", 1024)); got.Value.Any() != int64(1024) {
		t.Fatalf("son qiymati o'zgardi: %v", got.Value)
	}

	if got := redactAttr(slog.Duration("ttl", time.Second)); got.Value.Any() != time.Second {
		t.Fatalf("duration o'zgardi: %v", got.Value)
	}
}

func TestRedactAttr_AnyCompositeAndPrimitive(t *testing.T) {
	got := redactAttr(slog.Any("cfg", map[string]string{"password": "sirli_parol"}))
	if strings.Contains(got.Value.String(), "sirli_parol") {
		t.Fatalf("map ichidagi parol chiqib ketdi: %v", got.Value)
	}
	if got.Value.Kind() != slog.KindString {
		t.Fatalf("map satrga aylantirilmadi: %v", got.Value.Kind())
	}

	ptr := redactAttr(slog.Any("cam", &struct{ Pass string }{"yashirin"}))
	if strings.Contains(ptr.Value.String(), "yashirin") {
		t.Fatalf("struct pointer ichida maxfiy qoldi: %v", ptr.Value)
	}

	pr := redactAttr(slog.Any("n", 42))
	if fmt.Sprint(pr.Value.Any()) != "42" {
		t.Fatalf("primitiv o'zgardi: %v (%v)", pr.Value, pr.Value.Kind())
	}
}
