package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/push"
	"birrcare/backend/internal/sse"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type VitalsHandler struct {
	DB     *sql.DB
	Broker *sse.Broker
	Push   *push.Notifier // nil = no Web Push
}

// CalculateBPStatus classifies a reading. Order: CRISIS, STAGE_2, LOW, ELEVATED,
// NORMAL, so a high systolic is reported as high even with a low diastolic.
// Must match classifyBloodPressure in frontend/src/lib/clinical.ts; both are tested
// against clinical/bp-cases.json.
func CalculateBPStatus(systolic, diastolic int) string {
	if systolic >= 180 || diastolic >= 120 {
		return "CRISIS"
	}
	if systolic >= 140 || diastolic >= 90 {
		return "STAGE_2"
	}
	if low := currentBPLow(); systolic < low.LowSystolic || diastolic < low.LowDiastolic {
		return "LOW"
	}
	if systolic >= 120 || diastolic >= 80 {
		return "ELEVATED"
	}
	return "NORMAL"
}

// CalculateSpO2Status (issue #22): CRITICAL at or below the critical threshold
// (alert), LOW at or below the warning one (re-check, call the doctor). Must match
// classifySpO2 in frontend/src/lib/clinical.ts (clinical/vitals-bands.json).
func CalculateSpO2Status(spo2 *int) string {
	if spo2 == nil {
		return ""
	}
	t := currentOxygenFever()
	switch {
	case *spo2 <= t.SpO2Critical:
		return "CRITICAL"
	case *spo2 <= t.SpO2Warn:
		return "LOW"
	}
	return "NORMAL"
}

// CalculateTempStatus: FEVER at or above the fever threshold, LOW below 35.0 C
// (hypothermia), else NORMAL. Must match classifyTemperature in clinical.ts.
func CalculateTempStatus(c *float64) string {
	if c == nil {
		return ""
	}
	switch {
	case *c >= currentOxygenFever().FeverC:
		return "FEVER"
	case *c < 35.0:
		return "LOW"
	}
	return "NORMAL"
}

func CalculateSugarStatus(sugar *float64) string {
	if sugar == nil {
		return ""
	}
	s := *sugar
	if s < 3.9 {
		return "LOW"
	}
	if s >= 13.9 {
		return "VERY_HIGH"
	}
	if s > 7.8 {
		return "ELEVATED"
	}
	return "NORMAL"
}

