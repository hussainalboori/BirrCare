package handlers_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/db"
	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"

	_ "modernc.org/sqlite"
)

func setupShiftsTestDB(t *testing.T) (*sql.DB, *handlers.ShiftsHandler) {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed opening in-memory db: %v", err)
	}

	if err := db.Migrate(database); err != nil {
		t.Fatalf("failed running migrations: %v", err)
	}

	_, _ = database.Exec(`
		INSERT OR IGNORE INTO caregivers (id, name, email, role, pin_hash, avatar_color, preferred_lang)
		VALUES ('cg_fatima', 'Fatima', 'fatima@test.local', 'caregiver', 'hash', '#059669', 'en')
	`)

	broker := sse.InitBroker()
	t.Cleanup(func() { broker.Close() })
	handler := &handlers.ShiftsHandler{DB: database, Broker: broker}
	return database, handler
}

func contextWithCaregiver(c *models.Caregiver) context.Context {
	return context.WithValue(context.Background(), auth.CaregiverContextKey, c)
}

func TestShiftsRBAC(t *testing.T) {
	database, handler := setupShiftsTestDB(t)
	defer database.Close()

	superAdmin := &models.Caregiver{ID: "cg_hussain", Name: "Hussain", Role: models.RoleSuperAdmin}
	admin := &models.Caregiver{ID: "cg_ali", Name: "Ali", Role: models.RoleAdmin}
	caregiver := &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}
	viewer := &models.Caregiver{ID: "cg_zainab", Name: "Zainab", Role: models.RoleViewer}

	// 1. All roles can list shifts
	for _, user := range []*models.Caregiver{superAdmin, admin, caregiver, viewer} {
		req := httptest.NewRequest("GET", "/api/v1/shifts", nil).WithContext(contextWithCaregiver(user))
		rec := httptest.NewRecorder()
		handler.ListShifts(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 for role %s, got %d", user.Role, rec.Code)
		}
	}

	// 2. Only Super Admin can create shift; others must get 403
	createPayload := `{"name_ar":"فترة 4 ساعات","name_en":"4-Hour Shift","start_time":"08:00","end_time":"12:00"}`
	for _, nonSuper := range []*models.Caregiver{admin, caregiver, viewer} {
		req := httptest.NewRequest("POST", "/api/v1/shifts", bytes.NewBufferString(createPayload)).WithContext(contextWithCaregiver(nonSuper))
		rec := httptest.NewRecorder()
		handler.CreateShift(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403 for non-super_admin role %s, got %d", nonSuper.Role, rec.Code)
		}
	}

	// Super Admin creates successfully
	req := httptest.NewRequest("POST", "/api/v1/shifts", bytes.NewBufferString(createPayload)).WithContext(contextWithCaregiver(superAdmin))
	rec := httptest.NewRecorder()
	handler.CreateShift(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for super_admin, got %d: %s", rec.Code, rec.Body.String())
	}

	var created models.Shift
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed decoding created shift: %v", err)
	}
	if created.DurationHours != 4.0 {
		t.Errorf("expected duration 4.0, got %f", created.DurationHours)
	}
}

func TestShiftsPresetsAndDuty(t *testing.T) {
	database, handler := setupShiftsTestDB(t)
	defer database.Close()

	superAdmin := &models.Caregiver{ID: "cg_hussain", Name: "Hussain", Role: models.RoleSuperAdmin}

	// Apply 4-Hour Shift Preset (as specifically requested by user)
	presetReq := httptest.NewRequest("POST", "/api/v1/shifts/presets", bytes.NewBufferString(`{"preset":"4h"}`)).WithContext(contextWithCaregiver(superAdmin))
	rec := httptest.NewRecorder()
	handler.ApplyPreset(rec, presetReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 applying 4h preset, got %d: %s", rec.Code, rec.Body.String())
	}

	var shifts []models.Shift
	if err := json.Unmarshal(rec.Body.Bytes(), &shifts); err != nil {
		t.Fatalf("failed unmarshaling shifts: %v", err)
	}
	if len(shifts) != 6 {
		t.Fatalf("expected 6 active shifts for 4h preset, got %d", len(shifts))
	}

	// Assign caregiver to first shift for today
	today := time.Now().Format("2006-01-02")
	assignBody, _ := json.Marshal(map[string]string{
		"shift_id":     shifts[0].ID,
		"caregiver_id": "cg_fatima",
		"date":         today,
		"notes":        "Morning coverage",
	})
	assignReq := httptest.NewRequest("POST", "/api/v1/shifts/assignments", bytes.NewReader(assignBody)).WithContext(contextWithCaregiver(superAdmin))
	recAssign := httptest.NewRecorder()
	handler.AssignShift(recAssign, assignReq)

	if recAssign.Code != http.StatusOK {
		t.Fatalf("expected 200 assigning shift, got %d: %s", recAssign.Code, recAssign.Body.String())
	}

	// Query assignments list
	listReq := httptest.NewRequest("GET", "/api/v1/shifts/assignments?date="+today, nil).WithContext(contextWithCaregiver(superAdmin))
	recList := httptest.NewRecorder()
	handler.ListAssignments(recList, listReq)

	var assignments []models.ShiftAssignment
	if err := json.Unmarshal(recList.Body.Bytes(), &assignments); err != nil {
		t.Fatalf("failed decoding assignments: %v", err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(assignments))
	}
	if assignments[0].CaregiverID != "cg_fatima" {
		t.Errorf("expected assigned caregiver cg_fatima, got %s", assignments[0].CaregiverID)
	}

	// Switch back to 24-Hour Shift preset
	p24Req := httptest.NewRequest("POST", "/api/v1/shifts/presets", bytes.NewBufferString(`{"preset":"24h"}`)).WithContext(contextWithCaregiver(superAdmin))
	rec24 := httptest.NewRecorder()
	handler.ApplyPreset(rec24, p24Req)

	if rec24.Code != http.StatusOK {
		t.Fatalf("expected 200 applying 24h preset, got %d", rec24.Code)
	}
	var s24 []models.Shift
	json.Unmarshal(rec24.Body.Bytes(), &s24)
	if len(s24) != 1 || s24[0].DurationHours != 24.0 {
		t.Errorf("expected 1 shift of 24h duration, got %+v", s24)
	}
}

func TestHandoverWithCustomShift(t *testing.T) {
	database, _ := setupShiftsTestDB(t)
	defer database.Close()

	broker := sse.InitBroker()
	t.Cleanup(func() { broker.Close() })
	handoverHandler := &handlers.HandoverHandler{DB: database, Broker: broker}
	caregiver := &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}

	// Create handover with custom shift "shift_4h_1"
	body := `{"shift_type":"shift_4h_1","summary":"Patient took evening medication calmly and slept well","urgent_flag":false}`
	req := httptest.NewRequest("POST", "/api/v1/handovers", bytes.NewBufferString(body)).WithContext(contextWithCaregiver(caregiver))
	rec := httptest.NewRecorder()
	handoverHandler.CreateHandover(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 created handover with custom shift, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify stored handover
	var storedShift string
	err := database.QueryRow("SELECT shift_type FROM handover_notes ORDER BY created_at DESC LIMIT 1").Scan(&storedShift)
	if err != nil {
		t.Fatalf("failed querying handover note: %v", err)
	}
	if storedShift != "shift_4h_1" {
		t.Errorf("expected stored shift 'shift_4h_1', got '%s'", storedShift)
	}
}
