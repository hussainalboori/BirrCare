package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/push"
	"birrcare/backend/internal/sse"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Issue #19: an emergency card for paramedics. GET /emergency/{token} needs no
// login (Cloudflare Access bypasses /emergency/* only); the 256-bit token in the
// QR code is the secret. It returns only what a paramedic needs: no PINs, no
// emails, no audit data. Every view is audited and the family is alerted.

type EmergencyHandler struct {
	DB       *sql.DB
	Broker   *sse.Broker
	Push     *push.Notifier
	Contacts Contacts
	Limiter  *IPRateLimiter // nil = 10 per minute per IP
}

// PatientProfile is the single patient's details (admin-edited).
type PatientProfile struct {
	Name        string `json:"name"`
	DOB         string `json:"dob"` // YYYY-MM-DD
	DiagnosisAr string `json:"diagnosis_ar"`
	DiagnosisEn string `json:"diagnosis_en"`
	StrokeDate  string `json:"stroke_date"` // YYYY-MM-DD
	AllergiesAr string `json:"allergies_ar"`
	AllergiesEn string `json:"allergies_en"`
	BloodType   string `json:"blood_type"`
	BaselineBP  string `json:"baseline_bp"`
	Notes       string `json:"notes"`
}

type EmergencyMed struct {
	NameAr   string   `json:"name_ar"`
	NameEn   string   `json:"name_en"`
	Dosage   string   `json:"dosage"`
	Slots    []string `json:"slots"`
	Times    []string `json:"times,omitempty"`
	Critical bool     `json:"blood_thinner"` // anticoagulant / antiplatelet
}

type EmergencyBP struct {
	Systolic   int    `json:"systolic"`
	Diastolic  int    `json:"diastolic"`
	Pulse      *int   `json:"pulse,omitempty"`
	RecordedAt string `json:"recorded_at"`
}

type EmergencyCard struct {
	Patient     PatientProfile    `json:"patient"`
	Medications []EmergencyMed    `json:"medications"`
	LastBP      []EmergencyBP     `json:"last_bp"`
	LastSugar   *EmergencyValue   `json:"last_sugar,omitempty"`
	LastAlert   *EmergencyValue   `json:"last_alertness,omitempty"`
	LastFAST    *EmergencyFAST    `json:"last_fast,omitempty"`
	Contacts    Contacts          `json:"contacts"`
	Family      []EmergencyFamily `json:"family"`
	GeneratedAt string            `json:"generated_at"`
	TimeZone    string            `json:"time_zone"`
}

type EmergencyValue struct {
	Value      string `json:"value"`
	RecordedAt string `json:"recorded_at"`
}

type EmergencyFAST struct {
	Face         bool   `json:"face"`
	Arm          bool   `json:"arm"`
	Speech       bool   `json:"speech"`
	RecordedAt   string `json:"recorded_at"`
	LastNormalAt string `json:"last_normal_at,omitempty"`
}

type EmergencyFamily struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

const profileCols = `name, dob, diagnosis_ar, diagnosis_en, stroke_date, allergies_ar, allergies_en, blood_type, baseline_bp, notes`

func loadProfile(db *sql.DB) (PatientProfile, error) {
	var p PatientProfile
	err := db.QueryRow(`SELECT `+profileCols+` FROM patient_profile WHERE id = 1`).Scan(
		&p.Name, &p.DOB, &p.DiagnosisAr, &p.DiagnosisEn, &p.StrokeDate, &p.AllergiesAr, &p.AllergiesEn, &p.BloodType, &p.BaselineBP, &p.Notes)
	return p, err
}

