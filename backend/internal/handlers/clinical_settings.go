package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
)

// Low blood pressure thresholds (issue #13): LOW when systolic < LowSystolic or
// diastolic < LowDiastolic. Defaults 90/60; an admin can set the doctor's values,
// stored in app_settings. Kept in memory for the classifier.
type BPLowThresholds struct {
	LowSystolic  int `json:"bp_low_systolic"`
	LowDiastolic int `json:"bp_low_diastolic"`
}

var defaultBPLow = BPLowThresholds{LowSystolic: 90, LowDiastolic: 60}
var bpLow atomic.Pointer[BPLowThresholds]

func currentBPLow() BPLowThresholds {
	if t := bpLow.Load(); t != nil {
		return *t
	}
	return defaultBPLow
}

// SetBPLowThresholds replaces the in-memory thresholds (startup, admin change, tests).
func SetBPLowThresholds(t BPLowThresholds) { bpLow.Store(&t) }

// LoadClinicalSettings reads the doctor's thresholds from app_settings, if set.
func LoadClinicalSettings(db *sql.DB) {
	t := defaultBPLow
	for key, dst := range map[string]*int{"bp_low_systolic": &t.LowSystolic, "bp_low_diastolic": &t.LowDiastolic} {
		var v string
		if db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&v) == nil {
			if n, err := strconv.Atoi(v); err == nil {
				*dst = n
			}
		}
	}
	SetBPLowThresholds(t)
}

type ClinicalSettingsHandler struct {
	DB *sql.DB
}

func (h *ClinicalSettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(currentBPLow())
}

// Put sets the doctor's low-BP thresholds (admin). Systolic 70-120, diastolic 40-80.
func (h *ClinicalSettingsHandler) Put(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageMedications(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only an admin can change clinical thresholds")
		return
	}
	var t BPLowThresholds
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil ||
		t.LowSystolic < 70 || t.LowSystolic > 120 || t.LowDiastolic < 40 || t.LowDiastolic > 80 {
		writeJSONError(w, http.StatusBadRequest, "INVALID_THRESHOLDS", "bp_low_systolic must be 70-120 and bp_low_diastolic 40-80")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `INSERT INTO app_settings (key, value) VALUES ('bp_low_systolic', ?), ('bp_low_diastolic', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, strconv.Itoa(t.LowSystolic), strconv.Itoa(t.LowDiastolic)); err != nil {
		internalError(w, "failed to save thresholds", err)
		return
	}
	SetBPLowThresholds(t)
	audit.Log(h.DB, me.ID, "CLINICAL_SETTINGS", "app_settings", "bp_low", fmt.Sprintf("Low BP thresholds set to %d/%d", t.LowSystolic, t.LowDiastolic))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}
