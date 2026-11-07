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

func putMedOrder(h *handlers.ShiftsHandler, c *models.Caregiver, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/api/v1/shifts/med-order", strings.NewReader(body))
	req = req.WithContext(authContext(req.Context(), c))
	rec := httptest.NewRecorder()
	h.PutMedOrder(rec, req)
	return rec
}

func scheduleOrderFrom(t *testing.T, h *handlers.MedicationHandler, c *models.Caregiver) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/medications/schedule", nil)
	req = req.WithContext(authContext(req.Context(), c))
	rec := httptest.NewRecorder()
	h.GetSchedule(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("schedule: got %d: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		OrderFrom string `json:"order_from"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res.OrderFrom
}

func TestMedOrderFollowsTheCurrentShift(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	shifts := &handlers.ShiftsHandler{DB: med.DB}
	super := &models.Caregiver{ID: "cg_hussain", Name: "Hussain", Role: models.RoleSuperAdmin}
	carer := &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}

	// Unchanged until the Super Admin chooses it.
	if got := scheduleOrderFrom(t, med, carer); got != "" {
		t.Fatalf("default order_from = %q, want clock order", got)
	}

	if rec := putMedOrder(shifts, &models.Caregiver{ID: "cg_ali", Role: models.RoleAdmin}, `{"order":"shift"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("admin: got %d, want 403", rec.Code)
	}
	if rec := putMedOrder(shifts, carer, `{"order":"shift"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("caregiver: got %d, want 403", rec.Code)
	}
	if rec := putMedOrder(shifts, super, `{"order":"sideways"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad order: got %d, want 400", rec.Code)
	}

	if rec := putMedOrder(shifts, super, `{"order":"shift"}`); rec.Code != http.StatusOK {
		t.Fatalf("super admin: got %d: %s", rec.Code, rec.Body.String())
	}
	if got := scheduleOrderFrom(t, med, carer); got != "08:00" { // the seeded 24-hour shift
		t.Fatalf("order_from = %q, want 08:00", got)
	}

	var audited int
	if err := med.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'MED_ORDER_CHANGED'`).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("audit rows = %d (err %v), want 1", audited, err)
	}

	// Saving again updates the same setting, and clock order turns it off.
	if rec := putMedOrder(shifts, super, `{"order":"clock"}`); rec.Code != http.StatusOK {
		t.Fatalf("back to clock: got %d", rec.Code)
	}
	if got := scheduleOrderFrom(t, med, carer); got != "" {
		t.Fatalf("after clock: order_from = %q", got)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/shifts/med-order", nil)
	rec := httptest.NewRecorder()
	shifts.GetMedOrder(rec, req)
	if !strings.Contains(rec.Body.String(), `"clock"`) {
		t.Fatalf("GET: %s", rec.Body.String())
	}
}
