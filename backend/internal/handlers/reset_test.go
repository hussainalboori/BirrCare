package handlers_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
)

// "Start fresh": the Super Admin removes the records from the family's trial
// weeks; people, medicines, the roster ahead and upcoming visits stay.

func resetCall(h *handlers.ResetHandler, c *models.Caregiver, method, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/admin/reset", strings.NewReader(body))
	req = req.WithContext(authContext(req.Context(), c))
	rec := httptest.NewRecorder()
	if method == http.MethodGet {
		h.Preview(rec, req)
	} else {
		h.Reset(rec, req)
	}
	return rec
}

func countRows(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// seedTrial fills the database the way two weeks of trying the app would.
func seedTrial(t *testing.T, med *handlers.MedicationHandler) {
	t.Helper()
	if res := postDose(med, siblings[2], handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: today(), Status: "taken"}); res.code != http.StatusCreated {
		t.Fatalf("log dose: %d %s", res.code, res.raw)
	}
	now := time.Now().UTC()
	past := now.AddDate(0, 0, -3).Format(time.RFC3339)
	future := now.AddDate(0, 0, 5).Format(time.RFC3339)
	yesterday := time.Now().In(handlers.ClinicalLocation()).AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := time.Now().In(handlers.ClinicalLocation()).AddDate(0, 0, 1).Format("2006-01-02")
	for _, q := range []string{
		`INSERT INTO vitals_logs (id, caregiver_id, systolic, diastolic) VALUES ('v1', 'cg_fatima', 185, 95)`,
		`INSERT INTO vitals_logs (id, caregiver_id, systolic, diastolic, supersedes_id) VALUES ('v2', 'cg_fatima', 135, 85, 'v1')`,
		`UPDATE vitals_logs SET superseded_by = 'v2' WHERE id = 'v1'`,
		`INSERT INTO alerts (id, kind, payload) VALUES ('a1', 'bp_crisis', '{}')`,
		`INSERT INTO alert_acks (alert_id, caregiver_id) VALUES ('a1', 'cg_ali')`,
		`INSERT INTO handover_notes (id, caregiver_id, summary) VALUES ('n1', 'cg_fatima', 'test note')`,
		`INSERT INTO care_events (id, kind, value, caregiver_id) VALUES ('e1', 'fluid', '200', 'cg_fatima')`,
		`INSERT INTO shift_assignments (id, shift_id, caregiver_id, date) VALUES ('s_old', 'shift_24h', 'cg_fatima', '` + yesterday + `')`,
		`INSERT INTO shift_assignments (id, shift_id, caregiver_id, date) VALUES ('s_new', 'shift_24h', 'cg_ali', '` + tomorrow + `')`,
		`INSERT INTO appointments (id, kind, title, starts_at, created_by, created_at, updated_at) VALUES ('ap_old', 'doctor', 'Old visit', '` + past + `', 'cg_hussain', '` + past + `', '` + past + `')`,
		`INSERT INTO appointments (id, kind, title, starts_at, created_by, created_at, updated_at) VALUES ('ap_new', 'doctor', 'Neurologist', '` + future + `', 'cg_hussain', '` + past + `', '` + past + `')`,
		`INSERT INTO doctor_questions (id, text, created_by, created_at, appointment_id) VALUES ('q_open', 'Can she walk more?', 'cg_ali', '` + past + `', 'ap_old')`,
		`INSERT INTO doctor_questions (id, text, created_by, created_at, answered_at, answer) VALUES ('q_done', 'Old', 'cg_ali', '` + past + `', '` + past + `', 'yes')`,
		`UPDATE medications SET low_stock_alerted_at = '` + past + `'`,
	} {
		if _, err := med.DB.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func TestResetRemovesTrialRecordsAndKeepsSetup(t *testing.T) {
	_, med, dir := backupFixture(t)
	r := &handlers.ResetHandler{DB: med.DB, Dir: dir}
	seedTrial(t, med)
	triggersBefore := countRows(t, med.DB, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger'`)

	// Preview: what will go, nothing removed yet.
	rec := resetCall(r, siblings[0], http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
	}
	var preview struct {
		Counts map[string]int64 `json:"counts"`
	}
	json.NewDecoder(rec.Body).Decode(&preview)
	want := map[string]int64{"doses": 1, "readings": 2, "alerts": 1, "notes": 1, "care": 1, "shifts": 1, "visits": 2}
	for k, v := range want {
		if preview.Counts[k] != v {
			t.Errorf("preview %s = %d, want %d (%v)", k, preview.Counts[k], v, preview.Counts)
		}
	}

	// Without the confirmation word: refused.
	if rec := resetCall(r, siblings[0], http.MethodPost, `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("no confirm: %d", rec.Code)
	}
	if countRows(t, med.DB, `SELECT COUNT(*) FROM medication_logs`) != 1 {
		t.Fatal("a refused reset removed records")
	}

	rec = resetCall(r, siblings[0], http.MethodPost, `{"confirm":"start_fresh"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Copy string `json:"copy"`
	}
	json.NewDecoder(rec.Body).Decode(&res)

	for q, n := range map[string]int{
		`SELECT COUNT(*) FROM medication_logs`:                                    0,
		`SELECT COUNT(*) FROM vitals_logs`:                                        0,
		`SELECT COUNT(*) FROM alerts`:                                             0,
		`SELECT COUNT(*) FROM alert_acks`:                                         0,
		`SELECT COUNT(*) FROM handover_notes`:                                     0,
		`SELECT COUNT(*) FROM care_events`:                                        0,
		`SELECT COUNT(*) FROM stock_events`:                                       0,
		`SELECT COUNT(*) FROM medications WHERE low_stock_alerted_at IS NOT NULL`: 0,
		// Kept: people, medicines, the roster ahead, the next visit, open questions.
		`SELECT COUNT(*) FROM caregivers`:                                                      4,
		`SELECT COUNT(*) FROM medications`:                                                     2,
		`SELECT COUNT(*) FROM shift_assignments WHERE id = 's_new'`:                            1,
		`SELECT COUNT(*) FROM shift_assignments`:                                               1,
		`SELECT COUNT(*) FROM appointments WHERE id = 'ap_new'`:                                1,
		`SELECT COUNT(*) FROM appointments`:                                                    1,
		`SELECT COUNT(*) FROM doctor_questions WHERE id = 'q_open' AND appointment_id IS NULL`: 1,
		`SELECT COUNT(*) FROM doctor_questions`:                                                1,
		`SELECT COUNT(*) FROM audit_logs WHERE action = 'DATA_RESET'`:                          1,
	} {
		if got := countRows(t, med.DB, q); got != n {
			t.Errorf("%s = %d, want %d", q, got, n)
		}
	}

	// The "never delete" protections are back.
	if got := countRows(t, med.DB, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger'`); got != triggersBefore {
		t.Fatalf("triggers after reset = %d, want %d", got, triggersBefore)
	}
	if res := postDose(med, siblings[2], handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: today(), Status: "taken"}); res.code != http.StatusCreated {
		t.Fatalf("log dose after reset: %d %s", res.code, res.raw)
	}
	if _, err := med.DB.Exec(`DELETE FROM medication_logs`); err == nil || !strings.Contains(err.Error(), "cannot be deleted") {
		t.Fatalf("medication logs can be deleted after a reset: %v", err)
	}

	// The copy taken first still has everything.
	copyPath := filepath.Join(filepath.Dir(dir), res.Copy)
	if _, err := os.Stat(copyPath); err != nil {
		t.Fatalf("no copy at %s: %v", copyPath, err)
	}
	cp, err := sql.Open("sqlite", "file:"+copyPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer cp.Close()
	if n := countRows(t, cp, `SELECT COUNT(*) FROM medication_logs`) + countRows(t, cp, `SELECT COUNT(*) FROM vitals_logs`); n != 3 {
		t.Fatalf("copy has %d doses+readings, want 3", n)
	}
}

// A second reset keeps the first copy (the one with the trial records).
func TestResetNeverOverwritesAnEarlierCopy(t *testing.T) {
	_, med, dir := backupFixture(t)
	r := &handlers.ResetHandler{DB: med.DB, Dir: dir}
	seedTrial(t, med)
	for i := 0; i < 2; i++ {
		if rec := resetCall(r, siblings[0], http.MethodPost, `{"confirm":"start_fresh"}`); rec.Code != http.StatusOK {
			t.Fatalf("reset %d: %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	copies, _ := filepath.Glob(filepath.Join(dir, "before-reset", "*.db"))
	if len(copies) != 2 {
		t.Fatalf("copies = %v, want 2", copies)
	}
	found := false
	for _, c := range copies {
		cp, err := sql.Open("sqlite", "file:"+c+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		if countRows(t, cp, `SELECT COUNT(*) FROM medication_logs`) == 1 {
			found = true
		}
		cp.Close()
	}
	if !found {
		t.Fatal("the copy with the trial records was overwritten")
	}
}

func TestResetIsSuperAdminOnly(t *testing.T) {
	_, med, dir := backupFixture(t)
	r := &handlers.ResetHandler{DB: med.DB, Dir: dir}
	seedTrial(t, med)
	for _, c := range siblings[1:] {
		for _, m := range []string{http.MethodGet, http.MethodPost} {
			if rec := resetCall(r, c, m, `{"confirm":"start_fresh"}`); rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s: %d, want 403", c.Role, m, rec.Code)
			}
		}
	}
	if countRows(t, med.DB, `SELECT COUNT(*) FROM medication_logs`) != 1 {
		t.Fatal("a forbidden reset removed records")
	}
}
