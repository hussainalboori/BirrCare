package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
)

// The medicines page lists the day in clock order (Night first) unless the Super
// Admin chooses "shift": the list then starts at the current shift's start time.
const (
	medOrderKey   = "med_order"
	medOrderClock = "clock"
	medOrderShift = "shift"
)

func readMedOrder(q querier) (string, error) {
	var v string
	err := q.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, medOrderKey).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && v != medOrderShift) {
		return medOrderClock, nil
	}
	return v, err
}

// medOrderFrom is the "HH:MM" the medicines list starts from, or "" for clock order.
// Each person sees the list from their own shift, so the order doesn't change under
// them. Anyone without a shift gets the one running now; shifts can overlap (a 24-hour
// shift beside an 8 PM one), so that is the one that began most recently. Who is on
// duty still comes from findCurrentShift.
func medOrderFrom(q querier, caregiverID string, now time.Time) (string, error) {
	order, err := readMedOrder(q)
	if err != nil || order != medOrderShift {
		return "", err
	}
	now = now.In(ClinicalLocation())
	if own, err := ownShiftStart(q, caregiverID, now); err != nil || own != "" {
		return own, err
	}
	rows, err := q.Query(`SELECT start_time, end_time FROM shifts WHERE is_active = 1 ORDER BY display_order ASC, start_time ASC`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	clock := now.Format("15:04")
	from, nearest := "", minutesPerDay
	for rows.Next() {
		var start, end string
		if err := rows.Scan(&start, &end); err != nil {
			return "", err
		}
		if !isTimeInShift(clock, start, end) {
			continue
		}
		if since := minutesSince(clock, start); since < nearest {
			from, nearest = start, since
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	rows.Close() // the only connection: free it before the next query
	if from != "" {
		return from, nil
	}

	// Nothing covers this hour: the same fallback as the duty roster.
	shift, _, err := findCurrentShift(q, now)
	if err != nil || shift == nil {
		return "", err
	}
	return shift.StartTime, nil
}

// ownShiftStart is the start of the caregiver's shift: today's, or last night's while
// it is still running. With several, the one running now, else the next to begin.
func ownShiftStart(q querier, caregiverID string, now time.Time) (string, error) {
	if caregiverID == "" {
		return "", nil
	}
	today, yesterday := now.Format("2006-01-02"), now.AddDate(0, 0, -1).Format("2006-01-02")
	rows, err := q.Query(`
		SELECT s.start_time, s.end_time, a.date
		FROM shift_assignments a JOIN shifts s ON s.id = a.shift_id
		WHERE a.caregiver_id = ? AND a.date IN (?, ?) AND s.is_active = 1
		ORDER BY s.display_order ASC, s.start_time ASC
	`, caregiverID, today, yesterday)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	clock := now.Format("15:04")
	from, best := "", 2*minutesPerDay
	for rows.Next() {
		var start, end, date string
		if err := rows.Scan(&start, &end, &date); err != nil {
			return "", err
		}
		running := isTimeInShift(clock, start, end)
		// Yesterday's shift counts only if it reaches into today and hasn't ended.
		if date == yesterday && (start < end || clock >= start || !running) {
			continue
		}
		// Today's overnight shift hasn't begun in the small hours; last night's has.
		if date == today && start >= end && clock < start {
			running = false
		}
		rank := minutesPerDay + (minutesPerDay-minutesSince(clock, start))%minutesPerDay
		if running {
			rank = minutesSince(clock, start)
		}
		if rank < best {
			from, best = start, rank
		}
	}
	return from, rows.Err()
}

const minutesPerDay = 24 * 60

// minutesSince is how long ago "HH:MM" start last came round, from 0 to a day.
func minutesSince(clock, start string) int {
	mins := func(hhmm string) int {
		t, err := time.Parse("15:04", hhmm)
		if err != nil {
			return 0
		}
		return t.Hour()*60 + t.Minute()
	}
	return (mins(clock) - mins(start) + minutesPerDay) % minutesPerDay
}

type medOrderBody struct {
	Order string `json:"order"`
}

// GetMedOrder reports the medicines list order chosen in the shift settings.
func (h *ShiftsHandler) GetMedOrder(w http.ResponseWriter, r *http.Request) {
	order, err := readMedOrder(h.DB)
	if err != nil {
		internalError(w, "failed to read the medicines order", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(medOrderBody{Order: order})
}

// PutMedOrder changes it (Super Admin, like the rest of the shift settings).
func (h *ShiftsHandler) PutMedOrder(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageShifts(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only Super Admin can change the medicines order")
		return
	}
	var req medOrderBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Order != medOrderClock && req.Order != medOrderShift) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_ORDER", "order must be clock or shift")
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		internalError(w, "failed to start the transaction", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO app_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, medOrderKey, req.Order); err != nil {
		internalError(w, "failed to save the medicines order", err)
		return
	}
	if err := audit.Record(tx, caregiver.ID, "MED_ORDER_CHANGED", "app_settings", medOrderKey, "Medicines list order: "+req.Order); err != nil {
		internalError(w, "failed to audit the medicines order", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to save the medicines order", err)
		return
	}

	if h.Broker != nil {
		h.Broker.Broadcast("SHIFT_UPDATED", models.ActorInfo{ID: caregiver.ID, Name: caregiver.Name, AvatarColor: caregiver.AvatarColor}, map[string]string{"med_order": req.Order})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(medOrderBody{Order: req.Order})
}
