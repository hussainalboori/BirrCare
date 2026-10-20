package handlers_test

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
)

// Issue #14: adherence against expected doses; daily BP by household date.

type report struct {
	Adherence     handlers.MedAdherence `json:"adherence"`
	PerMedication []handlers.MedReport  `json:"per_medication"`
	DailyBP       []handlers.DailyBP    `json:"daily_bp"`
}

func runExport(t *testing.T, h *handlers.ExportHandler, days int) (int, report) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ExportDoctorSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export/summary?days="+strconv.Itoa(days), nil))
	var out report
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func medByID(out report, id string) handlers.MedReport {
	for _, m := range out.PerMedication {
		if m.MedicationID == id {
			return m
		}
	}
	return handlers.MedReport{}
}

// addMed inserts an active medication created 30 days ago; a 00:00 slot is always due.
func addMed(t *testing.T, h *handlers.MedicationHandler, id, days, slotTime string, critical bool) {
	t.Helper()
	if _, err := h.DB.Exec(`INSERT INTO medications (id, name_ar, name_en, dosage, frequency_slots, days_of_week, slot_times, is_critical, created_by, created_at)
		VALUES (?, ?, ?, '5mg', '["morning"]', ?, ?, ?, 'cg_hussain', datetime('now', '-30 days'))`,
		id, id, id, days, `{"morning":"`+slotTime+`"}`, critical); err != nil {
		t.Fatal(err)
	}
}

func logTaken(t *testing.T, h *handlers.MedicationHandler, med, date, dosage string) {
	t.Helper()
	if _, err := h.DB.Exec(`INSERT INTO medication_logs (id, medication_id, caregiver_id, slot, scheduled_date, status, dosage_snapshot)
		VALUES (lower(hex(randomblob(8))), ?, 'cg_fatima', 'morning', ?, 'taken', ?)`, med, date, dosage); err != nil {
		t.Fatal(err)
	}
}

func TestAdherenceCountsUnloggedDosesAsNotRecorded(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	h.DB.Exec(`UPDATE medications SET is_active = 0`) // only the test medication
	addMed(t, h, "med_daily", `["sun","mon","tue","wed","thu","fri","sat"]`, "00:00", false)
	for i := 0; i < 14; i++ {
		if i == 3 || i == 9 {
			continue // nobody tapped anything those days
		}
		logTaken(t, h, "med_daily", clinicalDay(-i), "5mg")
	}
	code, out := runExport(t, &handlers.ExportHandler{DB: h.DB}, 14)
	if code != http.StatusOK {
		t.Fatalf("export: %d", code)
	}
	m := medByID(out, "med_daily")
	if m.Expected != 14 || m.Taken != 12 || m.NotRecorded != 2 || math.Abs(m.Rate-85.71) > 0.01 {
		t.Fatalf("want expected 14, taken 12, not_recorded 2, rate 85.7; got %+v", m)
	}
	if len(m.MissedDates) != 2 || m.MissedDates[0] != clinicalDay(-9) || m.MissedDates[1] != clinicalDay(-3) {
		t.Fatalf("missed dates: %v", m.MissedDates)
	}
	if out.Adherence.TotalScheduled != 14 || out.Adherence.TotalNotRecorded != 2 || math.Abs(out.Adherence.AdherenceRate-85.71) > 0.01 {
		t.Fatalf("totals: %+v", out.Adherence)
	}
}

func TestAdherenceHonoursDaysOfWeekAndDueTime(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	h.DB.Exec(`UPDATE medications SET is_active = 0`)
	addMed(t, h, "med_sun_wed", `["sun","wed"]`, "00:00", false)
	addMed(t, h, "med_late", `["sun","mon","tue","wed","thu","fri","sat"]`, "23:59", true)
	_, out := runExport(t, &handlers.ExportHandler{DB: h.DB}, 14)
	if m := medByID(out, "med_sun_wed"); m.Expected != 4 || m.NotRecorded != 4 {
		t.Fatalf("sun+wed over 14 days: want expected 4, got %+v", m)
	}
	late := medByID(out, "med_late")
	if late.Expected != 13 && !(time.Now().In(handlers.ClinicalLocation()).Format("15:04") >= "23:59" && late.Expected == 14) {
		t.Fatalf("today's dose isn't due before 23:59 and must not count yet: %+v", late)
	}
	if !out.PerMedication[0].IsCritical {
		t.Fatalf("critical medications must come first: %+v", out.PerMedication)
	}
}

func TestAdherenceShowsEarlierDosage(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	h.DB.Exec(`UPDATE medications SET is_active = 0`)
	addMed(t, h, "med_dose", `["sun","mon","tue","wed","thu","fri","sat"]`, "00:00", false)
	logTaken(t, h, "med_dose", clinicalDay(-2), "2.5mg") // before the dose was raised
	logTaken(t, h, "med_dose", clinicalDay(-1), "5mg")
	_, out := runExport(t, &handlers.ExportHandler{DB: h.DB}, 7)
	if m := medByID(out, "med_dose"); m.Dosage != "5mg" || len(m.PastDosages) != 1 || m.PastDosages[0] != "2.5mg" {
		t.Fatalf("dosage history: %+v", m)
	}
}

func TestDailyBPUsesHouseholdDate(t *testing.T) {
	bahrain, err := time.LoadLocation("Asia/Bahrain")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	prev := handlers.ClinicalLocation()
	handlers.SetClinicalLocationForTest(bahrain)
	defer handlers.SetClinicalLocationForTest(prev)

	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	// 22:30 UTC two days ago is 01:30 the next day in Bahrain (UTC+3).
	utcDay := time.Now().UTC().AddDate(0, 0, -2)
	at := time.Date(utcDay.Year(), utcDay.Month(), utcDay.Day(), 22, 30, 0, 0, time.UTC)
	if _, err := h.DB.Exec(`INSERT INTO vitals_logs (id, caregiver_id, recorded_at, systolic, diastolic, vital_type) VALUES ('v1', 'cg_fatima', ?, 150, 95, 'bp')`,
		at.Format("2006-01-02 15:04:05")); err != nil {
		t.Fatal(err)
	}
	_, out := runExport(t, &handlers.ExportHandler{DB: h.DB}, 7)
	want := at.In(bahrain).Format("2006-01-02")
	if len(out.DailyBP) != 1 || out.DailyBP[0].Date != want || out.DailyBP[0].Readings != 1 || out.DailyBP[0].AvgSys != 150 {
		t.Fatalf("want one daily_bp row on %s (local), got %+v", want, out.DailyBP)
	}
	if want == at.Format("2006-01-02") {
		t.Fatal("test setup: local date should differ from the UTC date")
	}
}

func TestExportReturns500OnQueryError(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := h.DB.Exec(`ALTER TABLE vitals_logs RENAME TO vitals_logs_gone`); err != nil {
		t.Fatal(err)
	}
	if code, _ := runExport(t, &handlers.ExportHandler{DB: h.DB}, 7); code != http.StatusInternalServerError {
		t.Fatalf("a failed query must be a 500, not an empty report: got %d", code)
	}
}
