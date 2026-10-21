package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"

	"github.com/go-chi/chi/v5"
)

func createPRNMedication(t *testing.T, h *handlers.MedicationHandler, id string, max24h int, minHours float64) {
	t.Helper()
	_, err := h.DB.Exec(`
		INSERT INTO medications (id, name_ar, name_en, dosage, instructions_ar, instructions_en, frequency_slots, is_prn, prn_max_per_24h, prn_min_hours, created_by)
		VALUES (?, 'باراسيتامول', 'Paracetamol', '500mg', 'عند الحاجة', 'As needed', '[]', 1, ?, ?, 'cg_hussain')
	`, id, max24h, minHours)
	if err != nil {
		t.Fatalf("failed inserting PRN medication: %v", err)
	}
}

type prnPostResult struct {
	code int
	body map[string]interface{}
	raw  string
}

func postPRNDose(h *handlers.MedicationHandler, c *models.Caregiver, req handlers.LogPRNDoseRequest) prnPostResult {
	b, _ := json.Marshal(req)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/medications/prn/log", bytes.NewReader(b))
	r = r.WithContext(authContext(r.Context(), c))
	rec := httptest.NewRecorder()
	h.LogPRNDose(rec, r)
	res := prnPostResult{code: rec.Code, raw: rec.Body.String()}
	_ = json.Unmarshal(rec.Body.Bytes(), &res.body)
	return res
}

// TestPRNLimitsAndIntervals covers Acceptance Criteria 1:
// Max 4/24 h -> 5th within 24 h -> 409 PRN_LIMIT
// Min 4 h -> second at +2 h -> 409 PRN_TOO_SOON
func TestPRNLimitsAndIntervals(t *testing.T) {
	medHandler, _, cleanup := setupTestDB(t)
	defer cleanup()

	createPRNMedication(t, medHandler, "med_prn_paracetamol", 4, 4.0)
	c := siblings[0] // Hussain (Super Admin)
	siblingCaregiver := siblings[2] // Fatima (Caregiver)

	now := time.Now().UTC()

	// 1. Give 1st dose at T-10h
	t1 := now.Add(-10 * time.Hour).Format(time.RFC3339)
	res1 := postPRNDose(medHandler, siblingCaregiver, handlers.LogPRNDoseRequest{
		MedicationID: "med_prn_paracetamol",
		ReasonTag:    "headache",
		GivenAt:      t1,
		Notes:        "Mild headache",
	})
	if res1.code != http.StatusCreated {
		t.Fatalf("dose 1: expected 201, got %d: %s", res1.code, res1.raw)
	}

	// 2. Try 2nd dose at T-8h (+2h after dose 1, with min 4h interval required) -> should return 409 PRN_TOO_SOON
	t2TooSoon := now.Add(-8 * time.Hour).Format(time.RFC3339)
	res2TooSoon := postPRNDose(medHandler, siblingCaregiver, handlers.LogPRNDoseRequest{
		MedicationID: "med_prn_paracetamol",
		ReasonTag:    "headache",
		GivenAt:      t2TooSoon,
	})
	if res2TooSoon.code != http.StatusConflict {
		t.Fatalf("dose 2 too soon: expected 409 Conflict, got %d: %s", res2TooSoon.code, res2TooSoon.raw)
	}
	if res2TooSoon.body["error"] != "PRN_TOO_SOON" {
		t.Fatalf("dose 2 too soon: expected error PRN_TOO_SOON, got %v", res2TooSoon.body["error"])
	}
	if res2TooSoon.body["caregiver_name"] != siblingCaregiver.Name {
		t.Fatalf("dose 2 too soon: expected caregiver_name %v, got %v", siblingCaregiver.Name, res2TooSoon.body["caregiver_name"])
	}

	// 3. Give 2nd dose at T-5.5h (4.5h after dose 1 -> allowed)
	t2 := now.Add(-5*time.Hour - 30*time.Minute).Format(time.RFC3339)
	res2 := postPRNDose(medHandler, siblingCaregiver, handlers.LogPRNDoseRequest{
		MedicationID: "med_prn_paracetamol",
		ReasonTag:    "pain",
		GivenAt:      t2,
	})
	if res2.code != http.StatusCreated {
		t.Fatalf("dose 2 valid: expected 201, got %d: %s", res2.code, res2.raw)
	}

	// 4. Give 3rd dose at T-1h (4.5h after dose 2 -> allowed)
	t3 := now.Add(-1 * time.Hour).Format(time.RFC3339)
	res3 := postPRNDose(medHandler, c, handlers.LogPRNDoseRequest{
		MedicationID: "med_prn_paracetamol",
		ReasonTag:    "fever",
		GivenAt:      t3,
	})
	if res3.code != http.StatusCreated {
		t.Fatalf("dose 3 valid: expected 201, got %d: %s", res3.code, res3.raw)
	}

	// 5. Override interval for dose 4 (admin override with reason at T-0.5h) -> allowed
	t4 := now.Add(-30 * time.Minute).Format(time.RFC3339)
	res4 := postPRNDose(medHandler, c, handlers.LogPRNDoseRequest{
		MedicationID:     "med_prn_paracetamol",
		ReasonTag:        "pain",
		GivenAt:          t4,
		OverrideInterval: true,
		OverrideReason:   "Severe breakthrough post-stroke pain per doctor phone order",
	})
	if res4.code != http.StatusCreated {
		t.Fatalf("dose 4 override: expected 201, got %d: %s", res4.code, res4.raw)
	}

	// Now 4 doses have been administered within the rolling 24 hours (max is 4).
	// 6. 5th dose attempt -> must return 409 PRN_LIMIT
	t5 := now.Format(time.RFC3339)
	res5 := postPRNDose(medHandler, siblingCaregiver, handlers.LogPRNDoseRequest{
		MedicationID: "med_prn_paracetamol",
		ReasonTag:    "fever",
		GivenAt:      t5,
	})
	if res5.code != http.StatusConflict {
		t.Fatalf("dose 5: expected 409 Conflict, got %d: %s", res5.code, res5.raw)
	}
	if res5.body["error"] != "PRN_LIMIT" {
		t.Fatalf("dose 5: expected error PRN_LIMIT, got %v", res5.body["error"])
	}
	if count, ok := res5.body["count_last_24h"].(float64); !ok || int(count) != 4 {
		t.Fatalf("dose 5: expected count_last_24h 4, got %v", res5.body["count_last_24h"])
	}
	if res5.body["caregiver_name"] != c.Name {
		t.Fatalf("dose 5: expected last caregiver to be identified (%v), got %v", c.Name, res5.body["caregiver_name"])
	}
	if res5.body["next_allowed_at"] == nil || res5.body["next_allowed_at"] == "" {
		t.Fatalf("dose 5: expected next_allowed_at timestamp to be provided")
	}

	// 7. Verify status endpoint reflects 4 doses, lock reason "limit_reached", and can_give false
	recGet := httptest.NewRecorder()
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/medications/prn", nil)
	reqGet = reqGet.WithContext(authContext(reqGet.Context(), c))
	medHandler.GetPRNMedications(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET /medications/prn failed: %d: %s", recGet.Code, recGet.Body.String())
	}
	var statuses []models.PRNMedicationStatus
	if err := json.Unmarshal(recGet.Body.Bytes(), &statuses); err != nil {
		t.Fatalf("failed decoding PRN statuses: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 PRN med, got %d", len(statuses))
	}
	st := statuses[0]
	if st.CountLast24h != 4 {
		t.Fatalf("expected count_last_24h 4, got %d", st.CountLast24h)
	}
	if st.CanGive != false {
		t.Fatalf("expected can_give to be false")
	}
	if st.LockReason != "limit_reached" {
		t.Fatalf("expected lock_reason 'limit_reached', got %s", st.LockReason)
	}
}

