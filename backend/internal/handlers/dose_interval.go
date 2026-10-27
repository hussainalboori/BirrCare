package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"birrcare/backend/internal/models"
)

// Cross-midnight double-dose guard (issue #7). The UNIQUE(medication_id,
// scheduled_date, slot) lock cannot see that a bedtime dose given at 23:30 on D
// and the bedtime card for D+1 at 03:45 are the same risk. LogDose therefore also
// refuses a "taken" dose that falls within a minimum interval of any other taken
// dose of the same medication.

const maxIntervalHours = 72

// intervalHours is the minimum gap enforced between two taken doses: the doctor's
// value when an admin set one (0 turns the lock off), otherwise ¾ of the shortest
// gap between the medication's scheduled times. Once daily → 18 h; 08:00 and
// 20:00 → 9 h; 20:00 and 22:30 → 1.9 h, so a closely spaced pair is never blocked.
func intervalHours(configured *float64, slots []string, times map[string]string) float64 {
	if configured != nil {
		return *configured
	}
	var mins []int
	for _, s := range slots {
		t := times[s]
		if t == "" {
			t = defaultSlotTimes[s]
		}
		if hh, mm, ok := strings.Cut(t, ":"); ok {
			h, err1 := strconv.Atoi(hh)
			m, err2 := strconv.Atoi(mm)
			if err1 == nil && err2 == nil {
				mins = append(mins, h*60+m)
			}
		}
	}
	slices.Sort(mins)
	mins = slices.Compact(mins)
	gap := 24 * 60
	for i := range mins {
		next := mins[(i+1)%len(mins)]
		if i == len(mins)-1 {
			next += 24 * 60
		}
		if d := next - mins[i]; d > 0 && d < gap {
			gap = d
		}
	}
	return math.Round(float64(gap)*0.75/60*100) / 100
}

// validIntervalHours accepts nil (automatic) or 0..72 hours.
func validIntervalHours(v *float64) bool {
	return v == nil || (*v >= 0 && *v <= maxIntervalHours && !math.IsNaN(*v))
}

// parseOptionalInterval reads min_hours_between from an update payload:
// absent → (nil, false); null → (nil, true) = back to automatic; number → set.
func parseOptionalInterval(raw json.RawMessage) (*float64, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	if string(raw) == "null" {
		return nil, true, nil
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false, err
	}
	return &v, true, nil
}

func nullableFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

// sameIngredientIDs is an SQL subquery taking one medication id: that medicine
// and every other one with the same active ingredient (issue #61), so a brand and
// its generic share one interval lock and one 24-hour limit.
const sameIngredientIDs = `SELECT m2.id FROM medications m1 JOIN medications m2
	ON m2.id = m1.id OR (COALESCE(m1.active_ingredient, '') <> '' AND m2.active_ingredient = m1.active_ingredient)
	WHERE m1.id = ?`

// queryer is satisfied by *sql.DB and *sql.Tx.
type queryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

// nearestTakenDose returns the taken dose of medicationID (or a medicine with the
// same active ingredient) closest in time to at.
func nearestTakenDose(q queryer, medicationID string, at time.Time) (*models.LastDose, error) {
	var d models.LastDose
	var takenAt string
	err := q.QueryRow(`
		SELECT l.taken_at, l.slot, l.scheduled_date, c.name, COALESCE(c.avatar_color, '')
		FROM medication_logs l JOIN caregivers c ON c.id = l.caregiver_id
		WHERE l.medication_id IN (`+sameIngredientIDs+`) AND l.status IN ('taken', 'partial') AND l.taken_at IS NOT NULL AND l.voided_at IS NULL
		ORDER BY ABS(julianday(l.taken_at) - julianday(?)) LIMIT 1
	`, medicationID, at.UTC().Format(sqliteTimeLayout)).Scan(&takenAt, &d.Slot, &d.ScheduledDate, &d.CaregiverName, &d.AvatarColor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t, err := time.Parse(sqliteTimeLayout, takenAt)
	if err != nil {
		return nil, err
	}
	d.TakenAt = t.UTC().Format(time.RFC3339)
	d.HoursSince = math.Round(time.Since(t).Hours()*100) / 100
	return &d, nil
}

func writeDoseTooSoon(w http.ResponseWriter, req LogDoseRequest, other *models.LastDose, minHours float64) {
	t, _ := time.Parse(time.RFC3339, other.TakenAt)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":           "DOSE_TOO_SOON",
		"message":         "This medication was given too recently",
		"medication_id":   req.MedicationID,
		"slot":            req.Slot,
		"scheduled_date":  req.ScheduledDate,
		"last_taken_at":   other.TakenAt,
		"last_slot":       other.Slot,
		"caregiver_name":  other.CaregiverName,
		"avatar_color":    other.AvatarColor,
		"hours_since":     other.HoursSince,
		"min_hours":       minHours,
		"next_allowed_at": t.Add(time.Duration(minHours * float64(time.Hour))).UTC().Format(time.RFC3339),
	})
}
