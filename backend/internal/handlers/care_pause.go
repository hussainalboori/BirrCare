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
	"birrcare/backend/internal/sse"
	"github.com/google/uuid"
)

// Hospital stay / away mode (issue #59). While care is paused the dose and
// repositioning watchers stay quiet, low-stock alerts wait, and the doctor report
// counts the doses of those hours as "paused" rather than "not recorded".
// Logging is still allowed (a dose given at home before leaving).

type CarePause struct {
	ID          string `json:"id"`
	Reason      string `json:"reason"` // hospital | travel | other
	StartedAt   string `json:"started_at"`
	EndedAt     string `json:"ended_at,omitempty"`
	StartedBy   string `json:"started_by_name,omitempty"`
	EndedBy     string `json:"ended_by_name,omitempty"`
	Notes       string `json:"notes,omitempty"`
	startedTime time.Time
	endedTime   time.Time // zero = still paused
}

type CarePauseHandler struct {
	DB     *sql.DB
	Broker *sse.Broker
}

// covers: was care paused at that moment?
func (p CarePause) covers(t time.Time) bool {
	return !t.Before(p.startedTime) && (p.endedTime.IsZero() || t.Before(p.endedTime))
}

const pauseSelect = `SELECT p.id, p.reason, p.started_at, COALESCE(p.ended_at, ''), COALESCE(s.name, ''), COALESCE(e.name, ''), p.notes
	FROM care_pauses p LEFT JOIN caregivers s ON s.id = p.started_by LEFT JOIN caregivers e ON e.id = p.ended_by`

func scanPauses(rows *sql.Rows) ([]CarePause, error) {
	defer rows.Close()
	var out []CarePause
	for rows.Next() {
		var p CarePause
		if err := rows.Scan(&p.ID, &p.Reason, &p.StartedAt, &p.EndedAt, &p.StartedBy, &p.EndedBy, &p.Notes); err != nil {
			return nil, err
		}
		p.startedTime, _ = time.Parse(sqliteTimeLayout, p.StartedAt)
		if p.EndedAt != "" {
			p.endedTime, _ = time.Parse(sqliteTimeLayout, p.EndedAt)
			p.EndedAt = sqliteToRFC3339(p.EndedAt)
		}
		p.StartedAt = sqliteToRFC3339(p.StartedAt)
		out = append(out, p)
	}
	return out, rows.Err()
}

// pausesBetween lists the pauses overlapping [from, to).
func pausesBetween(q querier, from, to time.Time) ([]CarePause, error) {
	rows, err := q.Query(pauseSelect+` WHERE p.started_at < ? AND (p.ended_at IS NULL OR p.ended_at > ?) ORDER BY p.started_at`,
		to.UTC().Format(sqliteTimeLayout), from.UTC().Format(sqliteTimeLayout))
	if err != nil {
		return nil, err
	}
	return scanPauses(rows)
}

// pausedAt reports whether care was paused at t (and the pause, if so).
func pausedAt(q querier, t time.Time) (*CarePause, error) {
	ps, err := pausesBetween(q, t, t.Add(time.Second))
	if err != nil || len(ps) == 0 {
		return nil, err
	}
	return &ps[0], nil
}

func inAnyPause(ps []CarePause, t time.Time) bool {
	for _, p := range ps {
		if p.covers(t) {
			return true
		}
	}
	return false
}

// Status: GET /care/pause → {"paused": bool, "pause": {...}}
func (h *CarePauseHandler) Status(w http.ResponseWriter, r *http.Request) {
	p, err := pausedAt(h.DB, time.Now())
	if err != nil {
		internalError(w, "failed to read the pause", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"paused": p != nil, "pause": p})
}

type pauseRequest struct {
	Reason string `json:"reason"`
	Notes  string `json:"notes"`
}

// Pause: POST /care/pause {reason, notes}. 409 PAUSE_OPEN if already paused.
func (h *CarePauseHandler) Pause(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers cannot pause care")
		return
	}
	var req pauseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	req.Notes = strings.TrimSpace(req.Notes)
	if req.Reason != "hospital" && req.Reason != "travel" && req.Reason != "other" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REASON", "reason must be hospital, travel or other")
		return
	}
	if len(req.Notes) > maxNotesLen {
		writeJSONError(w, http.StatusBadRequest, "NOTES_TOO_LONG", "notes are too long")
		return
	}
	now := time.Now().UTC().Truncate(time.Second)
	id := uuid.New().String()
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to pause care", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO care_pauses (id, reason, started_at, started_by, notes) VALUES (?, ?, ?, ?, ?)`,
		id, req.Reason, now.Format(sqliteTimeLayout), me.ID, req.Notes); err != nil {
		if isUniqueViolation(err) {
			writeJSONError(w, http.StatusConflict, "PAUSE_OPEN", "care is already paused; resume it first")
			return
		}
		internalError(w, "failed to pause care", err)
		return
	}
	if err := audit.Record(tx, me.ID, "CARE_PAUSED", "care_pause", id, fmt.Sprintf("Care paused (%s). %s", req.Reason, req.Notes)); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to pause care", err)
		return
	}
	p := CarePause{ID: id, Reason: req.Reason, StartedAt: now.Format(time.RFC3339), StartedBy: me.Name, Notes: req.Notes}
	h.Broker.Broadcast("CARE_PAUSED", actorOf(me), p)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(p)
}

// Resume: POST /care/resume. 409 NOT_PAUSED if care isn't paused.
func (h *CarePauseHandler) Resume(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers cannot resume care")
		return
	}
	now := time.Now().UTC().Truncate(time.Second)
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to resume care", err)
		return
	}
	defer tx.Rollback()
	var id, reason string
	err = tx.QueryRow(`SELECT id, reason FROM care_pauses WHERE ended_at IS NULL`).Scan(&id, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusConflict, "NOT_PAUSED", "care is not paused")
		return
	} else if err != nil {
		internalError(w, "failed to resume care", err)
		return
	}
	if _, err := tx.Exec(`UPDATE care_pauses SET ended_at = ?, ended_by = ? WHERE id = ?`, now.Format(sqliteTimeLayout), me.ID, id); err != nil {
		internalError(w, "failed to resume care", err)
		return
	}
	if err := audit.Record(tx, me.ID, "CARE_RESUMED", "care_pause", id, fmt.Sprintf("Care resumed at home (was %s)", reason)); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to resume care", err)
		return
	}
	payload := map[string]interface{}{"id": id, "reason": reason, "ended_at": now.Format(time.RFC3339)}
	h.Broker.Broadcast("CARE_RESUMED", actorOf(me), payload)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}
