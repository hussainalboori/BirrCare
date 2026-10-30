package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"
)

// "Start fresh": after the family has tried the app for a while, the Super
// Admin removes the test records (doses, readings, notes, alerts…) so real use
// starts with a clean screen. People, PINs, medicines, schedules, the roster
// ahead, upcoming appointments and settings stay. The database is copied to
// data/backup/before-reset/ first, and the audit log is never touched (it is
// append-only): the reset itself is recorded in it.

const resetConfirmWord = "start_fresh"

// resetDir is where the copy taken just before a reset is kept.
const resetDir = "before-reset"

// resetStep is one DELETE (or UPDATE) of a reset, in foreign-key order.
type resetStep struct {
	key   string // shown in the preview ("" = not counted)
	table string
	where string // "" = every row
}

// resetSteps lists what a reset removes. Children before parents.
func resetSteps(today string) []resetStep {
	pastAppointment := `(status <> 'scheduled' OR datetime(starts_at) < datetime('now'))`
	return []resetStep{
		{"alerts", "alert_acks", ""},
		{"alerts", "alerts", ""},
		{"", "refill_tasks", ""},
		{"doses", "medication_log_events", ""},
		{"doses", "medication_logs", ""},
		{"doses", "prn_logs", ""},
		{"", "stock_events", ""},
		{"readings", "insulin_logs", ""},
		{"readings", "vitals_logs", ""},
		{"care", "care_events", ""},
		{"care", "bruise_logs", ""},
		{"readings", "lab_results", ""},
		{"notes", "duty_handovers", ""},
		{"notes", "handover_notes", ""},
		{"", "daily_summary_sent", ""},
		{"", "care_pauses", ""},
		{"shifts", "shift_assignments", fmt.Sprintf("date < '%s'", today)},
		{"visits", "doctor_questions", "answered_at IS NOT NULL OR removed_at IS NOT NULL"},
		{"visits", "appointments", pastAppointment},
	}
}

// ResetCounts is how many records a reset removes, by what the family calls them.
type ResetCounts map[string]int64

func resetCounts(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}, today string) (ResetCounts, error) {
	out := ResetCounts{"alerts": 0, "doses": 0, "readings": 0, "care": 0, "notes": 0, "shifts": 0, "visits": 0}
	for _, s := range resetSteps(today) {
		if s.key == "" || s.table == "alert_acks" || s.table == "medication_log_events" {
			continue // counted through their parent
		}
		query := "SELECT COUNT(*) FROM " + s.table
		if s.where != "" {
			query += " WHERE " + s.where
		}
		var n int64
		if err := q.QueryRowContext(ctx, query).Scan(&n); err != nil {
			return nil, fmt.Errorf("counting %s: %w", s.table, err)
		}
		out[s.key] += n
	}
	return out, nil
}

type ResetHandler struct {
	DB     *sql.DB
	Dir    string // data/backup: the copy goes in Dir/before-reset/
	Broker *sse.Broker
}

// Preview: GET /admin/reset — what "Start fresh" would remove.
func (h *ResetHandler) Preview(w http.ResponseWriter, r *http.Request) {
	if superAdmin(w, r) == nil {
		return
	}
	counts, err := resetCounts(r.Context(), h.DB, householdToday())
	if err != nil {
		internalError(w, "failed counting records", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"counts": counts})
}

