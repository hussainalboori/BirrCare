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

func TestFluidTotalsGroupByHouseholdLocalDay(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()

	// 1. Configure timezone: Asia/Riyadh (UTC+3)
	riyadh, err := time.LoadLocation("Asia/Riyadh")
	if err != nil {
		t.Skip("Asia/Riyadh timezone not loaded on system")
	}
	prev := handlers.ClinicalLocation()
	handlers.SetClinicalLocationForTest(riyadh)
	defer handlers.SetClinicalLocationForTest(prev)

	h := &handlers.CareHandler{DB: med.DB, Broker: med.Broker}
	fatima := &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}

	handlers.SetFluidDailyTarget(1500)

	// In Asia/Riyadh, local midnight of today:
	now := time.Now().In(riyadh)
	y, mo, d := now.Date()
	todayMidnightLocal := time.Date(y, mo, d, 0, 0, 0, 0, riyadh)

	// Event A: 30 minutes before local midnight (yesterday in local time, even if same or different UTC date)
	yesterdayLocalTimeUTC := todayMidnightLocal.Add(-30 * time.Minute).UTC().Format(time.RFC3339)
	_, err = med.DB.Exec(`
		INSERT INTO care_events (id, kind, value, caregiver_id, thickened, created_at)
		VALUES ('ev_fluid_yest', 'fluid', '100', 'cg_fatima', 0, ?)
	`, yesterdayLocalTimeUTC)
	if err != nil {
		t.Fatal(err)
	}

	// Event B: 30 minutes after local midnight (today in local time)
	todayEarlyLocalTimeUTC := todayMidnightLocal.Add(30 * time.Minute).UTC().Format(time.RFC3339)
	_, err = med.DB.Exec(`
		INSERT INTO care_events (id, kind, value, caregiver_id, thickened, created_at)
		VALUES ('ev_fluid_today_1', 'fluid', '150', 'cg_fatima', 1, ?)
	`, todayEarlyLocalTimeUTC)
	if err != nil {
		t.Fatal(err)
	}

	// Event C: logged right now via API
	code, out := logCareEvent(t, h, fatima, map[string]interface{}{
		"kind":      "fluid",
		"value":     "200",
		"thickened": false,
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201 from logCareEvent, got %d %v", code, out)
	}
	if out["thickened"] != false {
		t.Fatalf("expected thickened=false, got %v", out["thickened"])
	}

	// Fetch fluid status
	code, status := getCareStatus(t, h, fatima, "fluid")
	if code != http.StatusOK {
		t.Fatalf("expected 200 from getCareStatus, got %d %v", code, status)
	}

	// Acceptance criteria: Fluid totals group by household-local day (with APP_TIMEZONE)
	// Today's total must be 150 + 200 = 350 ml (Event A of 100 ml belongs to yesterday's local day!)
	totalML, _ := status["today_total_ml"].(float64)
	if int(totalML) != 350 {
		t.Fatalf("expected today_total_ml=350, got %v (status: %v)", totalML, status)
	}

	thickenedML, _ := status["today_thickened_ml"].(float64)
	if int(thickenedML) != 150 {
		t.Fatalf("expected today_thickened_ml=150, got %v", thickenedML)
	}

	targetML, _ := status["target_ml"].(float64)
	if int(targetML) != 1500 {
		t.Fatalf("expected target_ml=1500, got %v", targetML)
	}

	progress, _ := status["progress_percent"].(float64)
	expectedProgress := 350 * 100 / 1500 // 23%
	if int(progress) != expectedProgress {
		t.Fatalf("expected progress_percent=%d, got %v", expectedProgress, progress)
	}
}

