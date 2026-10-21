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

// Issue #20: medicine stock and the low-stock warning.

func clinicalDayOffset(d int) string {
	return time.Now().In(handlers.ClinicalLocation()).AddDate(0, 0, d).Format("2006-01-02")
}

func stockOf(t *testing.T, h *handlers.MedicationHandler) float64 {
	t.Helper()
	var n float64
	if err := h.DB.QueryRow(`SELECT stock_units FROM medications WHERE id = 'med_aspirin'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func postStock(t *testing.T, h *handlers.MedicationHandler, who *models.Caregiver, body map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/medications/med_aspirin/stock", bytes.NewReader(b))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", "med_aspirin")
	r = r.WithContext(context.WithValue(authContext(r.Context(), who), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	h.UpdateStock(rec, r)
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// trackAspirin: 1 a day (morning, every day), no interval lock so the test can
// log one dose per day for past days.
func trackAspirin(t *testing.T, h *handlers.MedicationHandler, units float64) {
	t.Helper()
	if _, err := h.DB.Exec(`UPDATE medications SET stock_units = ?, units_per_dose = 1, refill_threshold_days = 4, min_hours_between = 0 WHERE id = 'med_aspirin'`, units); err != nil {
		t.Fatal(err)
	}
}

func lowStockAlerts(t *testing.T, h *handlers.MedicationHandler) int {
	t.Helper()
	var n int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM alerts WHERE kind = 'low_stock'`).Scan(&n)
	return n
}

func TestLowStockAlertsOnce(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	fatima := siblings[2]
	trackAspirin(t, med, 10)

	for day := -7; day <= -1; day++ {
		if res := postDose(med, fatima, handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: clinicalDayOffset(day), Status: "taken"}); res.code != http.StatusCreated {
			t.Fatalf("dose %d: %d %s", day, res.code, res.raw)
		}
		if day < -1 && lowStockAlerts(t, med) != 0 {
			t.Fatalf("alerted early, at %v units", stockOf(t, med))
		}
	}
	if got := stockOf(t, med); got != 3 {
		t.Fatalf("after 7 doses from 10: want 3, got %v", got)
	}
	if lowStockAlerts(t, med) != 1 {
		t.Fatal("3 days left (threshold 4): want one low-stock alert")
	}

	rec := httptest.NewRecorder()
	med.ListStock(rec, httptest.NewRequest(http.MethodGet, "/api/v1/medications/stock", nil))
	var list []handlers.StockStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].DaysLeft == nil || *list[0].DaysLeft != 3 || !list[0].Low {
		t.Fatalf("stock list: %+v", list)
	}

	// A partial dose takes a whole unit, and no second alert follows.
	if res := postDose(med, fatima, handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: clinicalDayOffset(0), Status: "partial"}); res.code != http.StatusCreated {
		t.Fatalf("partial: %d %s", res.code, res.raw)
	}
	if stockOf(t, med) != 2 || lowStockAlerts(t, med) != 1 {
		t.Fatalf("after a partial dose: stock %v, alerts %d; want 2 and still 1", stockOf(t, med), lowStockAlerts(t, med))
	}

	// Refill: above the threshold, so the next drop alerts again.
	if code, out := postStock(t, med, fatima, map[string]interface{}{"action": "refill", "units": 30}); code != http.StatusOK || out["stock_units"].(float64) != 32 || out["low"] != false {
		t.Fatalf("refill: %d %v", code, out)
	}
	if code, _ := postStock(t, med, fatima, map[string]interface{}{"action": "count", "units": 2}); code != http.StatusOK {
		t.Fatal("count failed")
	}
	if lowStockAlerts(t, med) != 2 {
		t.Fatal("dropping low again after a refill should alert again")
	}
	if code, _ := postStock(t, med, siblings[2], map[string]interface{}{"action": "refill", "units": 0}); code != http.StatusBadRequest {
		t.Fatal("a refill of 0 was accepted")
	}
	viewer := &models.Caregiver{ID: "cg_zainab", Name: "Zainab", Role: models.RoleViewer}
	if code, _ := postStock(t, med, viewer, map[string]interface{}{"action": "refill", "units": 10}); code != http.StatusForbidden {
		t.Fatal("a viewer changed the stock")
	}
}

func TestDoseVoidRestoresStock(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	fatima := siblings[2]
	trackAspirin(t, med, 10)

	res := postDose(med, fatima, handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: clinicalDayOffset(0), Status: "taken"})
	if res.code != http.StatusCreated || stockOf(t, med) != 9 {
		t.Fatalf("dose: %d, stock %v", res.code, stockOf(t, med))
	}
	logID := res.body["log_id"].(string)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/medications/log/"+logID+"/void", bytes.NewReader([]byte(`{}`)))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", logID)
	r = r.WithContext(context.WithValue(authContext(r.Context(), fatima), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	med.VoidDose(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("void: %d %s", rec.Code, rec.Body.String())
	}
	if got := stockOf(t, med); got != 10 {
		t.Fatalf("log + void: want 10, got %v", got)
	}
	var dose, back int
	_ = med.DB.QueryRow(`SELECT COUNT(*) FROM stock_events WHERE log_id = ? AND reason = 'dose' AND delta = -1`, logID).Scan(&dose)
	_ = med.DB.QueryRow(`SELECT COUNT(*) FROM stock_events WHERE log_id = ? AND reason = 'correction' AND delta = 1`, logID).Scan(&back)
	if dose != 1 || back != 1 {
		t.Fatalf("want both events recorded, got dose=%d correction=%d", dose, back)
	}

	// Skipped and refused doses leave the box alone; untracked medicines are untouched.
	if res := postDose(med, fatima, handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: clinicalDayOffset(-1), Status: "skipped", Notes: "asleep"}); res.code != http.StatusCreated || stockOf(t, med) != 10 {
		t.Fatalf("skipped dose changed the stock: %d %v", res.code, stockOf(t, med))
	}
	if res := postDose(med, fatima, handlers.LogDoseRequest{MedicationID: "med_citicoline", Slot: "morning", ScheduledDate: clinicalDayOffset(0), Status: "taken"}); res.code != http.StatusCreated {
		t.Fatalf("untracked medicine: %d %s", res.code, res.raw)
	}
	var n int
	_ = med.DB.QueryRow(`SELECT COUNT(*) FROM stock_events WHERE medication_id = 'med_citicoline'`).Scan(&n)
	if n != 0 {
		t.Fatal("an untracked medicine got stock events")
	}
}

func TestConcurrentDosesCountStockExactly(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	trackAspirin(t, med, 50)
	const n = 20
	codes := make([]int, n)
	var start, wg sync.WaitGroup
	start.Add(1)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start.Wait()
			// 20 different days; 2 phones racing for each of 10 of them.
			codes[i] = postDose(med, siblings[i%len(siblings)], handlers.LogDoseRequest{
				MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: clinicalDayOffset(-(i % 10)), Status: "taken",
			}).code
		}(i)
	}
	start.Done()
	wg.Wait()
	given := 0
	for _, c := range codes {
		if c == http.StatusCreated {
			given++
		}
	}
	if given != 10 {
		t.Fatalf("want exactly 10 doses given (10 days), got %d (%v)", given, codes)
	}
	if got := stockOf(t, med); got != 40 {
		t.Fatalf("50 units, 10 doses given: want 40, got %v", got)
	}
	var events int
	_ = med.DB.QueryRow(`SELECT COUNT(*) FROM stock_events WHERE reason = 'dose'`).Scan(&events)
	if events != 10 {
		t.Fatalf("want 10 dose events, got %d", events)
	}
}
