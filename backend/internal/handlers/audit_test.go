package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/handlers"

	"github.com/go-chi/chi/v5"
)

// Acceptance Criterion 1:
// Go test: UPDATE audit_logs SET details='x' → error; DELETE → error.
func TestAuditLogsImmutability(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()

	// Insert an initial audit log entry
	if err := audit.Record(h.DB, hussain.ID, "TEST_IMMUTABLE", "test", "id1", "initial details"); err != nil {
		t.Fatalf("failed recording audit log: %v", err)
	}

	// UPDATE audit_logs SET details='x' → error
	_, err := h.DB.Exec("UPDATE audit_logs SET details = 'x' WHERE action = 'TEST_IMMUTABLE'")
	if err == nil {
		t.Fatalf("expected error on UPDATE audit_logs, got nil")
	}
	if !strings.Contains(err.Error(), "audit_logs is append-only") {
		t.Fatalf("expected 'audit_logs is append-only' error, got: %v", err)
	}

	// DELETE FROM audit_logs → error
	_, err = h.DB.Exec("DELETE FROM audit_logs WHERE action = 'TEST_IMMUTABLE'")
	if err == nil {
		t.Fatalf("expected error on DELETE FROM audit_logs, got nil")
	}
	if !strings.Contains(err.Error(), "audit_logs is append-only") {
		t.Fatalf("expected 'audit_logs is append-only' error, got: %v", err)
	}

	// Also verify medication_logs clinical columns cannot be updated and rows cannot be deleted
	medLogID := mustLog(t, h, fatima, aspirinToday())
	_, err = h.DB.Exec("UPDATE medication_logs SET dosage_snapshot = '999mg' WHERE id = ?", medLogID)
	if err == nil {
		t.Fatalf("expected error on updating clinical column dosage_snapshot, got nil")
	}
	if !strings.Contains(err.Error(), "medication log entries are immutable") {
		t.Fatalf("expected immutable error, got: %v", err)
	}

	_, err = h.DB.Exec("DELETE FROM medication_logs WHERE id = ?", medLogID)
	if err == nil {
		t.Fatalf("expected error on deleting medication_logs, got nil")
	}
	if !strings.Contains(err.Error(), "medication logs cannot be deleted") {
		t.Fatalf("expected cannot be deleted error, got: %v", err)
	}
}

// Acceptance Criterion 2:
// Go test: inject an audit insert failure in LogVitals → no vitals row persisted.
func TestLogVitalsAuditFailureRollback(t *testing.T) {
	_, vh, cleanup := setupTestDB(t)
	defer cleanup()

	// Inject an audit insert failure trigger
	if _, err := vh.DB.Exec(`
		CREATE TRIGGER inject_audit_failure
		BEFORE INSERT ON audit_logs
		BEGIN SELECT RAISE(ABORT, 'injected audit failure'); END;
	`); err != nil {
		t.Fatalf("failed injecting audit failure trigger: %v", err)
	}

	systolic := 125
	diastolic := 82
	pulse := 72
	reqBody, _ := json.Marshal(handlers.LogVitalsRequest{
		Systolic:  &systolic,
		Diastolic: &diastolic,
		Pulse:     &pulse,
		Notes:     "Routine check",
	})

	r := httptest.NewRequest(http.MethodPost, "/api/v1/vitals", bytes.NewReader(reqBody))
	r = r.WithContext(authContext(r.Context(), fatima))
	rec := httptest.NewRecorder()

	vh.LogVitals(rec, r)

	// LogVitals should fail due to atomic rollback
	if rec.Code == http.StatusCreated {
		t.Fatalf("expected LogVitals to fail with audit error, got 201")
	}

	// Verify NO vitals row persisted
	var count int
	if err := vh.DB.QueryRow("SELECT COUNT(*) FROM vitals_logs WHERE notes = 'Routine check'").Scan(&count); err != nil {
		t.Fatalf("failed querying vitals_logs: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 vitals rows persisted on audit failure, got %d", count)
	}
}

