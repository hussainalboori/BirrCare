package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
)

// Issue #60: falls and seizures.

func logIncident(t *testing.T, h *handlers.CareHandler, who *models.Caregiver, body string) (int, handlers.Incident) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r = r.WithContext(authContext(r.Context(), who))
	rec := httptest.NewRecorder()
	h.LogIncident(rec, r)
	var inc handlers.Incident
	_ = json.Unmarshal(rec.Body.Bytes(), &inc)
	return rec.Code, inc
}

func alertEscalate(t *testing.T, h *handlers.CareHandler, id string) (string, bool) {
	t.Helper()
	var kind, payload string
	if err := h.DB.QueryRow(`SELECT kind, payload FROM alerts WHERE id = ?`, id).Scan(&kind, &payload); err != nil {
		t.Fatal(err)
	}
	var p struct {
		Escalate bool `json:"escalate"`
	}
	json.Unmarshal([]byte(payload), &p)
	return kind, p.Escalate
}

func TestFallAlerts(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	h := &handlers.CareHandler{DB: med.DB, Broker: med.Broker}
	med.DB.Exec(`INSERT INTO medications (id, name_ar, name_en, dosage, frequency_slots, is_critical, created_by) VALUES ('med_apix', 'أبيكسابان', 'Apixaban', '5 mg', '["morning"]', 1, 'cg_hussain')`)

	code, inc := logIncident(t, h, siblings[2], `{"kind":"fall","hit_head":true,"awake_normal":true,"pain_or_bleeding":false}`)
	if code != http.StatusCreated || !inc.Escalate || len(inc.BloodThinners) != 1 || inc.BloodThinners[0] != "Apixaban" {
		t.Fatalf("head knock on a blood thinner: %d %+v", code, inc)
	}
	if kind, esc := alertEscalate(t, h, inc.AlertID); kind != "fall" || !esc {
		t.Fatalf("want a red fall alert, got %s escalate=%t", kind, esc)
	}
	code, inc = logIncident(t, h, siblings[2], `{"kind":"fall","hit_head":false,"awake_normal":true,"pain_or_bleeding":false}`)
	if code != http.StatusCreated || inc.Escalate {
		t.Fatalf("a fall with no head knock, awake, no pain: not escalated, got %+v", inc)
	}
	if _, inc = logIncident(t, h, siblings[2], `{"kind":"fall","hit_head":false,"awake_normal":false,"pain_or_bleeding":false}`); !inc.Escalate {
		t.Fatal("not awake and talking normally must escalate")
	}
	if code, _ := logIncident(t, h, siblings[2], `{"kind":"fall","hit_head":true}`); code != http.StatusBadRequest {
		t.Fatalf("missing answers: want 400, got %d", code)
	}
	if code, _ := logIncident(t, h, &models.Caregiver{ID: "cg_zainab", Role: models.RoleViewer}, `{"kind":"fall","hit_head":false,"awake_normal":true,"pain_or_bleeding":false}`); code != http.StatusForbidden {
		t.Fatalf("viewer: want 403, got %d", code)
	}
	// The same tap sent twice is one fall.
	body := `{"kind":"fall","hit_head":false,"awake_normal":true,"pain_or_bleeding":false,"client_request_id":"tap-1"}`
	_, first := logIncident(t, h, siblings[2], body)
	_, again := logIncident(t, h, siblings[2], body)
	if first.ID == "" || first.ID != again.ID {
		t.Fatalf("idempotent resend: %s vs %s", first.ID, again.ID)
	}
}

func TestSeizureAlerts(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	h := &handlers.CareHandler{DB: med.DB, Broker: med.Broker}

	_, short := logIncident(t, h, siblings[2], `{"kind":"seizure","duration_seconds":90}`)
	if short.Escalate {
		t.Fatalf("a first 90-second seizure is not red: %+v", short)
	}
	_, second := logIncident(t, h, siblings[2], `{"kind":"seizure","duration_seconds":60}`)
	if !second.Escalate || !second.Repeat {
		t.Fatalf("a second seizure within 24 h is red: %+v", second)
	}
	med.DB.Exec(`DELETE FROM care_events WHERE kind = 'seizure'`)
	_, long := logIncident(t, h, siblings[2], `{"kind":"seizure","duration_seconds":360}`)
	if !long.Escalate {
		t.Fatalf("a 6-minute seizure is red: %+v", long)
	}
	if kind, esc := alertEscalate(t, h, long.AlertID); kind != "seizure" || !esc {
		t.Fatalf("alert: %s escalate=%t", kind, esc)
	}
	// Red alerts are emergencies (crisis bar, no backup or update restart).
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(authContext(r.Context(), siblings[1]))
	rec := httptest.NewRecorder()
	(&handlers.AlertsHandler{DB: med.DB}).Unacked(rec, r)
	var alerts []handlers.Alert
	json.Unmarshal(rec.Body.Bytes(), &alerts)
	flags := map[bool]int{}
	for _, a := range alerts {
		flags[a.Emergency]++
	}
	if flags[true] != 2 || flags[false] != 1 {
		t.Fatalf("emergency flags: want 2 red (second, long) and 1 not, got %v", flags)
	}
	if code, _ := logIncident(t, h, siblings[2], `{"kind":"seizure"}`); code != http.StatusBadRequest {
		t.Fatalf("missing duration: want 400, got %d", code)
	}
}

func TestIncidentsOnEmergencyCardAndReport(t *testing.T) {
	ins, _ := setupInsulin(t)
	db := ins.DB
	db.Exec(`INSERT OR IGNORE INTO patient_profile (id) VALUES (1)`)
	db.Exec(`UPDATE patient_profile SET emergency_token_hash = ? WHERE id = 1`, handlers.EmergencyTokenHashForTest(emergencyTestToken))
	h := &handlers.CareHandler{DB: db, Broker: ins.Broker}
	logIncident(t, h, siblings[2], `{"kind":"seizure","duration_seconds":120}`)
	time.Sleep(1100 * time.Millisecond) // a later created_at
	logIncident(t, h, siblings[2], `{"kind":"fall","hit_head":true,"awake_normal":true,"pain_or_bleeding":false,"notes":"by the bathroom door"}`)

	card, raw := emergencyCard(t, &handlers.EmergencyHandler{DB: db, Broker: ins.Broker, Limiter: handlers.NewIPRateLimiter(100)})
	if card.LastIncident == nil || card.LastIncident.Kind != "fall" || card.LastIncident.HitHead == nil || !*card.LastIncident.HitHead {
		t.Fatalf("the card shows the latest fall: %+v", card.LastIncident)
	}
	if strings.Contains(raw, "bathroom") || strings.Contains(raw, "Fatima") || strings.Contains(raw, "فاطمة") {
		t.Fatal("no notes or names on the public card")
	}

	rec := httptest.NewRecorder()
	(&handlers.ExportHandler{DB: db}).ExportDoctorSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export/summary?days=7", nil))
	var out struct {
		Incidents []handlers.Incident `json:"incidents"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Incidents) != 2 || out.Incidents[0].Kind != "fall" || out.Incidents[1].DurationSeconds == nil || *out.Incidents[1].DurationSeconds != 120 {
		t.Fatalf("report incidents: %+v", out.Incidents)
	}
}
