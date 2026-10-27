package handlers_test

import (
	"net/http"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
)

// Issue #62: warfarin 5 mg on weekdays, 2.5 mg at weekends.
func TestDoseByWeekday(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	h.DB.Exec(`INSERT INTO medications (id, name_ar, name_en, dosage, frequency_slots, slot_times, dose_by_day, created_by, created_at)
		VALUES ('med_w', 'وارفارين', 'Warfarin', '5 mg', '["evening"]', '{"evening":"20:00"}', '{"sat":"2.5 mg","sun":"2.5 mg"}', 'cg_hussain', datetime('now', '-30 days'))`)
	day := time.Now().In(handlers.ClinicalLocation()).AddDate(0, 0, -7)
	for day.Weekday() != time.Saturday {
		day = day.AddDate(0, 0, 1)
	}
	sat, fri := day.Format("2006-01-02"), day.AddDate(0, 0, -1).Format("2006-01-02")
	doseOf := func(date string) (string, bool) {
		for _, d := range scheduleDoses(t, h, date) {
			if d.Medication.ID == "med_w" {
				return d.Medication.Dosage, d.DoseChanged
			}
		}
		return "", false
	}
	if dose, changed := doseOf(sat); dose != "2.5 mg" || !changed {
		t.Fatalf("Saturday: want 2.5 mg, changed from Friday; got %q %t", dose, changed)
	}
	if dose, _ := doseOf(fri); dose != "5 mg" {
		t.Fatalf("Friday: want 5 mg, got %q", dose)
	}
	res := postDose(h, siblings[2], handlers.LogDoseRequest{MedicationID: "med_w", Slot: "evening", ScheduledDate: sat, Status: "taken"})
	if res.code != http.StatusCreated {
		t.Fatalf("log: %d %s", res.code, res.raw)
	}
	var snap string
	h.DB.QueryRow(`SELECT dosage_snapshot FROM medication_logs WHERE id = ?`, res.body["log_id"]).Scan(&snap)
	if snap != "2.5 mg" {
		t.Fatalf("dosage_snapshot: want 2.5 mg, got %q", snap)
	}
}
