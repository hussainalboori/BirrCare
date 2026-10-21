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

func voidPRN(h *handlers.MedicationHandler, who *models.Caregiver, logID, reason string) int {
	b, _ := json.Marshal(handlers.VoidPRNDoseRequest{Reason: reason})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/medications/prn/log/"+logID+"/void", bytes.NewReader(b))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", logID)
	r = r.WithContext(context.WithValue(authContext(r.Context(), who), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	h.VoidPRNDose(rec, r)
	return rec.Code
}

// Cancelling a PRN dose frees its place in the 24h cap, so it follows the same
// rule as scheduled doses (#8): your own entry within 10 minutes; after that, or
// someone else's, only an admin with a reason.
func TestPRNVoidIsRestricted(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := med.DB.Exec(`INSERT INTO caregivers (id, name, role, pin_hash) VALUES ('cg_omar', 'Omar', 'caregiver', 'x')`); err != nil {
		t.Fatal(err)
	}
	createPRNMedication(t, med, "med_prn_void_rules", 4, 0)
	fatima := siblings[2]
	omar := &models.Caregiver{ID: "cg_omar", Name: "Omar", Role: models.RoleCaregiver}
	admin := siblings[1]

	give := func() string {
		res := postPRNDose(med, fatima, handlers.LogPRNDoseRequest{MedicationID: "med_prn_void_rules", ReasonTag: "pain"})
		if res.code != http.StatusCreated {
			t.Fatalf("give: %d %s", res.code, res.raw)
		}
		return res.body["id"].(string)
	}

	first := give()
	if code := voidPRN(med, omar, first, "not mine"); code != http.StatusForbidden {
		t.Fatalf("a sibling cancelled someone else's PRN dose: %d", code)
	}
	if code := voidPRN(med, fatima, first, ""); code != http.StatusOK {
		t.Fatalf("own undo within 10 minutes: %d", code)
	}

	// The same person, an hour later: no longer an undo.
	second := give()
	// Simulate an hour passing (the trigger forbids editing recorded_at).
	if _, err := med.DB.Exec(`DROP TRIGGER prn_logs_values_immutable`); err != nil {
		t.Fatal(err)
	}
	if _, err := med.DB.Exec(`UPDATE prn_logs SET recorded_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Hour).Format("2006-01-02 15:04:05"), second); err != nil {
		t.Fatal(err)
	}
	if code := voidPRN(med, fatima, second, "oops"); code != http.StatusForbidden {
		t.Fatalf("own cancel after an hour: want 403, got %d", code)
	}
	if code := voidPRN(med, admin, second, ""); code != http.StatusBadRequest {
		t.Fatalf("admin cancel without a reason: want 400, got %d", code)
	}
	if code := voidPRN(med, admin, second, "Entered on the wrong medicine"); code != http.StatusOK {
		t.Fatalf("admin cancel with a reason: want 200, got %d", code)
	}
}

// The minimum interval is checked against the nearest dose in either direction,
// so a late entry with an earlier given_at can't land next to an older dose.
func TestPRNIntervalChecksNearestDose(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	createPRNMedication(t, med, "med_prn_nearest", 10, 4)
	fatima := siblings[2]
	now := time.Now().UTC()
	for _, at := range []time.Time{now.Add(-10 * time.Hour), now} {
		if res := postPRNDose(med, fatima, handlers.LogPRNDoseRequest{MedicationID: "med_prn_nearest", ReasonTag: "pain", GivenAt: at.Format(time.RFC3339)}); res.code != http.StatusCreated {
			t.Fatalf("setup dose: %d %s", res.code, res.raw)
		}
	}
	// 1 hour after the -10h dose (and 9 h before the latest one).
	res := postPRNDose(med, fatima, handlers.LogPRNDoseRequest{MedicationID: "med_prn_nearest", ReasonTag: "pain", GivenAt: now.Add(-9 * time.Hour).Format(time.RFC3339)})
	if res.code != http.StatusConflict || res.body["error"] != "PRN_TOO_SOON" {
		t.Fatalf("dose 1 h after another: want 409 PRN_TOO_SOON, got %d %s", res.code, res.raw)
	}
}

// PRN history follows the audit-trail rules of #23: values can't be edited in
// place, a dose is cancelled once, and rows are never deleted.
func TestPRNLogsAreImmutable(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	createPRNMedication(t, med, "med_prn_immutable", 4, 0)
	res := postPRNDose(med, siblings[2], handlers.LogPRNDoseRequest{MedicationID: "med_prn_immutable", ReasonTag: "fever"})
	id := res.body["id"].(string)
	if _, err := med.DB.Exec(`UPDATE prn_logs SET given_at = '2020-01-01 00:00:00' WHERE id = ?`, id); err == nil {
		t.Fatal("a PRN dose time was changed in place")
	}
	if _, err := med.DB.Exec(`DELETE FROM prn_logs WHERE id = ?`, id); err == nil {
		t.Fatal("a PRN dose was deleted")
	}
}

// No PRN medicines yet (every family on day one): the list is [], never null,
// or the Home page that renders it breaks.
func TestPRNListIsEmptyArrayNotNull(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	rec := httptest.NewRecorder()
	med.GetPRNMedications(rec, httptest.NewRequest(http.MethodGet, "/api/v1/medications/prn", nil))
	if body := bytes.TrimSpace(rec.Body.Bytes()); string(body) != "[]" {
		t.Fatalf("want [], got %s", body)
	}
}
