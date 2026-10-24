package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"github.com/go-chi/chi/v5"
)

var (
	insAdmin     = &models.Caregiver{ID: "cg_ali", Name: "Ali", Role: models.RoleAdmin}
	insCaregiver = &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}
	insViewer    = &models.Caregiver{ID: "cg_zainab", Name: "Zainab", Role: models.RoleViewer}
)

// Example correction scale (test data only, not clinical advice).
var testScale = []handlers.ScaleRow{
	{Min: 0, Max: 8, Units: 0},
	{Min: 8, Max: 11, Units: 2},
	{Min: 11, Max: 14, Units: 4},
	{Min: 14, Max: 17, Units: 6},
}

func f(v float64) *float64 { return &v }

func setupInsulin(t *testing.T) (*handlers.InsulinHandler, string) {
	t.Helper()
	med, _, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	h := &handlers.InsulinHandler{DB: med.DB, Broker: med.Broker}

	rec := call(h.CreatePlan, insAdmin, handlers.SavePlanRequest{
		NameAr: "نوفورابيد", NameEn: "NovoRapid", Kind: "scale", Scale: testScale,
		MinHoursBetween: 4, PrescribedBy: "Dr. Test",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create plan: %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]string
	json.Unmarshal(rec.Body.Bytes(), &out)
	return h, out["id"]
}

func call(fn http.HandlerFunc, who *models.Caregiver, body interface{}) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(b))
	req = req.WithContext(authContext(req.Context(), who))
	rec := httptest.NewRecorder()
	fn(rec, req)
	return rec
}

func errCode(rec *httptest.ResponseRecorder) string {
	var m map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &m)
	s, _ := m["error"].(string)
	return s
}

func TestLookupScale(t *testing.T) {
	cases := []struct {
		sugar float64
		want  *float64
	}{
		{5.0, f(0)}, {7.99, f(0)}, {8.0, f(2)}, {12.4, f(4)}, {16.9, f(6)}, {17.0, nil}, {25, nil},
	}
	for _, c := range cases {
		got := handlers.LookupScale(testScale, c.sugar)
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("sugar %.2f: got %v want %v", c.sugar, got, c.want)
		}
	}
}

func TestInsulinPlanValidation(t *testing.T) {
	h, _ := setupInsulin(t)
	bad := []handlers.SavePlanRequest{
		{NameEn: "gap", Kind: "scale", Scale: []handlers.ScaleRow{{0, 8, 0}, {9, 12, 2}}},
		{NameEn: "overlap", Kind: "scale", Scale: []handlers.ScaleRow{{0, 8, 0}, {7, 12, 2}}},
		{NameEn: "quarter units", Kind: "scale", Scale: []handlers.ScaleRow{{0, 8, 0.25}}},
		{NameEn: "empty", Kind: "scale"},
		{NameEn: "fixed zero", Kind: "fixed", FixedUnits: f(0)},
	}
	for _, p := range bad {
		if rec := call(h.CreatePlan, insAdmin, p); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", p.NameEn, rec.Code)
		}
	}
	if rec := call(h.CreatePlan, insCaregiver, handlers.SavePlanRequest{NameEn: "x", Kind: "fixed", FixedUnits: f(10)}); rec.Code != http.StatusForbidden {
		t.Errorf("caregiver must not edit the doctor's plan, got %d", rec.Code)
	}
}

