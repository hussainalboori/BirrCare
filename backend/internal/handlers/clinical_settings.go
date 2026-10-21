package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
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

// OxygenFeverThresholds (issue #22): SpO2 <= SpO2Warn is LOW (re-check, call the
// doctor), <= SpO2Critical is CRITICAL (alert); temperature >= FeverC is FEVER.
type OxygenFeverThresholds struct {
	SpO2Warn     int     `json:"spo2_warn"`
	SpO2Critical int     `json:"spo2_critical"`
	FeverC       float64 `json:"fever_c"`
}

var defaultOxygenFever = OxygenFeverThresholds{SpO2Warn: 92, SpO2Critical: 88, FeverC: 38.0}
var oxygenFever atomic.Pointer[OxygenFeverThresholds]

func currentOxygenFever() OxygenFeverThresholds {
	if t := oxygenFever.Load(); t != nil {
		return *t
	}
	return defaultOxygenFever
}

// SetOxygenFeverThresholds replaces the in-memory thresholds (startup, admin change, tests).
func SetOxygenFeverThresholds(t OxygenFeverThresholds) { oxygenFever.Store(&t) }

// ClinicalSettings is everything GET/PUT /settings/clinical carries.
type ClinicalSettings struct {
	BPLowThresholds
	OxygenFeverThresholds
}

// LoadClinicalSettings reads the doctor's thresholds from app_settings, if set.
func LoadClinicalSettings(db *sql.DB) {
	o := defaultOxygenFever
	for key, dst := range map[string]*int{"spo2_warn": &o.SpO2Warn, "spo2_critical": &o.SpO2Critical} {
		var v string
		if db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&v) == nil {
			if n, err := strconv.Atoi(v); err == nil {
				*dst = n
			}
		}
	}
	var fv string
	if db.QueryRow(`SELECT value FROM app_settings WHERE key = 'fever_c'`).Scan(&fv) == nil {
		if f, err := strconv.ParseFloat(fv, 64); err == nil {
			o.FeverC = f
		}
	}
	SetOxygenFeverThresholds(o)

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
	json.NewEncoder(w).Encode(ClinicalSettings{currentBPLow(), currentOxygenFever()})
}

type clinicalSettingsRequest struct {
	LowSystolic  *int     `json:"bp_low_systolic"`
	LowDiastolic *int     `json:"bp_low_diastolic"`
	SpO2Warn     *int     `json:"spo2_warn"`
	SpO2Critical *int     `json:"spo2_critical"`
	FeverC       *float64 `json:"fever_c"`
}

// Put sets the doctor's thresholds (admin). Fields left out keep their value.
// Low BP: systolic 70-120, diastolic 40-80. SpO2: critical 80-94, warning above
// it and at most 96. Fever: 37.5-39.0 C.
func (h *ClinicalSettingsHandler) Put(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageMedications(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only an admin can change clinical thresholds")
		return
	}
	var req clinicalSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_THRESHOLDS", "invalid request payload")
		return
	}
	bp, o := currentBPLow(), currentOxygenFever()
	if req.LowSystolic != nil {
		bp.LowSystolic = *req.LowSystolic
	}
	if req.LowDiastolic != nil {
		bp.LowDiastolic = *req.LowDiastolic
	}
	if req.SpO2Warn != nil {
		o.SpO2Warn = *req.SpO2Warn
	}
	if req.SpO2Critical != nil {
		o.SpO2Critical = *req.SpO2Critical
	}
	if req.FeverC != nil {
		o.FeverC = math.Round(*req.FeverC*10) / 10
	}
	if bp.LowSystolic < 70 || bp.LowSystolic > 120 || bp.LowDiastolic < 40 || bp.LowDiastolic > 80 {
		writeJSONError(w, http.StatusBadRequest, "INVALID_THRESHOLDS", "bp_low_systolic must be 70-120 and bp_low_diastolic 40-80")
		return
	}
	if o.SpO2Critical < 80 || o.SpO2Critical > 94 || o.SpO2Warn <= o.SpO2Critical || o.SpO2Warn > 96 {
		writeJSONError(w, http.StatusBadRequest, "INVALID_THRESHOLDS", "spo2_critical must be 80-94 and spo2_warn above it, at most 96")
		return
	}
	if o.FeverC < 37.5 || o.FeverC > 39.0 {
		writeJSONError(w, http.StatusBadRequest, "INVALID_THRESHOLDS", "fever_c must be 37.5-39.0")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `INSERT INTO app_settings (key, value) VALUES
		('bp_low_systolic', ?), ('bp_low_diastolic', ?), ('spo2_warn', ?), ('spo2_critical', ?), ('fever_c', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		strconv.Itoa(bp.LowSystolic), strconv.Itoa(bp.LowDiastolic), strconv.Itoa(o.SpO2Warn), strconv.Itoa(o.SpO2Critical),
		strconv.FormatFloat(o.FeverC, 'f', 1, 64)); err != nil {
		internalError(w, "failed to save thresholds", err)
		return
	}
	SetBPLowThresholds(bp)
	SetOxygenFeverThresholds(o)
	audit.Log(h.DB, me.ID, "CLINICAL_SETTINGS", "app_settings", "thresholds",
		fmt.Sprintf("Low BP %d/%d; SpO2 warn <=%d, critical <=%d; fever >=%.1f C", bp.LowSystolic, bp.LowDiastolic, o.SpO2Warn, o.SpO2Critical, o.FeverC))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ClinicalSettings{bp, o})
}
