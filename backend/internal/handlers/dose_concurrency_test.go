package handlers_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"birrcare/backend/internal/db"
	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
)

var siblings = []*models.Caregiver{
	{ID: "cg_hussain", Name: "حسين (Hussain)", Role: models.RoleSuperAdmin, AvatarColor: "#d97706"},
	{ID: "cg_ali", Name: "علي (Ali)", Role: models.RoleAdmin, AvatarColor: "#2563eb"},
	{ID: "cg_fatima", Name: "فاطمة (Fatima)", Role: models.RoleCaregiver, AvatarColor: "#059669"},
}

type doseResult struct {
	code int
	body map[string]interface{}
	raw  string
}

func postDose(h *handlers.MedicationHandler, c *models.Caregiver, req handlers.LogDoseRequest) doseResult {
	b, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/medications/log", bytes.NewReader(b))
	r = r.WithContext(authContext(r.Context(), c))
	rec := httptest.NewRecorder()
	h.LogDose(rec, r)
	res := doseResult{code: rec.Code, raw: rec.Body.String()}
	_ = json.Unmarshal(rec.Body.Bytes(), &res.body)
	return res
}

func today() string { return time.Now().In(handlers.ClinicalLocation()).Format("2006-01-02") }

// fireConcurrentDoses releases N goroutines at once against the given handlers
// (round-robin) all logging the SAME dose.
func fireConcurrentDoses(t *testing.T, hs []*handlers.MedicationHandler, n int) []doseResult {
	t.Helper()
	req := handlers.LogDoseRequest{MedicationID: "med_citicoline", Slot: "evening", ScheduledDate: today(), Status: "taken"}
	results := make([]doseResult, n)
	var start, wg sync.WaitGroup
	start.Add(1)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start.Wait()
			results[i] = postDose(hs[i%len(hs)], siblings[i%len(siblings)], req)
		}(i)
	}
	start.Done()
	wg.Wait()
	return results
}