// TestPRNConcurrencyCap covers Acceptance Criteria 2:
// 10 concurrent PRN logs -> never exceeds the cap.
func TestPRNConcurrencyCap(t *testing.T) {
	medHandler, _, cleanup := setupTestDB(t)
	defer cleanup()

	// Limit to max 3 doses per 24 hours, with 0 min interval to test strictly the 24h cap
	createPRNMedication(t, medHandler, "med_prn_paracetamol_cap", 3, 0)

	const totalAttempts = 10
	results := make([]prnPostResult, totalAttempts)

	var start, wg sync.WaitGroup
	start.Add(1)

	for i := 0; i < totalAttempts; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			start.Wait()
			cg := siblings[idx%len(siblings)]
			results[idx] = postPRNDose(medHandler, cg, handlers.LogPRNDoseRequest{
				MedicationID: "med_prn_paracetamol_cap",
				ReasonTag:    "pain",
				Notes:        "Concurrent test",
			})
		}(i)
	}

	start.Done()
	wg.Wait()

	successes := 0
	conflicts := 0
	for _, res := range results {
		if res.code == http.StatusCreated {
			successes++
		} else if res.code == http.StatusConflict && res.body["error"] == "PRN_LIMIT" {
			conflicts++
		} else {
			t.Errorf("unexpected concurrent result: code %d, body %s", res.code, res.raw)
		}
	}

	if successes != 3 {
		t.Fatalf("expected exactly 3 successes (cap=3), got %d successes and %d conflicts", successes, conflicts)
	}
	if conflicts != 7 {
		t.Fatalf("expected exactly 7 conflicts (total=10, cap=3), got %d", conflicts)
	}

	// Verify database row count
	var rowCount int
	err := medHandler.DB.QueryRow(`
		SELECT COUNT(*) FROM prn_logs
		WHERE medication_id = 'med_prn_paracetamol_cap' AND voided_at IS NULL
	`).Scan(&rowCount)
	if err != nil {
		t.Fatalf("failed querying row count: %v", err)
	}
	if rowCount != 3 {
		t.Fatalf("expected exactly 3 rows in database, got %d", rowCount)
	}
}

