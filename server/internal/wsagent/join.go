// §3 (1.0-draft.10): ulanish so'rovi. Bir martalik kod bekor qilindi —
// agent so'rov yuboradi, mijoz faqat admin tasdig'idan (yoki IP bo'yicha
// oldindan ruxsat ro'yxatiga mos kelishidan) keyin yaratiladi.
//
// Sir sxemasi: agent o'z `agent_secret`ini O'ZI yaratadi va so'rovda
// yuboradi; server uni faqat sha256 hash ko'rinishida saqlaydi. Tasdiqdan
// keyin polling javobida faqat `agent_id` qaytadi — sir serverda hech
// qachon ochiq holda saqlanmaydi va tashqariga chiqmaydi.
package wsagent

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"central-vms/server/internal/registry"
)

// HashSecret — agent_secret uchun yagona hash manbai (WS auth ham shu orqali).
func HashSecret(secret string) string {
	s := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(s[:])
}

// Join xato kodlari (docs/api.md: 401/403/409/429).
var (
	ErrRateLimited = errors.New("join: juda ko'p urinish")
	ErrNameTaken   = errors.New("join: bunday nom band")
	ErrRejected    = errors.New("join: so'rov rad etilgan")
	ErrBadKey      = errors.New("join: kalit yaroqsiz")
)

// JoinInput — §3.1 so'rov maydonlari.
type JoinInput struct {
	JoinKey      string `json:"join_key"`     // agentning doimiy kaliti (hex, ≥32)
	AgentSecret  string `json:"agent_secret"` // agent yaratgan sir (≥32 belgi)
	Hostname     string `json:"hostname"`
	Name         string `json:"name"` // ko'rinadigan nom (ixtiyoriy)
	OS           string `json:"os"`
	LanIP        string `json:"lan_ip"`
	AgentVersion string `json:"agent_version"`
	Caps         any    `json:"caps"`
}

// JoinOutcome — §3.2 javobi.
type JoinOutcome struct {
	Status    string // pending | approved | rejected
	AgentID   string
	RequestID int64
}

// Joiner — so'rovlarni qabul qiladi va admin amallarini bajaradi.
type Joiner struct {
	db  *sql.DB
	reg *registry.Registry
	// rate — YANGI so'rovlar uchun (IP bo'yicha 5/5 min): ro'yxatni
	// shovutlab qo'ymaslik. poll — o'z holatini so'rash (polling) uchun
	// kengroq oyna: agent 15 s davrda tekshiradi, tasdiqni kutish
	// normal holat (§3.2) va u limitga urilmasligi kerak (2026-11-20:
	// bitta limit polling'ni ham 5 urinishda to'xtatib qo'ygandi).
	rate *rateWin
	poll *rateWin
	lg   *slog.Logger
}

// NewJoiner — limit: IP bo'yicha 5 so'rov / 5 daqiqa (eski enroll limiti
// bilan bir xil: operator xatolik bilan ro'yxatni shovutlab qo'ymasligi uchun).
func NewJoiner(db *sql.DB, reg *registry.Registry, lg *slog.Logger, ratePer5m int, c clock) *Joiner {
	if lg == nil {
		lg = slog.Default()
	}
	if ratePer5m <= 0 {
		ratePer5m = 5
	}
	return &Joiner{db: db, reg: reg,
		rate: newRateWin(ratePer5m, 5*time.Minute, c),
		poll: newRateWin(40, 5*time.Minute, c), lg: lg}
}

