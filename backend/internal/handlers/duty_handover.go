package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/push"
	"birrcare/backend/internal/sse"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Issue #18: "pass duty". The caregiver on duty hands the current shift to a
// sibling with a few tags, in one transaction: the note, the moved assignment,
// the duty record, an alert for the recipient and the audit row. The recipient
// taps "Accept duty"; if nobody accepts within 15 minutes, the sender is alerted.

// DutyAcceptWindow is how long a passed duty may wait before the sender is told.
const DutyAcceptWindow = 15 * time.Minute

type PassDutyRequest struct {
	ToCaregiverID string   `json:"to_caregiver_id"`
	Tags          []string `json:"tags,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	UrgentFlag    bool     `json:"urgent_flag,omitempty"`
}

var errNotOnDuty = errors.New("not on duty")

func (h *ShiftsHandler) PassDuty(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "viewers cannot pass duty")
		return
	}
	var req PassDutyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ToCaregiverID == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "to_caregiver_id is required")
		return
	}
	req.Summary = strings.TrimSpace(req.Summary)
	if len(req.Summary) > 10000 {
		writeJSONError(w, http.StatusBadRequest, "SUMMARY_TOO_LONG", "summary is too long")
		return
	}
	tags, tagUrgent, err := validateTags(req.Tags)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "UNKNOWN_TAG", err.Error())
		return
	}
	urgent := req.UrgentFlag || tagUrgent
	urgentVal := 0
	if urgent {
		urgentVal = 1
	}
	if req.ToCaregiverID == me.ID {
		writeJSONError(w, http.StatusBadRequest, "SAME_CAREGIVER", "you are already on duty")
		return
	}
	var toName, toRole string
	if err := h.DB.QueryRow(`SELECT name, role FROM caregivers WHERE id = ? AND is_active = 1`, req.ToCaregiverID).Scan(&toName, &toRole); err != nil {
		writeJSONError(w, http.StatusNotFound, "CAREGIVER_NOT_FOUND", "caregiver not found")
		return
	}
	if toRole == models.RoleViewer {
		writeJSONError(w, http.StatusBadRequest, "CANNOT_TAKE_DUTY", "a viewer cannot be on duty")
		return
	}

	now := time.Now()
	nowStr := now.UTC().Format(sqliteTimeLayout)
	noteID, dutyID := uuid.New().String(), uuid.New().String()
	var alertID, shiftID, shiftDate string

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to pass duty", err)
		return
	}
	defer tx.Rollback()
	err = func() error {
		// Checked inside the transaction so two siblings can't both pass the same shift.
		shift, date, err := findCurrentShift(tx, now)
		if err != nil {
			return err
		}
		if shift == nil {
			return errNotOnDuty
		}
		var assignmentID, onDuty string
		if err := tx.QueryRow(`SELECT id, caregiver_id FROM shift_assignments WHERE shift_id = ? AND date = ?`, shift.ID, date).
			Scan(&assignmentID, &onDuty); err != nil || onDuty != me.ID {
			return errNotOnDuty
		}
		shiftID, shiftDate = shift.ID, date
		if _, err := tx.Exec(`INSERT INTO handover_notes (id, caregiver_id, created_at, shift_type, summary, urgent_flag, tags)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, noteID, me.ID, nowStr, shift.ID, req.Summary, urgentVal, tagsJSON(tags)); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE shift_assignments SET caregiver_id = ? WHERE id = ?`, req.ToCaregiverID, assignmentID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO duty_handovers (id, note_id, assignment_id, shift_id, date, from_caregiver_id, to_caregiver_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, dutyID, noteID, assignmentID, shift.ID, date, me.ID, req.ToCaregiverID, nowStr); err != nil {
			return err
		}
		if alertID, err = recordAlertFor(tx, "duty_handover", dutyID, me.ID, req.ToCaregiverID, map[string]interface{}{
			"duty_handover_id": dutyID, "from_name": me.Name, "to_caregiver_id": req.ToCaregiverID,
			"tags": tags, "summary": req.Summary, "urgent_flag": urgent,
		}); err != nil {
			return err
		}
		return audit.Record(tx, me.ID, "DUTY_PASSED", "shift_assignment", assignmentID,
			fmt.Sprintf("Passed duty (%s, %s) to %s; tags=%v; %s", shift.ID, date, toName, tags, req.Summary))
	}()
	if errors.Is(err, errNotOnDuty) {
		writeJSONError(w, http.StatusForbidden, "NOT_ON_DUTY", "only the caregiver on duty now can pass it on")
		return
	}
	if err != nil {
		internalError(w, "failed to pass duty", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to pass duty", err)
		return
	}
	h.Push.NotifyCaregiverAsync(alertID, "duty_handover", req.ToCaregiverID)

	actor := models.ActorInfo{ID: me.ID, Name: me.Name, AvatarColor: me.AvatarColor}
	createdAt := now.UTC().Format(time.RFC3339)
	h.Broker.Broadcast("NOTE_ADDED", actor, map[string]interface{}{
		"id": noteID, "created_at": createdAt, "shift_type": shiftID, "summary": req.Summary, "urgent_flag": urgent,
		"tags": tags, "caregiver_name": me.Name, "avatar_color": me.AvatarColor, "passed_to_name": toName,
	})
	h.Broker.Broadcast("DUTY_PASSED", actor, map[string]interface{}{
		"duty_handover_id": dutyID, "shift_id": shiftID, "date": shiftDate,
		"from_caregiver_id": me.ID, "to_caregiver_id": req.ToCaregiverID, "to_name": toName,
		"alert_id": alertID, // lets the recipient's phone show "Accept duty" at once
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success", "duty_handover_id": dutyID, "note_id": noteID, "created_at": createdAt,
		"to_caregiver_id": req.ToCaregiverID, "to_name": toName, "urgent_flag": urgent,
	})
}

// AcceptDuty is the recipient's "Accept duty". Idempotent.
func (h *ShiftsHandler) AcceptDuty(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	id := chi.URLParam(r, "id")
	var to, from, accepted string
	if err := h.DB.QueryRow(`SELECT to_caregiver_id, from_caregiver_id, COALESCE(accepted_at, '') FROM duty_handovers WHERE id = ?`, id).
		Scan(&to, &from, &accepted); err != nil {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "duty handover not found")
		return
	}
	if to != me.ID {
		writeJSONError(w, http.StatusForbidden, "NOT_YOURS", "this duty was passed to someone else")
		return
	}
	if accepted == "" {
		accepted = time.Now().UTC().Format(sqliteTimeLayout)
		tx, err := h.DB.BeginTx(r.Context(), nil)
		if err != nil {
			internalError(w, "failed to accept duty", err)
			return
		}
		defer tx.Rollback()
		err = func() error {
			if _, err := tx.Exec(`UPDATE duty_handovers SET accepted_at = ? WHERE id = ? AND accepted_at IS NULL`, accepted, id); err != nil {
				return err
			}
			// Clear the alert for the recipient, and any "not accepted yet" alert for the sender.
			if _, err := tx.Exec(`INSERT OR IGNORE INTO alert_acks (alert_id, caregiver_id, acked_at)
				SELECT id, target_caregiver_id, ? FROM alerts WHERE ref_id = ? AND kind IN ('duty_handover', 'duty_unaccepted')`, accepted, id); err != nil {
				return err
			}
			return audit.Record(tx, me.ID, "DUTY_ACCEPTED", "duty_handover", id, "Accepted duty")
		}()
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			internalError(w, "failed to accept duty", err)
			return
		}
		h.Broker.Broadcast("DUTY_ACCEPTED", models.ActorInfo{ID: me.ID, Name: me.Name, AvatarColor: me.AvatarColor},
			map[string]interface{}{"duty_handover_id": id, "from_caregiver_id": from, "to_caregiver_id": me.ID})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "accepted_at": sqliteToRFC3339(accepted)})
}

// RemindUnacceptedDuties alerts the sender of every duty that has waited longer
// than DutyAcceptWindow without being accepted. Each duty is reminded once.
func RemindUnacceptedDuties(ctx context.Context, db *sql.DB, notifier *push.Notifier, broker *sse.Broker, now time.Time) error {
	cutoff := now.UTC().Add(-DutyAcceptWindow).Format(sqliteTimeLayout)
	type due struct{ id, from, to, toName string }
	rows, err := db.QueryContext(ctx, `
		SELECT d.id, d.from_caregiver_id, d.to_caregiver_id, c.name
		FROM duty_handovers d JOIN caregivers c ON c.id = d.to_caregiver_id
		WHERE d.accepted_at IS NULL AND d.reminded_at IS NULL AND d.created_at <= ?`, cutoff)
	if err != nil {
		return err
	}
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.from, &d.to, &d.toName); err == nil {
			list = append(list, d)
		}
	}
	rows.Close()
	for _, d := range list {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var alertID string
		res, err := tx.Exec(`UPDATE duty_handovers SET reminded_at = ? WHERE id = ? AND reminded_at IS NULL AND accepted_at IS NULL`,
			now.UTC().Format(sqliteTimeLayout), d.id)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 1 {
				alertID, err = recordAlertFor(tx, "duty_unaccepted", d.id, d.to, d.from, map[string]interface{}{
					"duty_handover_id": d.id, "to_name": d.toName, "minutes": int(DutyAcceptWindow.Minutes()),
				})
			}
		}
		if err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
		if err != nil {
			return err
		}
		if alertID != "" {
			notifier.NotifyCaregiverAsync(alertID, "duty_unaccepted", d.from)
			if broker != nil {
				broker.Broadcast("DUTY_REMINDER", models.ActorInfo{ID: d.to, Name: d.toName}, map[string]interface{}{
					"duty_handover_id": d.id, "from_caregiver_id": d.from, "alert_id": alertID,
				})
			}
		}
	}
	return nil
}

// StartDutyReminders checks for unaccepted duties every minute until ctx ends.
func StartDutyReminders(ctx context.Context, db *sql.DB, notifier *push.Notifier, broker *sse.Broker) {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				if err := RemindUnacceptedDuties(ctx, db, notifier, broker, now); err != nil {
					log.Printf("[DUTY] reminder check failed: %v", err)
				}
			}
		}
	}()
}
