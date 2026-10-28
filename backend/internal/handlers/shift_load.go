package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Night-shift fairness and rest (issue #66). The roster already knows who did
// which nights; this summarises it as a thank-you ("Fatima: 9 nights") and warns
// the Super Admin, without blocking, before someone's 4th night in a row. Nights
// are household time (APP_TIMEZONE), 22:00–07:00; a shift "covers a night" when
// it overlaps that window by 4 hours or more. A passed duty counts for whoever
// it was passed to (the assignment moves).

const (
	nightStartHour  = 22
	nightEndHour    = 7
	nightCoverMin   = 4 * time.Hour
	nightStreakWarn = 4 // warn when this would be someone's Nth night in a row
)

type dutyBlock struct {
	caregiverID, shiftID, date string
	start, end                 time.Time
}

func addDays(date string, n int) string {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return d.AddDate(0, 0, n).Format("2006-01-02")
}

// blockFor is the duty a shift (start/end "HH:MM") on a household date covers.
func blockFor(caregiverID, shiftID, date, startHHMM, endHHMM string) (dutyBlock, bool) {
	start, err := time.ParseInLocation("2006-01-02 15:04", date+" "+startHHMM, ClinicalLocation())
	if err != nil {
		return dutyBlock{}, false
	}
	return dutyBlock{caregiverID: caregiverID, shiftID: shiftID, date: date, start: start,
		end: start.Add(time.Duration(calculateDuration(startHHMM, endHHMM) * float64(time.Hour)))}, true
}

// loadDutyBlocks returns the assignments dated fromDate..toDate (inclusive).
// Retired shifts count: those nights were still worked.
func loadDutyBlocks(q querier, fromDate, toDate string) ([]dutyBlock, error) {
	rows, err := q.Query(`SELECT a.caregiver_id, a.shift_id, a.date, s.start_time, s.end_time
		FROM shift_assignments a JOIN shifts s ON s.id = a.shift_id WHERE a.date >= ? AND a.date <= ?`, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dutyBlock
	for rows.Next() {
		var cg, shift, date, st, en string
		if err := rows.Scan(&cg, &shift, &date, &st, &en); err != nil {
			return nil, err
		}
		if b, ok := blockFor(cg, shift, date, st, en); ok {
			out = append(out, b)
		}
	}
	return out, rows.Err()
}

// nightWindow is the night that starts on a household date: 22:00 → 07:00.
func nightWindow(date string) (time.Time, time.Time) {
	d, _ := time.ParseInLocation("2006-01-02", date, ClinicalLocation())
	return time.Date(d.Year(), d.Month(), d.Day(), nightStartHour, 0, 0, 0, ClinicalLocation()),
		time.Date(d.Year(), d.Month(), d.Day()+1, nightEndHour, 0, 0, 0, ClinicalLocation())
}

func overlap(a0, a1, b0, b1 time.Time) time.Duration {
	s, e := a0, a1
	if b0.After(s) {
		s = b0
	}
	if b1.Before(e) {
		e = b1
	}
	if e.After(s) {
		return e.Sub(s)
	}
	return 0
}

// nightTime is how much of the night starting on date the caregiver was on duty,
// counting only up to `until`.
func nightTime(blocks []dutyBlock, cg, date string, until time.Time) time.Duration {
	n0, n1 := nightWindow(date)
	if until.Before(n1) {
		n1 = until
	}
	var d time.Duration
	for _, b := range blocks {
		if b.caregiverID == cg {
			d += overlap(b.start, b.end, n0, n1)
		}
	}
	return d
}

func coversNight(blocks []dutyBlock, cg, date string) bool {
	return nightTime(blocks, cg, date, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)) >= nightCoverMin
}

// nightsInARow counts the nights the caregiver covered going back from date.
func nightsInARow(blocks []dutyBlock, cg, date string) int {
	n := 0
	for d := date; n < 60 && coversNight(blocks, cg, d); d = addDays(d, -1) {
		n++
	}
	return n
}

// streakIfAssigned is the run of nights the caregiver would have if they took
// shiftID on date (replacing whoever has it); 0 when that shift isn't a night.
func streakIfAssigned(q querier, cg, shiftID, date string) (int, error) {
	var st, en string
	if err := q.QueryRow(`SELECT start_time, end_time FROM shifts WHERE id = ?`, shiftID).Scan(&st, &en); err != nil {
		return 0, err
	}
	blocks, err := loadDutyBlocks(q, addDays(date, -10), addDays(date, 10))
	if err != nil {
		return 0, err
	}
	kept := blocks[:0]
	for _, b := range blocks {
		if !(b.shiftID == shiftID && b.date == date) {
			kept = append(kept, b)
		}
	}
	if b, ok := blockFor(cg, shiftID, date, st, en); ok {
		kept = append(kept, b)
	}
	if !coversNight(kept, cg, date) {
		return 0, nil
	}
	n := nightsInARow(kept, cg, date)
	for d := addDays(date, 1); n < 60 && coversNight(kept, cg, d); d = addDays(d, 1) {
		n++
	}
	return n, nil
}

// unavailableOn returns the caregiver's reason if they marked the date as away.
func unavailableOn(q querier, cg, date string) (bool, string, error) {
	var reason string
	err := q.QueryRow(`SELECT reason FROM caregiver_unavailability WHERE caregiver_id = ? AND start_date <= ? AND end_date >= ? ORDER BY start_date LIMIT 1`,
		cg, date, date).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", nil
	}
	return err == nil, reason, err
}

