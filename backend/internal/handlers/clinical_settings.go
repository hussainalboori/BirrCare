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
// FeverHighC (issue #54) raises the red fever_high alert for everyone.
type OxygenFeverThresholds struct {
	SpO2Warn     int     `json:"spo2_warn"`
	SpO2Critical int     `json:"spo2_critical"`
	FeverC       float64 `json:"fever_c"`
	FeverHighC   float64 `json:"fever_high_c"`
}

var defaultOxygenFever = OxygenFeverThresholds{SpO2Warn: 92, SpO2Critical: 88, FeverC: 38.0, FeverHighC: 39.0}
var oxygenFever atomic.Pointer[OxygenFeverThresholds]

func currentOxygenFever() OxygenFeverThresholds {
	if t := oxygenFever.Load(); t != nil {
		return *t
	}
	return defaultOxygenFever
}

// SetOxygenFeverThresholds replaces the in-memory thresholds (startup, admin change,
// tests). A missing high-fever threshold falls back to one degree above fever.
func SetOxygenFeverThresholds(t OxygenFeverThresholds) {
	if t.FeverHighC <= t.FeverC {
		t.FeverHighC = math.Max(defaultOxygenFever.FeverHighC, t.FeverC+1.0)
	}
	oxygenFever.Store(&t)
}

var defaultRepositionInterval = 120 // 2 hours default
var repositionInterval atomic.Int64

func currentRepositionInterval() int {
	v := repositionInterval.Load()
	if v > 0 {
		return int(v)
	}
	return defaultRepositionInterval
}

// SetRepositionInterval replaces the in-memory interval in minutes (startup, admin change, tests).
func SetRepositionInterval(minutes int) {
	if minutes > 0 {
		repositionInterval.Store(int64(minutes))
	}
}

var defaultFluidDailyTarget = 1500 // 1500 ml default (issue #28)
var fluidDailyTarget atomic.Int64

func currentFluidDailyTarget() int {
	v := fluidDailyTarget.Load()
	if v > 0 {
		return int(v)
	}
	return defaultFluidDailyTarget
}

// SetFluidDailyTarget replaces the in-memory fluid target in ml (startup, admin change, tests).
func SetFluidDailyTarget(target int) {
	if target > 0 {
		fluidDailyTarget.Store(int64(target))
	}
}

// INRTarget (issue #62): the doctor's INR range for warfarin; a result outside
// it alerts the admins. Defaults 2.0-3.0.
type INRTarget struct {
	Low  float64 `json:"inr_target_low"`
	High float64 `json:"inr_target_high"`
}

var defaultINRTarget = INRTarget{Low: 2.0, High: 3.0}
var inrTarget atomic.Pointer[INRTarget]

func currentINRTarget() INRTarget {
	if t := inrTarget.Load(); t != nil {
		return *t
	}
	return defaultINRTarget
}

// SetINRTarget replaces the in-memory range (startup, admin change, tests).
func SetINRTarget(t INRTarget) { inrTarget.Store(&t) }

// ClinicalSettings is everything GET/PUT /settings/clinical carries.
type ClinicalSettings struct {
	BPLowThresholds
	OxygenFeverThresholds
	RepositionIntervalMinutes int `json:"reposition_interval_minutes"`
	FluidDailyTargetML        int `json:"fluid_daily_target_ml"`
	INRTarget
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
	for key, dst := range map[string]*float64{"fever_c": &o.FeverC, "fever_high_c": &o.FeverHighC} {
		var v string
		if db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&v) == nil {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				*dst = f
			}
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

	var rStr string
	if db.QueryRow(`SELECT value FROM app_settings WHERE key = 'reposition_interval_minutes'`).Scan(&rStr) == nil {
		if n, err := strconv.Atoi(rStr); err == nil && n >= 30 && n <= 360 {
			SetRepositionInterval(n)
		}
	} else {
		SetRepositionInterval(defaultRepositionInterval)
	}

	inr := defaultINRTarget
	for key, dst := range map[string]*float64{"inr_target_low": &inr.Low, "inr_target_high": &inr.High} {
		var v string
		if db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&v) == nil {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				*dst = f
			}
		}
	}
	if inr.Low >= inr.High {
		inr = defaultINRTarget
	}
	SetINRTarget(inr)

	var fTargetStr string
	if db.QueryRow(`SELECT value FROM app_settings WHERE key = 'fluid_daily_target_ml'`).Scan(&fTargetStr) == nil {
		if n, err := strconv.Atoi(fTargetStr); err == nil && n >= 500 && n <= 5000 {
			SetFluidDailyTarget(n)
		}
	} else {
		SetFluidDailyTarget(defaultFluidDailyTarget)
	}
}

type ClinicalSettingsHandler struct {
	DB *sql.DB
}

func (h *ClinicalSettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ClinicalSettings{currentBPLow(), currentOxygenFever(), currentRepositionInterval(), currentFluidDailyTarget(), currentINRTarget()})
}

