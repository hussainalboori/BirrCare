package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
)

// The medicines page lists the day in clock order (Night first) unless the Super
// Admin chooses "shift": the list then starts at the current shift's start time.
const (
	medOrderKey   = "med_order"
	medOrderClock = "clock"
	medOrderShift = "shift"
)

func readMedOrder(q querier) (string, error) {
	var v string
	err := q.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, medOrderKey).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && v != medOrderShift) {
		return medOrderClock, nil
	}
	return v, err
}

// medOrderFrom is the "HH:MM" the medicines list starts from, or "" for clock order.
func medOrderFrom(q querier, now time.Time) (string, error) {
	order, err := readMedOrder(q)
	if err != nil || order != medOrderShift {
		return "", err
	}
	shift, _, err := findCurrentShift(q, now.In(ClinicalLocation()))
	if err != nil || shift == nil {
		return "", err
	}
	return shift.StartTime, nil
}

type medOrderBody struct {
	Order string `json:"order"`
}

// GetMedOrder reports the medicines list order chosen in the shift settings.
func (h *ShiftsHandler) GetMedOrder(w http.ResponseWriter, r *http.Request) {
	order, err := readMedOrder(h.DB)
	if err != nil {
		internalError(w, "failed to read the medicines order", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(medOrderBody{Order: order})
}

// PutMedOrder changes it (Super Admin, like the rest of the shift settings).
func (h *ShiftsHandler) PutMedOrder(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageShifts(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only Super Admin can change the medicines order")
		return
	}
	var req medOrderBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Order != medOrderClock && req.Order != medOrderShift) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_ORDER", "order must be clock or shift")
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		internalError(w, "failed to start the transaction", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO app_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, medOrderKey, req.Order); err != nil {
		internalError(w, "failed to save the medicines order", err)
		return
	}
	if err := audit.Record(tx, caregiver.ID, "MED_ORDER_CHANGED", "app_settings", medOrderKey, "Medicines list order: "+req.Order); err != nil {
		internalError(w, "failed to audit the medicines order", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to save the medicines order", err)
		return
	}

	if h.Broker != nil {
		h.Broker.Broadcast("SHIFT_UPDATED", models.ActorInfo{ID: caregiver.ID, Name: caregiver.Name, AvatarColor: caregiver.AvatarColor}, map[string]string{"med_order": req.Order})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(medOrderBody{Order: req.Order})
}