func (h *EmergencyHandler) buildCard() (*EmergencyCard, error) {
	p, err := loadProfile(h.DB)
	if err != nil {
		return nil, err
	}
	card := &EmergencyCard{Patient: p, Contacts: h.Contacts, Medications: []EmergencyMed{}, LastBP: []EmergencyBP{}, Family: []EmergencyFamily{},
		GeneratedAt: time.Now().UTC().Format(time.RFC3339), TimeZone: ClinicalLocation().String()}

	rows, err := h.DB.Query(`SELECT name_ar, name_en, dosage, frequency_slots, COALESCE(slot_times, ''), COALESCE(is_critical, 0)
		FROM medications WHERE is_active = 1 ORDER BY is_critical DESC, name_en`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m EmergencyMed
		var slots, times string
		if err := rows.Scan(&m.NameAr, &m.NameEn, &m.Dosage, &slots, &times, &m.Critical); err != nil {
			rows.Close()
			return nil, err
		}
		_ = json.Unmarshal([]byte(slots), &m.Slots)
		var byslot map[string]string
		if json.Unmarshal([]byte(times), &byslot) == nil {
			for _, s := range m.Slots {
				if t := byslot[s]; t != "" {
					m.Times = append(m.Times, t)
				}
			}
		}
		card.Medications = append(card.Medications, m)
	}
	rows.Close()

	// Corrected readings (issue #15) are replaced by their correction.
	bp, err := h.DB.Query(`SELECT systolic, diastolic, pulse, recorded_at FROM vitals_logs
		WHERE superseded_by IS NULL AND systolic IS NOT NULL AND diastolic IS NOT NULL ORDER BY recorded_at DESC LIMIT 3`)
	if err != nil {
		return nil, err
	}
	for bp.Next() {
		var b EmergencyBP
		var pulse sql.NullInt64
		if err := bp.Scan(&b.Systolic, &b.Diastolic, &pulse, &b.RecordedAt); err != nil {
			bp.Close()
			return nil, err
		}
		if pulse.Valid {
			v := int(pulse.Int64)
			b.Pulse = &v
		}
		b.RecordedAt = sqliteToRFC3339(b.RecordedAt)
		card.LastBP = append(card.LastBP, b)
	}
	bp.Close()

	var sugar float64
	var at string
	if h.DB.QueryRow(`SELECT blood_sugar, recorded_at FROM vitals_logs WHERE superseded_by IS NULL AND blood_sugar IS NOT NULL
		ORDER BY recorded_at DESC LIMIT 1`).Scan(&sugar, &at) == nil {
		card.LastSugar = &EmergencyValue{Value: strconv.FormatFloat(sugar, 'f', -1, 64) + " mmol/L", RecordedAt: sqliteToRFC3339(at)}
	}
	var level string
	if h.DB.QueryRow(`SELECT alertness_level, recorded_at FROM vitals_logs WHERE superseded_by IS NULL AND alertness_level IS NOT NULL
		ORDER BY recorded_at DESC LIMIT 1`).Scan(&level, &at) == nil {
		card.LastAlert = &EmergencyValue{Value: level, RecordedAt: sqliteToRFC3339(at)}
	}
	var fast, lastNormal string
	if h.DB.QueryRow(`SELECT fast_result, recorded_at, COALESCE(last_normal_at, '') FROM vitals_logs
		WHERE superseded_by IS NULL AND vital_type = 'fast' AND fast_result IS NOT NULL ORDER BY recorded_at DESC LIMIT 1`).Scan(&fast, &at, &lastNormal) == nil {
		var f FastResult
		if json.Unmarshal([]byte(fast), &f) == nil {
			card.LastFAST = &EmergencyFAST{Face: f.Face, Arm: f.Arm, Speech: f.Speech, RecordedAt: sqliteToRFC3339(at)}
			if lastNormal != "" {
				card.LastFAST.LastNormalAt = sqliteToRFC3339(lastNormal)
			}
		}
	}

	fam, err := h.DB.Query(`SELECT name, phone FROM caregivers WHERE is_active = 1 AND COALESCE(phone, '') <> '' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	for fam.Next() {
		var f EmergencyFamily
		if err := fam.Scan(&f.Name, &f.Phone); err == nil {
			card.Family = append(card.Family, f)
		}
	}
	fam.Close()
	return card, nil
}

func emergencyTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// clientIP is the visitor's address: Cloudflare's Cf-Connecting-IP (the
// services are only reachable through the tunnel), else the connection.
func clientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("Cf-Connecting-Ip")); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// alertEvery limits "emergency card opened" alerts: paramedics may reload it.
const alertEvery = 10 * time.Minute

// Card is the public GET /emergency/{token}. A wrong or missing token is a
// plain 404, with no hint whether a card exists.
func (h *EmergencyHandler) Card(w http.ResponseWriter, r *http.Request) {
	lim := h.Limiter
	if lim == nil {
		lim = defaultEmergencyLimiter
	}
	if !lim.Allow(clientIP(r), time.Now()) {
		w.Header().Set("Retry-After", "60")
		writeJSONError(w, http.StatusTooManyRequests, "TOO_MANY_REQUESTS", "too many requests, try again in a minute")
		return
	}
	token := chi.URLParam(r, "token")
	var stored sql.NullString
	_ = h.DB.QueryRow(`SELECT emergency_token_hash FROM patient_profile WHERE id = 1`).Scan(&stored)
	if len(token) < 32 || !stored.Valid || subtle.ConstantTimeCompare([]byte(emergencyTokenHash(token)), []byte(stored.String)) != 1 {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "not found")
		return
	}
	card, err := h.buildCard()
	if err != nil {
		internalError(w, "failed to build the emergency card", err)
		return
	}

	// A signed-in caregiver's phone refreshing its offline copy is not a view.
	me := auth.GetCaregiverFromContext(r.Context())
	if !(me != nil && r.URL.Query().Get("prefetch") == "1") {
		h.recordView(me)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(card)
}

func (h *EmergencyHandler) recordView(me *models.Caregiver) {
	by, name := "", ""
	if me != nil {
		by, name = me.ID, me.Name
	}
	detail := "Emergency card opened (no login)"
	if name != "" {
		detail = "Emergency card opened by " + name
	}
	// Anonymous (paramedic) views have no caregiver: NULL, not "".
	if _, err := h.DB.Exec(`INSERT INTO audit_logs (id, caregiver_id, action, entity_type, entity_id, details, created_at)
		VALUES (?, ?, 'EMERGENCY_CARD_VIEWED', 'patient_profile', '1', ?, datetime('now'))`, uuid.New().String(), nullIfEmpty(by), detail); err != nil {
		log.Printf("[AUDIT] emergency card view: %v", err)
	}

	since := time.Now().UTC().Add(-alertEvery).Format(sqliteTimeLayout)
	var recent int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM alerts WHERE kind = 'emergency_card' AND created_at >= ?`, since).Scan(&recent)
	alertID := ""
	if recent == 0 {
		tx, err := h.DB.Begin()
		if err == nil {
			alertID, err = recordAlertFor(tx, "emergency_card", "1", by, "", map[string]interface{}{"opened_by": name})
			if err == nil {
				err = tx.Commit()
			} else {
				tx.Rollback()
			}
		}
		if err != nil {
			alertID = ""
		} else {
			h.Push.NotifyAsync(alertID, "emergency_card", by)
		}
	}
	h.Broker.Broadcast("EMERGENCY_CARD_OPENED", models.ActorInfo{ID: by, Name: name}, map[string]interface{}{
		"opened_by": name, "alert_id": alertID,
	})
}