// assignmentWarnings: "consecutive_nights" (4th night in a row or more) and
// "unavailable" (they marked the date as away). Never a block.
func assignmentWarnings(q querier, cg, shiftID, date string) (map[string]interface{}, error) {
	out := map[string]interface{}{}
	var warnings []string
	streak, err := streakIfAssigned(q, cg, shiftID, date)
	if err != nil {
		return nil, err
	}
	if streak >= nightStreakWarn {
		warnings = append(warnings, "consecutive_nights")
		out["consecutive_nights"] = streak
	}
	away, reason, err := unavailableOn(q, cg, date)
	if err != nil {
		return nil, err
	}
	if away {
		warnings = append(warnings, "unavailable")
		out["unavailable_reason"] = reason
	}
	if len(warnings) > 0 {
		out["warning"] = warnings[0]
		out["warnings"] = warnings
	}
	return out, nil
}

// ---- Summary ----

type CaregiverLoad struct {
	CaregiverID       string  `json:"caregiver_id"`
	Name              string  `json:"name"`
	AvatarColor       string  `json:"avatar_color"`
	HoursOnDuty       float64 `json:"hours_on_duty"`
	NightHours        float64 `json:"night_hours"`
	Nights            int     `json:"nights"`
	ConsecutiveNights int     `json:"consecutive_nights"`
	LastNightOff      string  `json:"last_night_off,omitempty"` // household date the night started; "" = none in the period
	NightAlerts       int     `json:"night_alerts_answered"`
}

// shiftSummary sums the last `days` household days up to now.
func shiftSummary(q querier, days int, now time.Time) (map[string]interface{}, error) {
	loc := ClinicalLocation()
	now = now.In(loc)
	today := now.Format("2006-01-02")
	fromDate := addDays(today, -(days - 1))
	from, _ := time.ParseInLocation("2006-01-02", fromDate, loc)
	blocks, err := loadDutyBlocks(q, addDays(fromDate, -1), today)
	if err != nil {
		return nil, err
	}
	// The latest night that has started: tonight after 22:00, else last night.
	latest := addDays(today, -1)
	if n0, _ := nightWindow(today); !now.Before(n0) {
		latest = today
	}

	people := map[string]*CaregiverLoad{}
	rows, err := q.Query(`SELECT id, name, COALESCE(avatar_color, '') FROM caregivers WHERE is_active = 1 AND role != 'viewer'`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c CaregiverLoad
		if rows.Scan(&c.CaregiverID, &c.Name, &c.AvatarColor) == nil {
			people[c.CaregiverID] = &c
		}
	}
	rows.Close()

	for id, c := range people {
		var hours time.Duration
		for _, b := range blocks {
			if b.caregiverID == id {
				hours += overlap(b.start, b.end, from, now)
			}
		}
		var night time.Duration
		for d := addDays(fromDate, -1); d <= latest; d = addDays(d, 1) {
			n0, n1 := nightWindow(d)
			if n0.Before(from) {
				n0 = from
			}
			if now.Before(n1) {
				n1 = now
			}
			for _, b := range blocks {
				if b.caregiverID == id {
					night += overlap(b.start, b.end, n0, n1)
				}
			}
			if d >= fromDate && nightTime(blocks, id, d, now) >= nightCoverMin {
				c.Nights++
			}
		}
		for d := latest; d >= fromDate; d = addDays(d, -1) {
			if nightTime(blocks, id, d, now) == 0 {
				c.LastNightOff = d
				break
			}
		}
		c.HoursOnDuty = float64(int(hours.Hours()*10+0.5)) / 10
		c.NightHours = float64(int(night.Hours()*10+0.5)) / 10
		c.ConsecutiveNights = nightsInARow(blocks, id, latest)
	}

	// Night alerts they answered: alerts they tapped "Seen" on between 22:00 and 07:00.
	arows, err := q.Query(`SELECT caregiver_id, acked_at FROM alert_acks WHERE acked_at >= ?`, from.UTC().Format(sqliteTimeLayout))
	if err != nil {
		return nil, err
	}
	for arows.Next() {
		var cg, at string
		if arows.Scan(&cg, &at) != nil {
			continue
		}
		t, err := time.Parse(sqliteTimeLayout, at)
		if err != nil || people[cg] == nil {
			continue
		}
		if h := t.In(loc).Hour(); h >= nightStartHour || h < nightEndHour {
			people[cg].NightAlerts++
		}
	}
	arows.Close()

	list := make([]CaregiverLoad, 0, len(people))
	for _, c := range people {
		list = append(list, *c)
	}
	// By name: a thank-you, not a ranking.
	sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name) })
	return map[string]interface{}{"from": fromDate, "to": today, "days": days, "night_start": "22:00", "night_end": "07:00", "caregivers": list}, nil
}

