package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"github.com/go-chi/chi/v5"
)

// Issue #57: stopping a medicine or changing its schedule never rewrites the past.

func addVersion(t *testing.T, h *handlers.MedicationHandler, med, from, to, slots, times string) {
	t.Helper()
	var toVal interface{}
	if to != "" {
		toVal = to
	}
	if _, err := h.DB.Exec(`INSERT INTO medication_schedule_versions (id, medication_id, effective_from, effective_to, frequency_slots, days_of_week, slot_times, dosage)
		VALUES (lower(hex(randomblob(8))), ?, ?, ?, ?, '["sun","mon","tue","wed","thu","fri","sat"]', ?, '5mg')`, med, from, toVal, slots, times); err != nil {
		t.Fatal(err)
	}
}

func scheduleFor(t *testing.T, h *handlers.MedicationHandler, date string) map[string][]string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.GetSchedule(rec, httptest.NewRequest(http.MethodGet, "/api/v1/medications/schedule?date="+date, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("schedule %s: %d %s", date, rec.Code, rec.Body.String())
	}
	var out struct {
		Schedule []models.ScheduledDose `json:"schedule"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	got := map[string][]string{}
	for _, d := range out.Schedule {
		slot := d.Slot
		if d.Log != nil {
			slot += "(logged)"
		}
		got[d.Medication.ID] = append(got[d.Medication.ID], slot)
	}
	return got
}

func medCall(t *testing.T, fn http.HandlerFunc, who *models.Caregiver, id string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(b))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", id)
	r = r.WithContext(context.WithValue(authContext(r.Context(), who), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	fn(rec, r)
	return rec
}

func TestReportKeepsAStoppedMedicine(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	h.DB.Exec(`UPDATE medications SET is_active = 0`) // only the test medication
	// Aspirin daily, stopped on day 10 of the 14-day report (by Ali).
	addMed(t, h, "med_asp", `["sun","mon","tue","wed","thu","fri","sat"]`, "00:00", false)
	h.DB.Exec(`UPDATE medications SET is_active = 0, stopped_on = ?, stopped_by = 'cg_ali' WHERE id = 'med_asp'`, clinicalDay(-4))
	addVersion(t, h, "med_asp", clinicalDay(-30), clinicalDay(-5), `["morning"]`, `{"morning":"00:00"}`)
	for day := 1; day <= 9; day++ {
		if day == 3 || day == 6 {
			continue // two missed before it was stopped
		}
		logTaken(t, h, "med_asp", clinicalDay(day-14), "5mg")
	}
	_, out := runExport(t, &handlers.ExportHandler{DB: h.DB}, 14)
	m := medByID(out, "med_asp")
	if m.Expected != 9 || m.NotRecorded != 2 || m.Taken != 7 || m.StoppedOn != clinicalDay(-4) || m.StoppedBy != "علي (Ali)" {
		t.Fatalf("want expected 9, not_recorded 2, stopped on day 10 by Ali; got %+v", m)
	}
}

func TestReportUsesTheScheduleOfEachDay(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	h.DB.Exec(`UPDATE medications SET is_active = 0`)
	addMed(t, h, "med_cit", `["sun","mon","tue","wed","thu","fri","sat"]`, "00:00", false)
	// Twice a day until day 7, once a day from day 8.
	addVersion(t, h, "med_cit", clinicalDay(-30), clinicalDay(-7), `["morning","evening"]`, `{"morning":"00:00","evening":"00:00"}`)
	addVersion(t, h, "med_cit", clinicalDay(-6), "", `["morning"]`, `{"morning":"00:00"}`)
	_, out := runExport(t, &handlers.ExportHandler{DB: h.DB}, 14)
	m := medByID(out, "med_cit")
	if m.Expected != 7*2+7*1 || m.NotRecorded != 21 {
		t.Fatalf("days 1-7 expect 2, days 8-14 expect 1 (21); got %+v", m)
	}
}

func TestStoppingKeepsYesterdayOnTheSchedule(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	h.DB.Exec(`UPDATE medications SET created_at = datetime('now', '-30 days')`)
	logTaken(t, h, "med_aspirin", clinicalDay(-1), "81mg")

	// Stop it today (UpdateMedication, as the medicine form does).
	if rec := medCall(t, h.UpdateMedication, siblings[1], "med_aspirin", map[string]interface{}{"is_active": false}); rec.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body.String())
	}
	if got := scheduleFor(t, h, clinicalDay(-1))["med_aspirin"]; len(got) != 1 || got[0] != "morning(logged)" {
		t.Fatalf("yesterday should still show the logged aspirin dose, got %v", got)
	}
	if got := scheduleFor(t, h, clinicalDay(-2))["med_aspirin"]; len(got) != 1 || got[0] != "morning" {
		t.Fatalf("two days ago it was still scheduled (not recorded), got %v", got)
	}
	if got := scheduleFor(t, h, clinicalDay(0))["med_aspirin"]; len(got) != 0 {
		t.Fatalf("stopped from today: not on today's schedule, got %v", got)
	}
	var stoppedOn, stoppedBy string
	h.DB.QueryRow(`SELECT COALESCE(stopped_on, ''), COALESCE(stopped_by, '') FROM medications WHERE id = 'med_aspirin'`).Scan(&stoppedOn, &stoppedBy)
	if stoppedOn != clinicalDay(0) || stoppedBy != "cg_ali" {
		t.Fatalf("stop date and who: %q %q", stoppedOn, stoppedBy)
	}
	// The report keeps it, with the stop.
	_, out := runExport(t, &handlers.ExportHandler{DB: h.DB}, 7)
	if m := medByID(out, "med_aspirin"); m.Expected != 6 || m.Taken != 1 || m.StoppedOn != clinicalDay(0) {
		t.Fatalf("report after stopping: %+v", m)
	}
}

func TestNewMedicineIsNotOnYesterdaysSchedule(t *testing.T) {
	h, _, cleanup := setupTestDB(t) // the seeded medicines were added just now
	defer cleanup()
	if got := scheduleFor(t, h, clinicalDay(-1)); len(got) != 0 {
		t.Fatalf("medicines added today must not appear on yesterday's screen, got %v", got)
	}
	if got := scheduleFor(t, h, clinicalDay(0)); len(got["med_aspirin"]) != 1 {
		t.Fatalf("but they are on today's, got %v", got)
	}
}

func TestScheduleChangeFromTomorrowLeavesToday(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	h.DB.Exec(`UPDATE medications SET created_at = datetime('now', '-30 days')`)
	// Citicoline morning + evening; from tomorrow, morning only.
	rec := medCall(t, h.UpdateSchedule, siblings[1], "med_citicoline", map[string]interface{}{
		"frequency_slots": []string{"morning"}, "slot_times": map[string]string{"morning": "08:00"}, "effective_from": "tomorrow",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("schedule: %d %s", rec.Code, rec.Body.String())
	}
	if got := scheduleFor(t, h, clinicalDay(0))["med_citicoline"]; len(got) != 2 {
		t.Fatalf("today keeps morning and evening, got %v", got)
	}
	if got := scheduleFor(t, h, clinicalDay(1))["med_citicoline"]; len(got) != 1 || got[0] != "morning" {
		t.Fatalf("tomorrow: morning only, got %v", got)
	}
	if got := scheduleFor(t, h, clinicalDay(-3))["med_citicoline"]; len(got) != 2 {
		t.Fatalf("the past keeps twice a day, got %v", got)
	}
	// Today's evening dose can still be logged although it's gone from tomorrow.
	if res := postDose(h, siblings[2], handlers.LogDoseRequest{MedicationID: "med_citicoline", Slot: "evening", ScheduledDate: clinicalDay(0), Status: "taken"}); res.code != http.StatusCreated {
		t.Fatalf("today's evening dose: %d %s", res.code, res.raw)
	}
	if rec := medCall(t, h.UpdateSchedule, siblings[1], "med_citicoline", map[string]interface{}{"effective_from": "next week"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad effective_from: want 400, got %d", rec.Code)
	}
}