func TestInsulinSafetyRules(t *testing.T) {
	h, plan := setupInsulin(t)

	// Viewer cannot log.
	if rec := call(h.LogInsulin, insViewer, handlers.LogInsulinRequest{PlanID: plan, Status: "given", Units: 4, BloodSugar: f(12)}); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer: got %d", rec.Code)
	}
	// Scale plan without a reading.
	if rec := call(h.LogInsulin, insCaregiver, handlers.LogInsulinRequest{PlanID: plan, Status: "given", Units: 4}); errCode(rec) != "SUGAR_REQUIRED" {
		t.Fatalf("missing sugar: %d %s", rec.Code, rec.Body.String())
	}
	// Hypo: correction insulin refused even with a reason.
	if rec := call(h.LogInsulin, insCaregiver, handlers.LogInsulinRequest{PlanID: plan, Status: "given", Units: 2, BloodSugar: f(3.2), Reason: "x"}); errCode(rec) != "HYPO_NO_INSULIN" {
		t.Fatalf("hypo: %d %s", rec.Code, rec.Body.String())
	}
	// Dose different from the scale needs a reason.
	if rec := call(h.LogInsulin, insCaregiver, handlers.LogInsulinRequest{PlanID: plan, Status: "given", Units: 6, BloodSugar: f(12)}); errCode(rec) != "REASON_REQUIRED" {
		t.Fatalf("deviation: %d %s", rec.Code, rec.Body.String())
	}
	// Above the scale: any dose needs a reason (doctor must be called).
	if rec := call(h.LogInsulin, insCaregiver, handlers.LogInsulinRequest{PlanID: plan, Status: "given", Units: 8, BloodSugar: f(20)}); errCode(rec) != "REASON_REQUIRED" {
		t.Fatalf("outside scale: %d %s", rec.Code, rec.Body.String())
	}
	// Holding a dose never locks the next one.
	if rec := call(h.LogInsulin, insCaregiver, handlers.LogInsulinRequest{PlanID: plan, Status: "held", BloodSugar: f(6.5)}); rec.Code != http.StatusCreated {
		t.Fatalf("held: %d %s", rec.Code, rec.Body.String())
	}
	// Scale dose: accepted.
	if rec := call(h.LogInsulin, insCaregiver, handlers.LogInsulinRequest{PlanID: plan, Status: "given", Units: 4, BloodSugar: f(12.4)}); rec.Code != http.StatusCreated {
		t.Fatalf("scale dose: %d %s", rec.Code, rec.Body.String())
	}
	// Second injection within the interval: locked, with who gave the first.
	rec := call(h.LogInsulin, insCaregiver, handlers.LogInsulinRequest{PlanID: plan, Status: "given", Units: 4, BloodSugar: f(12.1)})
	if rec.Code != http.StatusConflict || errCode(rec) != "INSULIN_TOO_SOON" {
		t.Fatalf("stacking lock: %d %s", rec.Code, rec.Body.String())
	}
	var conflict struct {
		Last handlers.InsulinLog `json:"last"`
	}
	json.Unmarshal(rec.Body.Bytes(), &conflict)
	if conflict.Last.CaregiverName != "فاطمة (Fatima)" || conflict.Last.Units != 4 {
		t.Fatalf("conflict should say who gave what: %+v", conflict.Last)
	}
	// Only an admin may override, and only with a reason.
	if rec := call(h.LogInsulin, insCaregiver, handlers.LogInsulinRequest{PlanID: plan, Status: "given", Units: 4, BloodSugar: f(12.1), OverrideInterval: true, Reason: "doctor"}); rec.Code != http.StatusForbidden {
		t.Fatalf("caregiver override: %d", rec.Code)
	}
	if rec := call(h.LogInsulin, insAdmin, handlers.LogInsulinRequest{PlanID: plan, Status: "given", Units: 4, BloodSugar: f(12.1), OverrideInterval: true}); errCode(rec) != "REASON_REQUIRED" {
		t.Fatalf("override without reason: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(h.LogInsulin, insAdmin, handlers.LogInsulinRequest{PlanID: plan, Status: "given", Units: 4, BloodSugar: f(12.1), OverrideInterval: true, Reason: "Doctor said by phone"}); rec.Code != http.StatusCreated {
		t.Fatalf("admin override: %d %s", rec.Code, rec.Body.String())
	}

	// The reading taken with each injection lands in vitals history too.
	var n int
	h.DB.QueryRow("SELECT COUNT(*) FROM vitals_logs WHERE vital_type = 'sugar'").Scan(&n)
	if n != 3 {
		t.Fatalf("expected 3 sugar readings in vitals, got %d", n)
	}
}

// Two siblings tapping "give" at the same moment: exactly one injection is recorded.
func TestInsulinConcurrentTaps(t *testing.T) {
	h, plan := setupInsulin(t)
	const taps = 8
	codes := make(chan int, taps)
	var wg sync.WaitGroup
	for i := 0; i < taps; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- call(h.LogInsulin, insCaregiver, handlers.LogInsulinRequest{PlanID: plan, Status: "given", Units: 4, BloodSugar: f(12)}).Code
		}()
	}
	wg.Wait()
	close(codes)
	created := 0
	for c := range codes {
		if c == http.StatusCreated {
			created++
		} else if c != http.StatusConflict {
			t.Fatalf("unexpected status %d", c)
		}
	}
	var n int
	h.DB.QueryRow("SELECT COUNT(*) FROM insulin_logs WHERE status = 'given'").Scan(&n)
	if created != 1 || n != 1 {
		t.Fatalf("expected exactly one injection, got %d responses / %d rows", created, n)
	}
}

