package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/db"
	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"
)

func setupTestDB(t *testing.T) (*handlers.MedicationHandler, *handlers.VitalsHandler, func()) {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "birrcare-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp db: %v", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	database, err := db.InitDB(tmpPath)
	if err != nil {
		t.Fatalf("Failed to init test db: %v", err)
	}

	if err := db.Migrate(database); err != nil {
		t.Fatalf("Failed to migrate test db: %v", err)
	}

	broker := sse.InitBroker()
	medHandler := &handlers.MedicationHandler{DB: database, Broker: broker}
	vitalsHandler := &handlers.VitalsHandler{DB: database, Broker: broker}

	cleanup := func() {
		database.Close()
		os.Remove(tmpPath)
		os.Remove(tmpPath + "-wal")
		os.Remove(tmpPath + "-shm")
	}

	return medHandler, vitalsHandler, cleanup
}

func TestDoubleDosingLock(t *testing.T) {
	medHandler, _, cleanup := setupTestDB(t)
	defer cleanup()

	// Prepare dummy caregiver
	dummyCaregiver := &models.Caregiver{
		ID:          "cg_fatima",
		Name:        "فاطمة",
		Role:        models.RoleCaregiver,
		AvatarColor: "#059669",
	}

	reqBody := handlers.LogDoseRequest{
		MedicationID:  "med_aspirin",
		Slot:          "morning",
		ScheduledDate: "2026-10-19",
		Status:        "taken",
		Notes:         "Given with milk",
	}
	bodyBytes, _ := json.Marshal(reqBody)

	// 1. First Dose Log - Should Succeed (201 Created)
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/medications/log", bytes.NewReader(bodyBytes))
	ctx1 := auth.GetCaregiverFromContext(req1.Context())
	_ = ctx1
	// Inject caregiver into context
	req1 = req1.WithContext(authContext(req1.Context(), dummyCaregiver))
	rec1 := httptest.NewRecorder()

	medHandler.LogDose(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for first dose, got %d: %s", rec1.Code, rec1.Body.String())
	}

	// 2. Second Simultaneous or Subsequent Dose Log - MUST BE LOCKED WITH 409 Conflict
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/medications/log", bytes.NewReader(bodyBytes))
	req2 = req2.WithContext(authContext(req2.Context(), dummyCaregiver))
	rec2 := httptest.NewRecorder()

	medHandler.LogDose(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("Double-dosing lock failed! Expected 409 Conflict, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var conflictResp map[string]interface{}
	json.Unmarshal(rec2.Body.Bytes(), &conflictResp)
	if conflictResp["error"] != "DOSE_ALREADY_LOGGED" {
		t.Fatalf("Expected DOSE_ALREADY_LOGGED error code, got %v", conflictResp["error"])
	}
}

func TestStrokeBPStatusCalculation(t *testing.T) {
	tests := []struct {
		systolic  int
		diastolic int
		expected  string
	}{
		{115, 75, "NORMAL"},
		{125, 82, "ELEVATED"},
		{145, 92, "STAGE_2"},
		{185, 95, "CRISIS"},
		{130, 122, "CRISIS"},
	}

	for _, tt := range tests {
		status := handlers.CalculateBPStatus(tt.systolic, tt.diastolic)
		if status != tt.expected {
			t.Errorf("CalculateBPStatus(%d, %d) = %s; want %s", tt.systolic, tt.diastolic, status, tt.expected)
		}
	}
}

func TestCalculateSugarStatusMmolL(t *testing.T) {
	ptr := func(f float64) *float64 { return &f }
	tests := []struct {
		sugar    *float64
		expected string
	}{
		{nil, ""},
		{ptr(3.2), "LOW"},
		{ptr(3.8), "LOW"},
		{ptr(5.5), "NORMAL"},
		{ptr(7.2), "NORMAL"},
		{ptr(8.5), "ELEVATED"},
		{ptr(13.8), "ELEVATED"},
		{ptr(14.0), "VERY_HIGH"},
		{ptr(18.5), "VERY_HIGH"},
	}

	for _, tt := range tests {
		status := handlers.CalculateSugarStatus(tt.sugar)
		if status != tt.expected {
			var val float64
			if tt.sugar != nil {
				val = *tt.sugar
			}
			t.Errorf("CalculateSugarStatus(%v) = %s; want %s", val, status, tt.expected)
		}
	}
}

func TestRBACPermissions(t *testing.T) {
	medHandler, _, cleanup := setupTestDB(t)
	defer cleanup()

	usersHandler := &handlers.UsersHandler{DB: medHandler.DB, Broker: medHandler.Broker}

	viewer := &models.Caregiver{ID: "cg_zainab", Name: "زينب", Role: models.RoleViewer}
	caregiver := &models.Caregiver{ID: "cg_fatima", Name: "فاطمة", Role: models.RoleCaregiver}
	admin := &models.Caregiver{ID: "cg_ali", Name: "علي", Role: models.RoleAdmin}
	superAdmin := &models.Caregiver{ID: "cg_hussain", Name: "حسين", Role: models.RoleSuperAdmin}

	// 1. Viewer cannot log doses (403 Forbidden)
	doseBody, _ := json.Marshal(handlers.LogDoseRequest{
		MedicationID:  "med_aspirin",
		Slot:          "morning",
		ScheduledDate: "2026-10-19",
		Status:        "taken",
	})
	reqViewerDose := httptest.NewRequest(http.MethodPost, "/api/v1/medications/log", bytes.NewReader(doseBody))
	reqViewerDose = reqViewerDose.WithContext(authContext(reqViewerDose.Context(), viewer))
	recViewerDose := httptest.NewRecorder()
	medHandler.LogDose(recViewerDose, reqViewerDose)
	if recViewerDose.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 Forbidden for viewer logging dose, got %d", recViewerDose.Code)
	}

	// 2. Caregiver cannot create medication (403 Forbidden)
	medBody, _ := json.Marshal(handlers.CreateMedicationRequest{
		NameAr:         "دواء جديد",
		NameEn:         "New Medicine",
		Dosage:         "50mg",
		FrequencySlots: []string{"morning"},
	})
	reqCaregiverMed := httptest.NewRequest(http.MethodPost, "/api/v1/medications", bytes.NewReader(medBody))
	reqCaregiverMed = reqCaregiverMed.WithContext(authContext(reqCaregiverMed.Context(), caregiver))
	recCaregiverMed := httptest.NewRecorder()
	medHandler.CreateMedication(recCaregiverMed, reqCaregiverMed)
	if recCaregiverMed.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 Forbidden for caregiver creating medication, got %d", recCaregiverMed.Code)
	}

	// 3. Admin can create medication (201 Created)
	reqAdminMed := httptest.NewRequest(http.MethodPost, "/api/v1/medications", bytes.NewReader(medBody))
	reqAdminMed = reqAdminMed.WithContext(authContext(reqAdminMed.Context(), admin))
	recAdminMed := httptest.NewRecorder()
	medHandler.CreateMedication(recAdminMed, reqAdminMed)
	if recAdminMed.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for admin creating medication, got %d: %s", recAdminMed.Code, recAdminMed.Body.String())
	}

	// 4. Admin cannot manage users (403 Forbidden)
	reqAdminListUsers := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	reqAdminListUsers = reqAdminListUsers.WithContext(authContext(reqAdminListUsers.Context(), admin))
	recAdminListUsers := httptest.NewRecorder()
	usersHandler.ListUsers(recAdminListUsers, reqAdminListUsers)
	if recAdminListUsers.Code != http.StatusForbidden {
		t.Fatalf("Expected 403 Forbidden for admin listing users, got %d", recAdminListUsers.Code)
	}

	// 5. Super Admin can list and create users (200 OK & 201 Created)
	reqSuperListUsers := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	reqSuperListUsers = reqSuperListUsers.WithContext(authContext(reqSuperListUsers.Context(), superAdmin))
	recSuperListUsers := httptest.NewRecorder()
	usersHandler.ListUsers(recSuperListUsers, reqSuperListUsers)
	if recSuperListUsers.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for super admin listing users, got %d: %s", recSuperListUsers.Code, recSuperListUsers.Body.String())
	}

	newUserBody, _ := json.Marshal(models.CreateUserRequest{
		Name: "سارة (Sarah)",
		Role: models.RoleCaregiver,
		PIN:  "4321",
	})
	reqSuperCreateUser := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(newUserBody))
	reqSuperCreateUser = reqSuperCreateUser.WithContext(authContext(reqSuperCreateUser.Context(), superAdmin))
	recSuperCreateUser := httptest.NewRecorder()
	usersHandler.CreateUser(recSuperCreateUser, reqSuperCreateUser)
	if recSuperCreateUser.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for super admin creating user, got %d: %s", recSuperCreateUser.Code, recSuperCreateUser.Body.String())
	}
}

// helper
func authContext(ctx testingContext, c *models.Caregiver) testingContext {
	return contextWithVal(ctx, auth.CaregiverContextKey, c)
}

type testingContext = context.Context

func contextWithVal(ctx testingContext, key interface{}, val interface{}) testingContext {
	return context.WithValue(ctx, key, val)
}
