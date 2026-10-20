package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"birrcare/backend/internal/db"
	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"

	"github.com/go-chi/chi/v5"
)

// Issue #9: partial, vomited and refused doses.

func doseEvent(t *testing.T, h *handlers.MedicationHandler, who *models.Caregiver, logID string, req handlers.DoseEventRequest) (int, map[string]interface{}) {
	t.Helper()
	body, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/medications/log/"+logID+"/event", bytes.NewReader(body))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", logID)
	r = r.WithContext(context.WithValue(authContext(r.Context(), who), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	h.AddDoseEvent(rec, r)
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func intPtr(i int) *int { return &i }

func TestPartialDoseHoldsBothLocks(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	addBedtimeMed(t, h)

	given := time.Now().Add(-4 * time.Hour).UTC().Format(time.RFC3339)
	partial := handlers.LogDoseRequest{MedicationID: "med_warfarin", Slot: "bedtime", ScheduledDate: clinicalDay(-1), Status: "partial", GivenAt: given, Notes: "spat out half"}
	mustLog(t, h, fatima, partial)

	again := partial
	again.Status, again.GivenAt = "taken", ""
	if code, out := logDose(t, h, ali, again); code != http.StatusConflict || out["error"] != "DOSE_ALREADY_LOGGED" {
		t.Fatalf("taken after partial, same card: want 409 DOSE_ALREADY_LOGGED, got %d %v", code, out)
	}
	if code, out := logDose(t, h, ali, handlers.LogDoseRequest{MedicationID: "med_warfarin", Slot: "bedtime", ScheduledDate: clinicalDay(0)}); code != http.StatusConflict || out["error"] != "DOSE_TOO_SOON" {
		t.Fatalf("D+1 within the interval of a partial dose: want 409 DOSE_TOO_SOON, got %d %v", code, out)
	}
}

func TestRefusedDoseDoesNotStartInterval(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	addBedtimeMed(t, h)
	mustLog(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_warfarin", Slot: "bedtime", ScheduledDate: clinicalDay(-1), Status: "refused"})
	if code, out := logDose(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_warfarin", Slot: "bedtime", ScheduledDate: clinicalDay(0)}); code != http.StatusCreated {
		t.Fatalf("a refused dose was never given; the next one must not be blocked: %d %v", code, out)
	}
}

func TestVomitedIsAFollowUpOnAGivenDose(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()

	if code, out := logDose(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: clinicalDay(0), Status: "vomited"}); code != http.StatusBadRequest || out["error"] != "USE_EVENT" {
		t.Fatalf("status vomited must be refused (use the event): %d %v", code, out)
	}

	skipped := mustLog(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_citicoline", Slot: "morning", ScheduledDate: clinicalDay(0), Status: "skipped"})
	if code, out := doseEvent(t, h, fatima, skipped, handlers.DoseEventRequest{Type: "vomited", MinutesAfter: intPtr(10)}); code != http.StatusBadRequest {
		t.Fatalf("vomited on a skipped log: want 400, got %d %v", code, out)
	}

	taken := mustLog(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: clinicalDay(0)})
	if code, out := doseEvent(t, h, fatima, taken, handlers.DoseEventRequest{Type: "vomited", MinutesAfter: intPtr(10)}); code != http.StatusCreated {
		t.Fatalf("vomited on a taken dose: want 201, got %d %v", code, out)
	}
	if code, _ := doseEvent(t, h, ali, taken, handlers.DoseEventRequest{Type: "vomited", MinutesAfter: intPtr(15)}); code != http.StatusConflict {
		t.Fatalf("second vomited event: want 409, got %d", code)
	}
	if code, _ := doseEvent(t, h, fatima, taken, handlers.DoseEventRequest{Type: "vomited", MinutesAfter: intPtr(-5)}); code != http.StatusBadRequest {
		t.Fatalf("negative minutes: want 400, got %d", code)
	}
	viewer := &models.Caregiver{ID: "cg_zainab", Role: models.RoleViewer}
	if code, _ := doseEvent(t, h, viewer, taken, handlers.DoseEventRequest{Type: "vomited"}); code != http.StatusForbidden {
		t.Fatalf("viewer: want 403, got %d", code)
	}

	// The dose still holds the card and shows "taken → vomited 10 min later".
	if code, _ := logDose(t, h, ali, handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: clinicalDay(0)}); code != http.StatusConflict {
		t.Fatalf("vomited dose must keep the slot locked, got %d", code)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/medications/schedule?date="+clinicalDay(0), nil)
	rec := httptest.NewRecorder()
	h.GetSchedule(rec, r)
	var sched struct {
		Schedule []models.ScheduledDose `json:"schedule"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &sched)
	ok := false
	for _, d := range sched.Schedule {
		if d.Medication.ID == "med_aspirin" && d.Log != nil && len(d.Log.Events) == 1 && d.Log.Events[0].Type == "vomited" && *d.Log.Events[0].MinutesAfter == 10 {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("schedule must carry the vomited event: %s", rec.Body.String())
	}

	// A cancelled entry can't get a follow-up.
	if code, _ := voidDose(t, h, fatima, taken, ""); code != http.StatusOK {
		t.Fatal("void failed")
	}
	if code, _ := doseEvent(t, h, fatima, taken, handlers.DoseEventRequest{Type: "vomited"}); code != http.StatusBadRequest {
		t.Fatalf("event on a voided entry: want 400, got %d", code)
	}
}

func TestExportCountsPartialVomitedRefused(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	addBedtimeMed(t, h)

	taken := mustLog(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: clinicalDay(-2)})
	doseEvent(t, h, fatima, taken, handlers.DoseEventRequest{Type: "vomited", MinutesAfter: intPtr(10)})
	mustLog(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: clinicalDay(-1), Status: "refused"})
	mustLog(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_warfarin", Slot: "bedtime", ScheduledDate: clinicalDay(-1), Status: "partial"})

	exp := &handlers.ExportHandler{DB: h.DB}
	rec := httptest.NewRecorder()
	exp.ExportDoctorSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export/summary?days=7", nil))
	var out struct {
		Adherence     handlers.MedAdherence `json:"adherence"`
		PerMedication []handlers.MedReport  `json:"per_medication"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	byID := map[string]handlers.MedReport{}
	for _, c := range out.PerMedication {
		byID[c.MedicationID] = c
	}
	if a := byID["med_aspirin"]; a.Taken != 1 || a.Vomited != 1 || a.Refused != 1 {
		t.Fatalf("aspirin counts: %+v", a)
	}
	if w := byID["med_warfarin"]; w.Partial != 1 || w.Taken != 0 {
		t.Fatalf("warfarin counts: %+v", w)
	}
	if out.Adherence.TotalPartial != 1 || out.Adherence.TotalVomited != 1 || out.Adherence.TotalRefused != 1 {
		t.Fatalf("adherence totals: %+v", out.Adherence)
	}
}

// A database already migrated for #8 is rebuilt for the new statuses with its void history intact.
func TestMigrateKeepsVoidHistoryWhenAddingStatuses(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "v8-*.db")
	f.Close()
	database, err := db.InitDB(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE medication_logs (id TEXT PRIMARY KEY, medication_id TEXT NOT NULL, caregiver_id TEXT NOT NULL, slot TEXT NOT NULL,
		scheduled_date TEXT NOT NULL, taken_at TEXT, recorded_at TEXT, status TEXT NOT NULL CHECK (status IN ('taken', 'skipped')), notes TEXT,
		voided_at TEXT, voided_by TEXT, void_reason TEXT);
		CREATE UNIQUE INDEX uq_med_dose_active ON medication_logs(medication_id, scheduled_date, slot) WHERE voided_at IS NULL;
		INSERT INTO medication_logs VALUES ('v', 'm', 'c', 'morning', '2026-10-19', '2026-10-19 08:00:00', '2026-10-19 08:00:30', 'taken', '', '2026-10-19 08:01:00', 'c', 'wrong card')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	var recorded, reason string
	if err := database.QueryRow(`SELECT recorded_at, void_reason FROM medication_logs WHERE id = 'v' AND voided_at IS NOT NULL`).Scan(&recorded, &reason); err != nil || recorded != "2026-10-19 08:00:30" || reason != "wrong card" {
		t.Fatalf("void history lost: %v %q %q", err, recorded, reason)
	}
	// Only the CHECK is under test here; the rows reference no real medication/caregiver.
	if _, err := database.Exec(`PRAGMA foreign_keys = OFF; INSERT INTO medication_logs (id, medication_id, caregiver_id, slot, scheduled_date, status) VALUES ('p', 'm', 'c', 'evening', '2026-10-19', 'partial')`); err != nil {
		t.Fatalf("partial status must be allowed after the rebuild: %v", err)
	}
}
