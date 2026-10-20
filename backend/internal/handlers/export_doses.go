package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Expected-dose adherence for the doctor report (issue #14). Expected doses come
// from each medication's schedule (frequency_slots × days_of_week, from the day
// it was added), not from what someone happened to tap. A dose with no entry is
// "not recorded" and is never counted as taken.

type MedReport struct {
	MedicationID string   `json:"medication_id"`
	NameAr       string   `json:"name_ar"`
	NameEn       string   `json:"name_en"`
	Dosage       string   `json:"dosage"`
	PastDosages  []string `json:"past_dosages,omitempty"` // other doses recorded in the period (dose changed)
	IsCritical   bool     `json:"is_critical"`
	Expected     int      `json:"expected"`
	Taken        int      `json:"taken"`
	Partial      int      `json:"partial"`
	Vomited      int      `json:"vomited"` // given (taken/partial) doses later vomited
	Refused      int      `json:"refused"`
	Skipped      int      `json:"skipped"`
	NotRecorded  int      `json:"not_recorded"`
	Rate         float64  `json:"rate"`         // fully taken / expected, %
	MissedDates  []string `json:"missed_dates"` // days with a dose not recorded, refused or skipped
}

type doseKey struct{ med, date, slot string }

type loggedDose struct {
	status  string
	vomited bool
	dosage  string
}

var weekdayKeys = [...]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// buildDoseReport counts expected doses for every active medication over the
// household-local dates [start, end] (inclusive). On "today", a slot whose time
// hasn't come yet is only counted if it was already logged.
func buildDoseReport(db *sql.DB, start, end, now time.Time) ([]MedReport, MedAdherence, error) {
	loc := ClinicalLocation()
	startStr, endStr := start.Format("2006-01-02"), end.Format("2006-01-02")

	// Logged doses (never voided ones), with any vomited follow-up.
	logs := map[doseKey]loggedDose{}
	rows, err := db.Query(`
		SELECT l.medication_id, l.scheduled_date, l.slot, l.status, COALESCE(l.dosage_snapshot, ''),
		       EXISTS (SELECT 1 FROM medication_log_events e WHERE e.log_id = l.id AND e.type = 'vomited')
		FROM medication_logs l
		WHERE l.scheduled_date BETWEEN ? AND ? AND l.voided_at IS NULL`, startStr, endStr)
	if err != nil {
		return nil, MedAdherence{}, fmt.Errorf("loading doses: %w", err)
	}
	for rows.Next() {
		var k doseKey
		var d loggedDose
		if err := rows.Scan(&k.med, &k.date, &k.slot, &d.status, &d.dosage, &d.vomited); err != nil {
			rows.Close()
			return nil, MedAdherence{}, fmt.Errorf("reading doses: %w", err)
		}
		logs[k] = d
	}
	rows.Close() // release the single pooled connection before the next query
	if err := rows.Err(); err != nil {
		return nil, MedAdherence{}, err
	}

	type med struct {
		MedReport
		slots   []string
		days    []string
		times   map[string]string
		created string // local YYYY-MM-DD
	}
	var meds []med
	mrows, err := db.Query(`SELECT id, name_ar, name_en, dosage, frequency_slots, COALESCE(days_of_week, ''), COALESCE(slot_times, ''),
		COALESCE(created_at, ''), COALESCE(is_critical, 0) FROM medications WHERE is_active = 1`)
	if err != nil {
		return nil, MedAdherence{}, fmt.Errorf("loading medications: %w", err)
	}
	for mrows.Next() {
		var m med
		var slotsJSON, daysJSON, timesJSON, created string
		if err := mrows.Scan(&m.MedicationID, &m.NameAr, &m.NameEn, &m.Dosage, &slotsJSON, &daysJSON, &timesJSON, &created, &m.IsCritical); err != nil {
			mrows.Close()
			return nil, MedAdherence{}, fmt.Errorf("reading medications: %w", err)
		}
		_ = json.Unmarshal([]byte(slotsJSON), &m.slots)
		_ = json.Unmarshal([]byte(daysJSON), &m.days)
		if len(m.days) == 0 {
			m.days = allDaysOfWeek
		}
		_ = json.Unmarshal([]byte(timesJSON), &m.times)
		m.times = normalizeSlotTimes(m.times, m.slots)
		if t, err := time.Parse(sqliteTimeLayout, created); err == nil {
			m.created = t.In(loc).Format("2006-01-02")
		}
		meds = append(meds, m)
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, MedAdherence{}, err
	}

	today := now.In(loc).Format("2006-01-02")
	nowClock := now.In(loc).Format("15:04")
	var total MedAdherence
	out := make([]MedReport, 0, len(meds))
	for _, m := range meds {
		r := m.MedReport
		r.MissedDates = []string{}
		missed := map[string]bool{}
		dosages := map[string]bool{}
		for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
			date := d.Format("2006-01-02")
			// A dose someone logged always counts. An unlogged one is only expected on
			// a scheduled weekday, from the day the medication was added, once due.
			scheduledDay := (m.created == "" || date >= m.created) && slices.Contains(m.days, weekdayKeys[d.Weekday()])
			for _, slot := range m.slots {
				l, logged := logs[doseKey{m.MedicationID, date, slot}]
				if !logged && (!scheduledDay || (date == today && m.times[slot] > nowClock)) {
					continue
				}
				r.Expected++
				if !logged {
					r.NotRecorded++
					missed[date] = true
					continue
				}
				if l.dosage != "" {
					dosages[l.dosage] = true
				}
				switch l.status {
				case "taken":
					r.Taken++
				case "partial":
					r.Partial++
				case "refused":
					r.Refused++
					missed[date] = true
				default:
					r.Skipped++
					missed[date] = true
				}
				if l.vomited {
					r.Vomited++
				}
			}
		}
		for d := range missed {
			r.MissedDates = append(r.MissedDates, d)
		}
		slices.Sort(r.MissedDates)
		for d := range dosages {
			if d != r.Dosage {
				r.PastDosages = append(r.PastDosages, d)
			}
		}
		slices.Sort(r.PastDosages)
		if r.Expected > 0 {
			r.Rate = float64(r.Taken) / float64(r.Expected) * 100
		}
		total.TotalScheduled += r.Expected
		total.TotalTaken += r.Taken
		total.TotalPartial += r.Partial
		total.TotalVomited += r.Vomited
		total.TotalRefused += r.Refused
		total.TotalSkipped += r.Skipped
		total.TotalNotRecorded += r.NotRecorded
		out = append(out, r)
	}
	// Anticoagulants / antiplatelets (is_critical) first, then by name.
	slices.SortStableFunc(out, func(a, b MedReport) int {
		if a.IsCritical != b.IsCritical {
			if a.IsCritical {
				return -1
			}
			return 1
		}
		return strings.Compare(a.NameEn, b.NameEn)
	})
	if total.TotalScheduled > 0 {
		total.AdherenceRate = float64(total.TotalTaken) / float64(total.TotalScheduled) * 100
	}
	return out, total, nil
}

// DailyBP is one household-local day of blood pressure readings.
type DailyBP struct {
	Date     string  `json:"date"`
	AvgSys   float64 `json:"avg_sys"`
	AvgDia   float64 `json:"avg_dia"`
	MaxSys   int     `json:"max_sys"`
	MinSys   int     `json:"min_sys"`
	Readings int     `json:"readings"`
}
