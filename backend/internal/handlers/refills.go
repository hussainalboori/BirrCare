package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/push"
	"birrcare/backend/internal/sse"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// "I'll get it" (issue #65). The low-stock warning appeared on four phones and
// everybody thought someone else would go to the pharmacy. Each low-stock alert
// now opens a refill task: one tap claims it (the alert goes quiet for everyone
// and they see who is getting it, by when); a refill closes it. A watcher follows
// up: unclaimed after 12 hours → everyone again; claimed but not bought by the
// due time → the owner, then everyone 4 hours later. Red for a blood thinner with
// 2 days or less left.

const (
	refillUnclaimedAfter = 12 * time.Hour
	refillEveryoneAfter  = 4 * time.Hour // after the owner's reminder
	refillDueHour        = 12            // "by tomorrow 12:00"
	refillFollowupKind   = "refill_followup"
)

type RefillTask struct {
	ID            string `json:"id"`
	MedicationID  string `json:"medication_id"`
	CreatedAt     string `json:"created_at"`
	ClaimedBy     string `json:"claimed_by,omitempty"`
	ClaimedByName string `json:"claimed_by_name,omitempty"`
	ClaimedAt     string `json:"claimed_at,omitempty"`
	DueBy         string `json:"due_by,omitempty"`
	Notes         string `json:"notes,omitempty"`
	createdTime   time.Time
	claimedTime   time.Time
	dueTime       time.Time
	unclaimedSent bool
	ownerSent     bool
	everyoneSent  bool
}

const refillSelect = `SELECT t.id, t.medication_id, t.created_at, COALESCE(t.claimed_by, ''), COALESCE(c.name, ''), COALESCE(t.claimed_at, ''),
	COALESCE(t.due_by, ''), t.notes, t.unclaimed_alerted_at IS NOT NULL, t.owner_reminded_at IS NOT NULL, t.everyone_reminded_at IS NOT NULL
	FROM refill_tasks t LEFT JOIN caregivers c ON c.id = t.claimed_by`

