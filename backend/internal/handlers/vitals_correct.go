package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Corrections (issue #15). A typo such as 190/100 instead of 119/80 is fixed by a
// new row that supersedes the original; the original keeps its values (a trigger
// refuses in-place updates) and stays visible as "original: 190/100". Lists and the
// doctor report use the correction. If the original raised alerts, they stop
// replaying and everyone gets a short "correction" alert instead.

const correctionWindow = 24 * time.Hour

type CorrectVitalsRequest struct {
	LogVitalsRequest
	Reason string `json:"reason"`
}

// correctionScan holds the joined original-reading columns of a list query.
type correctionScan struct {
	id, by, reason, at string
	sys, dia, pulse    sql.NullInt64
	sugar, temp        sql.NullFloat64
	alertness          sql.NullString
	spo2               sql.NullInt64
}

func (c correctionScan) toModel() *models.VitalCorrection {
	if c.id == "" {
		return nil
	}
	m := &models.VitalCorrection{OriginalID: c.id, OriginalLoggedByName: c.by, Reason: c.reason, CorrectedAt: sqliteToRFC3339(c.at)}
	if c.sys.Valid && c.dia.Valid {
		s, d := int(c.sys.Int64), int(c.dia.Int64)
		m.OriginalSystolic, m.OriginalDiastolic = &s, &d
	}
	if c.pulse.Valid {
		p := int(c.pulse.Int64)
		m.OriginalPulse = &p
	}
	if c.sugar.Valid {
		v := c.sugar.Float64
		m.OriginalBloodSugar = &v
	}
	if c.alertness.Valid {
		a := c.alertness.String
		m.OriginalAlertness = &a
	}
	if c.spo2.Valid {
		v := int(c.spo2.Int64)
		m.OriginalSpO2 = &v
	}
	if c.temp.Valid {
		v := c.temp.Float64
		m.OriginalTemperatureC = &v
	}
	return m
}

func (h *VitalsHandler) CorrectVitals(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers cannot correct readings")
		return
	}
	origID := chi.URLParam(r, "id")
	var req CorrectVitalsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Reason == "" || len(req.Reason) > maxOverrideReasonLen {
		writeJSONError(w, http.StatusBadRequest, "REASON_REQUIRED", "say why the reading is being corrected (e.g. typo)")
		return
	}
	if !req.hasMeasurement() {
		writeJSONError(w, http.StatusBadRequest, "MISSING_FIELDS", "the corrected reading needs blood pressure, sugar, oxygen, temperature or alertness")
		return
	}
	if code, msg := validateMeasurements(&req.LogVitalsRequest); code != "" {
		writeJSONError(w, http.StatusBadRequest, code, msg)
		return
	}
	if len(req.Notes) > maxNotesLen {
		writeJSONError(w, http.StatusBadRequest, "NOTES_TOO_LONG", "notes are too long")
		return
	}

	var loggedBy, recordedAt, vitalType string
	var supersededBy sql.NullString
	var oSys, oDia sql.NullInt64
	err := h.DB.QueryRowContext(r.Context(), `SELECT caregiver_id, recorded_at, COALESCE(vital_type, 'full'), superseded_by, systolic, diastolic
		FROM vitals_logs WHERE id = ?`, origID).Scan(&loggedBy, &recordedAt, &vitalType, &supersededBy, &oSys, &oDia)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "reading not found")
		return
	} else if err != nil {
		internalError(w, "failed to load reading", err)
		return
	}
	if supersededBy.Valid {
		writeJSONError(w, http.StatusConflict, "ALREADY_CORRECTED", "this reading was already corrected; correct the newer one")
		return
	}
	if vitalType == "fast" {
		writeJSONError(w, http.StatusBadRequest, "NOT_CORRECTABLE", "a F.A.S.T. check can't be corrected; do a new check")
		return
	}
	now := time.Now().UTC().Truncate(time.Second)
	recorded, _ := time.Parse(sqliteTimeLayout, recordedAt)
	if !(loggedBy == me.ID && now.Sub(recorded) <= correctionWindow) && !auth.CanManageMedications(me) {
		writeJSONError(w, http.StatusForbidden, "CORRECTION_NOT_ALLOWED",
			"Only the person who logged it can correct it within 24 hours; after that, ask an admin")
		return
	}

	var bpStatus string
	if req.Systolic != nil && req.Diastolic != nil {
		bpStatus = CalculateBPStatus(*req.Systolic, *req.Diastolic)
	}
	var alertness *string
	if req.AlertnessLevel != "" {
		alertness = &req.AlertnessLevel
	}
	newID := uuid.New().String()

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to correct reading", err)
		return
	}
	// The single pooled connection belongs to tx until Commit/Rollback: use tx only.
	fail := func(msg string, err error) {
		tx.Rollback()
		internalError(w, msg, err)
	}
	// Same measurement time as the original: it is the same reading, corrected.
	if _, err := tx.Exec(`INSERT INTO vitals_logs (id, caregiver_id, recorded_at, systolic, diastolic, pulse, blood_sugar, alertness_level, vital_type, notes,
		supersedes_id, correction_reason, corrected_at, spo2, temperature_c) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		newID, me.ID, recordedAt, req.Systolic, req.Diastolic, req.Pulse, req.BloodSugar, alertness, vitalType, req.Notes,
		origID, req.Reason, now.Format(sqliteTimeLayout), req.SpO2, req.TemperatureC); err != nil {
		fail("failed to save correction", err)
		return
	}
	res, err := tx.Exec(`UPDATE vitals_logs SET superseded_by = ? WHERE id = ? AND superseded_by IS NULL`, newID, origID)
	if err != nil {
		fail("failed to save correction", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		tx.Rollback()
		writeJSONError(w, http.StatusConflict, "ALREADY_CORRECTED", "this reading was already corrected")
		return
	}

	// Alerts the typo raised stop replaying; everyone gets one "correction" alert.
	var hadAlert int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM alerts WHERE ref_id = ?`, origID).Scan(&hadAlert); err != nil {
		fail("failed to check alerts", err)
		return
	}
	var correctionAlert string
	if hadAlert > 0 {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO alert_acks (alert_id, caregiver_id, acked_at)
			SELECT a.id, c.id, ? FROM alerts a, caregivers c WHERE a.ref_id = ? AND c.is_active = 1`, now.Format(sqliteTimeLayout), origID); err != nil {
			fail("failed to retire alerts", err)
			return
		}
		payload := map[string]interface{}{"reason": req.Reason, "original_id": origID}
		if oSys.Valid && oDia.Valid {
			payload["original_systolic"], payload["original_diastolic"] = oSys.Int64, oDia.Int64
		}
		if req.Systolic != nil {
			payload["systolic"], payload["diastolic"] = *req.Systolic, *req.Diastolic
		}
		if correctionAlert, err = recordAlert(tx, "correction", newID, me.ID, payload); err != nil {
			fail("failed to record alert", err)
			return
		}
	}
	// The corrected reading raises its own alerts if it is still a crisis, low, etc.
	newAlert, newKind, err := raiseVitalAlerts(tx, newID, me.ID, bpStatus, &req.LogVitalsRequest, alertness)
	if err != nil {
		fail("failed to record alert", err)
		return
	}
	details := fmt.Sprintf("Corrected reading %s -> %s. Reason: %s", origID, newID, req.Reason)
	if oSys.Valid && req.Systolic != nil {
		details = fmt.Sprintf("Corrected %d/%d to %d/%d (%s -> %s). Reason: %s", oSys.Int64, oDia.Int64, *req.Systolic, *req.Diastolic, origID, newID, req.Reason)
	}
	if err := audit.Record(tx, me.ID, "VITAL_CORRECTED", "vitals_log", newID, details); err != nil {
		fail("failed to write audit trail", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to correct reading", err)
		return
	}

	payload := map[string]interface{}{
		"id": newID, "original_id": origID, "reason": req.Reason, "bp_status": bpStatus,
		"systolic": req.Systolic, "diastolic": req.Diastolic, "alert_id": correctionAlert,
	}
	switch {
	case newAlert != "":
		payload["alert_id"] = newAlert
		h.Broker.BroadcastAlert(newAlert, "VITAL_CORRECTED", actorOf(me), payload)
		h.Push.NotifyAsync(newAlert, newKind, me.ID)
	case correctionAlert != "":
		h.Broker.BroadcastAlert(correctionAlert, "VITAL_CORRECTED", actorOf(me), payload)
		h.Push.NotifyAsync(correctionAlert, "correction", me.ID)
	default:
		h.Broker.Broadcast("VITAL_CORRECTED", actorOf(me), payload)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{"id": newID, "original_id": origID, "bp_status": bpStatus})
}
