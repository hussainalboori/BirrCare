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

	"context"
	"github.com/go-chi/chi/v5"
)

var repeatAdmin = &models.Caregiver{ID: "cg_hussain", Name: "Hussain", Role: models.RoleSuperAdmin}

func addRepeat(h *handlers.ShiftsHandler, c *models.Caregiver, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shifts/repeats", strings.NewReader(body))
	req = req.WithContext(authContext(req.Context(), c))
	rec := httptest.NewRecorder()
	h.AddRepeat(rec, req)
	return rec
}

func stopRepeat(h *handlers.ShiftsHandler, c *models.Caregiver, id string) *httptest.ResponseRecorder {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/shifts/repeats/"+id, nil)
	req = req.WithContext(context.WithValue(authContext(req.Context(), c), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.StopRepeat(rec, req)
	return rec
}

// nextThursday is the first Thursday after today, at home.
func nextThursday() time.Time {
	d := time.Now().In(handlers.ClinicalLocation()).AddDate(0, 0, 1)
	for d.Weekday() != time.Thursday {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

func assignedTo(t *testing.T, h *handlers.ShiftsHandler, date string) string {
	t.Helper()
	var who string
	_ = h.DB.QueryRow(`SELECT caregiver_id FROM shift_assignments WHERE shift_id = 'shift_24h' AND date = ?`, date).Scan(&who)
	return who
}

func TestWeeklyShiftRepeats(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	h := &handlers.ShiftsHandler{DB: med.DB}
	thu := nextThursday()
	day := func(weeks int) string { return thu.AddDate(0, 0, 7*weeks).Format("2006-01-02") }
	body := `{"shift_id":"shift_24h","caregiver_id":"cg_hussain","date":"` + day(0) + `"}`

	if rec := addRepeat(h, &models.Caregiver{ID: "cg_ali", Role: models.RoleAdmin}, body); rec.Code != http.StatusForbidden {
		t.Fatalf("admin: got %d, want 403", rec.Code)
	}
	// One-off swap already on the roster: it must survive the rule.
	if _, err := h.DB.Exec(`INSERT INTO shift_assignments (id, shift_id, caregiver_id, date) VALUES ('swap', 'shift_24h', 'cg_fatima', ?)`, day(1)); err != nil {
		t.Fatal(err)
	}
	rec := addRepeat(h, repeatAdmin, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add: got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID      string `json:"id"`
		Weekday int    `json:"weekday"`
	}
	json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Weekday != int(time.Thursday) {
		t.Fatalf("weekday = %d", created.Weekday)
	}
	for weeks, want := range map[int]string{0: "cg_hussain", 1: "cg_fatima", 2: "cg_hussain", 8: "cg_hussain"} {
		if got := assignedTo(t, h, day(weeks)); got != want {
			t.Fatalf("week %d: %q, want %q", weeks, got, want)
		}
	}

	// A day taken off by hand is not put back by the top-up.
	if _, err := h.DB.Exec(`DELETE FROM shift_assignments WHERE shift_id = 'shift_24h' AND date = ?`, day(2)); err != nil {
		t.Fatal(err)
	}
	if err := handlers.FillShiftRepeats(h.DB, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := assignedTo(t, h, day(2)); got != "" {
		t.Fatalf("removed day came back: %q", got)
	}

	// Time passes: the rule keeps reaching further.
	far := thu.AddDate(0, 0, 7*20).Format("2006-01-02")
	if got := assignedTo(t, h, far); got != "" {
		t.Fatalf("written too far ahead: %q", got)
	}
	if err := handlers.FillShiftRepeats(h.DB, time.Now().AddDate(0, 0, 70)); err != nil {
		t.Fatal(err)
	}
	if got := assignedTo(t, h, far); got != "cg_hussain" {
		t.Fatalf("not topped up: %q", got)
	}

	// The calendar shows a month beyond what is written.
	month := thu.AddDate(0, 8, 0).Format("2006-01")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/shifts/month?month="+month, nil)
	mrec := httptest.NewRecorder()
	h.Month(mrec, req)
	var res struct {
		Assignments []struct {
			Date     string `json:"date"`
			RepeatID string `json:"repeat_id"`
		} `json:"assignments"`
		Repeats []struct {
			ID string `json:"id"`
		} `json:"repeats"`
	}
	if err := json.Unmarshal(mrec.Body.Bytes(), &res); err != nil {
		t.Fatalf("%v: %s", err, mrec.Body.String())
	}
	if len(res.Assignments) < 4 || res.Assignments[0].RepeatID != created.ID || len(res.Repeats) != 1 {
		t.Fatalf("month: %s", mrec.Body.String())
	}
	d, _ := time.Parse("2006-01-02", res.Assignments[0].Date)
	if d.Weekday() != time.Thursday {
		t.Fatalf("calendar day %s is not a Thursday", res.Assignments[0].Date)
	}

	// A swap of one day leaves the rule alone, and stopping keeps the swap.
	if _, err := h.DB.Exec(`UPDATE shift_assignments SET caregiver_id = 'cg_fatima', repeat_id = NULL WHERE shift_id = 'shift_24h' AND date = ?`, day(3)); err != nil {
		t.Fatal(err)
	}
	if rec := stopRepeat(h, repeatAdmin, created.ID); rec.Code != http.StatusOK {
		t.Fatalf("stop: %d", rec.Code)
	}
	if got := assignedTo(t, h, day(4)); got != "" {
		t.Fatalf("future day kept after stop: %q", got)
	}
	if got := assignedTo(t, h, day(3)); got != "cg_fatima" {
		t.Fatalf("swap lost after stop: %q", got)
	}
	if rec := stopRepeat(h, repeatAdmin, created.ID); rec.Code != http.StatusNotFound {
		t.Fatalf("stop twice: %d", rec.Code)
	}
}

func TestWeeklyShiftReplacesTheOldPerson(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	h := &handlers.ShiftsHandler{DB: med.DB}
	thu := nextThursday()
	date := thu.Format("2006-01-02")
	for _, who := range []string{"cg_hussain", "cg_fatima"} {
		if rec := addRepeat(h, repeatAdmin, `{"shift_id":"shift_24h","caregiver_id":"`+who+`","date":"`+date+`"}`); rec.Code != http.StatusCreated {
			t.Fatalf("%s: %d %s", who, rec.Code, rec.Body.String())
		}
	}
	if got := assignedTo(t, h, thu.AddDate(0, 0, 7).Format("2006-01-02")); got != "cg_fatima" {
		t.Fatalf("got %q", got)
	}
	var rules int
	h.DB.QueryRow(`SELECT COUNT(*) FROM shift_repeats`).Scan(&rules)
	if rules != 1 {
		t.Fatalf("rules = %d", rules)
	}
}

// Thursday 8 AM to Friday 8 AM: at 2 AM on Friday it is still Thursday's shift.
func TestTwentyFourHourShiftPastMidnightBelongsToItsStartDay(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	_, date, err := handlers.FindCurrentShiftForTest(med.DB, time.Date(2026, 11, 13, 2, 0, 0, 0, time.UTC)) // a Friday
	if err != nil || date != "2026-11-12" {
		t.Fatalf("at 02:00: date %q, err %v", date, err)
	}
	_, date, _ = handlers.FindCurrentShiftForTest(med.DB, time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC))
	if date != "2026-11-13" {
		t.Fatalf("at 09:00: date %q", date)
	}
}

// The real roster: a 24-hour shift on some days, an 8 PM one on others. Tonight's
// 8 PM shift must not lose to a 24-hour shift nobody is on.
func TestStaffedShiftWinsOverAnEmptyOverlappingOne(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := med.DB.Exec(`
		INSERT INTO shifts (id, name_ar, name_en, start_time, end_time, duration_hours, display_order, is_active)
		VALUES ('s_night', 'ليل', 'normal', '20:00', '08:00', 12, 2, 1);
		INSERT INTO shift_assignments (id, shift_id, caregiver_id, date) VALUES
			('a1', 's_night', 'cg_fatima', '2026-11-07'),
			('a2', 'shift_24h', 'cg_hussain', '2026-11-12');
	`); err != nil {
		t.Fatal(err)
	}
	check := func(when time.Time, wantShift, wantDate string) {
		t.Helper()
		shift, date, err := handlers.FindCurrentShiftForTest(med.DB, when)
		if err != nil || shift != wantShift || date != wantDate {
			t.Fatalf("%s: %s on %s (err %v), want %s on %s", when, shift, date, err, wantShift, wantDate)
		}
	}
	check(time.Date(2026, 11, 7, 21, 0, 0, 0, time.UTC), "s_night", "2026-11-07")
	check(time.Date(2026, 11, 8, 2, 0, 0, 0, time.UTC), "s_night", "2026-11-07")
	check(time.Date(2026, 11, 12, 21, 0, 0, 0, time.UTC), "shift_24h", "2026-11-12")
	check(time.Date(2026, 11, 13, 2, 0, 0, 0, time.UTC), "shift_24h", "2026-11-12")

	// The medicines list: someone off today sees the day's roster, not the empty shift.
	super := &models.Caregiver{ID: "cg_hussain", Name: "Hussain", Role: models.RoleSuperAdmin}
	if rec := putMedOrder(&handlers.ShiftsHandler{DB: med.DB}, super, `{"order":"shift"}`); rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	got, err := handlers.MedOrderFromForTest(med.DB, "cg_hussain", time.Date(2026, 11, 7, 17, 0, 0, 0, handlers.ClinicalLocation()))
	if err != nil || got != "20:00" {
		t.Fatalf("off today at 17:00: %q (err %v), want 20:00", got, err)
	}
}