func assertExactlyOneWinner(t *testing.T, database *sql.DB, results []doseResult) {
	t.Helper()
	var winners []doseResult
	conflicts := 0
	for _, r := range results {
		if strings.Contains(strings.ToLower(r.raw), "locked") || strings.Contains(strings.ToLower(r.raw), "busy") {
			t.Fatalf("got a SQLite lock/busy error: %d %s", r.code, r.raw)
		}
		switch r.code {
		case http.StatusCreated:
			winners = append(winners, r)
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("unexpected status %d: %s", r.code, r.raw)
		}
	}
	if len(winners) != 1 || conflicts != len(results)-1 {
		t.Fatalf("expected exactly 1 success and %d conflicts, got %d successes and %d conflicts", len(results)-1, len(winners), conflicts)
	}

	winnerName := winners[0].body["caregiver_name"]
	for _, r := range results {
		if r.code != http.StatusConflict {
			continue
		}
		if r.body["error"] != "DOSE_ALREADY_LOGGED" {
			t.Fatalf("409 without DOSE_ALREADY_LOGGED: %s", r.raw)
		}
		if r.body["caregiver_name"] != winnerName || r.body["caregiver_id"] == "" {
			t.Fatalf("409 does not identify the giver (want %v): %s", winnerName, r.raw)
		}
		if _, err := time.Parse(time.RFC3339, r.body["taken_at"].(string)); err != nil {
			t.Fatalf("409 taken_at is not RFC3339: %v (%s)", r.body["taken_at"], r.raw)
		}
		if r.body["log_id"] != winners[0].body["log_id"] {
			t.Fatalf("409 log_id mismatch: %s", r.raw)
		}
	}

	var rows, audits int
	database.QueryRow(`SELECT COUNT(*) FROM medication_logs WHERE medication_id='med_citicoline' AND slot='evening'`).Scan(&rows)
	database.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='MED_LOGGED'`).Scan(&audits)
	if rows != 1 || audits != 1 {
		t.Fatalf("expected 1 dose row and 1 audit row, got %d and %d", rows, audits)
	}
}

func TestConcurrentDoubleDoseSinglePool(t *testing.T) {
	medHandler, _, cleanup := setupTestDB(t)
	defer cleanup()

	results := fireConcurrentDoses(t, []*handlers.MedicationHandler{medHandler}, 40)
	assertExactlyOneWinner(t, medHandler.DB, results)
}

// Two independent *sql.DB pools on the same WAL file (as with a second process)
// exercise busy_timeout + _txlock=immediate: still exactly one winner, no "database is locked".
func TestConcurrentDoubleDoseTwoPoolsWAL(t *testing.T) {
	medHandler, _, cleanup := setupTestDB(t)
	defer cleanup()

	var path string
	if err := medHandler.DB.QueryRow("SELECT file FROM pragma_database_list WHERE name='main'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	second, err := db.InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	other := &handlers.MedicationHandler{DB: second, Broker: medHandler.Broker}

	results := fireConcurrentDoses(t, []*handlers.MedicationHandler{medHandler, other}, 40)
	assertExactlyOneWinner(t, medHandler.DB, results)
}

func TestLogDoseValidation(t *testing.T) {
	medHandler, _, cleanup := setupTestDB(t)
	defer cleanup()
	c := siblings[2]
	d := today()

	cases := []struct {
		name string
		req  handlers.LogDoseRequest
		want int
	}{
		{"unknown status is not coerced to taken", handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: d, Status: "skip"}, http.StatusBadRequest},
		{"non-canonical date rejected", handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: d + "T00:00:00Z", Status: "taken"}, http.StatusBadRequest},
		{"garbage date rejected", handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: "19/10/2026", Status: "taken"}, http.StatusBadRequest},
		{"far future date rejected", handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: time.Now().AddDate(0, 0, 5).Format("2006-01-02"), Status: "taken"}, http.StatusBadRequest},
		{"slot not prescribed", handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "evening", ScheduledDate: d, Status: "taken"}, http.StatusBadRequest},
		{"unknown medication is 404 not 409", handlers.LogDoseRequest{MedicationID: "med_nope", Slot: "morning", ScheduledDate: d, Status: "taken"}, http.StatusNotFound},
		{"valid", handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: d, Status: ""}, http.StatusCreated},
		{"duplicate", handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: d, Status: "skipped"}, http.StatusConflict},
	}
	for _, tc := range cases {
		res := postDose(medHandler, c, tc.req)
		if res.code != tc.want {
			t.Errorf("%s: want %d, got %d: %s", tc.name, tc.want, res.code, res.raw)
		}
	}

	var status string
	medHandler.DB.QueryRow(`SELECT status FROM medication_logs WHERE medication_id='med_aspirin'`).Scan(&status)
	if status != "taken" {
		t.Fatalf("empty status should default to taken, got %q", status)
	}
}

func TestMigrateIdempotentKeepsDoseConstraint(t *testing.T) {
	medHandler, _, cleanup := setupTestDB(t)
	defer cleanup()
	if err := db.Migrate(medHandler.DB); err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}
	var supers int
	medHandler.DB.QueryRow(`SELECT COUNT(*) FROM caregivers WHERE role='super_admin'`).Scan(&supers)
	if supers != 1 {
		t.Fatalf("expected exactly 1 super_admin, got %d", supers)
	}
}

func TestMigrateRefusesDBWithoutDoseConstraint(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "legacy-*.db")
	f.Close()
	database, err := db.InitDB(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// A legacy table without the UNIQUE constraint that already holds a double dose:
	// the rebuild cannot add the lock, so boot must be refused.
	if _, err := database.Exec(`CREATE TABLE medication_logs (id TEXT PRIMARY KEY, medication_id TEXT, caregiver_id TEXT, slot TEXT, scheduled_date TEXT, taken_at TEXT, status TEXT, notes TEXT);
		INSERT INTO medication_logs VALUES ('a', 'm', 'c', 'morning', '2026-10-19', '2026-10-19 08:00:00', 'taken', ''),
		                                   ('b', 'm', 'c', 'morning', '2026-10-19', '2026-10-19 08:01:00', 'taken', '')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(database); err == nil || !strings.Contains(err.Error(), "double-dose") {
		t.Fatalf("expected migrate to refuse a schema without the UNIQUE dose constraint, got %v", err)
	}
}

func TestGetScheduleRejectsBadDate(t *testing.T) {
	medHandler, _, cleanup := setupTestDB(t)
	defer cleanup()
	rec := httptest.NewRecorder()
	medHandler.GetSchedule(rec, httptest.NewRequest(http.MethodGet, "/api/v1/medications/schedule?date=2026-1-1", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}