// Request — §3.1/§3.2: so'rovni qabul qilish yoki mavjud so'rovning
// holatini qaytarish (agent polling shu chaqiruvni takrorlaydi).
func (j *Joiner) Request(in JoinInput, sourceIP string) (JoinOutcome, error) {
	keyHash, err := hashJoinKey(in.JoinKey)
	if err != nil {
		return JoinOutcome{}, err
	}
	// Kalit allaqachon qayd etilgan — bu polling (holat so'rash): keng
	// oynada tekshiriladi. Yangi kalit — yangi so'rov: qat'iy limit.
	known, err := j.reg.GetJoinRequest(keyHash)
	which := j.rate
	if err == nil {
		which = j.poll
	} else if !errors.Is(err, registry.ErrNotFound) {
		return JoinOutcome{}, err
	}
	if !which.allow(sourceIP) {
		return JoinOutcome{}, ErrRateLimited
	}
	_ = known
	if len(in.AgentSecret) < 32 {
		return JoinOutcome{}, fmt.Errorf("%w: agent_secret kamida 32 belgi", ErrBadKey)
	}
	q, created, err := j.reg.UpsertJoinRequest(registry.JoinInfo{
		JoinKeyHash:     keyHash,
		AgentSecretHash: HashSecret(in.AgentSecret),
		Hostname:        strings.TrimSpace(in.Hostname),
		Name:            registry.SanitizeName(in.Name),
		OS:              strings.TrimSpace(in.OS),
		LanIP:           strings.TrimSpace(in.LanIP),
		SourceIP:        sourceIP,
		AgentVersion:    strings.TrimSpace(in.AgentVersion),
		CapsJSON:        mustJSON(in.Caps),
	})
	if err != nil {
		return JoinOutcome{}, err
	}
	switch q.Status {
	case "approved":
		// Agent qayta so'rayapti (masalan disk state'ini yo'qotgan):
		// agent_id qaytariladi, sir esa hech qachon qaytarmaydi.
		c, err := j.reg.GetClientByID(q.ClientID)
		if errors.Is(err, registry.ErrNotFound) {
			// Mijoz qatori server tomonidan o'chirilgan (yoki baza qayta
			// boshlangan): tasdiq yetim. Agentga sabani ochiq aytamiz —
			// aks holdu u cheksiz "pending" kutadi (§3.2, 1.0-draft.11).
			note := "mijoz qatori server tomonidan o'chirilgan — lokal UI'da Clear connection, so'ng Request access"
			if uerr := j.reg.InvalidateOrphanJoin(q.ID, note); uerr != nil {
				j.lg.Warn("yetim tasdiq holati yangilanmadi", "request_id", q.ID, "err", uerr)
			}
			j.lg.Info("tasdiqlangan so'rov yetim qoldi (mijoz o'chirilgan)",
				"request_id", q.ID, "hostname", q.Hostname)
			return JoinOutcome{Status: "rejected", RequestID: q.ID},
				fmt.Errorf("%w: %s", ErrRejected, note)
		}
		if err != nil {
			return JoinOutcome{}, err
		}
		return JoinOutcome{Status: "approved", AgentID: c.AgentID, RequestID: q.ID}, nil
	case "rejected":
		return JoinOutcome{Status: "rejected", RequestID: q.ID},
			fmt.Errorf("%w: %s", ErrRejected, q.Note)
	}
	// pending. Yangi so'rov bo'lsa va IP oldindan ruxsat etilgan bo'lsa —
	// darhol tasdiqlaymiz (operator IP'ni server UI'dan yozib qo'ygan).
	if created {
		if p, err := j.reg.MatchPreauth(sourceIP); err != nil {
			j.lg.Warn("preauth tekshiruvi", "ip", sourceIP, "err", err)
		} else if p != nil {
			out, aerr := j.approveAuto(q, p.Name)
			if aerr != nil {
				j.lg.Warn("preauth avtomatik tasdiqlanmadi",
					"ip", sourceIP, "preauth", p.IP, "err", aerr)
			} else {
				j.lg.Info("ulanish avtomatik tasdiqlandi (IP oldindan ruxsat)",
					"ip", sourceIP, "agent_id", out.AgentID, "name", p.Name)
				return out, nil
			}
		}
		j.lg.Info("ulanish so'rovi qabul qilindi — admin tasdig'i kutilmoqda",
			"request_id", q.ID, "hostname", q.Hostname, "name", q.Name,
			"ip", sourceIP, "lan_ip", q.LanIP)
	}
	return JoinOutcome{Status: "pending", RequestID: q.ID}, nil
}