func TestDeleteInsulinPlan(t *testing.T) {
	h, planID := setupInsulin(t)

	// 1. Caregiver cannot delete plan (403)
	rCaregiver := chi.NewRouteContext()
	rCaregiver.URLParams.Add("id", planID)
	reqCaregiver := httptest.NewRequest(http.MethodDelete, "/api/v1/insulin/plans/"+planID, nil)
	reqCaregiver = reqCaregiver.WithContext(context.WithValue(authContext(reqCaregiver.Context(), insCaregiver), chi.RouteCtxKey, rCaregiver))
	recCaregiver := httptest.NewRecorder()
	h.DeletePlan(recCaregiver, reqCaregiver)
	if recCaregiver.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for caregiver, got %d", recCaregiver.Code)
	}

	// 2. Admin can delete plan without logs -> hard deleted
	recCreated := call(h.CreatePlan, insAdmin, handlers.SavePlanRequest{
		NameAr: "لانتوس", NameEn: "Lantus", Kind: "fixed", FixedUnits: f(10),
		MinHoursBetween: 12,
	})
	if recCreated.Code != http.StatusCreated {
		t.Fatalf("create second plan: %d", recCreated.Code)
	}
	var out map[string]string
	json.Unmarshal(recCreated.Body.Bytes(), &out)
	unusedPlanID := out["id"]

	rAdmin := chi.NewRouteContext()
	rAdmin.URLParams.Add("id", unusedPlanID)
	reqAdmin := httptest.NewRequest(http.MethodDelete, "/api/v1/insulin/plans/"+unusedPlanID, nil)
	reqAdmin = reqAdmin.WithContext(context.WithValue(authContext(reqAdmin.Context(), insAdmin), chi.RouteCtxKey, rAdmin))
	recAdmin := httptest.NewRecorder()
	h.DeletePlan(recAdmin, reqAdmin)
	if recAdmin.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for admin deleting unused plan, got %d: %s", recAdmin.Code, recAdmin.Body.String())
	}

	var count int
	h.DB.QueryRow("SELECT COUNT(*) FROM insulin_plans WHERE id = ?", unusedPlanID).Scan(&count)
	if count != 0 {
		t.Fatalf("expected unused plan to be completely deleted, got count=%d", count)
	}

	// 3. Admin deleting plan WITH logs -> soft deleted (is_active = 0)
	recLog := call(h.LogInsulin, insCaregiver, handlers.LogInsulinRequest{
		PlanID: planID, Status: "given", Units: 4, BloodSugar: f(12),
	})
	if recLog.Code != http.StatusCreated {
		t.Fatalf("expected 201 for log, got %d", recLog.Code)
	}

	rAdmin2 := chi.NewRouteContext()
	rAdmin2.URLParams.Add("id", planID)
	reqAdmin2 := httptest.NewRequest(http.MethodDelete, "/api/v1/insulin/plans/"+planID, nil)
	reqAdmin2 = reqAdmin2.WithContext(context.WithValue(authContext(reqAdmin2.Context(), insAdmin), chi.RouteCtxKey, rAdmin2))
	recAdmin2 := httptest.NewRecorder()
	h.DeletePlan(recAdmin2, reqAdmin2)
	if recAdmin2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for admin deleting plan with logs, got %d: %s", recAdmin2.Code, recAdmin2.Body.String())
	}

	var isActive int
	err := h.DB.QueryRow("SELECT is_active FROM insulin_plans WHERE id = ?", planID).Scan(&isActive)
	if err != nil {
		t.Fatalf("expected plan with logs to still exist in DB, got err: %v", err)
	}
	if isActive != 0 {
		t.Fatalf("expected plan with logs to have is_active=0, got %d", isActive)
	}
}