// Acceptance Criterion 3:
// Go test: manually alter one row's details (drop trigger in test) → /audit/verify reports that row id.
func TestAuditVerifyReportsTamperedRowID(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()

	auditHandler := &handlers.AuditHandler{DB: h.DB}

	// Record legitimate audit entries
	if err := audit.Record(h.DB, hussain.ID, "MED_LOGGED", "medication_log", "log1", "Given dose 1"); err != nil {
		t.Fatalf("failed recording audit: %v", err)
	}
	if err := audit.Record(h.DB, ali.ID, "VITAL_LOGGED", "vitals_log", "vital1", "BP 120/80"); err != nil {
		t.Fatalf("failed recording audit: %v", err)
	}

	// Fetch second row's ID
	var tamperedID string
	err := h.DB.QueryRow("SELECT id FROM audit_logs WHERE action = 'VITAL_LOGGED'").Scan(&tamperedID)
	if err != nil {
		t.Fatalf("failed querying row id: %v", err)
	}

	// Temporarily drop trigger and alter the details
	if _, err := h.DB.Exec("DROP TRIGGER audit_no_update"); err != nil {
		t.Fatalf("failed dropping trigger: %v", err)
	}
	if _, err := h.DB.Exec("UPDATE audit_logs SET details = 'Tampered BP 200/120' WHERE id = ?", tamperedID); err != nil {
		t.Fatalf("failed altering row: %v", err)
	}

	// Call GET /audit/verify with super-admin
	r := httptest.NewRequest(http.MethodGet, "/api/v1/audit/verify", nil)
	r = r.WithContext(authContext(r.Context(), hussain))
	rec := httptest.NewRecorder()

	auditHandler.Verify(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from verify, got %d: %s", rec.Code, rec.Body.String())
	}

	var res map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed decoding verify response: %v", err)
	}

	if res["valid"] == true {
		t.Fatalf("expected valid=false after tampering, got true")
	}

	// /audit/verify reports that row id
	brokenID, _ := res["broken_id"].(string)
	if brokenID != tamperedID {
		t.Fatalf("expected verify to report broken row id %q, got %q (resp: %v)", tamperedID, brokenID, res)
	}

	// Non-superadmin access should be forbidden (403)
	rFatima := httptest.NewRequest(http.MethodGet, "/api/v1/audit/verify", nil)
	rFatima = rFatima.WithContext(authContext(rFatima.Context(), fatima))
	recFatima := httptest.NewRecorder()
	auditHandler.Verify(recFatima, rFatima)
	if recFatima.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for caregiver, got %d", recFatima.Code)
	}
}

// Acceptance Criterion 4:
// Go test: UpdateMedication with a failing third statement → no columns changed.
func TestUpdateMedicationAtomicityAndAuditBeforeAfter(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()

	// Initial medication
	medID := "med_citicoline"
	var origNameAr, origDays string
	err := h.DB.QueryRow("SELECT name_ar, days_of_week FROM medications WHERE id = ?", medID).Scan(&origNameAr, &origDays)
	if err != nil {
		t.Fatalf("failed querying original med: %v", err)
	}

	// Inject a failure in statement 3: updating slot_times
	if _, err := h.DB.Exec(`
		CREATE TRIGGER fail_slot_times
		BEFORE UPDATE OF slot_times ON medications
		BEGIN SELECT RAISE(ABORT, 'injected slot_times failure'); END;
	`); err != nil {
		t.Fatalf("failed injecting trigger: %v", err)
	}

	updateReq := handlers.UpdateMedicationRequest{
		NameAr:     "سيتيكولين معدل",
		DaysOfWeek: []string{"mon", "wed", "fri"},
		SlotTimes:  map[string]string{"morning": "09:00"},
	}
	body, _ := json.Marshal(updateReq)
	r := httptest.NewRequest(http.MethodPut, "/api/v1/medications/"+medID, bytes.NewReader(body))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", medID)
	r = r.WithContext(context.WithValue(authContext(r.Context(), ali), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()

	h.UpdateMedication(rec, r)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected UpdateMedication to fail on statement 3, got 200")
	}

	// Assert NO columns changed
	var currentNameAr, currentDays string
	err = h.DB.QueryRow("SELECT name_ar, days_of_week FROM medications WHERE id = ?", medID).Scan(&currentNameAr, &currentDays)
	if err != nil {
		t.Fatalf("failed querying current med: %v", err)
	}
	if currentNameAr != origNameAr {
		t.Fatalf("atomicity failed: name_ar changed from %q to %q", origNameAr, currentNameAr)
	}
	if currentDays != origDays {
		t.Fatalf("atomicity failed: days_of_week changed from %q to %q", origDays, currentDays)
	}

	// Now drop the injected trigger and verify successful UpdateMedication records before/after values
	if _, err := h.DB.Exec("DROP TRIGGER fail_slot_times"); err != nil {
		t.Fatalf("failed dropping trigger: %v", err)
	}

	rec2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodPut, "/api/v1/medications/"+medID, bytes.NewReader(body))
	r2 = r2.WithContext(context.WithValue(authContext(r2.Context(), ali), chi.RouteCtxKey, rc))
	h.UpdateMedication(rec2, r2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected success on UpdateMedication without failure trigger, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// Check audit details contains before and after values
	var details string
	err = h.DB.QueryRow("SELECT details FROM audit_logs WHERE action = 'UPDATE_MEDICATION' AND entity_id = ? ORDER BY rowid DESC LIMIT 1", medID).Scan(&details)
	if err != nil {
		t.Fatalf("failed querying audit details: %v", err)
	}
	if !strings.Contains(details, "before") || !strings.Contains(details, "after") {
		t.Fatalf("expected audit details to include before and after values, got: %s", details)
	}
	if !strings.Contains(details, origNameAr) || !strings.Contains(details, "سيتيكولين معدل") {
		t.Fatalf("expected before/after names in audit details, got: %s", details)
	}
}