func TestBowelMovementTrackingAndOverdue(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()

	h := &handlers.CareHandler{DB: med.DB, Broker: med.Broker}
	fatima := &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}

	// 1. Initial status with no bowel movements is overdue
	code, status := getCareStatus(t, h, fatima, "bowel")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if status["is_bowel_overdue"] != true {
		t.Fatalf("expected is_bowel_overdue=true when no bowel movement recorded yet, got %v", status["is_bowel_overdue"])
	}

	// 2. Log normal bowel movement
	code, out := logCareEvent(t, h, fatima, map[string]interface{}{
		"kind":  "bowel",
		"value": "normal",
		"notes": "well hydrated",
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d %v", code, out)
	}

	code, status = getCareStatus(t, h, fatima, "bowel")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if status["last_value"] != "normal" {
		t.Fatalf("expected last_value 'normal', got %v", status["last_value"])
	}
	if status["is_bowel_overdue"] != false {
		t.Fatalf("expected is_bowel_overdue=false immediately after log, got %v", status["is_bowel_overdue"])
	}

	// 3. Test bowel movement from 4 days ago -> triggers is_bowel_overdue
	fourDaysAgo := time.Now().UTC().Add(-4 * 24 * time.Hour).Format(time.RFC3339)
	_, err := med.DB.Exec(`
		UPDATE care_events SET created_at = ? WHERE kind = 'bowel'
	`, fourDaysAgo)
	if err != nil {
		t.Fatal(err)
	}

	code, status = getCareStatus(t, h, fatima, "bowel")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	daysSince, _ := status["days_since_bowel"].(float64)
	if int(daysSince) < 3 {
		t.Fatalf("expected days_since_bowel >= 3, got %v", daysSince)
	}
	if status["is_bowel_overdue"] != true {
		t.Fatalf("expected is_bowel_overdue=true after 4 days, got %v", status["is_bowel_overdue"])
	}
}

func TestDoctorExportFluidTableAndBowelGap(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()

	exp := &handlers.ExportHandler{DB: med.DB}
	handlers.SetFluidDailyTarget(1500)

	now := time.Now()
	// Insert fluid events across the past days
	for i := 0; i < 5; i++ {
		tPast := now.AddDate(0, 0, -i).UTC().Format(time.RFC3339)
		_, err := med.DB.Exec(`
			INSERT INTO care_events (id, kind, value, caregiver_id, thickened, created_at)
			VALUES (?, 'fluid', '200', 'cg_fatima', ?, ?)
		`, "ev_fl_"+time.Now().Format("150405")+string(rune('a'+i)), i%2, tPast)
		if err != nil {
			t.Fatal(err)
		}
	}

	// Insert bowel movements with known gap
	// Movement 1: 8 days ago
	tBowel1 := now.Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339)
	med.DB.Exec(`INSERT INTO care_events (id, kind, value, caregiver_id, created_at) VALUES ('b1', 'bowel', 'hard', 'cg_fatima', ?)`, tBowel1)
	// Movement 2: 4 days ago (gap of 4 days)
	tBowel2 := now.Add(-4 * 24 * time.Hour).UTC().Format(time.RFC3339)
	med.DB.Exec(`INSERT INTO care_events (id, kind, value, caregiver_id, created_at) VALUES ('b2', 'bowel', 'normal', 'cg_fatima', ?)`, tBowel2)

	rec := httptest.NewRecorder()
	exp.ExportDoctorSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export/summary?days=14", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from export, got %d %s", rec.Code, rec.Body.String())
	}

	var res handlers.DoctorSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed decoding doctor summary: %v", err)
	}

	// Acceptance criteria: 14-day fluid table
	if len(res.DailyFluid) != 14 {
		t.Fatalf("expected 14 rows in daily_fluid table, got %d", len(res.DailyFluid))
	}

	// Acceptance criteria: longest gap between bowel movements
	if res.BowelSummary.TotalMovements != 2 {
		t.Fatalf("expected 2 total bowel movements, got %d", res.BowelSummary.TotalMovements)
	}
	if res.BowelSummary.LongestGapDays < 4 {
		t.Fatalf("expected longest_gap_days >= 4, got %d", res.BowelSummary.LongestGapDays)
	}
	if res.BowelSummary.HardCount != 1 || res.BowelSummary.NormalCount != 1 {
		t.Fatalf("expected 1 hard and 1 normal bowel movement, got hard=%d, normal=%d", res.BowelSummary.HardCount, res.BowelSummary.NormalCount)
	}
}
