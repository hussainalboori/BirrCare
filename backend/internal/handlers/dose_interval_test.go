package handlers_test

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
)

// Issue #7: cross-midnight double doses of the same medication.

var (
	fatima = &models.Caregiver{ID: "cg_fatima", Name: "فاطمة (Fatima)", Role: models.RoleCaregiver, AvatarColor: "#059669"}
	ali    = &models.Caregiver{ID: "cg_ali", Name: "علي (Ali)", Role: models.RoleAdmin, AvatarColor: "#2563eb"}
)

func clinicalDay(offset int) string {
	return time.Now().In(handlers.ClinicalLocation()).AddDate(0, 0, offset).Format("2006-01-02")
}

func logDose(t *testing.T, h *handlers.MedicationHandler, who *models.Caregiver, req handlers.LogDoseRequest) (int, map[string]interface{}) {
	t.Helper()
	body, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/medications/log", bytes.NewReader(body))
	r = r.WithContext(authContext(r.Context(), who))
	rec := httptest.NewRecorder()
	h.LogDose(rec, r)
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func addBedtimeMed(t *testing.T, h *handlers.MedicationHandler) {
	t.Helper()
	if _, err := h.DB.Exec(`
		INSERT INTO medications (id, name_ar, name_en, dosage, frequency_slots, slot_times, created_by)
		VALUES ('med_warfarin', 'وارفارين', 'Warfarin', '5mg', '["bedtime"]', '{"bedtime":"22:30"}', 'cg_hussain')
	`); err != nil {
		t.Fatal(err)
	}
}

func TestIntervalHoursDefaults(t *testing.T) {
	ptr := func(f float64) *float64 { return &f }
	for _, c := range []struct {
		name       string
		configured *float64
		slots      []string
		times      map[string]string
		want       float64
	}{
		{"once daily", nil, []string{"bedtime"}, nil, 18},
		{"twice daily", nil, []string{"morning", "evening"}, map[string]string{"morning": "08:00", "evening": "20:00"}, 9},
		{"uneven pair", nil, []string{"evening", "bedtime"}, map[string]string{"evening": "20:00", "bedtime": "22:30"}, 1.88},
		{"doctor's value", ptr(6), []string{"morning"}, nil, 6},
		{"lock off", ptr(0), []string{"morning"}, nil, 0},
	} {
		if got := handlers.IntervalHoursForTest(c.configured, c.slots, c.times); math.Abs(got-c.want) > 0.01 {
			t.Errorf("%s: want %.2f h, got %.2f h", c.name, c.want, got)
		}
	}
}

// Bedtime given at "23:30" on D; at "03:45" the D+1 bedtime card must be locked.
func TestDoseIntervalCrossMidnight(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	addBedtimeMed(t, h)

	given := time.Now().Add(-(4*time.Hour + 15*time.Minute)).UTC().Format(time.RFC3339)
	if code, out := logDose(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_warfarin", Slot: "bedtime", ScheduledDate: clinicalDay(-1), GivenAt: given}); code != http.StatusCreated {
		t.Fatalf("first bedtime dose: %d %v", code, out)
	}

	code, out := logDose(t, h, ali, handlers.LogDoseRequest{MedicationID: "med_warfarin", Slot: "bedtime", ScheduledDate: clinicalDay(0)})
	if code != http.StatusConflict || out["error"] != "DOSE_TOO_SOON" {
		t.Fatalf("D+1 bedtime 4.25 h later: want 409 DOSE_TOO_SOON, got %d %v", code, out)
	}
	if out["caregiver_name"] != fatima.Name {
		t.Fatalf("conflict must name who gave it: %v", out["caregiver_name"])
	}
	if hs, _ := out["hours_since"].(float64); math.Abs(hs-4.25) > 0.05 {
		t.Fatalf("hours_since: want ≈4.25, got %v", out["hours_since"])
	}
	if out["min_hours"].(float64) != 18 || out["next_allowed_at"] == "" {
		t.Fatalf("expected min_hours 18 and next_allowed_at: %v", out)
	}

	// Skipping is never blocked by the interval.
	if code, out := logDose(t, h, ali, handlers.LogDoseRequest{MedicationID: "med_warfarin", Slot: "bedtime", ScheduledDate: clinicalDay(0), Status: "skipped"}); code != http.StatusCreated {
		t.Fatalf("skip inside the interval: want 201, got %d %v", code, out)
	}

	// The schedule shows the lock on every card of the medication, whatever the date.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/medications/schedule?date="+clinicalDay(1), nil)
	rec := httptest.NewRecorder()
	h.GetSchedule(rec, r)
	var sched struct {
		Schedule []models.ScheduledDose `json:"schedule"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &sched)
	found := false
	for _, d := range sched.Schedule {
		if d.Medication.ID == "med_warfarin" {
			found = true
			if d.LastTaken == nil || d.LastTaken.CaregiverName != fatima.Name || d.NextAllowedAt == "" || d.IntervalHours != 18 {
				t.Fatalf("schedule card lacks the lock: %+v last=%+v", d, d.LastTaken)
			}
		}
	}
	if !found {
		t.Fatal("warfarin missing from schedule")
	}
}

// Relies on _txlock=immediate: 20 siblings tapping D and D+1 bedtime at once → one dose.
func TestDoseIntervalConcurrent(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	addBedtimeMed(t, h)

	var wg sync.WaitGroup
	codes := make(chan int, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code, _ := logDose(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_warfarin", Slot: "bedtime", ScheduledDate: clinicalDay(-(i % 2))})
			codes <- code
		}(i)
	}
	wg.Wait()
	close(codes)
	created := 0
	for c := range codes {
		switch c {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
		default:
			t.Fatalf("unexpected status %d", c)
		}
	}
	var rows int
	h.DB.QueryRow(`SELECT COUNT(*) FROM medication_logs WHERE medication_id = 'med_warfarin'`).Scan(&rows)
	if created != 1 || rows != 1 {
		t.Fatalf("want exactly one dose, got %d created / %d rows", created, rows)
	}
}

func TestDoseIntervalOverride(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	addBedtimeMed(t, h)

	given := time.Now().Add(-4 * time.Hour).UTC().Format(time.RFC3339)
	if code, _ := logDose(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_warfarin", Slot: "bedtime", ScheduledDate: clinicalDay(-1), GivenAt: given}); code != http.StatusCreated {
		t.Fatal("first dose failed")
	}
	next := handlers.LogDoseRequest{MedicationID: "med_warfarin", Slot: "bedtime", ScheduledDate: clinicalDay(0), OverrideInterval: true, OverrideReason: "Doctor ordered an extra dose by phone"}

	if code, out := logDose(t, h, fatima, next); code != http.StatusForbidden {
		t.Fatalf("caregiver override: want 403, got %d %v", code, out)
	}
	noReason := next
	noReason.OverrideReason = "  "
	if code, out := logDose(t, h, ali, noReason); code != http.StatusBadRequest || out["error"] != "REASON_REQUIRED" {
		t.Fatalf("admin override without reason: want 400 REASON_REQUIRED, got %d %v", code, out)
	}
	if code, out := logDose(t, h, ali, next); code != http.StatusCreated {
		t.Fatalf("admin override with reason: want 201, got %d %v", code, out)
	}
	var audits int
	h.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'MED_INTERVAL_OVERRIDE' AND caregiver_id = 'cg_ali' AND details LIKE '%Doctor ordered%'`).Scan(&audits)
	if audits != 1 {
		t.Fatalf("want one MED_INTERVAL_OVERRIDE audit row, got %d", audits)
	}
}

// A twice-daily medicine at 08:00 and 20:00 (12 h apart, 9 h lock) is never blocked,
// including a morning dose ticked late with its real time.
func TestDoseIntervalTwiceDaily(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()

	morning := time.Now().Add(-12 * time.Hour).UTC().Format(time.RFC3339)
	if code, out := logDose(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_citicoline", Slot: "morning", ScheduledDate: clinicalDay(0), GivenAt: morning}); code != http.StatusCreated {
		t.Fatalf("morning (ticked late, given 12 h ago): %d %v", code, out)
	}
	if code, out := logDose(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_citicoline", Slot: "evening", ScheduledDate: clinicalDay(0)}); code != http.StatusCreated {
		t.Fatalf("evening 12 h later must not be blocked: %d %v", code, out)
	}

	// Backdating cannot slip a dose in just before a later one.
	sneaky := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	if code, out := logDose(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_citicoline", Slot: "morning", ScheduledDate: clinicalDay(-1), GivenAt: sneaky}); code != http.StatusConflict || out["error"] != "DOSE_TOO_SOON" {
		t.Fatalf("backdated dose 2 h before another: want 409 DOSE_TOO_SOON, got %d %v", code, out)
	}
	// given_at in the future or too far back is rejected.
	for _, bad := range []string{time.Now().Add(time.Hour).UTC().Format(time.RFC3339), time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339), "yesterday"} {
		if code, _ := logDose(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: clinicalDay(0), GivenAt: bad}); code != http.StatusBadRequest {
			t.Fatalf("given_at %q: want 400, got %d", bad, code)
		}
	}
}