type clinicalSettingsRequest struct {
	LowSystolic               *int     `json:"bp_low_systolic"`
	LowDiastolic              *int     `json:"bp_low_diastolic"`
	SpO2Warn                  *int     `json:"spo2_warn"`
	SpO2Critical              *int     `json:"spo2_critical"`
	FeverC                    *float64 `json:"fever_c"`
	FeverHighC                *float64 `json:"fever_high_c"`
	RepositionIntervalMinutes *int     `json:"reposition_interval_minutes"`
	FluidDailyTargetML        *int     `json:"fluid_daily_target_ml"`
	INRTargetLow              *float64 `json:"inr_target_low"`
	INRTargetHigh             *float64 `json:"inr_target_high"`
}

// Put sets the doctor's thresholds (admin). Fields left out keep their value.
// Low BP: systolic 70-120, diastolic 40-80. SpO2: critical 80-94, warning above
// it and at most 96. Fever: 37.5-39.0 C; high fever above it, 38.5-40.0 C.
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
	if req.FeverHighC != nil {
		o.FeverHighC = math.Round(*req.FeverHighC*10) / 10
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
	if o.FeverHighC < 38.5 || o.FeverHighC > 40.0 || o.FeverHighC <= o.FeverC {
		writeJSONError(w, http.StatusBadRequest, "INVALID_THRESHOLDS", "fever_high_c must be 38.5-40.0 and above fever_c")
		return
	}
	repInterval := currentRepositionInterval()
	if req.RepositionIntervalMinutes != nil {
		if *req.RepositionIntervalMinutes < 30 || *req.RepositionIntervalMinutes > 360 {
			writeJSONError(w, http.StatusBadRequest, "INVALID_THRESHOLDS", "reposition_interval_minutes must be between 30 and 360")
			return
		}
		repInterval = *req.RepositionIntervalMinutes
	}
	fluidTarget := currentFluidDailyTarget()
	if req.FluidDailyTargetML != nil {
		if *req.FluidDailyTargetML < 500 || *req.FluidDailyTargetML > 5000 {
			writeJSONError(w, http.StatusBadRequest, "INVALID_THRESHOLDS", "fluid_daily_target_ml must be between 500 and 5000")
			return
		}
		fluidTarget = *req.FluidDailyTargetML
	}
	inr := currentINRTarget()
	if req.INRTargetLow != nil {
		inr.Low = math.Round(*req.INRTargetLow*10) / 10
	}
	if req.INRTargetHigh != nil {
		inr.High = math.Round(*req.INRTargetHigh*10) / 10
	}
	if inr.Low < 1.5 || inr.High > 4.5 || inr.Low >= inr.High {
		writeJSONError(w, http.StatusBadRequest, "INVALID_THRESHOLDS", "inr_target_low and inr_target_high must be within 1.5-4.5, low below high")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to begin transaction", err)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO app_settings (key, value) VALUES ('inr_target_low', ?), ('inr_target_high', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, strconv.FormatFloat(inr.Low, 'f', 1, 64), strconv.FormatFloat(inr.High, 'f', 1, 64)); err != nil {
		internalError(w, "failed to save thresholds", err)
		return
	}
	if _, err := tx.Exec(`INSERT INTO app_settings (key, value) VALUES
		('bp_low_systolic', ?), ('bp_low_diastolic', ?), ('spo2_warn', ?), ('spo2_critical', ?), ('fever_c', ?), ('fever_high_c', ?), ('reposition_interval_minutes', ?), ('fluid_daily_target_ml', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		strconv.Itoa(bp.LowSystolic), strconv.Itoa(bp.LowDiastolic), strconv.Itoa(o.SpO2Warn), strconv.Itoa(o.SpO2Critical),
		strconv.FormatFloat(o.FeverC, 'f', 1, 64), strconv.FormatFloat(o.FeverHighC, 'f', 1, 64), strconv.Itoa(repInterval), strconv.Itoa(fluidTarget)); err != nil {
		internalError(w, "failed to save thresholds", err)
		return
	}
	if err := audit.Record(tx, me.ID, "CLINICAL_SETTINGS", "app_settings", "thresholds",
		fmt.Sprintf("Low BP %d/%d; SpO2 warn <=%d, critical <=%d; fever >=%.1f C, high >=%.1f C; reposition %d min; fluid target %d ml; INR %.1f-%.1f", bp.LowSystolic, bp.LowDiastolic, o.SpO2Warn, o.SpO2Critical, o.FeverC, o.FeverHighC, repInterval, fluidTarget, inr.Low, inr.High)); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit thresholds", err)
		return
	}
	SetBPLowThresholds(bp)
	SetOxygenFeverThresholds(o)
	SetRepositionInterval(repInterval)
	SetFluidDailyTarget(fluidTarget)
	SetINRTarget(inr)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ClinicalSettings{bp, o, repInterval, fluidTarget, inr})
}