// Approve — admin panel buyrug'i (§3.3). Nom berilmasa so'rovdagisi
// ishlatiladi; band bo'lsa ErrNameTaken (admin nomni tahrirlaydi).
func (j *Joiner) Approve(requestID int64, name string, decidedBy int64) (JoinOutcome, error) {
	q, err := j.reg.GetJoinRequestByID(requestID)
	if err != nil {
		return JoinOutcome{}, err
	}
	base := registry.SanitizeName(name)
	if base == "" {
		base = q.Name
	}
	if base == "" {
		base = autoName()
	}
	return j.approveWith(requestID, base, q.AgentSecretHash, decidedBy, false)
}

// approveAuto — preauth holati: nom band bo'lsa avtomatik raqam qo'shiladi
// (hech kim qarab turgan emas, jarayon to'xtamasligi kerak).
func (j *Joiner) approveAuto(q registry.JoinRequest, preauthName string) (JoinOutcome, error) {
	base := registry.SanitizeName(preauthName)
	if base == "" {
		base = q.Name
	}
	if base == "" {
		base = autoName()
	}
	return j.approveWith(q.ID, base, q.AgentSecretHash, 0, true)
}

func (j *Joiner) approveWith(requestID int64, name, secretHash string, decidedBy int64, auto bool) (JoinOutcome, error) {
	if secretHash == "" {
		return JoinOutcome{}, fmt.Errorf("%w: so'rovda sir hash' yo'q", ErrBadKey)
	}
	agentID := "ag_" + randHexLower(16)
	if agentID == "ag_" {
		return JoinOutcome{}, errors.New("join: entropy ishlamadi")
	}
	chosen := name
	for attempt := 1; ; attempt++ {
		clientID, err := j.reg.ApproveJoin(requestID, agentID, secretHash, chosen, decidedBy)
		if err == nil {
			j.lg.Info("ulanish so'rovi tasdiqlandi", "request_id", requestID,
				"client_id", clientID, "agent_id", agentID, "name", chosen,
				"auto", auto, "decided_by", decidedBy)
			return JoinOutcome{Status: "approved", AgentID: agentID, RequestID: requestID}, nil
		}
		if !auto || !errors.Is(err, registry.ErrNameTaken) || attempt >= 20 {
			if errors.Is(err, registry.ErrNameTaken) {
				return JoinOutcome{}, fmt.Errorf("%w: %s", ErrNameTaken, chosen)
			}
			return JoinOutcome{}, err
		}
		chosen = fmt.Sprintf("%s (%d)", name, attempt+1)
	}
}

// Reject — admin rad etadi; sabab `note`da qoladi va agentga ko'rsatiladi.
func (j *Joiner) Reject(requestID int64, note string, decidedBy int64) error {
	if err := j.reg.RejectJoin(requestID, note, decidedBy); err != nil {
		return err
	}
	j.lg.Info("ulanish so'rovi rad etildi", "request_id", requestID, "note", note)
	return nil
}

// Delete — eski so'rovni o'chiradi (agent keyin yangi so'rov yuborishi mumkin).
func (j *Joiner) Delete(requestID int64) error {
	return j.reg.DeleteJoinRequest(requestID)
}

// hashJoinKey — agent kaliti: 32..128 belgili hex. Xom kalit saqlanmaydi,
// faqat sha256'i.
func hashJoinKey(raw string) (string, error) {
	k := strings.ToLower(strings.TrimSpace(raw))
	if len(k) < 32 || len(k) > 128 {
		return "", fmt.Errorf("%w: join_key 32..128 belgi bo'lishi kerak", ErrBadKey)
	}
	if _, err := hex.DecodeString(k); err != nil {
		return "", fmt.Errorf("%w: join_key faqat hex belgilardan iborat", ErrBadKey)
	}
	return HashSecret(k), nil
}

func autoName() string { return "agent-" + randHexLower(8) }

func randHexLower(n int) string {
	b := make([]byte, n/2)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func randToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b) // 256-bit → 43 belgi
}
