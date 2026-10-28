package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"birrcare/backend/internal/models"
	"birrcare/backend/internal/push"
	"birrcare/backend/internal/sse"
	"github.com/google/uuid"
)

// Appointment and blood-test reminders (issue #63), checked every minute:
//
//   - the evening before (20:00 household time) and 2 hours before an
//     appointment: appointment_reminder to the one taking her (everyone if
//     nobody is assigned);
//   - the evening before the next test date of the latest lab result (e.g. INR):
//     lab_test_due to everyone.
//
// Each reminder is one alert row (unique kind + ref_id). The ref includes the
// start time, so a rescheduled appointment is reminded again. A reminder whose
// moment had already passed when the appointment was added is not sent.

const (
	reminderEveningHour   = 20
	reminderBefore        = 2 * time.Hour
	appointmentReminder   = "appointment_reminder"
	labTestDueAlert       = "lab_test_due"
	reminderSystemActorID = "system"
)

// eveningBefore is 20:00 household time on the day before date.
func eveningBefore(date time.Time) time.Time {
	d := date.In(ClinicalLocation())
	return time.Date(d.Year(), d.Month(), d.Day()-1, reminderEveningHour, 0, 0, 0, ClinicalLocation())
}

// CheckAppointmentReminders sends the reminders due as of now.
func CheckAppointmentReminders(ctx context.Context, db *sql.DB, notifier *push.Notifier, broker *sse.Broker, now time.Time) error {
	rows, err := db.QueryContext(ctx, appointmentSelect+` WHERE a.status = 'scheduled' AND a.starts_at > ? AND a.starts_at <= ?`,
		now.UTC().Format(sqliteTimeLayout), now.Add(30*time.Hour).UTC().Format(sqliteTimeLayout))
	if err != nil {
		return err
	}
	appts, err := scanAppointments(rows) // closes rows before the inserts below
	if err != nil {
		return err
	}
	for _, a := range appts {
		for _, m := range []struct {
			name string
			at   time.Time
		}{{"evening", eveningBefore(a.startsTime)}, {"2h", a.startsTime.Add(-reminderBefore)}} {
			// The evening reminder only while the 2-hour one is still ahead.
			if now.Before(m.at) || a.createdTime.After(m.at) || (m.name == "evening" && !now.Before(a.startsTime.Add(-reminderBefore))) {
				continue
			}
			payload := map[string]interface{}{
				"appointment_id": a.ID, "kind": a.Kind, "title": a.Title, "starts_at": a.StartsAt, "date": a.Date, "time": a.Time,
				"location": a.Location, "assigned_caregiver_id": a.AssignedID, "assigned_name": a.AssignedName, "moment": m.name,
			}
			ref := a.ID + ":" + m.name + ":" + a.startsTime.UTC().Format(time.RFC3339)
			if err := raiseReminder(ctx, db, notifier, broker, now, appointmentReminder, ref, a.AssignedID, "APPOINTMENT_REMINDER", payload); err != nil {
				log.Printf("[APPOINTMENTS] %s %s: %v", a.ID, m.name, err)
			}
		}
	}

	// The next test date of the latest result of each kind; a newer result means
	// the test was done.
	lrows, err := db.QueryContext(ctx, `SELECT l.id, l.kind, l.next_test_on, l.created_at FROM lab_results l
		WHERE l.next_test_on IS NOT NULL AND l.created_at = (SELECT MAX(created_at) FROM lab_results WHERE kind = l.kind)`)
	if err != nil {
		return err
	}
	type due struct{ id, kind, next, created string }
	var dues []due
	for lrows.Next() {
		var d due
		if lrows.Scan(&d.id, &d.kind, &d.next, &d.created) == nil {
			dues = append(dues, d)
		}
	}
	lrows.Close()
	for _, d := range dues {
		_, day, ok := parseClinicalDate(d.next)
		if !ok {
			continue
		}
		at := eveningBefore(day)
		created, _ := time.Parse(sqliteTimeLayout, d.created)
		if now.Before(at) || !now.Before(day.AddDate(0, 0, 1)) || created.After(at) {
			continue
		}
		payload := map[string]interface{}{"lab_result_id": d.id, "kind": d.kind, "next_test_on": d.next}
		if err := raiseReminder(ctx, db, notifier, broker, now, labTestDueAlert, "lab:"+d.id+":"+d.next, "", "LAB_TEST_DUE", payload); err != nil {
			log.Printf("[APPOINTMENTS] lab %s: %v", d.id, err)
		}
	}
	return nil
}

// raiseReminder records a reminder once (unique kind + ref_id) and pushes it.
func raiseReminder(ctx context.Context, db *sql.DB, notifier *push.Notifier, broker *sse.Broker, now time.Time, kind, ref, target, event string, payload map[string]interface{}) error {
	data, _ := json.Marshal(payload)
	id := uuid.New().String()
	res, err := db.ExecContext(ctx, `INSERT INTO alerts (id, kind, ref_id, payload, created_at, target_caregiver_id) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`, id, kind, ref, string(data), now.UTC().Format(sqliteTimeLayout), nullIfEmpty(target))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // already sent
	}
	pushRaised(notifier, raisedAlert{ID: id, Kind: kind, Target: target}, reminderSystemActorID)
	if broker != nil {
		p := map[string]interface{}{"alert_id": id, "target_caregiver_id": target}
		for k, v := range payload {
			p[k] = v
		}
		broker.BroadcastAlert(id, event, models.ActorInfo{ID: reminderSystemActorID, Name: "Birrcare"}, p)
	}
	return nil
}

// StartAppointmentReminders checks for due reminders every minute.
func StartAppointmentReminders(ctx context.Context, db *sql.DB, notifier *push.Notifier, broker *sse.Broker) {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				if err := CheckAppointmentReminders(ctx, db, notifier, broker, now); err != nil {
					log.Printf("[APPOINTMENTS] reminder check failed: %v", err)
				}
			}
		}
	}()
}
