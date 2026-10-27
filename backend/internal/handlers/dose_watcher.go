package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"slices"
	"strings"
	"time"

	"birrcare/backend/internal/models"
	"birrcare/backend/internal/push"
	"birrcare/backend/internal/sse"
	"github.com/google/uuid"
)

// Overdue doses (issue #55). The overdue badge on a dose card only exists while
// someone has the app open; the most common missed dose is two tired siblings
// each assuming the other gave it. A server watcher checks every minute:
//
//   - first window after the slot time, nothing logged: dose_overdue to the
//     caregiver on duty (everyone if nobody is assigned), pushed;
//   - second window, still nothing: the same alert goes to everyone.
//
// Blood thinners (is_critical) use 30/60 minutes and show red; others 45/90.
// One alert row per dose (ref_id = medication:date:slot, unique index). Logging the
// dose in any way resolves it for everyone; voiding that entry reopens it.
// Medicines with remind = 0 are never chased, nor a dose the interval lock would
// refuse right now.

const (
	doseFirstWindow         = 45 * time.Minute
	doseEscalateWindow      = 90 * time.Minute
	criticalFirstWindow     = 30 * time.Minute
	criticalEscalateWindow  = 60 * time.Minute
	doseOverdueGiveUp       = 12 * time.Hour // older than this is history, not a reminder
	doseOverdueSystemActor  = "system"
	doseOverdueAlertKind    = "dose_overdue"
	doseOverdueResolvedType = "ALERT_RESOLVED"
)

func doseRef(medID, date, slot string) string { return medID + ":" + date + ":" + slot }

type watchedMed struct {
	id, nameAr, nameEn, dosage string
	slots, days                []string
	times                      map[string]string
	minHours                   float64
	critical                   bool
	createdAt                  time.Time
}

// CheckDoseOverdue raises or escalates dose_overdue alerts as of now.
func CheckDoseOverdue(ctx context.Context, db *sql.DB, notifier *push.Notifier, broker *sse.Broker, now time.Time) error {
	loc := ClinicalLocation()
	now = now.In(loc)

	rows, err := db.QueryContext(ctx, `
		SELECT id, name_ar, name_en, dosage, frequency_slots, COALESCE(days_of_week, ''), COALESCE(slot_times, ''),
		       min_hours_between, COALESCE(is_critical, 0), COALESCE(created_at, '')
		FROM medications
		WHERE is_active = 1 AND COALESCE(is_prn, 0) = 0 AND COALESCE(remind, 1) = 1`)
	if err != nil {
		return err
	}
	var meds []watchedMed
	for rows.Next() {
		var m watchedMed
		var slotsJSON, daysJSON, timesJSON, createdAt string
		var minHours sql.NullFloat64
		if err := rows.Scan(&m.id, &m.nameAr, &m.nameEn, &m.dosage, &slotsJSON, &daysJSON, &timesJSON, &minHours, &m.critical, &createdAt); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(slotsJSON), &m.slots)
		_ = json.Unmarshal([]byte(daysJSON), &m.days)
		if len(m.days) == 0 {
			m.days = allDaysOfWeek
		}
		_ = json.Unmarshal([]byte(timesJSON), &m.times)
		m.times = normalizeSlotTimes(m.times, m.slots)
		m.minHours = intervalHours(nullableFloat(minHours), m.slots, m.times)
		m.createdAt, _ = time.Parse(sqliteTimeLayout, createdAt)
		meds = append(meds, m)
	}
	rows.Close() // release the single pooled connection before the next query

	for _, m := range meds {
		// Yesterday too: a 22:30 dose is escalated after midnight.
		for _, day := range []time.Time{now.AddDate(0, 0, -1), now} {
			if !slices.Contains(m.days, strings.ToLower(day.Weekday().String()[:3])) {
				continue
			}
			date := day.Format("2006-01-02")
			for _, slot := range m.slots {
				if err := checkDose(ctx, db, notifier, broker, now, m, date, slot); err != nil {
					log.Printf("[DOSES] %s %s %s: %v", m.id, date, slot, err)
				}
			}
		}
	}
	return nil
}

