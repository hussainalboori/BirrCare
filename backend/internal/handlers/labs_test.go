package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
)

// Issue #62: INR results with the doctor's range.

func postLab(t *testing.T, h *handlers.LabHandler, who *models.Caregiver, body string) (int, handlers.LabResult) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r = r.WithContext(authContext(r.Context(), who))
	rec := httptest.NewRecorder()
	h.Create(rec, r)
	var out handlers.LabResult
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestINROutOfRangeAlertsAdmins(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	handlers.SetINRTarget(handlers.INRTarget{Low: 2.0, High: 3.0})
	h := &handlers.LabHandler{DB: med.DB, Broker: med.Broker}

	code, res := postLab(t, h, siblings[2], `{"kind":"inr","value":4.1,"next_test_on":"`+clinicalDay(7)+`"}`)
	if code != http.StatusCreated || !res.OutOfRange || res.TakenOn != clinicalDay(0) || res.NextTestOn != clinicalDay(7) {
		t.Fatalf("INR 4.1, target 2-3: %d %+v", code, res)
	}
	rows, _ := med.DB.Query(`SELECT target_caregiver_id FROM alerts WHERE kind = 'inr_out_of_range' ORDER BY target_caregiver_id`)
	var targets []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		targets = append(targets, id)
	}
	rows.Close()
	if strings.Join(targets, ",") != "cg_ali,cg_hussain" {
		t.Fatalf("want one alert per admin (Ali, Hussain), got %v", targets)
	}
	if _, ok := postLab(t, h, siblings[2], `{"kind":"inr","value":2.6}`); ok.OutOfRange {
		t.Fatal("INR 2.6 is in range")
	}
	var n int
	med.DB.QueryRow(`SELECT COUNT(*) FROM alerts WHERE kind = 'inr_out_of_range'`).Scan(&n)
	if n != 2 {
		t.Fatalf("an in-range INR raises nothing, got %d alerts", n)
	}
	for _, bad := range []string{`{"kind":"inr","value":12}`, `{"kind":"ldl","value":2}`, `{"kind":"inr","value":2.5,"taken_on":"` + clinicalDay(1) + `"}`, `{"kind":"other","value":5}`} {
		if code, _ := postLab(t, h, siblings[2], bad); code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", bad, code)
		}
	}
	if code, _ := postLab(t, h, &models.Caregiver{ID: "cg_zainab", Role: models.RoleViewer}, `{"kind":"inr","value":2.5}`); code != http.StatusForbidden {
		t.Fatalf("viewer: want 403, got %d", code)
	}

	// The doctor report shows the INR history of the period.
	rec := httptest.NewRecorder()
	(&handlers.ExportHandler{DB: med.DB}).ExportDoctorSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export/summary?days=14", nil))
	var out struct {
		LabResults []handlers.LabResult `json:"lab_results"`
		INRTarget  handlers.INRTarget   `json:"inr_target"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.LabResults) != 2 || out.INRTarget.High != 3.0 {
		t.Fatalf("report INR history: %+v", out)
	}
}

func TestINRTargetSetting(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	defer handlers.SetINRTarget(handlers.INRTarget{Low: 2.0, High: 3.0})
	h := &handlers.ClinicalSettingsHandler{DB: med.DB}
	admin := &models.Caregiver{ID: "cg_ali", Role: models.RoleAdmin}
	for _, bad := range []string{`{"inr_target_low": 3.5, "inr_target_high": 3.0}`, `{"inr_target_high": 5}`} {
		if rec := settingsPut(h, admin, bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", bad, rec.Code)
		}
	}
	if rec := settingsPut(h, admin, `{"inr_target_low": 2.5, "inr_target_high": 3.5}`); rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	handlers.SetINRTarget(handlers.INRTarget{Low: 2, High: 3})
	handlers.LoadClinicalSettings(med.DB)
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), `"inr_target_low":2.5`) || !strings.Contains(rec.Body.String(), `"inr_target_high":3.5`) {
		t.Fatalf("INR range should persist: %s", rec.Body.String())
	}
}

func TestDoseByDayThroughTheAPI(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	code, out := createMedication(t, h, map[string]interface{}{"name_ar": "وارفارين", "name_en": "Warfarin", "dosage": "5 mg", "frequency_slots": []string{"evening"},
		"dose_by_day": map[string]string{"sat": "2.5 mg", "SUN": " 2.5 mg ", "mon": ""}})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, out)
	}
	var stored string
	h.DB.QueryRow(`SELECT dose_by_day FROM medications WHERE id = ?`, out["id"]).Scan(&stored)
	if stored != `{"sat":"2.5 mg","sun":"2.5 mg"}` {
		t.Fatalf("stored weekday doses: %s", stored)
	}
	if code, out := createMedication(t, h, map[string]interface{}{"name_ar": "x", "name_en": "X", "dosage": "1", "frequency_slots": []string{"morning"}, "dose_by_day": map[string]string{"funday": "2"}}); code != http.StatusBadRequest {
		t.Fatalf("bad weekday: want 400, got %d %v", code, out)
	}
	// Clearing them from tomorrow keeps today's history.
	if rec := medCall(t, h.UpdateMedication, siblings[1], out["id"].(string), map[string]interface{}{"dose_by_day": map[string]string{}, "effective_from": "tomorrow"}); rec.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body.String())
	}
	h.DB.QueryRow(`SELECT dose_by_day FROM medications WHERE id = ?`, out["id"]).Scan(&stored)
	if stored != "" {
		t.Fatalf("cleared: %q", stored)
	}
	var versions int
	h.DB.QueryRow(`SELECT COUNT(*) FROM medication_schedule_versions WHERE medication_id = ?`, out["id"]).Scan(&versions)
	if versions != 2 {
		t.Fatalf("a dose change from tomorrow opens a new version, got %d", versions)
	}
}
