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
	"birrcare/backend/internal/sse"
	"github.com/google/uuid"
)

type VitalsHandler struct {
	DB     *sql.DB
	Broker *sse.Broker
}

func CalculateBPStatus(systolic, diastolic int) string {
	if systolic >= 180 || diastolic >= 120 {
		return "CRISIS"
	}
	if systolic >= 140 || diastolic >= 90 {
		return "STAGE_2"
	}
	if systolic >= 120 || diastolic >= 80 {
		return "ELEVATED"
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
		       c.name, c.avatar_color, v.suspect_default_sugar
		FROM vitals_logs v
		JOIN caregivers c ON v.caregiver_id = c.id
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

		if err := rows.Scan(&v.ID, &v.CaregiverID, &recordedAtStr, &sys, &dia, &pulse, &bloodSugar, &v.AlertnessLevel, &vitalType, &notes, &v.CaregiverName, &v.AvatarColor, &v.SuspectDefaultSugar); err != nil {
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
	VitalType      string   `json:"vital_type,omitempty"` // 'bp', 'sugar', 'alertness', 'full'
	Systolic       *int     `json:"systolic,omitempty"`
	Diastolic      *int     `json:"diastolic,omitempty"`
	Pulse          *int     `json:"pulse,omitempty"`
	BloodSugar     *float64 `json:"blood_sugar,omitempty"`
	AlertnessLevel string   `json:"alertness_level,omitempty"` // 'alert', 'confused', 'restless', 'drowsy'
	Notes          string   `json:"notes,omitempty"`
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
	if req.Systolic == nil && req.BloodSugar == nil && req.AlertnessLevel == "" {
		http.Error(w, `{"error":"at least one vital measurement (blood pressure, blood sugar, or alertness) must be provided"}`, http.StatusBadRequest)
		return
	}

	// Validate Blood Pressure if provided
	if req.Systolic != nil || req.Diastolic != nil {
		if req.Systolic == nil || req.Diastolic == nil {
			http.Error(w, `{"error":"both systolic and diastolic must be provided for blood pressure"}`, http.StatusBadRequest)
			return
		}
		if *req.Systolic < 50 || *req.Systolic > 300 || *req.Diastolic < 30 || *req.Diastolic > 200 {
			http.Error(w, `{"error":"blood pressure numbers out of clinical safety range"}`, http.StatusBadRequest)
			return
		}
	}

	// Validate Pulse if provided
	if req.Pulse != nil {
		if *req.Pulse < 30 || *req.Pulse > 220 {
			http.Error(w, `{"error":"pulse number out of clinical safety range"}`, http.StatusBadRequest)
			return
		}
	}

	// Validate Blood Sugar if provided
	if req.BloodSugar != nil {
		if *req.BloodSugar < 1.0 || *req.BloodSugar > 45.0 {
			http.Error(w, `{"error":"blood sugar out of clinical safety range"}`, http.StatusBadRequest)
			return
		}
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
		} else if req.AlertnessLevel != "" {
			vitalType = "alertness"
		} else {
			vitalType = "full"
		}
	}

	validAlertness := map[string]bool{"alert": true, "confused": true, "restless": true, "drowsy": true}
	if req.AlertnessLevel != "" && !validAlertness[req.AlertnessLevel] {
		// Never coerce an unrecognised cognitive state (e.g. a typo of "confused") into "alert".
		writeJSONError(w, http.StatusBadRequest, "INVALID_ALERTNESS", "alertness_level must be alert, confused, restless or drowsy")
		return
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
	now := time.Now().UTC()

	_, err := h.DB.Exec(`
		INSERT INTO vitals_logs (id, caregiver_id, recorded_at, systolic, diastolic, pulse, blood_sugar, alertness_level, vital_type, notes)
		VALUES (?, ?, datetime('now'), ?, ?, ?, ?, ?, ?, ?)
	`, id, caregiver.ID, req.Systolic, req.Diastolic, req.Pulse, req.BloodSugar, alertness, vitalType, req.Notes)

	if err != nil {
		internalError(w, "failed to log vitals", err)
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
	audit.Log(h.DB, caregiver.ID, "VITAL_LOGGED", "vitals_log", id, auditDetails)

	// Broadcast SSE event
	h.Broker.Broadcast("VITAL_LOGGED", models.ActorInfo{
		ID:          caregiver.ID,
		Name:        caregiver.Name,
		AvatarColor: caregiver.AvatarColor,
	}, map[string]interface{}{
		"id":              id,
		"recorded_at":     now.Format(time.RFC3339),
		"vital_type":      vitalType,
		"systolic":        req.Systolic,
		"diastolic":       req.Diastolic,
		"pulse":           req.Pulse,
		"blood_sugar":     req.BloodSugar,
		"sugar_status":    sugarStatus,
		"alertness_level": alertness,
		"bp_status":       bpStatus,
		"notes":           req.Notes,
		"caregiver_name":  caregiver.Name,
		"avatar_color":    caregiver.AvatarColor,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":           id,
		"vital_type":   vitalType,
		"bp_status":    bpStatus,
		"sugar_status": sugarStatus,
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