// Summary: GET /shifts/summary?days=30.
func (h *ShiftsHandler) Summary(w http.ResponseWriter, r *http.Request) {
	days := 30
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v >= 1 && v <= 92 {
		days = v
	}
	out, err := shiftSummary(h.DB, days, time.Now())
	if err != nil {
		internalError(w, "failed to summarise shifts", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// Load: GET /shifts/load?date=YYYY-MM-DD — for the assignment picker: which
// shifts that day are nights, and per caregiver the nights in a row before it
// and whether they marked the day as away.
func (h *ShiftsHandler) Load(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if _, _, ok := parseClinicalDate(date); !ok {
		writeJSONError(w, http.StatusBadRequest, "INVALID_DATE", "date must be YYYY-MM-DD")
		return
	}
	blocks, err := loadDutyBlocks(h.DB, addDays(date, -10), date)
	if err != nil {
		internalError(w, "failed to load the roster", err)
		return
	}
	nightShifts := []string{}
	rows, err := h.DB.Query(`SELECT id, start_time, end_time FROM shifts WHERE is_active = 1`)
	if err != nil {
		internalError(w, "failed to load shifts", err)
		return
	}
	for rows.Next() {
		var id, st, en string
		if rows.Scan(&id, &st, &en) != nil {
			continue
		}
		if b, ok := blockFor("_", id, date, st, en); ok && coversNight([]dutyBlock{b}, "_", date) {
			nightShifts = append(nightShifts, id)
		}
	}
	rows.Close()
	type person struct {
		CaregiverID  string `json:"caregiver_id"`
		NightsBefore int    `json:"nights_before"`
		Unavailable  bool   `json:"unavailable"`
		Reason       string `json:"unavailable_reason,omitempty"`
	}
	var ids []string
	crows, err := h.DB.Query(`SELECT id FROM caregivers WHERE is_active = 1 AND role != 'viewer'`)
	if err != nil {
		internalError(w, "failed to load caregivers", err)
		return
	}
	for crows.Next() {
		var id string
		if crows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	crows.Close()
	people := []person{}
	for _, id := range ids {
		away, reason, err := unavailableOn(h.DB, id, date)
		if err != nil {
			internalError(w, "failed to load availability", err)
			return
		}
		people = append(people, person{CaregiverID: id, NightsBefore: nightsInARow(blocks, id, addDays(date, -1)), Unavailable: away, Reason: reason})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"date": date, "night_shift_ids": nightShifts, "caregivers": people})
}

// ---- Unavailable dates ----

type Unavailability struct {
	ID          string `json:"id"`
	CaregiverID string `json:"caregiver_id"`
	Name        string `json:"name"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Reason      string `json:"reason,omitempty"`
}

// ListUnavailable: GET /shifts/unavailable?from=&to= (default today .. +90 days).
func (h *ShiftsHandler) ListUnavailable(w http.ResponseWriter, r *http.Request) {
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if _, _, ok := parseClinicalDate(from); !ok {
		from = clinicalToday()
	}
	if _, _, ok := parseClinicalDate(to); !ok {
		to = addDays(from, 90)
	}
	rows, err := h.DB.Query(`SELECT u.id, u.caregiver_id, COALESCE(c.name, ''), u.start_date, u.end_date, u.reason
		FROM caregiver_unavailability u LEFT JOIN caregivers c ON c.id = u.caregiver_id
		WHERE u.end_date >= ? AND u.start_date <= ? ORDER BY u.start_date`, from, to)
	if err != nil {
		internalError(w, "failed to load unavailable dates", err)
		return
	}
	defer rows.Close()
	out := []Unavailability{}
	for rows.Next() {
		var u Unavailability
		if err := rows.Scan(&u.ID, &u.CaregiverID, &u.Name, &u.StartDate, &u.EndDate, &u.Reason); err != nil {
			internalError(w, "failed to load unavailable dates", err)
			return
		}
		out = append(out, u)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// AddUnavailable: POST /shifts/unavailable {caregiver_id?, start_date, end_date, reason?}.
// Anyone for themselves; the Super Admin (who plans the roster) for anyone.
func (h *ShiftsHandler) AddUnavailable(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "viewers cannot change the roster")
		return
	}
	var req struct {
		CaregiverID string `json:"caregiver_id"`
		StartDate   string `json:"start_date"`
		EndDate     string `json:"end_date"`
		Reason      string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	if req.CaregiverID == "" {
		req.CaregiverID = me.ID
	}
	if req.CaregiverID != me.ID && !auth.CanManageShifts(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "you can only mark your own dates")
		return
	}
	if req.EndDate == "" {
		req.EndDate = req.StartDate
	}
	s, _, ok1 := parseClinicalDate(req.StartDate)
	e, _, ok2 := parseClinicalDate(req.EndDate)
	if !ok1 || !ok2 || e < s || e > addDays(s, 366) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_DATE", "start_date and end_date must be dates, end on or after start, within a year")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if len([]rune(req.Reason)) > 120 {
		writeJSONError(w, http.StatusBadRequest, "REASON_TOO_LONG", "keep the reason short")
		return
	}
	var n int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM caregivers WHERE id = ? AND is_active = 1`, req.CaregiverID).Scan(&n); err != nil || n == 0 {
		writeJSONError(w, http.StatusBadRequest, "NOT_FOUND", "caregiver not found")
		return
	}
	u := Unavailability{ID: uuid.New().String(), CaregiverID: req.CaregiverID, StartDate: s, EndDate: e, Reason: req.Reason}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to save", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO caregiver_unavailability (id, caregiver_id, start_date, end_date, reason, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.CaregiverID, s, e, u.Reason, me.ID, time.Now().UTC().Format(sqliteTimeLayout)); err != nil {
		internalError(w, "failed to save", err)
		return
	}
	if err := audit.Record(tx, me.ID, "UNAVAILABLE_ADDED", "caregiver", u.CaregiverID, fmt.Sprintf("%s to %s: %s", s, e, u.Reason)); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}
	if err := tx.QueryRow(`SELECT name FROM caregivers WHERE id = ?`, u.CaregiverID).Scan(&u.Name); err != nil {
		internalError(w, "failed to save", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to save", err)
		return
	}
	h.Broker.Broadcast("DUTY_UPDATED", actorOf(me), map[string]interface{}{"unavailable": u.ID})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(u)
}

// RemoveUnavailable: DELETE /shifts/unavailable/{id} (theirs, or the Super Admin).
func (h *ShiftsHandler) RemoveUnavailable(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "viewers cannot change the roster")
		return
	}
	id := chi.URLParam(r, "id")
	var owner, s, e string
	err := h.DB.QueryRow(`SELECT caregiver_id, start_date, end_date FROM caregiver_unavailability WHERE id = ?`, id).Scan(&owner, &s, &e)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "not found")
		return
	} else if err != nil {
		internalError(w, "failed to remove", err)
		return
	}
	if owner != me.ID && !auth.CanManageShifts(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "you can only remove your own dates")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to remove", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM caregiver_unavailability WHERE id = ?`, id); err != nil {
		internalError(w, "failed to remove", err)
		return
	}
	if err := audit.Record(tx, me.ID, "UNAVAILABLE_REMOVED", "caregiver", owner, fmt.Sprintf("%s to %s", s, e)); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to remove", err)
		return
	}
	h.Broker.Broadcast("DUTY_UPDATED", actorOf(me), map[string]interface{}{"unavailable": id, "status": "deleted"})
	w.WriteHeader(http.StatusNoContent)
}
