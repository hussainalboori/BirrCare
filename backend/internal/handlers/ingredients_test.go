package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
)

// Issue #61: the same active ingredient under two names.

func createMedication(t *testing.T, h *handlers.MedicationHandler, body map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	rec := medCall(t, h.CreateMedication, siblings[1], "", body)
	var out map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestDuplicateIngredientNeedsConfirmation(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	code, out := createMedication(t, h, map[string]interface{}{"name_ar": "كلوبيدوقريل", "name_en": "Clopidogrel", "dosage": "75 mg",
		"frequency_slots": []string{"morning"}, "active_ingredient": " Clopidogrel ", "drug_class": "antiplatelet"})
	if code != http.StatusCreated {
		t.Fatalf("first: %d %v", code, out)
	}
	firstID := out["id"].(string)

	plavix := map[string]interface{}{"name_ar": "بلافكس", "name_en": "Plavix", "dosage": "75 mg", "frequency_slots": []string{"morning"}, "active_ingredient": "clopidogrel"}
	code, out = createMedication(t, h, plavix)
	if code != http.StatusConflict || out["error"] != "DUPLICATE_INGREDIENT" {
		t.Fatalf("same ingredient: want 409 DUPLICATE_INGREDIENT, got %d %v", code, out)
	}
	if ex, _ := out["existing"].(map[string]interface{}); ex["name_en"] != "Clopidogrel" || ex["name_ar"] != "كلوبيدوقريل" {
		t.Fatalf("the 409 names the existing medicine: %v", out)
	}
	plavix["confirm_duplicate"] = true
	if code, out = createMedication(t, h, plavix); code != http.StatusBadRequest || out["error"] != "REASON_REQUIRED" {
		t.Fatalf("confirm without reason: want 400, got %d %v", code, out)
	}
	plavix["duplicate_reason"] = "Doctor split the dose: 75 mg morning on each box"
	if code, out = createMedication(t, h, plavix); code != http.StatusCreated {
		t.Fatalf("confirmed: %d %v", code, out)
	}
	var details string
	h.DB.QueryRow(`SELECT details FROM audit_logs WHERE action = 'DUPLICATE_INGREDIENT_CONFIRMED' AND entity_id = ?`, out["id"]).Scan(&details)
	if !strings.Contains(details, "Clopidogrel") || !strings.Contains(details, "Doctor split") {
		t.Fatalf("audit row: %q", details)
	}

	// A stopped one doesn't count; starting it again is checked.
	h.DB.Exec(`UPDATE medications SET is_active = 0 WHERE id = ?`, firstID)
	delete(plavix, "confirm_duplicate")
	delete(plavix, "duplicate_reason")
	plavix["name_en"] = "Clopidogrel generic 2"
	h.DB.Exec(`UPDATE medications SET is_active = 0 WHERE active_ingredient = 'clopidogrel'`)
	if code, out = createMedication(t, h, plavix); code != http.StatusCreated {
		t.Fatalf("no active duplicate: want 201, got %d %v", code, out)
	}
	if rec := medCall(t, h.UpdateMedication, siblings[1], firstID, map[string]interface{}{"is_active": true}); rec.Code != http.StatusConflict {
		t.Fatalf("re-activating a duplicate: want 409, got %d %s", rec.Code, rec.Body.String())
	}
	// Editing a medicine to an ingredient another active one has.
	if rec := medCall(t, h.UpdateMedication, siblings[1], "med_aspirin", map[string]interface{}{"active_ingredient": "clopidogrel"}); rec.Code != http.StatusConflict {
		t.Fatalf("edit to a duplicate ingredient: want 409, got %d", rec.Code)
	}
}

func TestTwoBloodThinnersWarn(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	createMedication(t, h, map[string]interface{}{"name_ar": "أبيكسابان", "name_en": "Apixaban", "dosage": "5 mg", "frequency_slots": []string{"morning"}, "active_ingredient": "apixaban", "drug_class": "anticoagulant"})
	code, out := createMedication(t, h, map[string]interface{}{"name_ar": "وارفارين", "name_en": "Warfarin", "dosage": "5 mg", "frequency_slots": []string{"evening"}, "active_ingredient": "warfarin", "drug_class": "anticoagulant"})
	if code != http.StatusCreated {
		t.Fatalf("a second anticoagulant is allowed: %d %v", code, out)
	}
	if w, _ := out["warnings"].([]interface{}); len(w) != 1 || w[0] != "two_blood_thinners" {
		t.Fatalf("want the two_blood_thinners warning, got %v", out)
	}
	if code, out := createMedication(t, h, map[string]interface{}{"name_ar": "x", "name_en": "X", "dosage": "1", "frequency_slots": []string{"morning"}, "drug_class": "magic"}); code != http.StatusBadRequest {
		t.Fatalf("bad drug class: want 400, got %d %v", code, out)
	}
}

func TestIntervalLockAcrossSameIngredient(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	// Clopidogrel and Plavix, both once a day (18 h lock).
	h.DB.Exec(`UPDATE medications SET created_at = datetime('now', '-30 days')`)
	h.DB.Exec(`INSERT INTO medications (id, name_ar, name_en, dosage, frequency_slots, active_ingredient, created_by, created_at) VALUES
		('med_a', 'أ', 'Clopidogrel', '75 mg', '["morning"]', 'clopidogrel', 'cg_hussain', datetime('now', '-30 days')),
		('med_b', 'ب', 'Plavix', '75 mg', '["evening"]', 'clopidogrel', 'cg_hussain', datetime('now', '-30 days'))`)
	at8 := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if res := postDose(h, siblings[2], handlers.LogDoseRequest{MedicationID: "med_a", Slot: "morning", ScheduledDate: clinicalDay(0), Status: "taken", GivenAt: at8}); res.code != http.StatusCreated {
		t.Fatalf("row A: %d %s", res.code, res.raw)
	}
	res := postDose(h, siblings[1], handlers.LogDoseRequest{MedicationID: "med_b", Slot: "evening", ScheduledDate: clinicalDay(0), Status: "taken"})
	if res.code != http.StatusConflict || res.body["error"] != "DOSE_TOO_SOON" {
		t.Fatalf("row B an hour later (same ingredient): want DOSE_TOO_SOON, got %d %s", res.code, res.raw)
	}
	// A different ingredient isn't affected.
	if res := postDose(h, siblings[1], handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: clinicalDay(0), Status: "taken"}); res.code != http.StatusCreated {
		t.Fatalf("aspirin: %d %s", res.code, res.raw)
	}
}
