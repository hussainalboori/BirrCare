package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"

	"github.com/go-chi/chi/v5"
)

// Issue #15: audited corrections; the original stays as it was.

func correct(t *testing.T, h *handlers.VitalsHandler, who *models.Caregiver, id string, body map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/vitals/"+id+"/correct", bytes.NewReader(b))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", id)
	r = r.WithContext(context.WithValue(authContext(r.Context(), who), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	h.CorrectVitals(rec, r)
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func rowSnapshot(t *testing.T, h *handlers.MedicationHandler, id string) string {
	t.Helper()
	var snap string
	if err := h.DB.QueryRow(`SELECT json_array(id, caregiver_id, recorded_at, systolic, diastolic, pulse, blood_sugar, alertness_level, vital_type, notes, supersedes_id, correction_reason)
		FROM vitals_logs WHERE id = ?`, id).Scan(&snap); err != nil {
		t.Fatal(err)
	}
	return snap
}

func exportCrisisCount(t *testing.T, h *handlers.MedicationHandler) (int, []models.VitalsLog) {
	t.Helper()
	rec := httptest.NewRecorder()
	(&handlers.ExportHandler{DB: h.DB}).ExportDoctorSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export/summary?days=7", nil))
	var out struct {
		VitalsStats struct {
			CrisisCount int `json:"crisis_count"`
		} `json:"vitals_stats"`
		RecentVitals []models.VitalsLog `json:"recent_vitals"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out.VitalsStats.CrisisCount, out.RecentVitals
}

func TestCorrectionSupersedesWithoutChangingTheOriginal(t *testing.T) {
	med, vitals, cleanup := setupTestDB(t)
	defer cleanup()
	postVitals(t, vitals, map[string]interface{}{"vital_type": "bp", "systolic": 190, "diastolic": 100, "pulse": 80})
	var origID string
	med.DB.QueryRow(`SELECT id FROM vitals_logs WHERE systolic = 190`).Scan(&origID)
	before := rowSnapshot(t, med, origID)
	if n, _ := exportCrisisCount(t, med); n != 1 {
		t.Fatalf("setup: want 1 crisis, got %d", n)
	}
	if k := alertKinds(t, med); k["bp_crisis"] != 1 {
		t.Fatalf("setup: crisis alert expected, got %v", k)
	}

	code, out := correct(t, vitals, fatima, origID, map[string]interface{}{"systolic": 119, "diastolic": 80, "pulse": 80, "reason": "Typo"})
	if code != http.StatusCreated {
		t.Fatalf("correct own reading: %d %v", code, out)
	}
	newID := out["id"].(string)

	if after := rowSnapshot(t, med, origID); after != before {
		t.Fatalf("original changed:\n%s\n%s", before, after)
	}
	var supersededBy string
	med.DB.QueryRow(`SELECT superseded_by FROM vitals_logs WHERE id = ?`, origID).Scan(&supersededBy)
	if supersededBy != newID {
		t.Fatalf("original must point to the correction, got %q", supersededBy)
	}

	n, recent := exportCrisisCount(t, med)
	if n != 0 {
		t.Fatalf("export crisis_count must drop to 0, got %d", n)
	}
	var shown *models.VitalsLog
	for i := range recent {
		if recent[i].ID == origID {
			t.Fatal("the superseded reading must not be listed on its own")
		}
		if recent[i].ID == newID {
			shown = &recent[i]
		}
	}
	if shown == nil || shown.Correction == nil || *shown.Correction.OriginalSystolic != 190 || shown.Correction.Reason != "Typo" || shown.Correction.OriginalLoggedByName != fatima.Name {
		t.Fatalf("correction must carry the original 190/100: %+v", shown)
	}

	// The typo's crisis alert stops replaying; everyone gets a correction alert.
	if k := alertKinds(t, med); k["bp_crisis"] != 0 || k["correction"] != 1 {
		t.Fatalf("want the crisis retired and one correction alert, got %v", k)
	}
	if code, _ := correct(t, vitals, fatima, origID, map[string]interface{}{"systolic": 118, "diastolic": 80, "reason": "Typo"}); code != http.StatusConflict {
		t.Fatalf("correcting an already corrected reading: want 409, got %d", code)
	}
	var audits int
	med.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'VITAL_CORRECTED' AND details LIKE '%190/100 to 119/80%'`).Scan(&audits)
	if audits != 1 {
		t.Fatalf("want an audit row for the correction, got %d", audits)
	}
}

func TestCorrectionPermissions(t *testing.T) {
	med, vitals, cleanup := setupTestDB(t)
	defer cleanup()
	insert := func(id, who string, age time.Duration, vitalType string) {
		at := time.Now().UTC().Add(-age).Format("2006-01-02 15:04:05")
		if _, err := med.DB.Exec(`INSERT INTO vitals_logs (id, caregiver_id, recorded_at, systolic, diastolic, vital_type) VALUES (?, ?, ?, 130, 85, ?)`, id, who, at, vitalType); err != nil {
			t.Fatal(err)
		}
	}
	insert("alis", "cg_ali", time.Hour, "bp")
	insert("old", "cg_fatima", 25*time.Hour, "bp")
	insert("mine", "cg_fatima", time.Hour, "bp")
	insert("fast", "cg_fatima", time.Hour, "fast")
	fix := map[string]interface{}{"systolic": 125, "diastolic": 82, "reason": "Device error"}

	if code, _ := correct(t, vitals, fatima, "alis", fix); code != http.StatusForbidden {
		t.Fatalf("caregiver correcting someone else's reading: want 403, got %d", code)
	}
	if code, _ := correct(t, vitals, fatima, "old", fix); code != http.StatusForbidden {
		t.Fatalf("own reading after 24 h: want 403, got %d", code)
	}
	if code, _ := correct(t, vitals, &models.Caregiver{ID: "cg_zainab", Role: models.RoleViewer}, "mine", fix); code != http.StatusForbidden {
		t.Fatalf("viewer: want 403, got %d", code)
	}
	if code, _ := correct(t, vitals, fatima, "mine", map[string]interface{}{"systolic": 125, "diastolic": 82}); code != http.StatusBadRequest {
		t.Fatalf("no reason: want 400, got %d", code)
	}
	if code, _ := correct(t, vitals, fatima, "mine", map[string]interface{}{"systolic": 400, "diastolic": 82, "reason": "Typo"}); code != http.StatusBadRequest {
		t.Fatalf("out-of-range correction: want 400, got %d", code)
	}
	if code, _ := correct(t, vitals, fatima, "fast", fix); code != http.StatusBadRequest {
		t.Fatalf("F.A.S.T. correction: want 400, got %d", code)
	}
	if code, _ := correct(t, vitals, fatima, "mine", fix); code != http.StatusCreated {
		t.Fatalf("own reading within 24 h: want 201, got %d", code)
	}
	if code, _ := correct(t, vitals, ali, "old", fix); code != http.StatusCreated {
		t.Fatalf("admin, any time: want 201, got %d", code)
	}
}

func TestVitalsValuesAreImmutable(t *testing.T) {
	med, vitals, cleanup := setupTestDB(t)
	defer cleanup()
	postVitals(t, vitals, map[string]interface{}{"vital_type": "bp", "systolic": 190, "diastolic": 100})
	var id string
	med.DB.QueryRow(`SELECT id FROM vitals_logs`).Scan(&id)
	for _, q := range []string{
		`UPDATE vitals_logs SET systolic = 119 WHERE id = ?`,
		`UPDATE vitals_logs SET diastolic = 80 WHERE id = ?`,
		`UPDATE vitals_logs SET recorded_at = datetime('now', '-1 day') WHERE id = ?`,
		`UPDATE vitals_logs SET alertness_level = 'alert' WHERE id = ?`,
	} {
		if _, err := med.DB.Exec(q, id); err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("%s: want the immutability trigger to refuse, got %v", q, err)
		}
	}
	if _, err := med.DB.Exec(`UPDATE vitals_logs SET superseded_by = 'x' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := med.DB.Exec(`UPDATE vitals_logs SET superseded_by = 'y' WHERE id = ?`, id); err == nil {
		t.Fatal("a reading must not be superseded twice")
	}
	if _, err := med.DB.Exec(`UPDATE vitals_logs SET last_normal_at = datetime('now') WHERE id = ?`, id); err != nil {
		t.Fatalf("non-measurement fields (last seen normal) stay updatable: %v", err)
	}
}