// Reset: POST /admin/reset {"confirm":"start_fresh"} — copy the database,
// then remove the test records in one transaction.
func (h *ResetHandler) Reset(w http.ResponseWriter, r *http.Request) {
	me := superAdmin(w, r)
	if me == nil {
		return
	}
	var req struct {
		Confirm string `json:"confirm"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Confirm != resetConfirmWord {
		writeJSONError(w, http.StatusBadRequest, "CONFIRM_REQUIRED", `send {"confirm":"start_fresh"} to remove the test records`)
		return
	}

	// 1. A copy of everything, before anything is removed.
	dir := filepath.Join(h.Dir, resetDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		internalError(w, "failed creating the backup folder", err)
		return
	}
	// Never overwrite an earlier copy: it may be the only one with the real records.
	stamp := "birrcare-" + time.Now().In(ClinicalLocation()).Format("20060102-150405")
	copyPath := filepath.Join(dir, stamp+".db")
	for i := 2; ; i++ {
		if _, err := os.Stat(copyPath); os.IsNotExist(err) {
			break
		}
		copyPath = filepath.Join(dir, fmt.Sprintf("%s-%d.db", stamp, i))
	}
	if _, err := h.DB.ExecContext(r.Context(), "VACUUM INTO ?", copyPath); err != nil {
		internalError(w, "could not copy the database first, nothing was removed", err)
		return
	}
	if _, err := checkBackupFile(r.Context(), copyPath); err != nil {
		internalError(w, "the copy could not be checked, nothing was removed", err)
		return
	}

	// 2. Remove, all or nothing.
	today := householdToday()
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to start", err)
		return
	}
	defer tx.Rollback()
	counts, err := resetCounts(r.Context(), tx, today)
	if err != nil {
		internalError(w, "failed counting records", err)
		return
	}
	if err := resetInTx(tx, today); err != nil {
		internalError(w, "failed removing the test records, nothing was removed", err)
		return
	}
	details, _ := json.Marshal(map[string]interface{}{"removed": counts, "copy": filepath.Join(resetDir, filepath.Base(copyPath))})
	if err := audit.Record(tx, me.ID, "DATA_RESET", "system", "", string(details)); err != nil {
		internalError(w, "failed to write audit log", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to save", err)
		return
	}

	if h.Broker != nil {
		h.Broker.Broadcast("DATA_RESET", models.ActorInfo{ID: me.ID, Name: me.Name, AvatarColor: me.AvatarColor}, map[string]interface{}{})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"removed": counts,
		"copy":    filepath.Join("backup", resetDir, filepath.Base(copyPath)),
	})
}

// resetInTx removes the records. Doses, readings and PRN entries are protected
// by "never delete" triggers; they are lifted for this transaction only and put
// back exactly as they were before it commits.
func resetInTx(tx *sql.Tx, today string) error {
	steps := resetSteps(today)
	tables := make([]string, 0, len(steps))
	for _, s := range steps {
		tables = append(tables, "'"+s.table+"'")
	}
	rows, err := tx.Query(`SELECT name, sql FROM sqlite_master WHERE type = 'trigger' AND tbl_name IN (` + strings.Join(tables, ",") + `)`)
	if err != nil {
		return err
	}
	type trigger struct{ name, sql string }
	var triggers []trigger
	for rows.Next() {
		var t trigger
		if err := rows.Scan(&t.name, &t.sql); err != nil {
			rows.Close()
			return err
		}
		triggers = append(triggers, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, t := range triggers {
		if _, err := tx.Exec(`DROP TRIGGER "` + t.name + `"`); err != nil {
			return err
		}
	}
	// Questions kept for the doctor no longer point at a visit being removed.
	if _, err := tx.Exec(`UPDATE doctor_questions SET appointment_id = NULL WHERE appointment_id IN
		(SELECT id FROM appointments WHERE status <> 'scheduled' OR datetime(starts_at) < datetime('now'))`); err != nil {
		return err
	}
	for _, s := range steps {
		q := "DELETE FROM " + s.table
		if s.where != "" {
			q += " WHERE " + s.where
		}
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("%s: %w", s.table, err)
		}
	}
	// A low-stock warning may fire again for a medicine that is still low.
	if _, err := tx.Exec(`UPDATE medications SET low_stock_alerted_at = NULL`); err != nil {
		return err
	}
	for _, t := range triggers {
		if _, err := tx.Exec(t.sql); err != nil {
			return fmt.Errorf("restoring %s: %w", t.name, err)
		}
	}
	return nil
}

func householdToday() string {
	return time.Now().In(ClinicalLocation()).Format("2006-01-02")
}