// GetProfile returns the patient profile and whether an emergency link exists.
func (h *EmergencyHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	p, err := loadProfile(h.DB)
	if err != nil {
		internalError(w, "failed to load the patient profile", err)
		return
	}
	var token sql.NullString
	var rotated sql.NullString
	_ = h.DB.QueryRow(`SELECT emergency_token, token_rotated_at FROM patient_profile WHERE id = 1`).Scan(&token, &rotated)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"profile": p, "has_emergency_link": token.Valid && token.String != "", "token_rotated_at": sqliteToRFC3339(rotated.String),
	})
}

// UpdateProfile is admin only.
func (h *EmergencyHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageMedications(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "only an admin can edit the patient profile")
		return
	}
	var p PatientProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	for _, f := range []*string{&p.Name, &p.DOB, &p.DiagnosisAr, &p.DiagnosisEn, &p.StrokeDate, &p.AllergiesAr, &p.AllergiesEn, &p.BloodType, &p.BaselineBP, &p.Notes} {
		*f = strings.TrimSpace(*f)
		if len(*f) > 2000 {
			writeJSONError(w, http.StatusBadRequest, "TOO_LONG", "a field is too long")
			return
		}
	}
	for _, d := range []string{p.DOB, p.StrokeDate} {
		if d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				writeJSONError(w, http.StatusBadRequest, "INVALID_DATE", "dates must be YYYY-MM-DD")
				return
			}
		}
	}
	if _, err := h.DB.Exec(`UPDATE patient_profile SET name = ?, dob = ?, diagnosis_ar = ?, diagnosis_en = ?, stroke_date = ?,
		allergies_ar = ?, allergies_en = ?, blood_type = ?, baseline_bp = ?, notes = ?, updated_at = ?, updated_by = ? WHERE id = 1`,
		p.Name, p.DOB, p.DiagnosisAr, p.DiagnosisEn, p.StrokeDate, p.AllergiesAr, p.AllergiesEn, p.BloodType, p.BaselineBP, p.Notes,
		time.Now().UTC().Format(sqliteTimeLayout), me.ID); err != nil {
		internalError(w, "failed to save the patient profile", err)
		return
	}
	audit.Log(h.DB, me.ID, "PATIENT_PROFILE_UPDATED", "patient_profile", "1", "Updated the emergency card details")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "profile": p})
}

