package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"birrcare/backend/internal/db"
	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
)

// Issue #10: nothing is recorded that nobody measured or assessed.

func postVitals(t *testing.T, h *handlers.VitalsHandler, body map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/vitals", bytes.NewReader(b))
	r = r.WithContext(authContext(r.Context(), fatima))
	rec := httptest.NewRecorder()
	h.LogVitals(rec, r)
	if rec.Code != http.StatusCreated {
		t.Fatalf("log vitals: %d %s", rec.Code, rec.Body.String())
	}
}

type summary struct {
	VitalsStats struct {
		AvgBloodSugar        *float64 `json:"avg_blood_sugar"`
		UnverifiedSugarCount int      `json:"unverified_sugar_count"`
	} `json:"vitals_stats"`
	Alertness    map[string]int     `json:"alertness_distribution"`
	RecentVitals []models.VitalsLog `json:"recent_vitals"`
}

func exportSummary(t *testing.T, h *handlers.ExportHandler) summary {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ExportDoctorSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export/summary?days=7", nil))
	var out summary
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("export: %v %s", err, rec.Body.String())
	}
	return out
}

func TestAlertnessNotAssessedIsNull(t *testing.T) {
	med, vitals, cleanup := setupTestDB(t)
	defer cleanup()

	// A "full" check with BP only: no sugar, no alertness tapped.
	postVitals(t, vitals, map[string]interface{}{"vital_type": "full", "systolic": 130, "diastolic": 85, "pulse": 72})
	var alertness, sugar interface{}
	med.DB.QueryRow(`SELECT alertness_level, blood_sugar FROM vitals_logs`).Scan(&alertness, &sugar)
	if alertness != nil || sugar != nil {
		t.Fatalf("not assessed / not measured must be NULL, got alertness=%v sugar=%v", alertness, sugar)
	}

	rec := httptest.NewRecorder()
	vitals.ListVitals(rec, httptest.NewRequest(http.MethodGet, "/api/v1/vitals", nil))
	var list []map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("list: %s", rec.Body.String())
	}
	if v, ok := list[0]["alertness_level"]; !ok || v != nil {
		t.Fatalf("API must send alertness_level: null, got %v (present=%v)", v, ok)
	}

	out := exportSummary(t, &handlers.ExportHandler{DB: med.DB})
	if out.Alertness["not_assessed"] != 1 || out.Alertness["alert"] != 0 {
		t.Fatalf("export distribution: %v", out.Alertness)
	}

	// An explicit tap is still recorded.
	postVitals(t, vitals, map[string]interface{}{"vital_type": "alertness", "alertness_level": "confused"})
	out = exportSummary(t, &handlers.ExportHandler{DB: med.DB})
	if out.Alertness["confused"] != 1 || out.Alertness["not_assessed"] != 1 {
		t.Fatalf("export distribution after a real assessment: %v", out.Alertness)
	}
}

// Pre-#10 rows: a full reading saved with 6.4 is flagged "unverified" (not rewritten),
// left out of the average; BP/sugar-only rows report alertness as not assessed.
func TestLegacyPrefilledSugarIsFlagged(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "legacy-vitals-*.db")
	f.Close()
	database, err := db.InitDB(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`
		CREATE TABLE vitals_logs (id TEXT PRIMARY KEY, caregiver_id TEXT NOT NULL, recorded_at TEXT DEFAULT (datetime('now')),
			systolic INTEGER, diastolic INTEGER, pulse INTEGER, blood_sugar REAL,
			alertness_level TEXT DEFAULT 'alert' CHECK (alertness_level IN ('alert', 'confused', 'restless', 'drowsy')),
			vital_type TEXT DEFAULT 'full', notes TEXT);
		INSERT INTO vitals_logs (id, caregiver_id, recorded_at, systolic, diastolic, pulse, blood_sugar, alertness_level, vital_type) VALUES
			('full64', 'cg_fatima', datetime('now', '-1 hour'), 130, 85, 70, 6.4, 'alert', 'full'),
			('full70', 'cg_fatima', datetime('now', '-2 hour'), 125, 80, 70, 7.0, 'alert', 'full'),
			('sugar64', 'cg_fatima', datetime('now', '-3 hour'), NULL, NULL, NULL, 6.4, 'alert', 'sugar'),
			('bp', 'cg_fatima', datetime('now', '-4 hour'), 140, 90, 75, NULL, 'alert', 'bp');
	`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO caregivers (id, name, role, pin_hash) VALUES ('cg_fatima', 'Fatima', 'caregiver', 'x')`); err != nil {
		t.Fatal(err)
	}

	flags := map[string]bool{}
	rows, _ := database.Query(`SELECT id, suspect_default_sugar, blood_sugar FROM vitals_logs`)
	for rows.Next() {
		var id string
		var flag bool
		var sugar interface{}
		rows.Scan(&id, &flag, &sugar)
		flags[id] = flag
		if id == "full64" && sugar.(float64) != 6.4 {
			t.Fatalf("history must be kept, not rewritten: %v", sugar)
		}
	}
	rows.Close()
	if !flags["full64"] || flags["full70"] || flags["sugar64"] || flags["bp"] {
		t.Fatalf("only full readings with 6.4 are flagged: %v", flags)
	}

	// A real 6.4 measured after the fix is never flagged.
	if _, err := database.Exec(`INSERT INTO vitals_logs (id, caregiver_id, systolic, diastolic, pulse, blood_sugar, vital_type) VALUES ('new64', 'cg_fatima', 120, 80, 70, 6.4, 'full')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	var newFlag bool
	database.QueryRow(`SELECT suspect_default_sugar FROM vitals_logs WHERE id = 'new64'`).Scan(&newFlag)
	if newFlag {
		t.Fatal("a 6.4 recorded after the migration must not be flagged")
	}

	out := exportSummary(t, &handlers.ExportHandler{DB: database})
	if out.VitalsStats.UnverifiedSugarCount != 1 {
		t.Fatalf("unverified count: %+v", out.VitalsStats)
	}
	// Average of 7.0, 6.4 (sugar-only, real) and the new 6.4; the flagged one is excluded.
	if out.VitalsStats.AvgBloodSugar == nil || *out.VitalsStats.AvgBloodSugar < 6.59 || *out.VitalsStats.AvgBloodSugar > 6.61 {
		t.Fatalf("average must leave out the flagged reading: %v", out.VitalsStats.AvgBloodSugar)
	}
	marked := false
	for _, v := range out.RecentVitals {
		if v.ID == "full64" && v.SuspectDefaultSugar {
			marked = true
		}
		if v.ID == "bp" && v.AlertnessLevel != nil {
			t.Fatalf("BP-only reading must report alertness as not assessed, got %v", *v.AlertnessLevel)
		}
	}
	if !marked {
		t.Fatal("export must mark the flagged 6.4 as unverified")
	}
	if out.Alertness["not_assessed"] != 3 || out.Alertness["alert"] != 2 {
		t.Fatalf("distribution: %v (legacy full rows keep their value; bp/sugar rows and the new reading are not assessed)", out.Alertness)
	}
}
