package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Same medicine, two names (issue #61). The admin records each medicine's active
// ingredient (lowercase INN, e.g. "clopidogrel"); a second active medicine with
// the same one is refused unless confirmed with a reason, and the interval lock
// and PRN limits count doses across both (sameIngredientIDs).

var validDrugClasses = map[string]bool{
	"": true, "antiplatelet": true, "anticoagulant": true, "insulin": true, "antihypertensive": true,
	"statin": true, "antidiabetic": true, "anticonvulsant": true, "painkiller": true, "other": true,
}

const maxIngredientLen = 80

// IngredientFields are shared by create and update.
type IngredientFields struct {
	ActiveIngredient *string `json:"active_ingredient,omitempty"`
	DrugClass        *string `json:"drug_class,omitempty"`
	// ConfirmDuplicate + DuplicateReason: add it anyway (e.g. a deliberate dose split).
	ConfirmDuplicate bool   `json:"confirm_duplicate,omitempty"`
	DuplicateReason  string `json:"duplicate_reason,omitempty"`
}

func normalizeIngredient(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// validate normalises the fields in place; it returns an error code and message.
func (f *IngredientFields) validate() (string, string) {
	if f.ActiveIngredient != nil {
		v := normalizeIngredient(*f.ActiveIngredient)
		if len(v) > maxIngredientLen {
			return "INVALID_INGREDIENT", "active_ingredient is too long"
		}
		f.ActiveIngredient = &v
	}
	if f.DrugClass != nil {
		v := strings.ToLower(strings.TrimSpace(*f.DrugClass))
		if !validDrugClasses[v] {
			return "INVALID_DRUG_CLASS", "drug_class must be antiplatelet, anticoagulant, insulin, antihypertensive, statin, antidiabetic, anticonvulsant, painkiller or other"
		}
		f.DrugClass = &v
	}
	f.DuplicateReason = strings.TrimSpace(f.DuplicateReason)
	if len(f.DuplicateReason) > maxOverrideReasonLen {
		return "REASON_TOO_LONG", "duplicate_reason is too long"
	}
	return "", ""
}

type sameMedicine struct {
	ID     string `json:"id"`
	NameAr string `json:"name_ar"`
	NameEn string `json:"name_en"`
	Dosage string `json:"dosage"`
}

// activeWithIngredient finds another active medicine with this ingredient.
func activeWithIngredient(q querier, ingredient, excludeID string) (*sameMedicine, error) {
	if ingredient == "" {
		return nil, nil
	}
	var m sameMedicine
	err := q.QueryRow(`SELECT id, name_ar, name_en, dosage FROM medications
		WHERE is_active = 1 AND active_ingredient = ? AND id <> ? ORDER BY created_at LIMIT 1`, ingredient, excludeID).
		Scan(&m.ID, &m.NameAr, &m.NameEn, &m.Dosage)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &m, err
}

// checkDuplicate answers 409 DUPLICATE_INGREDIENT (or 400 REASON_REQUIRED when
// confirmed without a reason) and returns false; true means go ahead, with the
// duplicate the admin confirmed, if any.
func checkDuplicate(w http.ResponseWriter, q querier, f IngredientFields, ingredient, excludeID string) (*sameMedicine, bool) {
	dup, err := activeWithIngredient(q, ingredient, excludeID)
	if err != nil {
		internalError(w, "failed to check for the same medicine", err)
		return nil, false
	}
	if dup == nil {
		return nil, true
	}
	if !f.ConfirmDuplicate {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":    "DUPLICATE_INGREDIENT",
			"message":  "Another active medicine has the same active ingredient: " + dup.NameEn,
			"existing": dup,
		})
		return nil, false
	}
	if f.DuplicateReason == "" {
		writeJSONError(w, http.StatusBadRequest, "REASON_REQUIRED", "write why both are needed (e.g. the doctor split the dose)")
		return nil, false
	}
	return dup, true
}

// bloodThinnerWarning: "two_blood_thinners" when this anticoagulant joins another
// anticoagulant, or this antiplatelet another antiplatelet. A warning, not a block.
func bloodThinnerWarning(q querier, class, excludeID string) string {
	if class != "anticoagulant" && class != "antiplatelet" {
		return ""
	}
	var n int
	_ = q.QueryRow(`SELECT COUNT(*) FROM medications WHERE is_active = 1 AND drug_class = ? AND id <> ?`, class, excludeID).Scan(&n)
	if n > 0 {
		return "two_blood_thinners"
	}
	return ""
}
