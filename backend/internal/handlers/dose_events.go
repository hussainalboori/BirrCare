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

// Issue #9: "she vomited 10 minutes after the pill" is a follow-up on the dose that
// was given, not a new dose. The original row keeps the slot (so the double-dose
// and interval locks still hold) and the card shows "Taken → vomited 10 min later"
// with a warning to call the doctor. The app never suggests a re-dose.

type DoseEventRequest struct {
	Type         string `json:"type"` // 'vomited'
	MinutesAfter *int   `json:"minutes_after,omitempty"`
	Notes        string `json:"notes,omitempty"`
}

func (h *MedicationHandler) AddDoseEvent(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		writeJSONError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Select your profile and enter your PIN")
		return
	}
	if !auth.CanTakeActions(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers cannot change dose entries")
		return
	}
	logID := chi.URLParam(r, "id")

	var req DoseEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	if req.Type != "vomited" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_EVENT", "type must be 'vomited'")
		return
	}
	if req.MinutesAfter != nil && (*req.MinutesAfter < 0 || *req.MinutesAfter > 1440) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_MINUTES", "minutes_after must be 0–1440")
		return
	}
	req.Notes = strings.TrimSpace(req.Notes)
	if len(req.Notes) > maxNotesLen {
		writeJSONError(w, http.StatusBadRequest, "NOTES_TOO_LONG", "notes are too long")
		return
	}

	var status, medID, slot, date, nameAr, nameEn string
	var voidedAt sql.NullString
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT l.status, l.voided_at, l.medication_id, l.slot, l.scheduled_date, m.name_ar, m.name_en
		FROM medication_logs l JOIN medications m ON m.id = l.medication_id WHERE l.id = ?`, logID).
		Scan(&status, &voidedAt, &medID, &slot, &date, &nameAr, &nameEn)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "LOG_NOT_FOUND", "dose entry not found")
		return
	} else if err != nil {
		internalError(w, "failed to look up dose entry", err)
		return
	}
	if voidedAt.Valid {
		writeJSONError(w, http.StatusBadRequest, "LOG_VOIDED", "this entry was cancelled")
		return
	}
	if status != "taken" && status != "partial" {
		writeJSONError(w, http.StatusBadRequest, "NOT_GIVEN", "only a dose that was given can be followed by vomiting")
		return
	}

	eventID := uuid.New().String()
	now := time.Now().UTC().Truncate(time.Second)
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to record event", err)
		return
	}
	// The single pooled connection belongs to tx until Commit/Rollback: use tx only.
	if _, err := tx.Exec(`INSERT INTO medication_log_events (id, log_id, type, minutes_after, caregiver_id, notes, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		eventID, logID, req.Type, req.MinutesAfter, caregiver.ID, req.Notes, now.Format(sqliteTimeLayout)); err != nil {
		tx.Rollback()
		if isUniqueViolation(err) {
			writeJSONError(w, http.StatusConflict, "EVENT_ALREADY_LOGGED", "vomiting was already recorded for this dose")
			return
		}
		internalError(w, "failed to record event", err)
		return
	}
	mins := "unknown"
	if req.MinutesAfter != nil {
		mins = fmt.Sprintf("%d", *req.MinutesAfter)
	}
	if err := audit.Record(tx, caregiver.ID, "MED_EVENT", "medication_log", logID,
		fmt.Sprintf("Dose of %s (slot %s, date %s) %s %s min after", medID, slot, date, req.Type, mins)); err != nil {
		tx.Rollback()
		internalError(w, "failed to write audit trail", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit", err)
		return
	}

	h.Broker.Broadcast("MED_EVENT", actorOf(caregiver), map[string]interface{}{
		"log_id":         logID,
		"event_id":       eventID,
		"type":           req.Type,
		"minutes_after":  req.MinutesAfter,
		"medication_id":  medID,
		"name_ar":        nameAr,
		"name_en":        nameEn,
		"slot":           slot,
		"scheduled_date": date,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(models.MedicationLogEvent{
		ID: eventID, Type: req.Type, MinutesAfter: req.MinutesAfter, Notes: req.Notes,
		CaregiverName: caregiver.Name, CreatedAt: now.Format(time.RFC3339),
	})
}

// logEventsForDate returns the follow-up events of every dose scheduled on date, by log id.
func logEventsForDate(db *sql.DB, date string) (map[string][]models.MedicationLogEvent, error) {
	rows, err := db.Query(`
		SELECT e.log_id, e.id, e.type, e.minutes_after, COALESCE(e.notes, ''), c.name, e.created_at
		FROM medication_log_events e
		JOIN medication_logs l ON l.id = e.log_id
		JOIN caregivers c ON c.id = e.caregiver_id
		WHERE l.scheduled_date = ? ORDER BY e.created_at`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]models.MedicationLogEvent{}
	for rows.Next() {
		var logID, createdAt string
		var mins sql.NullInt64
		var e models.MedicationLogEvent
		if err := rows.Scan(&logID, &e.ID, &e.Type, &mins, &e.Notes, &e.CaregiverName, &createdAt); err != nil {
			return nil, err
		}
		if mins.Valid {
			m := int(mins.Int64)
			e.MinutesAfter = &m
		}
		e.CreatedAt = sqliteToRFC3339(createdAt)
		out[logID] = append(out[logID], e)
	}
	return out, rows.Err()
}
