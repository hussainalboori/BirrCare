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

func (h *VitalsHandler) ListVitals(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 30
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	rows, err := h.DB.Query(`
		SELECT v.id, v.caregiver_id, v.recorded_at, v.systolic, v.diastolic, v.pulse, v.blood_sugar, v.alertness_level, v.notes,
		       c.name, c.avatar_color
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
		var notes sql.NullString
		var bloodSugar sql.NullFloat64
		var recordedAtStr string

		if err := rows.Scan(&v.ID, &v.CaregiverID, &recordedAtStr, &v.Systolic, &v.Diastolic, &v.Pulse, &bloodSugar, &v.AlertnessLevel, &notes, &v.CaregiverName, &v.AvatarColor); err != nil {
			continue
		}
		if notes.Valid {
			v.Notes = notes.String
		}
		if bloodSugar.Valid {
			v.BloodSugar = &bloodSugar.Float64
		}
		v.RecordedAt, _ = time.Parse("2006-01-02 15:04:05", recordedAtStr)
		v.BPStatus = CalculateBPStatus(v.Systolic, v.Diastolic)
		list = append(list, v)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

type LogVitalsRequest struct {
	Systolic       int      `json:"systolic"`
	Diastolic      int      `json:"diastolic"`
	Pulse          int      `json:"pulse"`
	BloodSugar     *float64 `json:"blood_sugar,omitempty"`
	AlertnessLevel string   `json:"alertness_level"` // 'alert', 'confused', 'restless', 'drowsy'
	Notes          string   `json:"notes,omitempty"`
}

func (h *VitalsHandler) LogVitals(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}

	var req LogVitalsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid vitals payload"}`, http.StatusBadRequest)
		return
	}

	if req.Systolic < 50 || req.Systolic > 300 || req.Diastolic < 30 || req.Diastolic > 200 || req.Pulse < 30 || req.Pulse > 220 {
		http.Error(w, `{"error":"vitals numbers out of clinical safety range"}`, http.StatusBadRequest)
		return
	}

	validAlertness := map[string]bool{"alert": true, "confused": true, "restless": true, "drowsy": true}
	if !validAlertness[req.AlertnessLevel] {
		req.AlertnessLevel = "alert"
	}

	id := uuid.New().String()
	bpStatus := CalculateBPStatus(req.Systolic, req.Diastolic)
	now := time.Now().UTC()

	_, err := h.DB.Exec(`
		INSERT INTO vitals_logs (id, caregiver_id, recorded_at, systolic, diastolic, pulse, blood_sugar, alertness_level, notes)
		VALUES (?, ?, datetime('now'), ?, ?, ?, ?, ?, ?)
	`, id, caregiver.ID, req.Systolic, req.Diastolic, req.Pulse, req.BloodSugar, req.AlertnessLevel, req.Notes)

	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to log vitals: %v"}`, err), http.StatusInternalServerError)
		return
	}

	audit.Log(h.DB, caregiver.ID, "VITAL_LOGGED", "vitals_log", id,
		fmt.Sprintf("BP %d/%d, Pulse %d, Alertness: %s (Status: %s)", req.Systolic, req.Diastolic, req.Pulse, req.AlertnessLevel, bpStatus))

	// Broadcast SSE event
	h.Broker.Broadcast("VITAL_LOGGED", models.ActorInfo{
		ID:          caregiver.ID,
		Name:        caregiver.Name,
		AvatarColor: caregiver.AvatarColor,
	}, map[string]interface{}{
		"id":              id,
		"recorded_at":     now.Format(time.RFC3339),
		"systolic":        req.Systolic,
		"diastolic":       req.Diastolic,
		"pulse":           req.Pulse,
		"blood_sugar":     req.BloodSugar,
		"alertness_level": req.AlertnessLevel,
		"bp_status":       bpStatus,
		"notes":           req.Notes,
		"caregiver_name":  caregiver.Name,
		"avatar_color":    caregiver.AvatarColor,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":          id,
		"bp_status":   bpStatus,
		"recorded_at": now.Format(time.RFC3339),
	})
}
