package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"birrcare/backend/internal/db"
	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"

	"github.com/go-chi/chi/v5"
)

// Issue #8: a mis-tapped dose can be undone/voided without ever deleting history.

var hussain = &models.Caregiver{ID: "cg_hussain", Name: "حسين (Hussain)", Role: models.RoleSuperAdmin, AvatarColor: "#d97706"}

func voidDose(t *testing.T, h *handlers.MedicationHandler, who *models.Caregiver, logID, reason string) (int, map[string]interface{}) {
	t.Helper()
	body, _ := json.Marshal(handlers.VoidDoseRequest{Reason: reason})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/medications/log/"+logID+"/void", bytes.NewReader(body))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", logID)
	r = r.WithContext(context.WithValue(authContext(r.Context(), who), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	h.VoidDose(rec, r)
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func aspirinToday() handlers.LogDoseRequest {
	return handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: clinicalDay(0)}
}

func mustLog(t *testing.T, h *handlers.MedicationHandler, who *models.Caregiver, req handlers.LogDoseRequest) string {
	t.Helper()
	code, out := logDose(t, h, who, req)
	if code != http.StatusCreated {
		t.Fatalf("log dose: %d %v", code, out)
	}
	return out["log_id"].(string)
}

func TestVoidThenRelogSameDose(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()

	id := mustLog(t, h, fatima, aspirinToday())
	if code, out := logDose(t, h, fatima, aspirinToday()); code != http.StatusConflict || out["error"] != "DOSE_ALREADY_LOGGED" {
		t.Fatalf("second tap before undo: want 409 DOSE_ALREADY_LOGGED, got %d %v", code, out)
	}
	if code, out := voidDose(t, h, fatima, id, ""); code != http.StatusOK {
		t.Fatalf("undo own dose: %d %v", code, out)
	}
	// The voided taken dose no longer counts for the interval lock either.
	mustLog(t, h, fatima, aspirinToday())

	var active, total int
	h.DB.QueryRow(`SELECT COUNT(*) FROM medication_logs WHERE medication_id='med_aspirin' AND voided_at IS NULL`).Scan(&active)
	h.DB.QueryRow(`SELECT COUNT(*) FROM medication_logs WHERE medication_id='med_aspirin'`).Scan(&total)
	if active != 1 || total != 2 {
		t.Fatalf("want 1 active + 1 voided row kept as history, got %d active / %d total", active, total)
	}
	if code, _ := voidDose(t, h, fatima, id, ""); code != http.StatusConflict {
		t.Fatalf("voiding twice: want 409, got %d", code)
	}

	// The schedule shows the active entry and the voided one as history.
	r := httptest.NewRequest(http.MethodGet, "/api/v1/medications/schedule?date="+clinicalDay(0), nil)
	rec := httptest.NewRecorder()
	h.GetSchedule(rec, r)
	var sched struct {
		Schedule []models.ScheduledDose `json:"schedule"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &sched)
	for _, d := range sched.Schedule {
		if d.Medication.ID == "med_aspirin" {
			if d.Log == nil || len(d.VoidedLogs) != 1 || d.VoidedLogs[0].VoidedByName != fatima.Name || d.VoidedLogs[0].VoidReason != "Undo" {
				t.Fatalf("schedule card: log=%+v voided=%+v", d.Log, d.VoidedLogs)
			}
		}
	}
}

func TestPartialIndexAllowsOnlyOneActiveRow(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	insert := func(id, voidedAt string) error {
		var v interface{}
		if voidedAt != "" {
			v = voidedAt
		}
		_, err := h.DB.Exec(`INSERT INTO medication_logs (id, medication_id, caregiver_id, slot, scheduled_date, status, voided_at)
			VALUES (?, 'med_aspirin', 'cg_fatima', 'morning', '2026-10-19', 'taken', ?)`, id, v)
		return err
	}
	if err := insert("v1", "2026-10-19 08:00:00"); err != nil {
		t.Fatal(err)
	}
	if err := insert("v2", "2026-10-19 08:01:00"); err != nil {
		t.Fatal("several voided rows for one dose must be allowed:", err)
	}
	if err := insert("a1", ""); err != nil {
		t.Fatal(err)
	}
	if err := insert("a2", ""); err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("two active rows for one dose must be impossible, got %v", err)
	}
}

func TestVoidPermissions(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	tappedAgo := func(id string, d time.Duration) {
		h.DB.Exec(`UPDATE medication_logs SET recorded_at = ? WHERE id = ?`, time.Now().UTC().Add(-d).Format("2006-01-02 15:04:05"), id)
	}

	id := mustLog(t, h, fatima, aspirinToday())
	tappedAgo(id, 9*time.Minute)
	if code, out := voidDose(t, h, fatima, id, ""); code != http.StatusOK {
		t.Fatalf("own dose at +9 min: want 200, got %d %v", code, out)
	}

	id = mustLog(t, h, fatima, aspirinToday())
	tappedAgo(id, 11*time.Minute)
	if code, out := voidDose(t, h, fatima, id, "wrong card"); code != http.StatusForbidden {
		t.Fatalf("own dose at +11 min: want 403, got %d %v", code, out)
	}
	if code, out := voidDose(t, h, ali, id, ""); code != http.StatusBadRequest || out["error"] != "REASON_REQUIRED" {
		t.Fatalf("admin without reason: want 400, got %d %v", code, out)
	}

	other := mustLog(t, h, ali, handlers.LogDoseRequest{MedicationID: "med_citicoline", Slot: "morning", ScheduledDate: clinicalDay(0)})
	if code, out := voidDose(t, h, fatima, other, "not mine"); code != http.StatusForbidden {
		t.Fatalf("voiding someone else's: want 403, got %d %v", code, out)
	}
	viewer := &models.Caregiver{ID: "cg_zainab", Name: "Zainab", Role: models.RoleViewer}
	if code, _ := voidDose(t, h, viewer, other, "x"); code != http.StatusForbidden {
		t.Fatalf("viewer: want 403, got %d", code)
	}
	if code, out := voidDose(t, h, ali, id, "wrong card, tapped aspirin instead of statin"); code != http.StatusOK {
		t.Fatalf("admin with reason: want 200, got %d %v", code, out)
	}
	if code, out := voidDose(t, h, hussain, other, "duplicate entry"); code != http.StatusOK {
		t.Fatalf("super admin voiding someone else's with reason: want 200, got %d %v", code, out)
	}
	var audits int
	h.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'MED_VOIDED'`).Scan(&audits)
	if audits != 3 {
		t.Fatalf("want 3 MED_VOIDED audit rows, got %d", audits)
	}
}

// The void and its audit row commit together: if the audit insert fails, nothing is voided.
func TestVoidIsAtomicWithAudit(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	id := mustLog(t, h, fatima, aspirinToday())
	if _, err := h.DB.Exec(`CREATE TRIGGER fail_void_audit BEFORE INSERT ON audit_logs
		WHEN NEW.action = 'MED_VOIDED' BEGIN SELECT RAISE(ABORT, 'audit down'); END`); err != nil {
		t.Fatal(err)
	}
	if code, _ := voidDose(t, h, fatima, id, ""); code != http.StatusInternalServerError {
		t.Fatalf("audit failure: want 500, got %d", code)
	}
	var voided int
	h.DB.QueryRow(`SELECT COUNT(*) FROM medication_logs WHERE id = ? AND voided_at IS NOT NULL`, id).Scan(&voided)
	if voided != 0 {
		t.Fatal("dose was voided although its audit row failed")
	}
}

// Boot refuses to start if the partial dose-lock index is removed or made non-partial.
func TestBootRefusesWithoutPartialDoseIndex(t *testing.T) {
	for name, tamper := range map[string]string{
		"removed":       `DROP INDEX uq_med_dose_active`,
		"non-partial":   `DROP INDEX uq_med_dose_active; CREATE UNIQUE INDEX uq_med_dose_active ON medication_logs(medication_id, scheduled_date, slot)`,
		"wrong partial": `DROP INDEX uq_med_dose_active; CREATE UNIQUE INDEX uq_med_dose_active ON medication_logs(medication_id, scheduled_date, slot) WHERE status = 'taken'`,
	} {
		t.Run(name, func(t *testing.T) {
			f, _ := os.CreateTemp(t.TempDir(), "boot-*.db")
			f.Close()
			database, err := db.InitDB(f.Name())
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			if err := db.Migrate(database); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(tamper); err != nil {
				t.Fatal(err)
			}
			if err := db.Migrate(database); err == nil || !strings.Contains(err.Error(), "SAFETY") {
				t.Fatalf("boot must be refused, got %v", err)
			}
		})
	}
}

// An existing database keeps its doses through the rebuild and gets the partial index.
func TestMigrateRebuildsLegacyDoseTable(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "legacy-*.db")
	f.Close()
	database, err := db.InitDB(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE medication_logs (id TEXT PRIMARY KEY, medication_id TEXT NOT NULL, caregiver_id TEXT NOT NULL, slot TEXT NOT NULL,
		scheduled_date TEXT NOT NULL, taken_at TEXT DEFAULT (datetime('now')), status TEXT NOT NULL CHECK (status IN ('taken', 'skipped')), notes TEXT,
		CONSTRAINT uq_med_dose UNIQUE (medication_id, scheduled_date, slot));
		INSERT INTO medication_logs VALUES ('old1', 'm', 'c', 'morning', '2026-10-19', '2026-10-19 08:00:00', 'taken', 'kept')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	var notes, recorded string
	if err := database.QueryRow(`SELECT notes, recorded_at FROM medication_logs WHERE id = 'old1' AND voided_at IS NULL`).Scan(&notes, &recorded); err != nil || notes != "kept" || recorded != "2026-10-19 08:00:00" {
		t.Fatalf("legacy dose lost in rebuild: %v %q %q", err, notes, recorded)
	}
	if err := db.Migrate(database); err != nil {
		t.Fatalf("second boot after rebuild: %v", err)
	}
}

// The doctor report counts a voided dose as not given.
func TestExportIgnoresVoidedDoses(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	id := mustLog(t, h, fatima, aspirinToday())
	if code, _ := voidDose(t, h, fatima, id, ""); code != http.StatusOK {
		t.Fatal("void failed")
	}
	mustLog(t, h, fatima, handlers.LogDoseRequest{MedicationID: "med_citicoline", Slot: "morning", ScheduledDate: clinicalDay(0)})

	exp := &handlers.ExportHandler{DB: h.DB}
	rec := httptest.NewRecorder()
	exp.ExportDoctorSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export/summary?days=7", nil))
	var out struct {
		Adherence struct {
			TotalScheduled int `json:"total_scheduled"`
			TotalTaken     int `json:"total_taken"`
		} `json:"adherence"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Adherence.TotalTaken != 1 || out.Adherence.TotalScheduled != 1 {
		t.Fatalf("voided dose counted in the doctor report: %+v (%s)", out.Adherence, rec.Body.String())
	}
}