func scanRefills(rows *sql.Rows) ([]RefillTask, error) {
	defer rows.Close()
	var out []RefillTask
	for rows.Next() {
		var t RefillTask
		if err := rows.Scan(&t.ID, &t.MedicationID, &t.CreatedAt, &t.ClaimedBy, &t.ClaimedByName, &t.ClaimedAt, &t.DueBy, &t.Notes,
			&t.unclaimedSent, &t.ownerSent, &t.everyoneSent); err != nil {
			return nil, err
		}
		t.createdTime, _ = time.Parse(sqliteTimeLayout, t.CreatedAt)
		t.CreatedAt = sqliteToRFC3339(t.CreatedAt)
		if t.ClaimedAt != "" {
			t.claimedTime, _ = time.Parse(sqliteTimeLayout, t.ClaimedAt)
			t.dueTime, _ = time.Parse(sqliteTimeLayout, t.DueBy)
			t.ClaimedAt, t.DueBy = sqliteToRFC3339(t.ClaimedAt), sqliteToRFC3339(t.DueBy)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// openRefillTasks returns the open tasks by medication id.
func openRefillTasks(q querier) (map[string]RefillTask, error) {
	rows, err := q.Query(refillSelect + ` WHERE t.done_at IS NULL`)
	if err != nil {
		return nil, err
	}
	list, err := scanRefills(rows)
	if err != nil {
		return nil, err
	}
	out := map[string]RefillTask{}
	for _, t := range list {
		out[t.MedicationID] = t
	}
	return out, nil
}

// ensureRefillTask opens a task for a medicine that just ran low (one open task
// per medicine).
func ensureRefillTask(tx *sql.Tx, medID, alertID string, now time.Time) error {
	_, err := tx.Exec(`INSERT OR IGNORE INTO refill_tasks (id, medication_id, alert_id, created_at) VALUES (?, ?, ?, ?)`,
		uuid.New().String(), medID, nullIfEmpty(alertID), now.UTC().Format(sqliteTimeLayout))
	return err
}

// completeRefillTask closes the medicine's open task and its alerts. It returns
// the task id, or "" when there was none.
func completeRefillTask(tx *sql.Tx, medID, by string, now time.Time) (string, error) {
	var id string
	err := tx.QueryRow(`SELECT id FROM refill_tasks WHERE medication_id = ? AND done_at IS NULL`, medID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	ts := now.UTC().Format(sqliteTimeLayout)
	if _, err := tx.Exec(`UPDATE refill_tasks SET done_at = ?, done_by = ? WHERE id = ?`, ts, nullIfEmpty(by), id); err != nil {
		return "", err
	}
	return id, quietRefillAlerts(tx, medID, id, ts)
}

// quietRefillAlerts resolves the medicine's low-stock alert and the task's
// follow-ups for everyone.
func quietRefillAlerts(tx *sql.Tx, medID, taskID, ts string) error {
	_, err := tx.Exec(`UPDATE alerts SET resolved_at = ? WHERE resolved_at IS NULL AND
		((kind = 'low_stock' AND ref_id = ?) OR (kind = ? AND ref_id LIKE ?))`, ts, medID, refillFollowupKind, taskID+":%")
	return err
}

// refillDueBy is the next pharmacy-open noon at home: today 12:00 if claimed in
// the small hours, else tomorrow 12:00.
func refillDueBy(now time.Time) time.Time {
	t := now.In(ClinicalLocation())
	day := t.AddDate(0, 0, 1)
	if t.Hour() < 6 {
		day = t
	}
	return time.Date(day.Year(), day.Month(), day.Day(), refillDueHour, 0, 0, 0, ClinicalLocation())
}

// ClaimRefill: POST /medications/{id}/refill/claim {notes?} — "I'll get it".
// Claiming again (someone else takes over) moves the owner and the due time.
func (h *MedicationHandler) ClaimRefill(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "viewers cannot claim a refill")
		return
	}
	medID := chi.URLParam(r, "id")
	var req struct {
		Notes string `json:"notes"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
			return
		}
	}
	req.Notes = strings.TrimSpace(req.Notes)
	if len(req.Notes) > maxNotesLen {
		writeJSONError(w, http.StatusBadRequest, "NOTES_TOO_LONG", "notes are too long")
		return
	}
	now := time.Now()
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to claim the refill", err)
		return
	}
	defer tx.Rollback()
	st, err := scanStock(tx.QueryRow(stockSelect+` WHERE id = ? AND is_active = 1`, medID).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "medication not found")
		return
	} else if err != nil {
		internalError(w, "failed to claim the refill", err)
		return
	}
	tasks, err := openRefillTasks(tx)
	if err != nil {
		internalError(w, "failed to claim the refill", err)
		return
	}
	task, ok := tasks[medID]
	if !ok {
		// A low medicine from before refill tasks existed gets one now.
		if !st.tracked || !st.Low {
			writeJSONError(w, http.StatusConflict, "NOT_LOW", "this medicine is not running low")
			return
		}
		if err := ensureRefillTask(tx, medID, "", now); err != nil {
			internalError(w, "failed to claim the refill", err)
			return
		}
	}
	due := refillDueBy(now)
	ts := now.UTC().Format(sqliteTimeLayout)
	if _, err := tx.Exec(`UPDATE refill_tasks SET claimed_by = ?, claimed_at = ?, due_by = ?, notes = ?, owner_reminded_at = NULL, everyone_reminded_at = NULL
		WHERE medication_id = ? AND done_at IS NULL`, me.ID, ts, due.UTC().Format(sqliteTimeLayout), req.Notes, medID); err != nil {
		internalError(w, "failed to claim the refill", err)
		return
	}
	if tasks, err = openRefillTasks(tx); err != nil {
		internalError(w, "failed to claim the refill", err)
		return
	}
	task = tasks[medID]
	if err := quietRefillAlerts(tx, medID, task.ID, ts); err != nil {
		internalError(w, "failed to quiet the alerts", err)
		return
	}
	detail := fmt.Sprintf("%s (%s) will get a new box of %s by %s", me.Name, me.ID, st.NameEn, due.Format("2006-01-02 15:04"))
	if task.ClaimedBy != "" && task.ClaimedBy != me.ID {
		detail += " (took over)"
	}
	if err := audit.Record(tx, me.ID, "REFILL_CLAIMED", "medication", medID, detail); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to claim the refill", err)
		return
	}
	// alert_id makes every open app reload its alerts (the low-stock one is now quiet).
	h.Broker.Broadcast("REFILL_CLAIMED", actorOf(me), map[string]interface{}{
		"medication_id": medID, "name_ar": st.NameAr, "name_en": st.NameEn, "claimed_by": me.ID, "claimed_by_name": me.Name,
		"due_by": task.DueBy, "alert_id": task.ID,
	})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

// ---- Pharmacy and prescription per medicine ----

var pharmacyPhonePattern = regexp.MustCompile(`^\+?[0-9][0-9 ()-]{2,19}$`)

// RefillFields are shared by create and update; absent = unchanged.
type RefillFields struct {
	PharmacyName      *string `json:"pharmacy_name,omitempty"`
	PharmacyPhone     *string `json:"pharmacy_phone,omitempty"`
	NeedsPrescription *bool   `json:"needs_prescription,omitempty"`
}

func (f *RefillFields) validate() (string, string) {
	if f.PharmacyName != nil {
		v := strings.TrimSpace(*f.PharmacyName)
		f.PharmacyName = &v
		if len([]rune(v)) > 80 {
			return "INVALID_PHARMACY", "the pharmacy name is too long"
		}
	}
	if f.PharmacyPhone != nil {
		v := strings.TrimSpace(*f.PharmacyPhone)
		f.PharmacyPhone = &v
		if v != "" && !pharmacyPhonePattern.MatchString(v) {
			return "INVALID_PHARMACY_PHONE", "write the pharmacy phone with digits only (and + at the start)"
		}
	}
	return "", ""
}

func (f *RefillFields) insertValues() (string, string, bool) {
	name, phone := "", ""
	if f.PharmacyName != nil {
		name = *f.PharmacyName
	}
	if f.PharmacyPhone != nil {
		phone = *f.PharmacyPhone
	}
	return name, phone, f.NeedsPrescription != nil && *f.NeedsPrescription
}

func (f *RefillFields) apply(tx *sql.Tx, medID string) error {
	for col, v := range map[string]interface{}{"pharmacy_name": f.PharmacyName, "pharmacy_phone": f.PharmacyPhone, "needs_prescription": f.NeedsPrescription} {
		switch p := v.(type) {
		case *string:
			if p == nil {
				continue
			}
			v = *p
		case *bool:
			if p == nil {
				continue
			}
			v = *p
		}
		if _, err := tx.Exec("UPDATE medications SET "+col+" = ? WHERE id = ?", v, medID); err != nil {
			return err
		}
	}
	return nil
}

// ---- Follow-ups ----

// CheckRefillTasks opens missing tasks, closes stale ones and sends the
// follow-ups due as of now.
func CheckRefillTasks(ctx context.Context, db *sql.DB, notifier *push.Notifier, broker *sse.Broker, now time.Time) error {
	ts := now.UTC().Format(sqliteTimeLayout)
	// A medicine stopped or no longer counted has nothing to buy.
	if _, err := db.ExecContext(ctx, `UPDATE refill_tasks SET done_at = ? WHERE done_at IS NULL AND medication_id IN
		(SELECT id FROM medications WHERE is_active = 0 OR stock_units IS NULL)`, ts); err != nil {
		return err
	}
	// Low and warned before refill tasks existed: open one. A task closed by a new
	// box after that warning counts (the box may still leave it under the threshold).
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO refill_tasks (id, medication_id, alert_id, created_at)
		SELECT lower(hex(randomblob(16))), m.id,
		       (SELECT a.id FROM alerts a WHERE a.kind = 'low_stock' AND a.ref_id = m.id ORDER BY a.created_at DESC LIMIT 1), ?
		FROM medications m
		WHERE m.is_active = 1 AND m.stock_units IS NOT NULL AND m.low_stock_alerted_at IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM refill_tasks t WHERE t.medication_id = m.id
		                  AND (t.done_at IS NULL OR t.done_at >= m.low_stock_alerted_at))`, ts); err != nil {
		return err
	}
	if paused, err := pausedAt(db, now); err != nil || paused != nil {
		return err // she isn't at home: no chasing
	}
	tasks, err := openRefillTasks(db)
	if err != nil {
		return err
	}
	for medID, t := range tasks {
		st, err := scanStock(db.QueryRowContext(ctx, stockSelect+` WHERE id = ?`, medID).Scan)
		if err != nil {
			continue
		}
		var stage, target, column string
		switch {
		case t.ClaimedBy == "" && !t.unclaimedSent && now.Sub(t.createdTime) >= refillUnclaimedAfter:
			stage, column = "unclaimed", "unclaimed_alerted_at"
		case t.ClaimedBy != "" && !t.ownerSent && !now.Before(t.dueTime):
			stage, target, column = "owner", t.ClaimedBy, "owner_reminded_at"
		case t.ClaimedBy != "" && t.ownerSent && !t.everyoneSent && !now.Before(t.dueTime.Add(refillEveryoneAfter)):
			stage, column = "everyone", "everyone_reminded_at"
		default:
			continue
		}
		daysLeft := -1.0
		if st.DaysLeft != nil {
			daysLeft = *st.DaysLeft
		}
		payload := map[string]interface{}{
			"medication_id": medID, "name_ar": st.NameAr, "name_en": st.NameEn, "stage": stage, "stock_units": st.StockUnits,
			"days_left": daysLeft, "blood_thinner": st.BloodThinner, "critical": st.BloodThinner && st.DaysLeft != nil && daysLeft <= 2,
			"claimed_by": t.ClaimedBy, "claimed_by_name": t.ClaimedByName, "due_by": t.DueBy,
			"needs_prescription": st.NeedsPrescription, "pharmacy_name": st.PharmacyName, "pharmacy_phone": st.PharmacyPhone,
		}
		ref := t.ID + ":" + stage + ":" + t.ClaimedAt
		if err := raiseReminder(ctx, db, notifier, broker, now, refillFollowupKind, ref, target, "REFILL_FOLLOWUP", payload); err != nil {
			log.Printf("[REFILLS] %s %s: %v", medID, stage, err)
			continue
		}
		if _, err := db.ExecContext(ctx, `UPDATE refill_tasks SET `+column+` = ? WHERE id = ?`, ts, t.ID); err != nil {
			return err
		}
	}
	return nil
}

// StartRefillWatcher checks the refill tasks every minute.
func StartRefillWatcher(ctx context.Context, db *sql.DB, notifier *push.Notifier, broker *sse.Broker) {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				if err := CheckRefillTasks(ctx, db, notifier, broker, now); err != nil {
					log.Printf("[REFILLS] check failed: %v", err)
				}
			}
		}
	}()
}