func (h *VitalsHandler) ListVitals(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 30
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	rows, err := h.DB.Query(`
		SELECT v.id, v.caregiver_id, v.recorded_at, v.systolic, v.diastolic, v.pulse, v.blood_sugar, v.alertness_level, v.vital_type, v.notes,
		       c.name, c.avatar_color, v.suspect_default_sugar, COALESCE(v.fast_result, ''), COALESCE(v.last_normal_at, ''),
		       COALESCE(v.supersedes_id, ''), o.systolic, o.diastolic, o.pulse, o.blood_sugar, o.alertness_level, COALESCE(oc.name, ''),
		       COALESCE(v.correction_reason, ''), COALESCE(v.corrected_at, ''),
		       v.spo2, v.temperature_c, o.spo2, o.temperature_c
		FROM vitals_logs v
		JOIN caregivers c ON v.caregiver_id = c.id
		LEFT JOIN vitals_logs o ON o.id = v.supersedes_id
		LEFT JOIN caregivers oc ON oc.id = o.caregiver_id
		WHERE v.superseded_by IS NULL -- a corrected reading is shown inside its correction
		ORDER BY v.recorded_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch vitals"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var list []models.VitalsLog
	for rows.Next() {
		var v models.VitalsLog
		var sys, dia, pulse sql.NullInt64
		var notes, vitalType sql.NullString
		var bloodSugar sql.NullFloat64
		var recordedAtStr string
		var fastResult, lastNormal string
		var corr correctionScan
		var spo2 sql.NullInt64
		var tempC sql.NullFloat64

		if err := rows.Scan(&v.ID, &v.CaregiverID, &recordedAtStr, &sys, &dia, &pulse, &bloodSugar, &v.AlertnessLevel, &vitalType, &notes, &v.CaregiverName, &v.AvatarColor, &v.SuspectDefaultSugar, &fastResult, &lastNormal,
			&corr.id, &corr.sys, &corr.dia, &corr.pulse, &corr.sugar, &corr.alertness, &corr.by, &corr.reason, &corr.at,
			&spo2, &tempC, &corr.spo2, &corr.temp); err != nil {
			continue
		}
		if sys.Valid && dia.Valid {
			s := int(sys.Int64)
			d := int(dia.Int64)
			v.Systolic = &s
			v.Diastolic = &d
			v.BPStatus = CalculateBPStatus(s, d)
		}
		if pulse.Valid {
			p := int(pulse.Int64)
			v.Pulse = &p
		}
		if bloodSugar.Valid {
			v.BloodSugar = &bloodSugar.Float64
			v.SugarStatus = CalculateSugarStatus(v.BloodSugar)
		}
		if vitalType.Valid {
			v.VitalType = vitalType.String
		}
		normalizeAlertness(&v)
		if fastResult != "" {
			v.FastResult = json.RawMessage(fastResult)
		}
		v.LastNormalAt = sqliteToRFC3339(lastNormal)
		v.Correction = corr.toModel()
		setOxygenTemp(&v, spo2, tempC)
		if notes.Valid {
			v.Notes = notes.String
		}
		v.RecordedAt, _ = time.Parse("2006-01-02 15:04:05", recordedAtStr)
		list = append(list, v)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

type LogVitalsRequest struct {
	VitalType      string   `json:"vital_type,omitempty"` // 'bp', 'sugar', 'alertness', 'full', 'fast', 'spo2', 'temp'
	Systolic       *int     `json:"systolic,omitempty"`
	Diastolic      *int     `json:"diastolic,omitempty"`
	Pulse          *int     `json:"pulse,omitempty"`
	BloodSugar     *float64 `json:"blood_sugar,omitempty"`
	AlertnessLevel string   `json:"alertness_level,omitempty"` // 'alert', 'confused', 'restless', 'drowsy'
	// Issue #22: pulse oximeter (%) and temperature (C).
	SpO2         *int     `json:"spo2,omitempty"`
	TemperatureC *float64 `json:"temperature_c,omitempty"`
	Notes          string   `json:"notes,omitempty"`
	// F.A.S.T. stroke check (vital_type 'fast', issue #13). Any true answer is positive.
	FastResult *FastResult `json:"fast_result,omitempty"`
	// Set by the offline outbox (issue #16): resending it returns the first reading.
	ClientRequestID string `json:"client_request_id,omitempty"`
}

type FastResult struct {
	Face   bool `json:"face"`
	Arm    bool `json:"arm"`
	Speech bool `json:"speech"`
}

func (f FastResult) Positive() bool { return f.Face || f.Arm || f.Speech }

func (req *LogVitalsRequest) hasMeasurement() bool {
	return req.Systolic != nil || req.BloodSugar != nil || req.AlertnessLevel != "" || req.FastResult != nil || req.SpO2 != nil || req.TemperatureC != nil
}

func (h *VitalsHandler) LogVitals(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	if !auth.CanTakeActions(caregiver) {
		http.Error(w, `{"error":"FORBIDDEN","message":"Viewers are not permitted to log vitals"}`, http.StatusForbidden)
		return
	}

	var req LogVitalsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid vitals payload"}`, http.StatusBadRequest)
		return
	}

	// At least one metric must be provided
	if req.VitalType == "fast" && req.FastResult == nil {
		writeJSONError(w, http.StatusBadRequest, "MISSING_FAST", "a F.A.S.T. check needs fast_result {face, arm, speech}")
		return
	}
	if req.VitalType != "fast" {
		req.FastResult = nil
	}
	if !req.hasMeasurement() {
		writeJSONError(w, http.StatusBadRequest, "MISSING_FIELDS", "at least one vital measurement (blood pressure, blood sugar, oxygen, temperature or alertness) must be provided")
		return
	}

	if code, msg := validateMeasurements(&req); code != "" {
		writeJSONError(w, http.StatusBadRequest, code, msg)
		return
	}
	if len(req.ClientRequestID) > 64 {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST_ID", "client_request_id is too long")
		return
	}
	if h.replyIfDuplicateVital(w, req.ClientRequestID) {
		return
	}

	// Determine vital_type
	vitalType := req.VitalType
	if vitalType == "" {
		if req.Systolic != nil && req.BloodSugar != nil {
			vitalType = "full"
		} else if req.Systolic != nil {
			vitalType = "bp"
		} else if req.BloodSugar != nil {
			vitalType = "sugar"
		} else if req.SpO2 != nil && req.TemperatureC == nil && req.AlertnessLevel == "" {
			vitalType = "spo2"
		} else if req.TemperatureC != nil && req.SpO2 == nil && req.AlertnessLevel == "" {
			vitalType = "temp"
		} else if req.AlertnessLevel != "" {
			vitalType = "alertness"
		} else {
			vitalType = "full"
		}
	}

	// Not assessed is stored as NULL, never as "alert" (issue #10).
	var alertness *string
	if req.AlertnessLevel != "" {
		alertness = &req.AlertnessLevel
	}
	if len(req.Notes) > maxNotesLen {
		writeJSONError(w, http.StatusBadRequest, "NOTES_TOO_LONG", "notes are too long")
		return
	}

	id := uuid.New().String()
	var bpStatus string
	if req.Systolic != nil && req.Diastolic != nil {
		bpStatus = CalculateBPStatus(*req.Systolic, *req.Diastolic)
	}
	sugarStatus := CalculateSugarStatus(req.BloodSugar)
	spo2Status, tempStatus := CalculateSpO2Status(req.SpO2), CalculateTempStatus(req.TemperatureC)
	now := time.Now().UTC()

	var fastJSON *string
	if req.FastResult != nil {
		b, _ := json.Marshal(req.FastResult)
		s := string(b)
		fastJSON = &s
	}

	// A reading and its alerts (issues #12, #13) commit together, or not at all.
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to log vitals", err)
		return
	}
	// The single pooled connection belongs to tx until Commit/Rollback: use tx only.
	if _, err := tx.Exec(`
		INSERT INTO vitals_logs (id, caregiver_id, recorded_at, systolic, diastolic, pulse, blood_sugar, alertness_level, vital_type, notes, fast_result, client_request_id, spo2, temperature_c)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, caregiver.ID, now.Format(sqliteTimeLayout), req.Systolic, req.Diastolic, req.Pulse, req.BloodSugar, alertness, vitalType, req.Notes, fastJSON, nullIfEmpty(req.ClientRequestID), req.SpO2, req.TemperatureC); err != nil {
		tx.Rollback()
		if isUniqueViolation(err) && h.replyIfDuplicateVital(w, req.ClientRequestID) {
			return // the same tap arrived twice at once
		}
		internalError(w, "failed to log vitals", err)
		return
	}
	raised, err := raiseVitalAlerts(tx, id, caregiver.ID, bpStatus, &req, alertness)
	if err != nil {
		tx.Rollback()
		internalError(w, "failed to record alert", err)
		return
	}
	auditDetails := fmt.Sprintf("Type: %s", vitalType)
	if req.Systolic != nil && req.Diastolic != nil {
		auditDetails += fmt.Sprintf(", BP %d/%d (Status: %s)", *req.Systolic, *req.Diastolic, bpStatus)
	}
	if req.Pulse != nil {
		auditDetails += fmt.Sprintf(", Pulse %d", *req.Pulse)
	}
	if req.BloodSugar != nil {
		auditDetails += fmt.Sprintf(", Sugar: %.1f mmol/L (%s)", *req.BloodSugar, sugarStatus)
	}
	if alertness != nil {
		auditDetails += fmt.Sprintf(", Alertness: %s", *alertness)
	} else {
		auditDetails += ", Alertness: not assessed"
	}
	if req.FastResult != nil {
		auditDetails += fmt.Sprintf(", F.A.S.T. face=%t arm=%t speech=%t", req.FastResult.Face, req.FastResult.Arm, req.FastResult.Speech)
	}
	if req.SpO2 != nil {
		auditDetails += fmt.Sprintf(", SpO2 %d%% (%s)", *req.SpO2, spo2Status)
	}
	if req.TemperatureC != nil {
		auditDetails += fmt.Sprintf(", Temp %.1f C (%s)", *req.TemperatureC, tempStatus)
	}
	if err := audit.Record(tx, caregiver.ID, "VITAL_LOGGED", "vitals_log", id, auditDetails); err != nil {
		tx.Rollback()
		internalError(w, "failed to record audit log", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to log vitals", err)
		return
	}

	// Broadcast SSE event (a crisis carries its alert id, also as the SSE "id:").
	vitalPayload := map[string]interface{}{
		"id":              id,
		"alert_id":        raised.ID,
		"recorded_at":     now.Format(time.RFC3339),
		"vital_type":      vitalType,
		"systolic":        req.Systolic,
		"diastolic":       req.Diastolic,
		"pulse":           req.Pulse,
		"blood_sugar":     req.BloodSugar,
		"sugar_status":    sugarStatus,
		"alertness_level": alertness,
		"bp_status":       bpStatus,
		"fast_result":     req.FastResult,
		"spo2":            req.SpO2,
		"spo2_status":     spo2Status,
		"temperature_c":   req.TemperatureC,
		"temp_status":     tempStatus,
		"notes":           req.Notes,
		"caregiver_name":  caregiver.Name,
		"avatar_color":    caregiver.AvatarColor,
	}
	if raised.ID != "" {
		h.Broker.BroadcastAlert(raised.ID, "VITAL_LOGGED", actorOf(caregiver), vitalPayload)
		pushRaised(h.Push, raised, caregiver.ID)
	} else {
		h.Broker.Broadcast("VITAL_LOGGED", actorOf(caregiver), vitalPayload)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":           id,
		"vital_type":   vitalType,
		"bp_status":    bpStatus,
		"sugar_status": sugarStatus,
		"spo2_status":  spo2Status,
		"temp_status":  tempStatus,
		"recorded_at":  now.Format(time.RFC3339),
	})
}

// normalizeAlertness reports alertness as not assessed (nil) on blood-pressure-only
// and sugar-only readings: those forms never asked about it, and rows saved before
// issue #10 carry the old 'alert' default there. Stored data is left unchanged.
func normalizeAlertness(v *models.VitalsLog) {
	if v.VitalType == "bp" || v.VitalType == "sugar" {
		v.AlertnessLevel = nil
	}
}

type LastNormalRequest struct {
	LastNormalAt string `json:"last_normal_at"` // RFC3339
}

// SetLastNormal records when she was last seen normal on a F.A.S.T. check, for the
// paramedics (issue #13). Within the last 7 days, not in the future.
func (h *VitalsHandler) SetLastNormal(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers cannot change readings")
		return
	}
	var req LastNormalRequest
	var t time.Time
	err := json.NewDecoder(r.Body).Decode(&req)
	if err == nil {
		t, err = time.Parse(time.RFC3339, req.LastNormalAt)
	}
	now := time.Now().UTC()
	if err != nil || t.After(now.Add(time.Minute)) || now.Sub(t) > 7*24*time.Hour {
		writeJSONError(w, http.StatusBadRequest, "INVALID_TIME", "last_normal_at must be an RFC3339 time within the last 7 days")
		return
	}
	id := chi.URLParam(r, "id")
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to begin transaction", err)
		return
	}
	defer tx.Rollback()

	res, err := tx.Exec(`UPDATE vitals_logs SET last_normal_at = ? WHERE id = ? AND vital_type = 'fast'`, t.UTC().Format(sqliteTimeLayout), id)
	if err != nil {
		internalError(w, "failed to save time", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "F.A.S.T. check not found")
		return
	}
	if err := audit.Record(tx, caregiver.ID, "FAST_LAST_NORMAL", "vitals_log", id, "Last seen normal at "+t.UTC().Format(time.RFC3339)); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit time", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"saved"}`))
}

