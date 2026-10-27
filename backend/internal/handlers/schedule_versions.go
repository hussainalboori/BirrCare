package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// Schedule history (issue #57). A medicine's slots, days, times and dosage are
// kept as dated versions in medication_schedule_versions, so a schedule edit only
// applies from the day chosen and stopping a medicine keeps its past days. The
// medications row keeps the current values for everything else.

// allSlotsInOrder lists every dose slot, in the order of a day.
var allSlotsInOrder = []string{"night", "morning", "afternoon", "evening", "bedtime"}

type scheduleVersion struct {
	From, To string // household YYYY-MM-DD, inclusive; To "" = still in effect
	Slots    []string
	Days     []string
	Times    map[string]string
	Dosage   string
}

func (v scheduleVersion) covers(date string) bool {
	return v.From <= date && (v.To == "" || date <= v.To)
}

// scheduledOn: does this version expect a dose of this slot on that date?
func (v scheduleVersion) scheduledOn(date string, slot string) bool {
	d, err := time.Parse("2006-01-02", date)
	if err != nil || !v.covers(date) || !slices.Contains(v.Slots, slot) {
		return false
	}
	return slices.Contains(v.Days, weekdayKeys[d.Weekday()])
}

func versionOn(vs []scheduleVersion, date string) (scheduleVersion, bool) {
	for i := len(vs) - 1; i >= 0; i-- {
		if vs[i].covers(date) {
			return vs[i], true
		}
	}
	return scheduleVersion{}, false
}

func parseVersion(slotsJSON, daysJSON, timesJSON, dosage string) scheduleVersion {
	var v scheduleVersion
	_ = json.Unmarshal([]byte(slotsJSON), &v.Slots)
	_ = json.Unmarshal([]byte(daysJSON), &v.Days)
	if len(v.Days) == 0 {
		v.Days = allDaysOfWeek
	}
	var times map[string]string
	_ = json.Unmarshal([]byte(timesJSON), &times)
	v.Times = normalizeSlotTimes(times, v.Slots)
	v.Dosage = dosage
	return v
}

// loadScheduleVersions returns every medicine's versions, oldest first. A medicine
// without any (added before the history existed and not migrated, e.g. in tests)
// gets one from its current row: from the day it was added, or none if stopped.
func loadScheduleVersions(q querier) (map[string][]scheduleVersion, error) {
	out := map[string][]scheduleVersion{}
	rows, err := q.Query(`SELECT medication_id, effective_from, COALESCE(effective_to, ''), frequency_slots, days_of_week, slot_times, dosage
		FROM medication_schedule_versions ORDER BY medication_id, effective_from`)
	if err != nil {
		return nil, fmt.Errorf("loading schedule history: %w", err)
	}
	for rows.Next() {
		var id, slots, days, times, dosage string
		var from, to string
		if err := rows.Scan(&id, &from, &to, &slots, &days, &times, &dosage); err != nil {
			rows.Close()
			return nil, err
		}
		v := parseVersion(slots, days, times, dosage)
		v.From, v.To = from, to
		out[id] = append(out[id], v)
	}
	rows.Close() // release the single pooled connection before the next query

	mrows, err := q.Query(`SELECT id, frequency_slots, COALESCE(days_of_week, ''), COALESCE(slot_times, ''), dosage, COALESCE(created_at, ''), COALESCE(is_active, 1)
		FROM medications`)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var id, slots, days, times, dosage, created string
		var active bool
		if err := mrows.Scan(&id, &slots, &days, &times, &dosage, &created, &active); err != nil {
			return nil, err
		}
		if _, ok := out[id]; ok {
			continue
		}
		v := parseVersion(slots, days, times, dosage)
		v.From = "2000-01-01"
		if t, err := time.Parse(sqliteTimeLayout, created); err == nil {
			v.From = t.In(ClinicalLocation()).Format("2006-01-02")
		}
		if !active {
			v.To = dayBefore(v.From)
		}
		out[id] = []scheduleVersion{v}
	}
	return out, mrows.Err()
}

func dayBefore(date string) string {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return d.AddDate(0, 0, -1).Format("2006-01-02")
}

// effectiveFrom turns the admin's "Starting when?" choice into a household date.
func effectiveFrom(choice string, now time.Time) (string, error) {
	today := now.In(ClinicalLocation())
	switch choice {
	case "", "today":
		return today.Format("2006-01-02"), nil
	case "tomorrow":
		return today.AddDate(0, 0, 1).Format("2006-01-02"), nil
	}
	return "", errors.New("effective_from must be 'today' or 'tomorrow'")
}