func checkDose(ctx context.Context, db *sql.DB, notifier *push.Notifier, broker *sse.Broker, now time.Time, m watchedMed, date, slot string) error {
	due, err := time.ParseInLocation("2006-01-02 15:04", date+" "+m.times[slot], now.Location())
	if err != nil {
		return err
	}
	first, escalate := doseFirstWindow, doseEscalateWindow
	if m.critical {
		first, escalate = criticalFirstWindow, criticalEscalateWindow
	}
	late := now.Sub(due)
	if late < first || late > doseOverdueGiveUp || (!m.createdAt.IsZero() && m.createdAt.After(due)) {
		return nil
	}

	var logged int
	if err := db.QueryRowContext(ctx, `SELECT 1 FROM medication_logs WHERE medication_id = ? AND scheduled_date = ? AND slot = ? AND voided_at IS NULL`,
		m.id, date, slot).Scan(&logged); err == nil {
		return nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// The interval lock would refuse this dose now (e.g. last night's dose given
	// late): nothing to give, so nothing to chase.
	if m.minHours > 0 {
		other, err := nearestTakenDose(db, m.id, now.UTC())
		if err != nil {
			return err
		}
		if other != nil {
			if t, perr := time.Parse(time.RFC3339, other.TakenAt); perr == nil && math.Abs(now.Sub(t).Hours()) < m.minHours {
				return nil
			}
		}
	}

	ref := doseRef(m.id, date, slot)
	everyone := late >= escalate
	payload := map[string]interface{}{
		"medication_id": m.id, "name_ar": m.nameAr, "name_en": m.nameEn, "dosage": m.dosage,
		"slot": slot, "scheduled_date": date, "slot_time": m.times[slot], "critical": m.critical, "escalated": everyone,
	}

	var alertID string
	var target, resolved sql.NullString
	err = db.QueryRowContext(ctx, `SELECT id, target_caregiver_id, resolved_at FROM alerts WHERE kind = ? AND ref_id = ?`, doseOverdueAlertKind, ref).
		Scan(&alertID, &target, &resolved)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		to := ""
		if !everyone {
			to = onDutyCaregiverAt(db, now)
		}
		data, _ := json.Marshal(payload)
		alertID = uuid.New().String()
		res, err := db.ExecContext(ctx, `INSERT INTO alerts (id, kind, ref_id, payload, created_at, target_caregiver_id) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT DO NOTHING`, alertID, doseOverdueAlertKind, ref, string(data), now.UTC().Format(sqliteTimeLayout), nullIfEmpty(to))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil // another run got there first
		}
		announceDoseOverdue(notifier, broker, raisedAlert{ID: alertID, Kind: doseOverdueAlertKind, Target: to}, payload)
	case err != nil:
		return err
	case resolved.Valid:
		// The dose was logged, then that entry was cancelled: it is missing again.
		to := ""
		if !everyone && target.Valid {
			to = target.String
		}
		data, _ := json.Marshal(payload)
		res, err := db.ExecContext(ctx, `UPDATE alerts SET resolved_at = NULL, payload = ?, target_caregiver_id = ? WHERE id = ? AND resolved_at IS NOT NULL`,
			string(data), nullIfEmpty(to), alertID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			announceDoseOverdue(notifier, broker, raisedAlert{ID: alertID, Kind: doseOverdueAlertKind, Target: to}, payload)
		}
	case everyone && target.Valid:
		data, _ := json.Marshal(payload)
		res, err := db.ExecContext(ctx, `UPDATE alerts SET target_caregiver_id = NULL, payload = ? WHERE id = ? AND target_caregiver_id IS NOT NULL`,
			string(data), alertID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			announceDoseOverdue(notifier, broker, raisedAlert{ID: alertID, Kind: doseOverdueAlertKind}, payload)
		}
	}
	return nil
}

func announceDoseOverdue(notifier *push.Notifier, broker *sse.Broker, a raisedAlert, payload map[string]interface{}) {
	pushRaised(notifier, a, doseOverdueSystemActor)
	if broker != nil {
		p := map[string]interface{}{"alert_id": a.ID, "target_caregiver_id": a.Target}
		for k, v := range payload {
			p[k] = v
		}
		broker.BroadcastAlert(a.ID, "DOSE_OVERDUE", models.ActorInfo{ID: doseOverdueSystemActor, Name: "Birrcare"}, p)
	}
}

// resolveDoseOverdue clears the dose's overdue alert for everyone, in the caller's
// transaction. It returns the alert id if there was one to clear.
func resolveDoseOverdue(tx *sql.Tx, medID, date, slot string, now time.Time) (string, error) {
	var id string
	err := tx.QueryRow(`SELECT id FROM alerts WHERE kind = ? AND ref_id = ? AND resolved_at IS NULL`, doseOverdueAlertKind, doseRef(medID, date, slot)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	if _, err := tx.Exec(`UPDATE alerts SET resolved_at = ? WHERE id = ?`, now.UTC().Format(sqliteTimeLayout), id); err != nil {
		return "", fmt.Errorf("resolving overdue alert: %w", err)
	}
	return id, nil
}

// StartDoseWatcher checks for overdue doses every minute.
func StartDoseWatcher(ctx context.Context, db *sql.DB, notifier *push.Notifier, broker *sse.Broker) {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				if err := CheckDoseOverdue(ctx, db, notifier, broker, now); err != nil {
					log.Printf("[DOSES] overdue check failed: %v", err)
				}
			}
		}
	}()
}
