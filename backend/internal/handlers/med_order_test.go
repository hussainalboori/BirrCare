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

func putMedOrder(h *handlers.ShiftsHandler, c *models.Caregiver, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/shifts/med-order", strings.NewReader(body))
	req = req.WithContext(authContext(req.Context(), c))
	rec := httptest.NewRecorder()
	h.PutMedOrder(rec, req)
	return rec
}

func scheduleOrderFrom(t *testing.T, h *handlers.MedicationHandler, c *models.Caregiver) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/medications/schedule", nil)
	req = req.WithContext(authContext(req.Context(), c))
	rec := httptest.NewRecorder()
	h.GetSchedule(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("schedule: got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		OrderFrom string `json:"order_from"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res.OrderFrom
}

func TestMedOrderFollowsTheCurrentShift(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	shifts := &handlers.ShiftsHandler{DB: med.DB}
	super := &models.Caregiver{ID: "cg_hussain", Name: "Hussain", Role: models.RoleSuperAdmin}
	carer := &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}

	// Unchanged until the Super Admin chooses it.
	if got := scheduleOrderFrom(t, med, carer); got != "" {
		t.Fatalf("default order_from = %q, want clock order", got)
	}

	if rec := putMedOrder(shifts, &models.Caregiver{ID: "cg_ali", Role: models.RoleAdmin}, `{"order":"shift"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("admin: got %d, want 403", rec.Code)
	}
	if rec := putMedOrder(shifts, carer, `{"order":"shift"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("caregiver: got %d, want 403", rec.Code)
	}
	if rec := putMedOrder(shifts, super, `{"order":"sideways"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad order: got %d, want 400", rec.Code)
	}

	if rec := putMedOrder(shifts, super, `{"order":"shift"}`); rec.Code != http.StatusOK {
		t.Fatalf("super admin: got %d: %s", rec.Code, rec.Body.String())
	}
	if got := scheduleOrderFrom(t, med, carer); got != "08:00" { // the seeded 24-hour shift
		t.Fatalf("order_from = %q, want 08:00", got)
	}

	var audited int
	if err := med.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'MED_ORDER_CHANGED'`).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("audit rows = %d (err %v), want 1", audited, err)
	}

	// Saving again updates the same setting, and clock order turns it off.
	if rec := putMedOrder(shifts, super, `{"order":"clock"}`); rec.Code != http.StatusOK {
		t.Fatalf("back to clock: got %d", rec.Code)
	}
	if got := scheduleOrderFrom(t, med, carer); got != "" {
		t.Fatalf("after clock: order_from = %q", got)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/shifts/med-order", nil)
	rec := httptest.NewRecorder()
	shifts.GetMedOrder(rec, req)
	if !strings.Contains(rec.Body.String(), `"clock"`) {
		t.Fatalf("GET: %s", rec.Body.String())
	}
}

// A 24-hour shift covers every hour, so it must not hide a shift inside it.
func TestMedOrderTakesTheShiftThatBeganLast(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	super := &models.Caregiver{ID: "cg_hussain", Name: "Hussain", Role: models.RoleSuperAdmin}
	if rec := putMedOrder(&handlers.ShiftsHandler{DB: med.DB}, super, `{"order":"shift"}`); rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	at := func(hour, minute int) time.Time {
		return time.Date(2026, 11, 7, hour, minute, 0, 0, handlers.ClinicalLocation())
	}
	check := func(when time.Time, want string) {
		t.Helper()
		got, err := handlers.MedOrderFromForTest(med.DB, "", when)
		if err != nil || got != want {
			t.Fatalf("at %s: got %q (err %v), want %s", when.Format("15:04"), got, err, want)
		}
	}

	if _, err := med.DB.Exec(`INSERT INTO shifts (id, name_ar, name_en, start_time, end_time, duration_hours, display_order, is_active)
		VALUES ('shift_night', 'ليل', 'normal', '20:00', '08:00', 12, 2, 1)`); err != nil {
		t.Fatal(err)
	}
	check(at(8, 0), "08:00")
	check(at(15, 30), "08:00")
	check(at(19, 59), "08:00")
	check(at(20, 0), "20:00")
	check(at(23, 0), "20:00")
	check(at(2, 0), "20:00") // past midnight, still the night shift
	check(at(7, 59), "20:00")

	// Hours no shift covers keep the roster's fallback, the first active shift.
	if _, err := med.DB.Exec(`UPDATE shifts SET start_time = '20:00', end_time = '23:00', display_order = 1 WHERE id = 'shift_24h'`); err != nil {
		t.Fatal(err)
	}
	if _, err := med.DB.Exec(`UPDATE shifts SET start_time = '06:00', end_time = '10:00', display_order = 2 WHERE id = 'shift_night'`); err != nil {
		t.Fatal(err)
	}
	check(at(15, 0), "20:00")
}

// Each person's list starts from their own shift, whatever is running now.
func TestMedOrderStartsFromTheirOwnShift(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	super := &models.Caregiver{ID: "cg_hussain", Name: "Hussain", Role: models.RoleSuperAdmin}
	if rec := putMedOrder(&handlers.ShiftsHandler{DB: med.DB}, super, `{"order":"shift"}`); rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	at := func(day, hour int) time.Time {
		return time.Date(2026, 11, day, hour, 0, 0, 0, handlers.ClinicalLocation())
	}
	check := func(who string, when time.Time, want string) {
		t.Helper()
		got, err := handlers.MedOrderFromForTest(med.DB, who, when)
		if err != nil || got != want {
			t.Fatalf("%s at %s: got %q (err %v), want %s", who, when.Format("Jan 2 15:04"), got, err, want)
		}
	}
	if _, err := med.DB.Exec(`
		INSERT INTO shifts (id, name_ar, name_en, start_time, end_time, duration_hours, display_order, is_active) VALUES
			('s_day', 'نهار', 'day', '08:00', '20:00', 12, 2, 1),
			('s_night', 'ليل', 'night', '20:00', '08:00', 12, 3, 1);
		INSERT INTO shift_assignments (id, shift_id, caregiver_id, date) VALUES
			('a1', 's_night', 'cg_hussain', '2026-11-07'),
			('a2', 's_day', 'cg_fatima', '2026-11-07');
	`); err != nil {
		t.Fatal(err)
	}

	check("cg_hussain", at(7, 15), "20:00") // before it begins, from the start of the day
	check("cg_hussain", at(7, 22), "20:00")
	check("cg_hussain", at(8, 2), "20:00") // past midnight, the same shift
	check("cg_fatima", at(7, 15), "08:00")
	check("cg_fatima", at(7, 22), "08:00")

	// Last night's shift is over and nothing is assigned today: back to what's running.
	check("cg_hussain", at(8, 9), "08:00")
	// Not on the roster at all.
	check("cg_ali", at(7, 22), "20:00")

	// A day shift doesn't carry over to the next morning.
	check("cg_fatima", at(8, 2), "20:00")

	// A shift that was removed no longer counts.
	if _, err := med.DB.Exec(`UPDATE shifts SET is_active = 0 WHERE id = 's_night'`); err != nil {
		t.Fatal(err)
	}
	check("cg_hussain", at(7, 22), "08:00")
}

func TestMedOrderOvernightEndingTodayStartingTonight(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	super := &models.Caregiver{ID: "cg_hussain", Name: "Hussain", Role: models.RoleSuperAdmin}
	if rec := putMedOrder(&handlers.ShiftsHandler{DB: med.DB}, super, `{"order":"shift"}`); rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	if _, err := med.DB.Exec(`
		INSERT INTO shifts (id, name_ar, name_en, start_time, end_time, duration_hours, display_order, is_active) VALUES
			('s_morning', 'ص', 'am', '06:00', '12:00', 6, 2, 1),
			('s_night', 'ل', 'pm', '20:00', '02:00', 6, 3, 1);
		INSERT INTO shift_assignments (id, shift_id, caregiver_id, date) VALUES
			('a1', 's_night', 'cg_fatima', '2026-11-07'),
			('a2', 's_morning', 'cg_fatima', '2026-11-07');
	`); err != nil {
		t.Fatal(err)
	}
	for hour, want := range map[int]string{7: "06:00", 15: "20:00", 21: "20:00"} {
		when := time.Date(2026, 11, 7, hour, 0, 0, 0, handlers.ClinicalLocation())
		if got, err := handlers.MedOrderFromForTest(med.DB, "cg_fatima", when); err != nil || got != want {
			t.Fatalf("at %d:00: got %q (err %v), want %s", hour, got, err, want)
		}
	}
}
