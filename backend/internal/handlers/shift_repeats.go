package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// repeatHorizonDays is how far ahead a weekly shift is written into the roster.
const repeatHorizonDays = 91

const dateLayout = "2006-01-02"

// rosterDB is *sql.DB or *sql.Tx.
type rosterDB interface {
	querier
	Exec(query string, args ...any) (sql.Result, error)
}

type shiftRepeat struct {
	ID            string `json:"id"`
	ShiftID       string `json:"shift_id"`
	CaregiverID   string `json:"caregiver_id"`
	CaregiverName string `json:"caregiver_name"`
	AvatarColor   string `json:"avatar_color"`
	Weekday       int    `json:"weekday"`
	filledUntil   string
	createdBy     string
}

func listRepeats(q querier) ([]shiftRepeat, error) {
	rows, err := q.Query(`
		SELECT r.id, r.shift_id, r.caregiver_id, c.name, COALESCE(c.avatar_color, ''), r.weekday, r.filled_until, r.created_by
		FROM shift_repeats r
		JOIN caregivers c ON c.id = r.caregiver_id
		JOIN shifts s ON s.id = r.shift_id
		WHERE c.is_active = 1 AND s.is_active = 1
		ORDER BY r.weekday ASC, s.start_time ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []shiftRepeat{}
	for rows.Next() {
		var r shiftRepeat
		if err := rows.Scan(&r.ID, &r.ShiftID, &r.CaregiverID, &r.CaregiverName, &r.AvatarColor, &r.Weekday, &r.filledUntil, &r.createdBy); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// repeatDates are the rule's weekdays after `after`, up to and including `until`.
func repeatDates(weekday int, after, until time.Time) []string {
	var out []string
	for d := after.AddDate(0, 0, 1); !d.After(until); d = d.AddDate(0, 0, 1) {
		if int(d.Weekday()) == weekday {
			out = append(out, d.Format(dateLayout))
		}
	}
	return out
}

// writeRepeat adds the rule's dates to the roster. A date already taken stays as it
// is: one-off swaps win. The same person on that date joins the rule.
func writeRepeat(q rosterDB, r shiftRepeat, dates []string) (int, error) {
	added := 0
	for _, date := range dates {
		res, err := q.Exec(`
			INSERT INTO shift_assignments (id, shift_id, caregiver_id, date, repeat_id)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(shift_id, date) DO UPDATE SET repeat_id = excluded.repeat_id
			WHERE shift_assignments.caregiver_id = excluded.caregiver_id
		`, "assign_"+uuid.New().String()[:8], r.ShiftID, r.CaregiverID, date, r.ID)
		if err != nil {
			return added, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added++
		}
	}
	return added, nil
}

// FillShiftRepeats keeps every weekly shift written repeatHorizonDays ahead.
func FillShiftRepeats(db *sql.DB, now time.Time) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	repeats, err := listRepeats(tx)
	if err != nil {
		return err
	}
	today := now.In(ClinicalLocation())
	until := dayOf(today).AddDate(0, 0, repeatHorizonDays)
	for _, r := range repeats {
		from, err := time.Parse(dateLayout, r.filledUntil)
		if err != nil {
			return fmt.Errorf("repeat %s: %w", r.ID, err)
		}
		if yesterday := dayOf(today).AddDate(0, 0, -1); from.Before(yesterday) {
			from = yesterday // switched off for a while: nothing is written into the past
		}
		dates := repeatDates(r.Weekday, from, until)
		if len(dates) == 0 {
			continue
		}
		added, err := writeRepeat(tx, r, dates)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE shift_repeats SET filled_until = ? WHERE id = ?`, until.Format(dateLayout), r.ID); err != nil {
			return err
		}
		if err := audit.Record(tx, r.createdBy, "SHIFT_REPEAT_FILLED", "shift_repeat", r.ID, fmt.Sprintf("%d more weeks", added)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// dayOf is the calendar date of t, as midnight UTC so that adding days is exact.
func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// StartShiftRepeats tops up the weekly shifts now and every hour.
func StartShiftRepeats(ctx context.Context, db *sql.DB) {
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			if err := FillShiftRepeats(db, time.Now()); err != nil {
				log.Printf("[SHIFTS] repeat top-up failed: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
}

type monthEntry struct {
	Date          string `json:"date"`
	ShiftID       string `json:"shift_id"`
	CaregiverID   string `json:"caregiver_id"`
	CaregiverName string `json:"caregiver_name"`
	AvatarColor   string `json:"avatar_color"`
	RepeatID      string `json:"repeat_id,omitempty"`
}

// Month returns the roster of a month for the calendar, and the weekly shifts.
// Dates a weekly shift hasn't reached yet are included, so a far month isn't empty.
func (h *ShiftsHandler) Month(w http.ResponseWriter, r *http.Request) {
	month := r.URL.Query().Get("month")
	first, err := time.Parse("2006-01", month)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_MONTH", "month must be YYYY-MM")
		return
	}
	last := first.AddDate(0, 1, -1)

	rows, err := h.DB.Query(`
		SELECT a.date, a.shift_id, a.caregiver_id, c.name, COALESCE(c.avatar_color, ''), COALESCE(a.repeat_id, '')
		FROM shift_assignments a
		JOIN caregivers c ON c.id = a.caregiver_id
		JOIN shifts s ON s.id = a.shift_id
		WHERE a.date >= ? AND a.date <= ? AND s.is_active = 1
		ORDER BY a.date ASC, s.display_order ASC, s.start_time ASC
	`, first.Format(dateLayout), last.Format(dateLayout))
	if err != nil {
		internalError(w, "failed to read the roster", err)
		return
	}
	entries := []monthEntry{}
	for rows.Next() {
		var e monthEntry
		if err := rows.Scan(&e.Date, &e.ShiftID, &e.CaregiverID, &e.CaregiverName, &e.AvatarColor, &e.RepeatID); err != nil {
			rows.Close()
			internalError(w, "failed to read the roster", err)
			return
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		internalError(w, "failed to read the roster", err)
		return
	}

	repeats, err := listRepeats(h.DB)
	if err != nil {
		internalError(w, "failed to read the weekly shifts", err)
		return
	}
	today := dayOf(time.Now().In(ClinicalLocation()))
	for _, rep := range repeats {
		from, err := time.Parse(dateLayout, rep.filledUntil)
		if err != nil {
			continue
		}
		if from.Before(first.AddDate(0, 0, -1)) {
			from = first.AddDate(0, 0, -1)
		}
		if from.Before(today.AddDate(0, 0, -1)) {
			from = today.AddDate(0, 0, -1)
		}
		for _, date := range repeatDates(rep.Weekday, from, last) {
			entries = append(entries, monthEntry{Date: date, ShiftID: rep.ShiftID, CaregiverID: rep.CaregiverID, CaregiverName: rep.CaregiverName, AvatarColor: rep.AvatarColor, RepeatID: rep.ID})
		}
	}
	slices.SortStableFunc(entries, func(a, b monthEntry) int { return strings.Compare(a.Date, b.Date) })

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"month": month, "assignments": entries, "repeats": repeats})
}

type addRepeatRequest struct {
	ShiftID     string `json:"shift_id"`
	CaregiverID string `json:"caregiver_id"`
	Date        string `json:"date"` // the first day; the rule repeats on its weekday
}

// AddRepeat makes a shift repeat every week from a date, replacing any rule the shift
// already has on that weekday.
func (h *ShiftsHandler) AddRepeat(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageShifts(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only Super Admin can set weekly shifts")
		return
	}
	var req addRepeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid repeat payload")
		return
	}
	start, err := time.Parse(dateLayout, req.Date)
	if err != nil || req.ShiftID == "" || req.CaregiverID == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_FIELDS", "shift_id, caregiver_id and a YYYY-MM-DD date are required")
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		internalError(w, "failed to start the transaction", err)
		return
	}
	defer tx.Rollback()

	var found int
	if err := tx.QueryRow(`SELECT (SELECT COUNT(*) FROM shifts WHERE id = ? AND is_active = 1) * (SELECT COUNT(*) FROM caregivers WHERE id = ? AND is_active = 1)`, req.ShiftID, req.CaregiverID).Scan(&found); err != nil {
		internalError(w, "failed to read the shift", err)
		return
	}
	if found == 0 {
		writeJSONError(w, http.StatusBadRequest, "NOT_FOUND", "active shift or caregiver not found")
		return
	}

	today := dayOf(time.Now().In(ClinicalLocation()))
	if start.Before(today) {
		for start.Before(today) {
			start = start.AddDate(0, 0, 7) // the next one from today, on the same weekday
		}
	}
	rep := shiftRepeat{ID: "repeat_" + uuid.New().String()[:8], ShiftID: req.ShiftID, CaregiverID: req.CaregiverID, Weekday: int(start.Weekday()), createdBy: me.ID}

	var oldID string
	err = tx.QueryRow(`SELECT id FROM shift_repeats WHERE shift_id = ? AND weekday = ?`, rep.ShiftID, rep.Weekday).Scan(&oldID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		internalError(w, "failed to read the weekly shifts", err)
		return
	}
	if oldID != "" {
		if err := removeRepeat(tx, oldID, today); err != nil {
			internalError(w, "failed to replace the weekly shift", err)
			return
		}
	}

	until := today.AddDate(0, 0, repeatHorizonDays)
	if until.Before(start) {
		until = start
	}
	if _, err := tx.Exec(`INSERT INTO shift_repeats (id, shift_id, caregiver_id, weekday, filled_until, created_by) VALUES (?, ?, ?, ?, ?, ?)`,
		rep.ID, rep.ShiftID, rep.CaregiverID, rep.Weekday, until.Format(dateLayout), rep.createdBy); err != nil {
		internalError(w, "failed to save the weekly shift", err)
		return
	}
	if _, err := writeRepeat(tx, rep, repeatDates(rep.Weekday, start.AddDate(0, 0, -1), until)); err != nil {
		internalError(w, "failed to write the weekly shift", err)
		return
	}
	if err := audit.Record(tx, me.ID, "SHIFT_REPEAT_SET", "shift_repeat", rep.ID,
		fmt.Sprintf("%s every %s for %s", rep.ShiftID, start.Weekday(), rep.CaregiverID)); err != nil {
		internalError(w, "failed to audit the weekly shift", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to save the weekly shift", err)
		return
	}
	h.dutyChanged(me, rep.ID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{"id": rep.ID, "weekday": rep.Weekday})
}

// removeRepeat drops a rule and the days it still has ahead. Today stays, because
// the shift may be running; past days stay as the record.
func removeRepeat(tx *sql.Tx, id string, today time.Time) error {
	if _, err := tx.Exec(`DELETE FROM shift_assignments WHERE repeat_id = ? AND date > ?`, id, today.Format(dateLayout)); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM shift_repeats WHERE id = ?`, id)
	return err
}

// StopRepeat ends a weekly shift.
func (h *ShiftsHandler) StopRepeat(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageShifts(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only Super Admin can stop weekly shifts")
		return
	}
	id := chi.URLParam(r, "id")
	tx, err := h.DB.Begin()
	if err != nil {
		internalError(w, "failed to start the transaction", err)
		return
	}
	defer tx.Rollback()
	var found int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM shift_repeats WHERE id = ?`, id).Scan(&found); err != nil {
		internalError(w, "failed to read the weekly shift", err)
		return
	}
	if found == 0 {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "weekly shift not found")
		return
	}
	if err := removeRepeat(tx, id, dayOf(time.Now().In(ClinicalLocation()))); err != nil {
		internalError(w, "failed to stop the weekly shift", err)
		return
	}
	if err := audit.Record(tx, me.ID, "SHIFT_REPEAT_STOPPED", "shift_repeat", id, "Stopped"); err != nil {
		internalError(w, "failed to audit the weekly shift", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to stop the weekly shift", err)
		return
	}
	h.dutyChanged(me, id)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

func (h *ShiftsHandler) dutyChanged(me *models.Caregiver, repeatID string) {
	if h.Broker != nil {
		h.Broker.Broadcast("DUTY_UPDATED", models.ActorInfo{ID: me.ID, Name: me.Name, AvatarColor: me.AvatarColor}, map[string]string{"repeat_id": repeatID})
	}
}
