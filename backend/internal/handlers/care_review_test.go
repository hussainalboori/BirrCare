package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
)

// The urgent "red area" handover note must show its real time in the shift notes,
// like every other note, and the care event is audited in its transaction.
func TestRedAreaHandoverNoteHasRealTime(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	h := &handlers.CareHandler{DB: med.DB, Broker: med.Broker}
	fatima := &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}
	if code, out := logCareEvent(t, h, fatima, map[string]interface{}{"kind": "reposition", "value": "back", "skin_condition": "red_area"}); code != http.StatusCreated {
		t.Fatalf("log: %d %v", code, out)
	}

	notes := &handlers.HandoverHandler{DB: med.DB, Broker: med.Broker}
	rec := httptest.NewRecorder()
	notes.ListHandovers(rec, httptest.NewRequest(http.MethodGet, "/api/v1/handovers", nil))
	var list []models.HandoverNote
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || !list[0].UrgentFlag {
		t.Fatalf("want one urgent note, got %+v", list)
	}
	if age := time.Since(list[0].CreatedAt); age < 0 || age > time.Minute {
		t.Fatalf("urgent skin note shows time %v (age %v), not now", list[0].CreatedAt, age)
	}

	var audited int
	_ = med.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'LOG_CARE_EVENT' AND row_hash IS NOT NULL`).Scan(&audited)
	if audited != 1 {
		t.Fatalf("want the care event audited once, got %d", audited)
	}
}