// validateMeasurements checks the ranges every reading must respect, new or corrected.
func validateMeasurements(req *LogVitalsRequest) (code, msg string) {
	if req.Systolic != nil || req.Diastolic != nil {
		if req.Systolic == nil || req.Diastolic == nil {
			return "INVALID_BP", "both systolic and diastolic must be provided for blood pressure"
		}
		if *req.Systolic < 50 || *req.Systolic > 300 || *req.Diastolic < 30 || *req.Diastolic > 200 {
			return "INVALID_BP", "blood pressure numbers out of clinical safety range"
		}
	}
	if req.Pulse != nil && (*req.Pulse < 30 || *req.Pulse > 220) {
		return "INVALID_PULSE", "pulse number out of clinical safety range"
	}
	if req.BloodSugar != nil && (*req.BloodSugar < 1.0 || *req.BloodSugar > 45.0) {
		return "INVALID_SUGAR", "blood sugar out of clinical safety range"
	}
	if req.SpO2 != nil && (*req.SpO2 < 50 || *req.SpO2 > 100) {
		return "INVALID_SPO2", "oxygen saturation must be 50-100%"
	}
	if req.TemperatureC != nil && (*req.TemperatureC < 30 || *req.TemperatureC > 43) {
		return "INVALID_TEMPERATURE", "temperature must be 30-43 C"
	}
	validAlertness := map[string]bool{"alert": true, "confused": true, "restless": true, "drowsy": true}
	if req.AlertnessLevel != "" && !validAlertness[req.AlertnessLevel] {
		// Never coerce an unrecognised cognitive state (e.g. a typo of "confused") into "alert".
		return "INVALID_ALERTNESS", "alertness_level must be alert, confused, restless or drowsy"
	}
	return "", ""
}

