package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"

	"github.com/go-chi/chi/v5"
)

// Issue #13: F.A.S.T. check, low BP band, escalated confusion / hard to wake.

func alertKinds(t *testing.T, h *handlers.MedicationHandler) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, a := range unacked(t, &handlers.AlertsHandler{DB: h.DB}, hussain) {
		out[a.Kind]++
	}
	return out
}

func TestFastPositiveRaisesAlertNegativeDoesNot(t *testing.T) {
	med, vitals, cleanup := setupTestDB(t)
	defer cleanup()

	postVitals(t, vitals, map[string]interface{}{"vital_type": "fast", "fast_result": map[string]bool{"face": false, "arm": false, "speech": false}})
	if k := alertKinds(t, med); len(k) != 0 {
		t.Fatalf("all-no F.A.S.T. must not alert, got %v", k)
	}
	postVitals(t, vitals, map[string]interface{}{"vital_type": "fast", "fast_result": map[string]bool{"face": false, "arm": true, "speech": false}})
	if k := alertKinds(t, med); k["fast_positive"] != 1 {
		t.Fatalf("arm weakness must raise a fast_positive alert, got %v", k)
	}

	var id, stored string
	med.DB.QueryRow(`SELECT id, fast_result FROM vitals_logs WHERE vital_type = 'fast' AND fast_result LIKE '%"arm":true%'`).Scan(&id, &stored)
	if id == "" {
		t.Fatalf("positive F.A.S.T. not stored: %q", stored)
	}

	// Last seen normal, for the paramedics.
	setLast := func(who *models.Caregiver, when string) int {
		b, _ := json.Marshal(map[string]string{"last_normal_at": when})
		r := httptest.NewRequest(http.MethodPut, "/api/v1/vitals/"+id+"/last-normal", bytes.NewReader(b))
		rc := chi.NewRouteContext()
		rc.URLParams.Add("id", id)
		r = r.WithContext(context.WithValue(authContext(r.Context(), who), chi.RouteCtxKey, rc))
		rec := httptest.NewRecorder()
		vitals.SetLastNormal(rec, r)
		return rec.Code
	}
	at := time.Now().Add(-40 * time.Minute).UTC().Truncate(time.Second)
	if code := setLast(fatima, at.Format(time.RFC3339)); code != http.StatusOK {
		t.Fatalf("set last normal: %d", code)
	}
	if code := setLast(fatima, time.Now().Add(time.Hour).UTC().Format(time.RFC3339)); code != http.StatusBadRequest {
		t.Fatalf("future time: want 400, got %d", code)
	}
	if code := setLast(&models.Caregiver{ID: "cg_zainab", Role: models.RoleViewer}, at.Format(time.RFC3339)); code != http.StatusForbidden {
		t.Fatalf("viewer: want 403, got %d", code)
	}
	rec := httptest.NewRecorder()
	vitals.ListVitals(rec, httptest.NewRequest(http.MethodGet, "/api/v1/vitals", nil))
	var list []models.VitalsLog
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	found := false
	for _, v := range list {
		if v.ID == id && v.LastNormalAt == at.Format(time.RFC3339) && len(v.FastResult) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("list must carry fast_result and last_normal_at: %s", rec.Body.String())
	}

	// A F.A.S.T. entry needs its answers.
	b, _ := json.Marshal(map[string]interface{}{"vital_type": "fast"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/vitals", bytes.NewReader(b))
	r = r.WithContext(authContext(r.Context(), fatima))
	rec = httptest.NewRecorder()
	vitals.LogVitals(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("fast without result: want 400, got %d", rec.Code)
	}

	exp := &handlers.ExportHandler{DB: med.DB}
	rec = httptest.NewRecorder()
	exp.ExportDoctorSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export/summary?days=7", nil))
	var s struct {
		VitalsStats struct {
			FastPositiveCount int `json:"fast_positive_count"`
		} `json:"vitals_stats"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &s)
	if s.VitalsStats.FastPositiveCount != 1 {
		t.Fatalf("export fast_positive_count: %s", rec.Body.String())
	}
}

func TestLowBPAndConfusionRaiseAlerts(t *testing.T) {
	med, vitals, cleanup := setupTestDB(t)
	defer cleanup()
	handlers.SetBPLowThresholds(handlers.BPLowThresholds{LowSystolic: 90, LowDiastolic: 60})

	postVitals(t, vitals, map[string]interface{}{"vital_type": "bp", "systolic": 82, "diastolic": 48})
	postVitals(t, vitals, map[string]interface{}{"vital_type": "alertness", "alertness_level": "drowsy"})
	postVitals(t, vitals, map[string]interface{}{"vital_type": "alertness", "alertness_level": "confused"})
	postVitals(t, vitals, map[string]interface{}{"vital_type": "alertness", "alertness_level": "restless"})
	postVitals(t, vitals, map[string]interface{}{"vital_type": "bp", "systolic": 119, "diastolic": 79})

	k := alertKinds(t, med)
	if k["bp_low"] != 1 || k["alertness"] != 2 || len(k) != 2 {
		t.Fatalf("want bp_low:1 and alertness:2 (drowsy, confused; not restless), got %v", k)
	}
	rec := httptest.NewRecorder()
	vitals.ListVitals(rec, httptest.NewRequest(http.MethodGet, "/api/v1/vitals", nil))
	var list []models.VitalsLog
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	for _, v := range list {
		if v.Systolic != nil && *v.Systolic == 82 && v.BPStatus != "LOW" {
			t.Fatalf("82/48 must be LOW, got %s", v.BPStatus)
		}
	}
}

func TestClinicalThresholdsAdminOnly(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	h := &handlers.ClinicalSettingsHandler{DB: med.DB}
	defer handlers.SetBPLowThresholds(handlers.BPLowThresholds{LowSystolic: 90, LowDiastolic: 60})
	put := func(who *models.Caregiver, body map[string]int) int {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPut, "/api/v1/settings/clinical", bytes.NewReader(b))
		r = r.WithContext(authContext(r.Context(), who))
		rec := httptest.NewRecorder()
		h.Put(rec, r)
		return rec.Code
	}
	if code := put(fatima, map[string]int{"bp_low_systolic": 100, "bp_low_diastolic": 60}); code != http.StatusForbidden {
		t.Fatalf("caregiver: want 403, got %d", code)
	}
	if code := put(ali, map[string]int{"bp_low_systolic": 30, "bp_low_diastolic": 60}); code != http.StatusBadRequest {
		t.Fatalf("out of range: want 400, got %d", code)
	}
	if code := put(ali, map[string]int{"bp_low_systolic": 100, "bp_low_diastolic": 60}); code != http.StatusOK {
		t.Fatalf("admin: want 200, got %d", code)
	}
	if got := handlers.CalculateBPStatus(95, 70); got != "LOW" {
		t.Fatalf("doctor's threshold must apply immediately: %s", got)
	}
	handlers.SetBPLowThresholds(handlers.BPLowThresholds{LowSystolic: 90, LowDiastolic: 60})
	handlers.LoadClinicalSettings(med.DB)
	if got := handlers.CalculateBPStatus(95, 70); got != "LOW" {
		t.Fatalf("doctor's threshold must survive a restart: %s", got)
	}
}
