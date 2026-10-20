package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"

	"github.com/go-chi/chi/v5"
)

// Issue #12: alerts are written with the event and replayed until each caregiver acks.

func unacked(t *testing.T, h *handlers.AlertsHandler, who *models.Caregiver) []handlers.Alert {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/unacked", nil)
	r = r.WithContext(authContext(r.Context(), who))
	rec := httptest.NewRecorder()
	h.Unacked(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("unacked: %d %s", rec.Code, rec.Body.String())
	}
	var out []handlers.Alert
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

func ack(t *testing.T, h *handlers.AlertsHandler, who *models.Caregiver, id string) int {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/"+id+"/ack", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", id)
	r = r.WithContext(context.WithValue(authContext(r.Context(), who), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	h.Ack(rec, r)
	return rec.Code
}

func TestCrisisVitalCreatesAlertUntilAcked(t *testing.T) {
	med, vitals, cleanup := setupTestDB(t)
	defer cleanup()
	alerts := &handlers.AlertsHandler{DB: med.DB}

	postVitals(t, vitals, map[string]interface{}{"vital_type": "bp", "systolic": 130, "diastolic": 85}) // not a crisis
	postVitals(t, vitals, map[string]interface{}{"vital_type": "bp", "systolic": 185, "diastolic": 115})

	var n int
	med.DB.QueryRow(`SELECT COUNT(*) FROM alerts a JOIN vitals_logs v ON v.id = a.ref_id WHERE a.kind = 'bp_crisis' AND v.systolic = 185`).Scan(&n)
	if n != 1 {
		t.Fatalf("want exactly one bp_crisis alert for the crisis reading, got %d", n)
	}

	if got := unacked(t, alerts, fatima); len(got) != 0 {
		t.Fatalf("the caregiver who logged it gets no alert, got %d", len(got))
	}
	for _, who := range []*models.Caregiver{ali, hussain} {
		got := unacked(t, alerts, who)
		if len(got) != 1 || got[0].Kind != "bp_crisis" || got[0].CreatedByName != fatima.Name {
			t.Fatalf("%s: want the crisis alert, got %+v", who.ID, got)
		}
		var p map[string]float64
		_ = json.Unmarshal(got[0].Payload, &p)
		if p["systolic"] != 185 || p["diastolic"] != 115 {
			t.Fatalf("alert payload: %s", got[0].Payload)
		}
	}

	id := unacked(t, alerts, ali)[0].ID
	if code := ack(t, alerts, ali, id); code != http.StatusOK {
		t.Fatalf("ack: %d", code)
	}
	if code := ack(t, alerts, ali, id); code != http.StatusOK {
		t.Fatalf("second ack must be idempotent: %d", code)
	}
	if got := unacked(t, alerts, ali); len(got) != 0 {
		t.Fatalf("acked alert must not come back for Ali, got %d", len(got))
	}
	if got := unacked(t, alerts, hussain); len(got) != 1 {
		t.Fatalf("Hussain hasn't acked yet: want 1, got %d", len(got))
	}
	if code := ack(t, alerts, ali, "nope"); code != http.StatusNotFound {
		t.Fatalf("unknown alert: want 404, got %d", code)
	}

	// Older than a day: no longer replayed.
	med.DB.Exec(`UPDATE alerts SET created_at = ?`, time.Now().UTC().Add(-25*time.Hour).Format("2006-01-02 15:04:05"))
	if got := unacked(t, alerts, hussain); len(got) != 0 {
		t.Fatalf("alerts older than 24 h must stop replaying, got %d", len(got))
	}
}

// The crisis reading and its alert are one transaction: no alert, no reading.
func TestCrisisAlertIsInTheSameTransaction(t *testing.T) {
	med, vitals, cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := med.DB.Exec(`CREATE TRIGGER fail_alert BEFORE INSERT ON alerts BEGIN SELECT RAISE(ABORT, 'alerts down'); END`); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]interface{}{"vital_type": "bp", "systolic": 190, "diastolic": 120})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/vitals", bytes.NewReader(b))
	r = r.WithContext(authContext(r.Context(), fatima))
	rec := httptest.NewRecorder()
	vitals.LogVitals(rec, r)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("alert failure: want 500, got %d", rec.Code)
	}
	var n int
	med.DB.QueryRow(`SELECT COUNT(*) FROM vitals_logs`).Scan(&n)
	if n != 0 {
		t.Fatal("the crisis reading was saved without its alert")
	}
}

func TestDoseOverrideAndVomitingCreateAlerts(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	addBedtimeMed(t, h)
	alerts := &handlers.AlertsHandler{DB: h.DB}

	given := time.Now().Add(-4 * time.Hour).UTC().Format(time.RFC3339)
	mustLog(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_warfarin", Slot: "bedtime", ScheduledDate: clinicalDay(-1), GivenAt: given})
	mustLog(t, h, ali, handlers.LogDoseRequest{MedicationID: "med_warfarin", Slot: "bedtime", ScheduledDate: clinicalDay(0), OverrideInterval: true, OverrideReason: "doctor's order"})
	taken := mustLog(t, h, fatima, aspirinToday())
	doseEvent(t, h, fatima, taken, handlers.DoseEventRequest{Type: "vomited", MinutesAfter: intPtr(10)})

	kinds := map[string]int{}
	for _, a := range unacked(t, alerts, hussain) {
		kinds[a.Kind]++
	}
	if kinds["dose_override"] != 1 || kinds["vomited"] != 1 || len(kinds) != 2 {
		t.Fatalf("want one dose_override and one vomited alert, got %v", kinds)
	}
	// A routine dose creates no alert.
	var routine int
	h.DB.QueryRow(`SELECT COUNT(*) FROM alerts WHERE ref_id = ?`, taken).Scan(&routine)
	if routine != 1 { // only the vomited follow-up references it
		t.Fatalf("routine dose must not alert by itself, got %d alerts referencing it", routine)
	}
}
