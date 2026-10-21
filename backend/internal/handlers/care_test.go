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
	"birrcare/backend/internal/push"
)

func logCareEvent(t *testing.T, h *handlers.CareHandler, who *models.Caregiver, body map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/care/events", bytes.NewReader(b))
	r = r.WithContext(authContext(r.Context(), who))
	rec := httptest.NewRecorder()
	h.LogEvent(rec, r)
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func getCareStatus(t *testing.T, h *handlers.CareHandler, who *models.Caregiver, kind string) (int, map[string]interface{}) {
	t.Helper()
	url := "/api/v1/care/status"
	if kind != "" {
		url += "?kind=" + kind
	}
	r := httptest.NewRequest(http.MethodGet, url, nil)
	r = r.WithContext(authContext(r.Context(), who))
	rec := httptest.NewRecorder()
	h.GetStatus(rec, r)
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestRepositionStatus(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()

	h := &handlers.CareHandler{DB: med.DB, Broker: med.Broker}
	fatima := &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}

	// 1. Initial status has no previous turns
	code, status := getCareStatus(t, h, fatima, "reposition")
	if code != http.StatusOK {
		t.Fatalf("initial status: want 200, got %d %v", code, status)
	}
	if status["last_at"] != nil || status["last_value"] != nil || status["next_due_at"] != nil {
		t.Fatalf("expected nil last_at, last_value, next_due_at initially, got: %v", status)
	}

	// 2. Log reposition to "right"
	req := map[string]interface{}{
		"kind":           "reposition",
		"value":          "right",
		"skin_condition": "ok",
	}
	code, logged := logCareEvent(t, h, fatima, req)
	if code != http.StatusCreated {
		t.Fatalf("log event: want 201, got %d %v", code, logged)
	}
	if logged["value"] != "right" {
		t.Fatalf("expected logged value 'right', got %v", logged["value"])
	}

	// 3. /care/status returns last_at, last_value, next_due_at
	code, status = getCareStatus(t, h, fatima, "reposition")
	if code != http.StatusOK {
		t.Fatalf("status after log: want 200, got %d %v", code, status)
	}
	if status["last_at"] == nil {
		t.Fatal("expected last_at to be non-nil after logging reposition")
	}
	if status["last_value"] != "right" {
		t.Fatalf("expected last_value 'right', got %v", status["last_value"])
	}
	if status["next_due_at"] == nil {
		t.Fatal("expected next_due_at to be non-nil after logging reposition")
	}
	if status["last_caregiver_name"] != "Fatima" && status["last_caregiver_name"] != "فاطمة (Fatima)" {
		t.Fatalf("expected last_caregiver_name 'فاطمة (Fatima)', got %v", status["last_caregiver_name"])
	}
}

func TestRepositionOverdueAlertsOnlyOnDutyCaregiver(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()

	now := time.Now()
	todayDate := now.Format("2006-01-02")
	handlers.SetRepositionInterval(120) // 2h interval

	// Create 24h shift covering now
	_, err := med.DB.Exec(`
		INSERT INTO shifts (id, name_ar, name_en, start_time, end_time, duration_hours, is_active, display_order)
		VALUES ('shift_day', 'نهاري', 'Day', '00:00', '23:59', 24, 1, 1)
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Assign Fatima on duty today
	_, err = med.DB.Exec(`
		INSERT INTO shift_assignments (id, shift_id, caregiver_id, date, notes)
		VALUES ('assign_1', 'shift_day', 'cg_fatima', ?, 'Day shift')
	`, todayDate)
	if err != nil {
		t.Fatal(err)
	}

	// Log reposition 3 hours ago (180 minutes ago, exceeding 120 + 45 = 165 minutes)
	threeHoursAgo := now.UTC().Add(-3 * time.Hour).Format(time.RFC3339)
	_, err = med.DB.Exec(`
		INSERT INTO care_events (id, kind, value, caregiver_id, skin_condition, created_at)
		VALUES ('ev_old', 'reposition', 'left', 'cg_ali', 'ok', ?)
	`, threeHoursAgo)
	if err != nil {
		t.Fatal(err)
	}

	notifier, err := push.Init(med.DB)
	if err != nil {
		t.Fatal(err)
	}

	// Run overdue check
	if err := handlers.CheckRepositionOverdue(context.Background(), med.DB, notifier, med.Broker, now); err != nil {
		t.Fatalf("CheckRepositionOverdue failed: %v", err)
	}

	// Verify alert was created in alerts table
	var alertID, kind, targetCaregiverID string
	err = med.DB.QueryRow(`
		SELECT id, kind, COALESCE(target_caregiver_id, '') FROM alerts WHERE kind = 'reposition_overdue'
	`).Scan(&alertID, &kind, &targetCaregiverID)
	if err != nil {
		t.Fatalf("expected reposition_overdue alert, got: %v", err)
	}

	// Acceptance criteria: Overdue alert goes ONLY to the on-duty caregiver (Fatima)
	if targetCaregiverID != "cg_fatima" {
		t.Fatalf("expected alert target_caregiver_id 'cg_fatima' (on-duty), got %q", targetCaregiverID)
	}

	// Re-checking should not duplicate alert
	if err := handlers.CheckRepositionOverdue(context.Background(), med.DB, notifier, med.Broker, now); err != nil {
		t.Fatal(err)
	}
	var count int
	med.DB.QueryRow(`SELECT COUNT(*) FROM alerts WHERE kind = 'reposition_overdue'`).Scan(&count)
	if count != 1 {
		t.Fatalf("expected exactly 1 alert, got %d", count)
	}
}

func TestRepositionSkinRedAreaCreatesUrgentHandover(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()

	h := &handlers.CareHandler{DB: med.DB, Broker: med.Broker}
	fatima := &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}

	req := map[string]interface{}{
		"kind":           "reposition",
		"value":          "back",
		"skin_condition": "red_area",
		"notes":          "slight erythema on sacrum",
	}
	code, out := logCareEvent(t, h, fatima, req)
	if code != http.StatusCreated {
		t.Fatalf("log event with red_area: want 201, got %d %v", code, out)
	}

	// Verify urgent handover note was created
	var summary string
	var urgentFlag int
	err := med.DB.QueryRow(`
		SELECT summary, urgent_flag FROM handover_notes ORDER BY created_at DESC LIMIT 1
	`).Scan(&summary, &urgentFlag)
	if err != nil {
		t.Fatalf("failed querying handover notes: %v", err)
	}
	if urgentFlag != 1 {
		t.Fatalf("expected urgent_flag=1 on handover note for red_area, got %d", urgentFlag)
	}
}
