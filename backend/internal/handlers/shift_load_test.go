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

// Issue #66: night-shift fairness and rest warnings.

func rosterFixture(t *testing.T) (*handlers.ShiftsHandler, *handlers.MedicationHandler, *time.Location) {
	t.Helper()
	med, _, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	riyadh := riyadhHousehold(t)
	mustExec(t, med, `DELETE FROM shift_assignments`)
	mustExec(t, med, `INSERT INTO shifts (id, name_ar, name_en, start_time, end_time, duration_hours) VALUES
		('sh_night', 'ليل', 'Night', '20:00', '08:00', 12), ('sh_day', 'نهار', 'Day', '08:00', '20:00', 12), ('sh_late', 'متأخر', 'Late', '23:00', '03:00', 4)`)
	return &handlers.ShiftsHandler{DB: med.DB, Broker: med.Broker}, med, riyadh
}

func assign(t *testing.T, med *handlers.MedicationHandler, id, shift, cg, date string) {
	t.Helper()
	mustExec(t, med, `INSERT INTO shift_assignments (id, shift_id, caregiver_id, date) VALUES (?, ?, ?, ?)`, id, shift, cg, date)
}

func TestShiftSummaryNightMaths(t *testing.T) {
	h, med, riyadh := rosterFixture(t)
	for i, d := range []string{"2030-03-01", "2030-03-02", "2030-03-03"} {
		assign(t, med, "n"+d, "sh_night", "cg_fatima", d) // 20:00 → 08:00 next day: 9 night hours
		assign(t, med, "d"+d, "sh_day", "cg_ali", d)
		_ = i
	}
	assign(t, med, "d2030-03-04", "sh_day", "cg_ali", "2030-03-04")
	assign(t, med, "l2030-03-02", "sh_late", "cg_hussain", "2030-03-02") // 23:00 → 03:00, across midnight
	// Fatima tapped "Seen" at 23:30 and at 14:00 home time; only the first is a night alert.
	mustExec(t, med, `INSERT INTO alerts (id, kind, payload, created_at) VALUES ('a1', 'fever', '{}', '2030-03-02 20:00:00'), ('a2', 'fever', '{}', '2030-03-02 10:00:00')`)
	mustExec(t, med, `INSERT INTO alert_acks (alert_id, caregiver_id, acked_at) VALUES ('a1', 'cg_fatima', ?), ('a2', 'cg_fatima', ?)`,
		time.Date(2030, 3, 2, 23, 30, 0, 0, riyadh).UTC().Format("2006-01-02 15:04:05"),
		time.Date(2030, 3, 2, 14, 0, 0, 0, riyadh).UTC().Format("2006-01-02 15:04:05"))

	now := time.Date(2030, 3, 4, 10, 0, 0, 0, riyadh)
	out, err := handlers.ShiftSummaryForTest(med.DB, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]handlers.CaregiverLoad{}
	for _, c := range out["caregivers"].([]handlers.CaregiverLoad) {
		by[c.CaregiverID] = c
	}
	// Household nights are 22:00–07:00 Riyadh (19:00–04:00 UTC); UTC windows would give 7 h a night.
	if f := by["cg_fatima"]; f.HoursOnDuty != 36 || f.NightHours != 27 || f.Nights != 3 || f.ConsecutiveNights != 3 || f.LastNightOff != "2030-02-28" || f.NightAlerts != 1 {
		t.Fatalf("Fatima: %+v", f)
	}
	if a := by["cg_ali"]; a.HoursOnDuty != 38 || a.NightHours != 0 || a.Nights != 0 || a.ConsecutiveNights != 0 || a.LastNightOff != "2030-03-03" {
		t.Fatalf("Ali (days, to 10:00 today): %+v", a)
	}
	if hu := by["cg_hussain"]; hu.HoursOnDuty != 4 || hu.NightHours != 4 || hu.Nights != 1 || hu.ConsecutiveNights != 0 {
		t.Fatalf("Hussain (23:00–03:00 once): %+v", hu)
	}
	_ = h
}

