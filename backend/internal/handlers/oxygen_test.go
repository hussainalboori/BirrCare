package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
)

// Issue #22: SpO2 and temperature.

func TestOxygenTempSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../clinical/vitals-bands.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		SpO2Warn     int     `json:"spo2_warn"`
		SpO2Critical int     `json:"spo2_critical"`
		FeverC       float64 `json:"fever_c"`
		SpO2Cases    []struct {
			Value    int
			Expected string
		} `json:"spo2_cases"`
		TempCases []struct {
			Value    float64
			Expected string
		} `json:"temp_cases"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	handlers.SetOxygenFeverThresholds(handlers.OxygenFeverThresholds{SpO2Warn: f.SpO2Warn, SpO2Critical: f.SpO2Critical, FeverC: f.FeverC})
	for _, c := range f.SpO2Cases {
		v := c.Value
		if got := handlers.CalculateSpO2Status(&v); got != c.Expected {
			t.Errorf("SpO2 %d: want %s, got %s", c.Value, c.Expected, got)
		}
	}
	for _, c := range f.TempCases {
		v := c.Value
		if got := handlers.CalculateTempStatus(&v); got != c.Expected {
			t.Errorf("temp %.1f: want %s, got %s", c.Value, c.Expected, got)
		}
	}
}

func postVitalsCode(t *testing.T, h *handlers.VitalsHandler, body map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/vitals", bytes.NewReader(b))
	r = r.WithContext(authContext(r.Context(), &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}))
	rec := httptest.NewRecorder()
	h.LogVitals(rec, r)
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestSpO2AndTemperature(t *testing.T) {
	med, vitals, cleanup := setupTestDB(t)
	defer cleanup()
	handlers.SetOxygenFeverThresholds(handlers.OxygenFeverThresholds{SpO2Warn: 92, SpO2Critical: 88, FeverC: 38.0})
	count := func(q string) int {
		var n int
		if err := med.DB.QueryRow(q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if code, out := postVitalsCode(t, vitals, map[string]interface{}{"spo2": 101}); code != http.StatusBadRequest || out["error"] != "INVALID_SPO2" {
		t.Fatalf("spo2=101: want 400 INVALID_SPO2, got %d %v", code, out)
	}
	if code, out := postVitalsCode(t, vitals, map[string]interface{}{"temperature_c": 44}); code != http.StatusBadRequest || out["error"] != "INVALID_TEMPERATURE" {
		t.Fatalf("44 C: want 400, got %d %v", code, out)
	}

	code, out := postVitalsCode(t, vitals, map[string]interface{}{"spo2": 87, "pulse": 104})
	if code != http.StatusCreated || out["spo2_status"] != "CRITICAL" || out["vital_type"] != "spo2" {
		t.Fatalf("spo2=87: want 201 CRITICAL spo2, got %d %v", code, out)
	}
	if count(`SELECT COUNT(*) FROM alerts WHERE kind = 'spo2_low'`) != 1 {
		t.Fatal("spo2=87 must raise one spo2_low alert")
	}
	code, out = postVitalsCode(t, vitals, map[string]interface{}{"spo2": 91})
	if code != http.StatusCreated || out["spo2_status"] != "LOW" || count(`SELECT COUNT(*) FROM alerts WHERE kind = 'spo2_low'`) != 1 {
		t.Fatalf("spo2=91 is LOW (re-check), not an alert: %d %v", code, out)
	}
	code, out = postVitalsCode(t, vitals, map[string]interface{}{"temperature_c": 38.4})
	if code != http.StatusCreated || out["temp_status"] != "FEVER" || out["vital_type"] != "temp" {
		t.Fatalf("38.4 C: want FEVER temp, got %d %v", code, out)
	}

	// Listed with their status; the values can't be edited in place.
	rec := httptest.NewRecorder()
	vitals.ListVitals(rec, httptest.NewRequest(http.MethodGet, "/api/v1/vitals", nil))
	var list []models.VitalsLog
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	found := map[string]bool{}
	for _, v := range list {
		if v.SpO2 != nil {
			found["spo2-"+v.SpO2Status] = true
		}
		if v.TemperatureC != nil {
			found["temp-"+v.TempStatus] = true
		}
	}
	if !found["spo2-CRITICAL"] || !found["spo2-LOW"] || !found["temp-FEVER"] {
		t.Fatalf("list: %v", found)
	}
	if _, err := med.DB.Exec(`UPDATE vitals_logs SET spo2 = 99 WHERE spo2 = 87`); err == nil {
		t.Fatal("SpO2 was changed in place; it must go through a correction")
	}
	if _, err := med.DB.Exec(`INSERT INTO vitals_logs (id, caregiver_id, spo2, vital_type) VALUES ('x', 'cg_fatima', 101, 'spo2')`); err == nil {
		t.Fatal("the database accepted SpO2 101")
	}
}

func TestOxygenFeverThresholdsAreAdjustable(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	defer handlers.SetOxygenFeverThresholds(handlers.OxygenFeverThresholds{SpO2Warn: 92, SpO2Critical: 88, FeverC: 38.0})
	h := &handlers.ClinicalSettingsHandler{DB: med.DB}
	put := func(who *models.Caregiver, body string) int {
		r := httptest.NewRequest(http.MethodPut, "/api/v1/settings/clinical", bytes.NewBufferString(body))
		r = r.WithContext(authContext(r.Context(), who))
		rec := httptest.NewRecorder()
		h.Put(rec, r)
		return rec.Code
	}
	admin := &models.Caregiver{ID: "cg_ali", Role: models.RoleAdmin}
	if put(&models.Caregiver{ID: "cg_fatima", Role: models.RoleCaregiver}, `{"spo2_critical": 85}`) != http.StatusForbidden {
		t.Fatal("a caregiver changed thresholds")
	}
	if put(admin, `{"spo2_warn": 85, "spo2_critical": 88}`) != http.StatusBadRequest {
		t.Fatal("warning below critical was accepted")
	}
	// A COPD patient's doctor: alert only at 85, re-check at 88; fever at 37.8.
	if put(admin, `{"spo2_warn": 88, "spo2_critical": 85, "fever_c": 37.8}`) != http.StatusOK {
		t.Fatal("valid thresholds refused")
	}
	v, c := 87, 37.8
	if handlers.CalculateSpO2Status(&v) != "LOW" || handlers.CalculateTempStatus(&c) != "FEVER" {
		t.Fatal("new thresholds not applied")
	}
	handlers.LoadClinicalSettings(med.DB) // survives a restart
	if handlers.CalculateSpO2Status(&v) != "LOW" {
		t.Fatal("thresholds not persisted")
	}
	// The low-BP thresholds were left as they were.
	if got := handlers.CalculateBPStatus(85, 70); got != "LOW" {
		t.Fatalf("low BP thresholds changed: %s", got)
	}
}

func TestCorrectOxygenTypo(t *testing.T) {
	med, vitals, cleanup := setupTestDB(t)
	defer cleanup()
	handlers.SetOxygenFeverThresholds(handlers.OxygenFeverThresholds{SpO2Warn: 92, SpO2Critical: 88, FeverC: 38.0})
	fatima := &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}
	_, out := postVitalsCode(t, vitals, map[string]interface{}{"spo2": 78})
	id := out["id"].(string)
	code, _ := correct(t, vitals, fatima, id, map[string]interface{}{"vital_type": "spo2", "spo2": 97, "reason": "Typo"})
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("correct 78 -> 97: %d", code)
	}
	rec := httptest.NewRecorder()
	vitals.ListVitals(rec, httptest.NewRequest(http.MethodGet, "/api/v1/vitals", nil))
	var list []models.VitalsLog
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].SpO2 == nil || *list[0].SpO2 != 97 || list[0].Correction == nil || list[0].Correction.OriginalSpO2 == nil || *list[0].Correction.OriginalSpO2 != 78 {
		t.Fatalf("want the 97 shown with the original 78 kept: %+v", list)
	}
	var unacked int
	_ = med.DB.QueryRow(`SELECT COUNT(*) FROM alerts a WHERE a.kind = 'spo2_low' AND NOT EXISTS (SELECT 1 FROM alert_acks k WHERE k.alert_id = a.id)`).Scan(&unacked)
	if unacked != 0 {
		t.Fatal("the typo's low-oxygen alert still replays after the correction")
	}
}

func TestReportDailyOxygenTemp(t *testing.T) {
	med, vitals, cleanup := setupTestDB(t)
	defer cleanup()
	handlers.SetOxygenFeverThresholds(handlers.OxygenFeverThresholds{SpO2Warn: 92, SpO2Critical: 88, FeverC: 38.0})
	for _, b := range []map[string]interface{}{{"spo2": 95}, {"spo2": 87}, {"temperature_c": 37.2}, {"temperature_c": 38.6}} {
		if code, out := postVitalsCode(t, vitals, b); code != http.StatusCreated {
			t.Fatalf("%v: %d %v", b, code, out)
		}
	}
	rec := httptest.NewRecorder()
	(&handlers.ExportHandler{DB: med.DB}).ExportDoctorSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export/summary?days=7", nil))
	var out struct {
		Stats handlers.VitalsStats        `json:"vitals_stats"`
		Daily []handlers.DailyOxygenTemp `json:"daily_oxygen_temp"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Daily) != 1 || *out.Daily[0].MinSpO2 != 87 || *out.Daily[0].MaxTemperatureC != 38.6 || out.Daily[0].Readings != 4 {
		t.Fatalf("daily: %+v", out.Daily)
	}
	if *out.Stats.MinSpO2 != 87 || out.Stats.CriticalSpO2Count != 1 || out.Stats.LowSpO2Count != 1 || out.Stats.FeverCount != 1 {
		t.Fatalf("stats: %+v", out.Stats)
	}
}