// ensureScheduleVersion records the medicine's current schedule as its history if
// it has none yet, before an edit changes it. Call inside the edit's transaction.
func ensureScheduleVersion(tx *sql.Tx, medID string) error {
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM medication_schedule_versions WHERE medication_id = ?`, medID).Scan(&n); err != nil || n > 0 {
		return err
	}
	var slots, days, times, dosage, created string
	var active bool
	err := tx.QueryRow(`SELECT frequency_slots, COALESCE(days_of_week, ''), COALESCE(slot_times, ''), dosage, COALESCE(created_at, ''), COALESCE(is_active, 1)
		FROM medications WHERE id = ?`, medID).Scan(&slots, &days, &times, &dosage, &created, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	v := parseVersion(slots, days, times, dosage)
	v.From = "2000-01-01"
	if t, err := time.Parse(sqliteTimeLayout, created); err == nil {
		v.From = t.In(ClinicalLocation()).Format("2006-01-02")
	}
	var to interface{}
	if !active {
		to = dayBefore(v.From)
	}
	return insertVersion(tx, medID, "", v, to)
}

func insertVersion(tx *sql.Tx, medID, by string, v scheduleVersion, to interface{}) error {
	slots, _ := json.Marshal(v.Slots)
	days, _ := json.Marshal(v.Days)
	times, _ := json.Marshal(v.Times)
	_, err := tx.Exec(`INSERT INTO medication_schedule_versions (id, medication_id, effective_from, effective_to, frequency_slots, days_of_week, slot_times, dosage, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, uuid.New().String(), medID, v.From, to, string(slots), string(days), string(times), v.Dosage, nullIfEmpty(by),
		time.Now().UTC().Format(sqliteTimeLayout))
	return err
}

// syncScheduleVersion makes the history match the medicine's row from the date
// `from` on, in the edit's transaction: a stopped medicine's versions end the day
// before; a changed schedule closes the open version and opens a new one. Versions
// that would only have started on or after `from` are replaced.
func syncScheduleVersion(tx *sql.Tx, medID, by, from string) error {
	var slots, days, times, dosage string
	var active bool
	err := tx.QueryRow(`SELECT frequency_slots, COALESCE(days_of_week, ''), COALESCE(slot_times, ''), dosage, COALESCE(is_active, 1)
		FROM medications WHERE id = ?`, medID).Scan(&slots, &days, &times, &dosage, &active)
	if err != nil {
		return err
	}
	prev := dayBefore(from)
	closeFrom := func() error {
		if _, err := tx.Exec(`DELETE FROM medication_schedule_versions WHERE medication_id = ? AND effective_from >= ?`, medID, from); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE medication_schedule_versions SET effective_to = ? WHERE medication_id = ? AND (effective_to IS NULL OR effective_to >= ?)`, prev, medID, from)
		return err
	}
	if !active {
		if err := closeFrom(); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE medications SET stopped_on = COALESCE(stopped_on, ?), stopped_by = COALESCE(stopped_by, NULLIF(?, '')) WHERE id = ?`, from, by, medID)
		return err
	}
	if _, err := tx.Exec(`UPDATE medications SET stopped_on = NULL, stopped_by = NULL WHERE id = ?`, medID); err != nil {
		return err
	}
	want := parseVersion(slots, days, times, dosage)
	var oSlots, oDays, oTimes, oDosage string
	err = tx.QueryRow(`SELECT frequency_slots, days_of_week, slot_times, dosage FROM medication_schedule_versions
		WHERE medication_id = ? AND effective_to IS NULL ORDER BY effective_from DESC LIMIT 1`, medID).Scan(&oSlots, &oDays, &oTimes, &oDosage)
	if err == nil && sameSchedule(parseVersion(oSlots, oDays, oTimes, oDosage), want) {
		return nil
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := closeFrom(); err != nil {
		return err
	}
	want.From = from
	return insertVersion(tx, medID, by, want, nil)
}

func sameSchedule(a, b scheduleVersion) bool {
	ja, _ := json.Marshal([]interface{}{a.Slots, a.Days, a.Times, a.Dosage})
	jb, _ := json.Marshal([]interface{}{b.Slots, b.Days, b.Times, b.Dosage})
	return string(ja) == string(jb)
}