// raiseVitalAlerts records the alerts a reading triggers, most urgent first, in the
// caller's transaction (issues #12, #13, #54). The first one is pushed and carried
// by the SSE event. Emergencies go to everyone; a fever or a very high sugar goes
// to whoever is on duty (everyone if nobody is assigned).
func raiseVitalAlerts(tx *sql.Tx, vitalID, caregiverID, bpStatus string, req *LogVitalsRequest, alertness *string) (raisedAlert, error) {
	type pending struct {
		kind    string
		payload map[string]interface{}
		onDuty  bool
	}
	t := currentOxygenFever()
	var toRaise []pending
	if req.FastResult != nil && req.FastResult.Positive() {
		toRaise = append(toRaise, pending{"fast_positive", map[string]interface{}{"face": req.FastResult.Face, "arm": req.FastResult.Arm, "speech": req.FastResult.Speech}, false})
	}
	switch bpStatus {
	case "CRISIS":
		toRaise = append(toRaise, pending{"bp_crisis", map[string]interface{}{"systolic": *req.Systolic, "diastolic": *req.Diastolic}, false})
	}
	sugar := CalculateSugarStatus(req.BloodSugar)
	if sugar == "LOW" {
		toRaise = append(toRaise, pending{"sugar_low", map[string]interface{}{"blood_sugar": *req.BloodSugar}, false})
	}
	if CalculateSpO2Status(req.SpO2) == "CRITICAL" {
		p := map[string]interface{}{"spo2": *req.SpO2}
		if req.Pulse != nil {
			p["pulse"] = *req.Pulse
		}
		if req.TemperatureC != nil {
			p["temperature_c"] = *req.TemperatureC
		}
		toRaise = append(toRaise, pending{"spo2_low", p, false})
	}
	if req.TemperatureC != nil && *req.TemperatureC >= t.FeverC {
		p := map[string]interface{}{"temperature_c": *req.TemperatureC}
		if req.SpO2 != nil {
			p["spo2"] = *req.SpO2
		}
		// A high fever, or a fever with low oxygen (possible aspiration), is red.
		if *req.TemperatureC >= t.FeverHighC || (req.SpO2 != nil && *req.SpO2 <= t.SpO2Warn) {
			toRaise = append(toRaise, pending{"fever_high", p, false})
		} else {
			toRaise = append(toRaise, pending{"fever", p, true})
		}
	}
	switch bpStatus {
	case "LOW":
		toRaise = append(toRaise, pending{"bp_low", map[string]interface{}{"systolic": *req.Systolic, "diastolic": *req.Diastolic}, false})
	}
	if alertness != nil && (*alertness == "confused" || *alertness == "drowsy") {
		toRaise = append(toRaise, pending{"alertness", map[string]interface{}{"level": *alertness}, false})
	}
	if sugar == "VERY_HIGH" {
		toRaise = append(toRaise, pending{"sugar_very_high", map[string]interface{}{"blood_sugar": *req.BloodSugar}, true})
	}
	var first raisedAlert
	onDuty, lookedUp := "", false
	for _, a := range toRaise {
		target := ""
		if a.onDuty {
			if !lookedUp {
				onDuty, lookedUp = onDutyCaregiverID(tx), true
			}
			target = onDuty
		}
		aid, err := recordAlertFor(tx, a.kind, vitalID, caregiverID, target, a.payload)
		if err != nil {
			return raisedAlert{}, err
		}
		if first.ID == "" {
			first = raisedAlert{ID: aid, Kind: a.kind, Target: target}
		}
	}
	return first, nil
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// replyIfDuplicateVital answers 200 with the first reading when this
// client_request_id was already saved (an outbox resend after a lost response).
func (h *VitalsHandler) replyIfDuplicateVital(w http.ResponseWriter, clientRequestID string) bool {
	if clientRequestID == "" {
		return false
	}
	var id, vitalType, recordedAt string
	var sys, dia sql.NullInt64
	if err := h.DB.QueryRow(`SELECT id, COALESCE(vital_type, ''), recorded_at, systolic, diastolic FROM vitals_logs WHERE client_request_id = ?`,
		clientRequestID).Scan(&id, &vitalType, &recordedAt, &sys, &dia); err != nil {
		return false
	}
	var bpStatus string
	if sys.Valid && dia.Valid {
		bpStatus = CalculateBPStatus(int(sys.Int64), int(dia.Int64))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id": id, "vital_type": vitalType, "bp_status": bpStatus, "recorded_at": sqliteToRFC3339(recordedAt), "duplicate": true,
	})
	return true
}

// setOxygenTemp fills SpO2 and temperature with their classification (issue #22).
func setOxygenTemp(v *models.VitalsLog, spo2 sql.NullInt64, tempC sql.NullFloat64) {
	if spo2.Valid {
		n := int(spo2.Int64)
		v.SpO2 = &n
		v.SpO2Status = CalculateSpO2Status(v.SpO2)
	}
	if tempC.Valid {
		t := tempC.Float64
		v.TemperatureC = &t
		v.TempStatus = CalculateTempStatus(v.TemperatureC)
	}
}