func voidInsulin(t *testing.T, h *handlers.InsulinHandler, who *models.Caregiver, logID, reason string) (int, map[string]interface{}) {
	t.Helper()
	body, _ := json.Marshal(handlers.VoidDoseRequest{Reason: reason})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/insulin/log/"+logID+"/void", bytes.NewReader(body))
	r := chi.NewRouteContext()
	r.URLParams.Add("id", logID)
	req = req.WithContext(context.WithValue(authContext(req.Context(), who), chi.RouteCtxKey, r))
	rec := httptest.NewRecorder()
	h.VoidInsulinLog(rec, req)
	var out map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestVoidInsulinLog(t *testing.T) {
	h, planID := setupInsulin(t)

	// 1. Log a dose
	recLog := call(h.LogInsulin, insCaregiver, handlers.LogInsulinRequest{
		PlanID: planID, Status: "given", Units: 4, BloodSugar: f(12),
	})
	if recLog.Code != http.StatusCreated {
		t.Fatalf("expected 201 for log, got %d", recLog.Code)
	}
	var logOut map[string]interface{}
	json.Unmarshal(recLog.Body.Bytes(), &logOut)
	logID := logOut["log_id"].(string)

	// Status shows last_given and next_allowed_at is locked
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/v1/insulin/status", nil)
	recStatus := httptest.NewRecorder()
	h.GetStatus(recStatus, reqStatus)
	var statuses []handlers.PlanStatus
	json.Unmarshal(recStatus.Body.Bytes(), &statuses)
	if len(statuses) != 1 || statuses[0].LastGiven == nil || statuses[0].NextAllowedAt == "" {
		t.Fatalf("expected status with last_given and next_allowed_at, got: %+v", statuses)
	}

	// 2. Viewer cannot void
	if code, _ := voidInsulin(t, h, insViewer, logID, ""); code != http.StatusForbidden {
		t.Fatalf("viewer void: want 403, got %d", code)
	}

	// 3. Different caregiver cannot void someone else's log
	otherCaregiver := &models.Caregiver{ID: "cg_other", Name: "Other", Role: models.RoleCaregiver}
	if code, _ := voidInsulin(t, h, otherCaregiver, logID, ""); code != http.StatusForbidden {
		t.Fatalf("other caregiver void: want 403, got %d", code)
	}

	// 4. Admin voiding someone else's log requires a reason
	if code, out := voidInsulin(t, h, insAdmin, logID, ""); code != http.StatusBadRequest || out["error"] != "REASON_REQUIRED" {
		t.Fatalf("admin void without reason: want 400 REASON_REQUIRED, got %d %+v", code, out)
	}

	// 5. Original caregiver can undo within 10 minutes (own undo)
	if code, out := voidInsulin(t, h, insCaregiver, logID, ""); code != http.StatusOK {
		t.Fatalf("caregiver own undo: want 200, got %d %+v", code, out)
	}

	// 6. Voiding again returns 409 ALREADY_VOIDED
	if code, _ := voidInsulin(t, h, insCaregiver, logID, ""); code != http.StatusConflict {
		t.Fatalf("voiding twice: want 409, got %d", code)
	}

	// 7. Status now reflects that the plan is unlocked
	recStatus2 := httptest.NewRecorder()
	h.GetStatus(recStatus2, reqStatus)
	var statuses2 []handlers.PlanStatus
	json.Unmarshal(recStatus2.Body.Bytes(), &statuses2)
	if statuses2[0].LastGiven != nil || statuses2[0].NextAllowedAt != "" {
		t.Fatalf("expected plan to be unlocked after void, got: %+v", statuses2[0])
	}

	// 8. Caregiver can log a new dose now that the mis-tap was voided
	recLog2 := call(h.LogInsulin, insCaregiver, handlers.LogInsulinRequest{
		PlanID: planID, Status: "given", Units: 4, BloodSugar: f(12),
	})
	if recLog2.Code != http.StatusCreated {
		t.Fatalf("expected 201 for second log after void, got %d: %s", recLog2.Code, recLog2.Body.String())
	}

	// 9. Check audit trail
	var auditCount int
	h.DB.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE action = 'INSULIN_VOIDED'").Scan(&auditCount)
	if auditCount != 1 {
		t.Fatalf("want 1 INSULIN_VOIDED audit log, got %d", auditCount)
	}

	// 10. Check immutability trigger
	if _, err := h.DB.Exec("DELETE FROM insulin_logs WHERE id = ?", logID); err == nil {
		t.Fatal("expected DELETE FROM insulin_logs to be rejected by trigger")
	}
}