// TestDoctorExportPRNLists covers Acceptance Criteria 3:
// Doctor export lists PRN use per day with reasons.
func TestDoctorExportPRNLists(t *testing.T) {
	medHandler, _, cleanup := setupTestDB(t)
	defer cleanup()

	createPRNMedication(t, medHandler, "med_prn_paracetamol", 4, 1.0)
	exportHandler := &handlers.ExportHandler{DB: medHandler.DB}

	c := siblings[0]

	now := time.Now().UTC()
	// Log PRN doses with different reasons
	doses := []struct {
		reason string
		delta  time.Duration
	}{
		{"headache", -36 * time.Hour}, // 2 days ago
		{"pain", -20 * time.Hour},     // yesterday
		{"fever", -2 * time.Hour},      // today
	}

	for _, d := range doses {
		tStr := now.Add(d.delta).Format(time.RFC3339)
		res := postPRNDose(medHandler, c, handlers.LogPRNDoseRequest{
			MedicationID: "med_prn_paracetamol",
			ReasonTag:    d.reason,
			GivenAt:      tStr,
			Notes:        "Dose for " + d.reason,
		})
		if res.code != http.StatusCreated {
			t.Fatalf("failed logging PRN dose for export test: %d: %s", res.code, res.raw)
		}
	}

	// Call export summary
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/export/summary?days=7", nil)
	req = req.WithContext(authContext(req.Context(), c))
	exportHandler.ExportDoctorSummary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("export doctor summary failed: %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		PRNLogs []models.PRNExportLog `json:"prn_logs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed decoding doctor summary: %v", err)
	}

	if len(resp.PRNLogs) != 3 {
		t.Fatalf("expected 3 PRN logs in export summary, got %d", len(resp.PRNLogs))
	}

	reasonsFound := map[string]bool{}
	for _, log := range resp.PRNLogs {
		if log.MedicationID != "med_prn_paracetamol" {
			t.Errorf("unexpected medication_id in PRN log: %s", log.MedicationID)
		}
		if log.NameAr != "باراسيتامول" || log.NameEn != "Paracetamol" {
			t.Errorf("unexpected bilingual name in PRN log: %s / %s", log.NameAr, log.NameEn)
		}
		if log.Date == "" {
			t.Errorf("missing date in PRN log")
		}
		if log.CaregiverName != c.Name {
			t.Errorf("expected caregiver name %s, got %s", c.Name, log.CaregiverName)
		}
		reasonsFound[log.ReasonTag] = true
	}

	for _, want := range []string{"headache", "pain", "fever"} {
		if !reasonsFound[want] {
			t.Errorf("missing PRN reason %q in doctor summary", want)
		}
	}
}

// TestPRNVoidRestoresCapacity verifies mis-tap voiding restores 24h cap.
func TestPRNVoidRestoresCapacity(t *testing.T) {
	medHandler, _, cleanup := setupTestDB(t)
	defer cleanup()

	createPRNMedication(t, medHandler, "med_prn_void_test", 1, 0)
	c := siblings[0]

	// 1. Give only allowed dose
	res1 := postPRNDose(medHandler, c, handlers.LogPRNDoseRequest{
		MedicationID: "med_prn_void_test",
		ReasonTag:    "headache",
	})
	if res1.code != http.StatusCreated {
		t.Fatalf("dose 1 failed: %d: %s", res1.code, res1.raw)
	}
	logID := res1.body["id"].(string)

	// 2. Next dose blocked
	res2 := postPRNDose(medHandler, c, handlers.LogPRNDoseRequest{
		MedicationID: "med_prn_void_test",
		ReasonTag:    "headache",
	})
	if res2.code != http.StatusConflict {
		t.Fatalf("expected 409 limit, got %d", res2.code)
	}

	// 3. Void first dose
	voidBody, _ := json.Marshal(handlers.VoidPRNDoseRequest{Reason: "Wrong patient button"})
	rVoid := httptest.NewRequest(http.MethodPost, "/api/v1/medications/prn/log/"+logID+"/void", bytes.NewReader(voidBody))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", logID)
	rVoid = rVoid.WithContext(context.WithValue(authContext(rVoid.Context(), c), chi.RouteCtxKey, rc))
	recVoid := httptest.NewRecorder()
	medHandler.VoidPRNDose(recVoid, rVoid)

	if recVoid.Code != http.StatusOK {
		t.Fatalf("void failed: %d: %s", recVoid.Code, recVoid.Body.String())
	}

	// 4. Now dose can be logged again!
	res3 := postPRNDose(medHandler, c, handlers.LogPRNDoseRequest{
		MedicationID: "med_prn_void_test",
		ReasonTag:    "pain",
	})
	if res3.code != http.StatusCreated {
		t.Fatalf("dose after void failed: %d: %s", res3.code, res3.raw)
	}
}