// EmergencyLink gives a signed-in family member the current link (for the SOS
// button and the QR on the fridge). 404 until an admin creates one.
func (h *EmergencyHandler) EmergencyLink(w http.ResponseWriter, r *http.Request) {
	var token, rotated sql.NullString
	_ = h.DB.QueryRow(`SELECT emergency_token, token_rotated_at FROM patient_profile WHERE id = 1`).Scan(&token, &rotated)
	if !token.Valid || token.String == "" {
		writeJSONError(w, http.StatusNotFound, "NO_EMERGENCY_LINK", "no emergency card link yet")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]string{"token": token.String, "rotated_at": sqliteToRFC3339(rotated.String)})
}

// RotateLink (admin) makes a new 256-bit token; old QR codes stop working.
func (h *EmergencyHandler) RotateLink(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageMedications(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "only an admin can make a new emergency link")
		return
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		internalError(w, "failed to make a token", err)
		return
	}
	token := hex.EncodeToString(b)
	now := time.Now().UTC().Format(sqliteTimeLayout)
	if _, err := h.DB.Exec(`UPDATE patient_profile SET emergency_token = ?, emergency_token_hash = ?, token_rotated_at = ? WHERE id = 1`,
		token, emergencyTokenHash(token), now); err != nil {
		internalError(w, "failed to save the token", err)
		return
	}
	audit.Log(h.DB, me.ID, "EMERGENCY_LINK_ROTATED", "patient_profile", "1", "Made a new emergency card link; old QR codes no longer work")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": token, "rotated_at": sqliteToRFC3339(now)})
}

// IPRateLimiter allows Limit requests per IP per one-minute window.
type IPRateLimiter struct {
	Limit int
	mu    sync.Mutex
	seen  map[string]*ipWindow
}

type ipWindow struct {
	start time.Time
	n     int
}

func NewIPRateLimiter(limit int) *IPRateLimiter {
	return &IPRateLimiter{Limit: limit, seen: map[string]*ipWindow{}}
}

var defaultEmergencyLimiter = NewIPRateLimiter(10)

func (l *IPRateLimiter) Allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.seen) > 10000 { // forget stale windows rather than grow forever
		for k, v := range l.seen {
			if now.Sub(v.start) >= time.Minute {
				delete(l.seen, k)
			}
		}
	}
	win := l.seen[ip]
	if win == nil || now.Sub(win.start) >= time.Minute {
		win = &ipWindow{start: now}
		l.seen[ip] = win
	}
	win.n++
	return win.n <= l.Limit
}