func TestFourthNightWarns(t *testing.T) {
	h, med, _ := rosterFixture(t)
	for _, d := range []string{"2030-03-01", "2030-03-02", "2030-03-03"} {
		assign(t, med, "n"+d, "sh_night", "cg_fatima", d)
	}
	post := func(who *models.Caregiver, body map[string]interface{}) (int, map[string]interface{}) {
		rec := medCall(t, h.AssignShift, who, "", body)
		var out map[string]interface{}
		json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	code, out := post(siblings[0], map[string]interface{}{"shift_id": "sh_night", "caregiver_id": "cg_fatima", "date": "2030-03-04"})
	if code != http.StatusOK || out["warning"] != "consecutive_nights" || out["consecutive_nights"] != float64(4) || out["status"] != "assigned" {
		t.Fatalf("4th night: want saved with warning consecutive_nights, got %d %v", code, out)
	}
	var who string
	med.DB.QueryRow(`SELECT caregiver_id FROM shift_assignments WHERE shift_id = 'sh_night' AND date = '2030-03-04'`).Scan(&who)
	if who != "cg_fatima" {
		t.Fatal("a warning, not a block: the assignment is saved")
	}
	if _, out := post(siblings[0], map[string]interface{}{"shift_id": "sh_day", "caregiver_id": "cg_fatima", "date": "2030-03-05"}); out["warning"] != nil {
		t.Fatalf("a day shift isn't a night: %v", out)
	}
	if _, out := post(siblings[0], map[string]interface{}{"shift_id": "sh_night", "caregiver_id": "cg_ali", "date": "2030-03-06"}); out["warning"] != nil {
		t.Fatalf("Ali's first night: %v", out)
	}
	// Filling a gap counts both sides: nights 1-4 and 6 + the 5th = 6 in a row.
	assign(t, med, "n2030-03-06b", "sh_late", "cg_fatima", "2030-03-06")
	if _, out := post(siblings[0], map[string]interface{}{"shift_id": "sh_night", "caregiver_id": "cg_fatima", "date": "2030-03-05"}); out["consecutive_nights"] != float64(6) {
		t.Fatalf("gap filled: want 6 in a row, got %v", out)
	}

	// The picker's view of 2030-03-05: which shifts are nights, and nights before it.
	rec := httpGet(t, h.Load, "/?date=2030-03-05")
	var load struct {
		NightShiftIDs []string `json:"night_shift_ids"`
		Caregivers    []struct {
			CaregiverID  string `json:"caregiver_id"`
			NightsBefore int    `json:"nights_before"`
		} `json:"caregivers"`
	}
	json.Unmarshal(rec.Body.Bytes(), &load)
	nights := map[string]bool{}
	for _, id := range load.NightShiftIDs {
		nights[id] = true
	}
	if !nights["sh_night"] || !nights["sh_late"] || nights["sh_day"] {
		t.Fatalf("night shifts: %v", load.NightShiftIDs)
	}
	for _, c := range load.Caregivers {
		if c.CaregiverID == "cg_fatima" && c.NightsBefore != 4 {
			t.Fatalf("Fatima has 4 nights before the 5th, got %d", c.NightsBefore)
		}
	}
}

func TestUnavailableDates(t *testing.T) {
	h, med, _ := rosterFixture(t)
	add := func(who *models.Caregiver, body map[string]interface{}) (int, map[string]interface{}) {
		rec := medCall(t, h.AddUnavailable, who, "", body)
		var out map[string]interface{}
		json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if code, _ := add(siblings[2], map[string]interface{}{"caregiver_id": "cg_ali", "start_date": "2030-03-10", "end_date": "2030-03-12"}); code != http.StatusForbidden {
		t.Fatalf("Fatima marking Ali away: want 403, got %d", code)
	}
	if code, _ := add(&models.Caregiver{ID: "cg_zainab", Role: models.RoleViewer}, map[string]interface{}{"start_date": "2030-03-10"}); code != http.StatusForbidden {
		t.Fatalf("viewer: want 403, got %d", code)
	}
	if code, _ := add(siblings[2], map[string]interface{}{"start_date": "2030-03-12", "end_date": "2030-03-10"}); code != http.StatusBadRequest {
		t.Fatalf("end before start: want 400, got %d", code)
	}
	code, out := add(siblings[1], map[string]interface{}{"start_date": "2030-03-10", "end_date": "2030-03-12", "reason": "Exams"})
	if code != http.StatusCreated || out["caregiver_id"] != "cg_ali" {
		t.Fatalf("Ali marks himself away: %d %v", code, out)
	}
	rec := medCall(t, h.AssignShift, siblings[0], "", map[string]interface{}{"shift_id": "sh_day", "caregiver_id": "cg_ali", "date": "2030-03-11"})
	var res map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["warning"] != "unavailable" || res["unavailable_reason"] != "Exams" {
		t.Fatalf("assigning Ali on an away day warns: %v", res)
	}
	if rec := medCall(t, h.RemoveUnavailable, siblings[2], out["id"].(string), nil); rec.Code != http.StatusForbidden {
		t.Fatalf("Fatima removing Ali's dates: want 403, got %d", rec.Code)
	}
	if rec := medCall(t, h.RemoveUnavailable, siblings[0], out["id"].(string), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("Super Admin removes: %d", rec.Code)
	}
	var n int
	med.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action IN ('UNAVAILABLE_ADDED', 'UNAVAILABLE_REMOVED')`).Scan(&n)
	if n != 2 {
		t.Fatalf("both changes audited, got %d", n)
	}
}

func httpGet(t *testing.T, fn http.HandlerFunc, url string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	fn(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}
