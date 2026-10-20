package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"birrcare/backend/internal/handlers"
)

// Issue #16: retries from the offline outbox are safe.

func TestOwnDoseRetryIsAlreadyLoggedByYou(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	first := mustLog(t, h, fatima, aspirinToday())

	code, out := logDose(t, h, fatima, aspirinToday())
	if code != http.StatusOK || out["already_logged_by_you"] != true || out["log_id"] != first {
		t.Fatalf("own retry: want 200 already_logged_by_you with the first log, got %d %v", code, out)
	}
	if code, out := logDose(t, h, ali, aspirinToday()); code != http.StatusConflict || out["error"] != "DOSE_ALREADY_LOGGED" {
		t.Fatalf("a sibling's tap is still a 409 conflict, got %d %v", code, out)
	}
	skip := aspirinToday()
	skip.Status = "skipped"
	if code, _ := logDose(t, h, fatima, skip); code != http.StatusConflict {
		t.Fatalf("own tap with a different status is a real conflict, not a retry: got %d", code)
	}
	var rows int
	h.DB.QueryRow(`SELECT COUNT(*) FROM medication_logs WHERE medication_id = 'med_aspirin'`).Scan(&rows)
	if rows != 1 {
		t.Fatalf("want one dose row, got %d", rows)
	}
}

func TestVitalsAndNotesAreIdempotent(t *testing.T) {
	med, vitals, cleanup := setupTestDB(t)
	defer cleanup()

	post := func(body map[string]interface{}) (int, map[string]interface{}) {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPost, "/api/v1/vitals", bytes.NewReader(b))
		r = r.WithContext(authContext(r.Context(), fatima))
		rec := httptest.NewRecorder()
		vitals.LogVitals(rec, r)
		var out map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	body := map[string]interface{}{"vital_type": "bp", "systolic": 130, "diastolic": 85, "client_request_id": "outbox-1"}
	code1, out1 := post(body)
	code2, out2 := post(body)
	if code1 != http.StatusCreated || code2 != http.StatusOK || out2["id"] != out1["id"] || out2["duplicate"] != true {
		t.Fatalf("same client_request_id twice: want 201 then 200 with the same id, got %d %v / %d %v", code1, out1, code2, out2)
	}
	var rows int
	med.DB.QueryRow(`SELECT COUNT(*) FROM vitals_logs`).Scan(&rows)
	if rows != 1 {
		t.Fatalf("want one reading, got %d", rows)
	}
	if code, _ := post(map[string]interface{}{"vital_type": "bp", "systolic": 131, "diastolic": 85, "client_request_id": "outbox-2"}); code != http.StatusCreated {
		t.Fatalf("a different request id is a new reading, got %d", code)
	}

	notes := &handlers.HandoverHandler{DB: med.DB, Broker: med.Broker}
	postNote := func() (int, map[string]interface{}) {
		b, _ := json.Marshal(map[string]interface{}{"summary": "Slept well", "client_request_id": "note-1"})
		r := httptest.NewRequest(http.MethodPost, "/api/v1/handovers", bytes.NewReader(b))
		r = r.WithContext(authContext(r.Context(), fatima))
		rec := httptest.NewRecorder()
		notes.CreateHandover(rec, r)
		var out map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	c1, n1 := postNote()
	c2, n2 := postNote()
	if c1 != http.StatusCreated || c2 != http.StatusOK || n1["id"] != n2["id"] {
		t.Fatalf("note retry: got %d %v / %d %v", c1, n1, c2, n2)
	}
	med.DB.QueryRow(`SELECT COUNT(*) FROM handover_notes`).Scan(&rows)
	if rows != 1 {
		t.Fatalf("want one note, got %d", rows)
	}
}
