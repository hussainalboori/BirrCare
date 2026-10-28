package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Issue #20: medicine stock. Optional per medicine (stock_units NULL = not
// tracked). Every given or partial dose takes units_per_dose out of the box in
// the same transaction as the dose; cancelling the dose puts it back. When the
// days left drop below refill_threshold_days, everyone is alerted once.

type StockStatus struct {
	MedicationID  string   `json:"medication_id"`
	NameAr        string   `json:"name_ar"`
	NameEn        string   `json:"name_en"`
	Dosage        string   `json:"dosage"`
	StockUnits    float64  `json:"stock_units"`
	UnitsPerDose  float64  `json:"units_per_dose"`
	DosesPerDay   float64  `json:"doses_per_day"`
	DaysLeft      *float64 `json:"days_left"` // nil when nothing is scheduled
	ThresholdDays int      `json:"refill_threshold_days"`
	Low           bool     `json:"low"`
	BloodThinner  bool     `json:"blood_thinner"`
	// Issue #65: where to buy it, whether the doctor must write a new
	// prescription first, and who is getting it (the open refill task).
	PharmacyName      string      `json:"pharmacy_name,omitempty"`
	PharmacyPhone     string      `json:"pharmacy_phone,omitempty"`
	NeedsPrescription bool        `json:"needs_prescription"`
	Refill            *RefillTask `json:"refill,omitempty"`
}

// dosesPerDay averages the schedule over a week: 2 slots on 3 days = 6/7 a day.
func dosesPerDay(slots, days []string) float64 {
	if len(days) == 0 {
		days = allDaysOfWeek
	}
	return float64(len(slots)) * float64(len(days)) / 7
}

func daysLeft(stock, perDose, perDay float64) *float64 {
	if perDose <= 0 || perDay <= 0 {
		return nil
	}
	d := math.Floor(stock/(perDose*perDay)*10) / 10
	return &d
}

const stockSelect = `SELECT id, name_ar, name_en, dosage, stock_units, units_per_dose, refill_threshold_days,
	frequency_slots, COALESCE(days_of_week, ''), COALESCE(is_critical, 0), COALESCE(low_stock_alerted_at, ''),
	COALESCE(pharmacy_name, ''), COALESCE(pharmacy_phone, ''), COALESCE(needs_prescription, 0)
	FROM medications`

type stockRow struct {
	StockStatus
	tracked bool
	alerted bool
	rate    float64 // exact doses per day
}

func scanStock(scan func(...any) error) (stockRow, error) {
	var r stockRow
	var stock sql.NullFloat64
	var slotsJSON, daysJSON, alerted string
	if err := scan(&r.MedicationID, &r.NameAr, &r.NameEn, &r.Dosage, &stock, &r.UnitsPerDose, &r.ThresholdDays,
		&slotsJSON, &daysJSON, &r.BloodThinner, &alerted, &r.PharmacyName, &r.PharmacyPhone, &r.NeedsPrescription); err != nil {
		return r, err
	}
	var slots, days []string
	_ = json.Unmarshal([]byte(slotsJSON), &slots)
	_ = json.Unmarshal([]byte(daysJSON), &days)
	r.tracked = stock.Valid
	r.StockUnits = stock.Float64
	r.alerted = alerted != ""
	r.rate = dosesPerDay(slots, days)
	r.DosesPerDay = math.Round(r.rate*100) / 100
	r.DaysLeft = daysLeft(r.StockUnits, r.UnitsPerDose, r.rate)
	r.Low = r.tracked && r.DaysLeft != nil && *r.DaysLeft < float64(r.ThresholdDays)
	return r, nil
}

// stockChange is what a write did to one medicine's stock, for SSE and push
// after the commit.
type stockChange struct {
	Status     StockStatus
	AlertID    string
	RefillDone string // the refill task this change closed (issue #65)
}

// changeStock adds delta (negative = out of the box) inside tx, never below 0,
// records the event, and raises the low-stock alert once. Not tracked: nil.
func changeStock(tx *sql.Tx, medID, logID, caregiverID, reason string, delta float64, now time.Time) (*stockChange, error) {
	r, err := scanStock(tx.QueryRow(stockSelect+` WHERE id = ?`, medID).Scan)
	if err != nil || !r.tracked {
		return nil, err
	}
	after := math.Max(0, r.StockUnits+delta)
	ts := now.UTC().Format(sqliteTimeLayout)
	if _, err := tx.Exec(`UPDATE medications SET stock_units = ? WHERE id = ?`, after, medID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT INTO stock_events (id, medication_id, delta, stock_after, reason, log_id, caregiver_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, uuid.New().String(), medID, after-r.StockUnits, after, reason, nullIfEmpty(logID), nullIfEmpty(caregiverID), ts); err != nil {
		return nil, err
	}
	r.StockUnits = after
	r.DaysLeft = daysLeft(after, r.UnitsPerDose, r.rate)
	r.Low = r.DaysLeft != nil && *r.DaysLeft < float64(r.ThresholdDays)
	ch := &stockChange{Status: r.StockStatus}
	// Issue #59: no low-stock alert while care is paused (she isn't at home);
	// it comes with the next dose after the return.
	paused, err := pausedAt(tx, now)
	if err != nil {
		return nil, err
	}
	switch {
	case r.Low && !r.alerted && paused == nil:
		if _, err := tx.Exec(`UPDATE medications SET low_stock_alerted_at = ? WHERE id = ?`, ts, medID); err != nil {
			return nil, err
		}
		if ch.AlertID, err = recordAlert(tx, "low_stock", medID, caregiverID, map[string]interface{}{
			"medication_id": medID, "name_ar": r.NameAr, "name_en": r.NameEn, "stock_units": after,
			"days_left": *r.DaysLeft, "blood_thinner": r.BloodThinner,
			"needs_prescription": r.NeedsPrescription, "pharmacy_name": r.PharmacyName, "pharmacy_phone": r.PharmacyPhone,
		}); err != nil {
			return nil, err
		}
		// Issue #65: someone has to say "I'll get it".
		if err := ensureRefillTask(tx, medID, ch.AlertID, now); err != nil {
			return nil, err
		}
	case !r.Low && r.alerted:
		// Refilled above the threshold: the next drop alerts again.
		if _, err := tx.Exec(`UPDATE medications SET low_stock_alerted_at = NULL WHERE id = ?`, medID); err != nil {
			return nil, err
		}
		if ch.RefillDone, err = completeRefillTask(tx, medID, caregiverID, now); err != nil {
			return nil, err
		}
	}
	return ch, nil
}

// takeDoseFromStock removes one dose (a partial dose too: the pill is gone).
func takeDoseFromStock(tx *sql.Tx, medID, logID, caregiverID, status string, now time.Time) (*stockChange, error) {
	if status != "taken" && status != "partial" {
		return nil, nil
	}
	var perDose float64
	if err := tx.QueryRow(`SELECT units_per_dose FROM medications WHERE id = ? AND stock_units IS NOT NULL`, medID).Scan(&perDose); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	reason := "dose"
	if status == "partial" {
		reason = "partial"
	}
	return changeStock(tx, medID, logID, caregiverID, reason, -perDose, now)
}

// returnDoseToStock puts back what a cancelled dose took out.
func returnDoseToStock(tx *sql.Tx, medID, logID, caregiverID string, now time.Time) (*stockChange, error) {
	var taken sql.NullFloat64
	if err := tx.QueryRow(`SELECT SUM(delta) FROM stock_events WHERE log_id = ?`, logID).Scan(&taken); err != nil || !taken.Valid || taken.Float64 == 0 {
		return nil, err
	}
	return changeStock(tx, medID, logID, caregiverID, "correction", -taken.Float64, now)
}

func (h *MedicationHandler) broadcastStock(ch *stockChange, c *models.Caregiver) {
	if ch == nil {
		return
	}
	if ch.AlertID != "" {
		h.Broker.BroadcastAlert(ch.AlertID, "STOCK_UPDATED", actorOf(c), map[string]interface{}{"stock": ch.Status, "alert_id": ch.AlertID})
		h.Push.NotifyAsync(ch.AlertID, "low_stock", c.ID)
		return
	}
	payload := map[string]interface{}{"stock": ch.Status}
	if ch.RefillDone != "" {
		payload["alert_id"] = ch.RefillDone // apps reload their alerts: the follow-ups are gone
	}
	h.Broker.Broadcast("STOCK_UPDATED", actorOf(c), payload)
}

// ListStock returns every medicine whose stock is tracked, lowest first.
func (h *MedicationHandler) ListStock(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), stockSelect+` WHERE is_active = 1 AND stock_units IS NOT NULL`)
	if err != nil {
		internalError(w, "failed to load stock", err)
		return
	}
	out := []StockStatus{}
	for rows.Next() {
		row, err := scanStock(rows.Scan)
		if err != nil {
			rows.Close()
			internalError(w, "failed to load stock", err)
			return
		}
		out = append(out, row.StockStatus)
	}
	rows.Close() // release the single pooled connection before the next query
	tasks, err := openRefillTasks(h.DB)
	if err != nil {
		internalError(w, "failed to load refill tasks", err)
		return
	}
	for i := range out {
		if t, ok := tasks[out[i].MedicationID]; ok {
			out[i].Refill = &t
		}
	}
	for i := 1; i < len(out); i++ { // few rows: insertion sort by days left
		for j := i; j > 0 && lessDays(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

func lessDays(a, b StockStatus) bool {
	if a.DaysLeft == nil {
		return false
	}
	return b.DaysLeft == nil || *a.DaysLeft < *b.DaysLeft
}

type StockRequest struct {
	// "refill": add units (a new box). "count": the number in the box now.
	Action string  `json:"action"`
	Units  float64 `json:"units"`
}

// UpdateStock records a refill or a count. Any caregiver may (they are the
// ones at the pharmacy); viewers may not.
func (h *MedicationHandler) UpdateStock(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "viewers cannot change stock")
		return
	}
	medID := chi.URLParam(r, "id")
	var req StockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	if (req.Action != "refill" && req.Action != "count") || req.Units < 0 || req.Units > 10000 || (req.Action == "refill" && req.Units == 0) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_STOCK", "action must be refill (units > 0) or count (units >= 0), up to 10000")
		return
	}
	now := time.Now()
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to update stock", err)
		return
	}
	defer tx.Rollback()
	var current sql.NullFloat64
	if err := tx.QueryRow(`SELECT stock_units FROM medications WHERE id = ? AND is_active = 1`, medID).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "medication not found")
		return
	} else if err != nil {
		internalError(w, "failed to update stock", err)
		return
	}
	if !current.Valid {
		writeJSONError(w, http.StatusConflict, "NOT_TRACKED", "stock is not tracked for this medicine; an admin can turn it on")
		return
	}
	delta := req.Units
	if req.Action == "count" {
		delta = req.Units - current.Float64
	}
	ch, err := changeStock(tx, medID, "", me.ID, req.Action, delta, now)
	// A new box closes the refill task even if the medicine is still below its
	// threshold: someone went to the pharmacy (issue #65).
	if err == nil && req.Action == "refill" && ch.RefillDone == "" {
		ch.RefillDone, err = completeRefillTask(tx, medID, me.ID, now)
		if err == nil && ch.RefillDone != "" {
			ch.Status.Refill = nil
		}
	}
	if err == nil {
		err = audit.Record(tx, me.ID, "STOCK_"+map[string]string{"refill": "REFILLED", "count": "COUNTED"}[req.Action], "medication", medID,
			fmt.Sprintf("%s %g units; now %g", req.Action, req.Units, ch.Status.StockUnits))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		internalError(w, "failed to update stock", err)
		return
	}
	h.broadcastStock(ch, me)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ch.Status)
}

// StockSettings are the admin's per-medicine stock options (create/update).
type StockSettings struct {
	// StockUnits turns tracking on with this count; TrackStock=false turns it off.
	StockUnits          *float64 `json:"stock_units,omitempty"`
	TrackStock          *bool    `json:"track_stock,omitempty"`
	UnitsPerDose        *float64 `json:"units_per_dose,omitempty"`
	RefillThresholdDays *int     `json:"refill_threshold_days,omitempty"`
}

func (s StockSettings) validate() string {
	if s.StockUnits != nil && (*s.StockUnits < 0 || *s.StockUnits > 10000) {
		return "stock_units must be between 0 and 10000"
	}
	if s.UnitsPerDose != nil && (*s.UnitsPerDose <= 0 || *s.UnitsPerDose > 100) {
		return "units_per_dose must be more than 0 and at most 100"
	}
	if s.RefillThresholdDays != nil && (*s.RefillThresholdDays < 1 || *s.RefillThresholdDays > 60) {
		return "refill_threshold_days must be between 1 and 60"
	}
	return ""
}

// applyStockSettings saves the admin's stock options in tx and returns the
// resulting change (for SSE), if the count was set.
func applyStockSettings(tx *sql.Tx, medID, caregiverID string, s StockSettings, now time.Time) (*stockChange, error) {
	if s.UnitsPerDose != nil {
		if _, err := tx.Exec(`UPDATE medications SET units_per_dose = ? WHERE id = ?`, *s.UnitsPerDose, medID); err != nil {
			return nil, err
		}
	}
	if s.RefillThresholdDays != nil {
		if _, err := tx.Exec(`UPDATE medications SET refill_threshold_days = ? WHERE id = ?`, *s.RefillThresholdDays, medID); err != nil {
			return nil, err
		}
	}
	if s.TrackStock != nil && !*s.TrackStock {
		_, err := tx.Exec(`UPDATE medications SET stock_units = NULL, low_stock_alerted_at = NULL WHERE id = ?`, medID)
		return nil, err
	}
	if s.StockUnits == nil {
		return nil, nil
	}
	var current sql.NullFloat64
	if err := tx.QueryRow(`SELECT stock_units FROM medications WHERE id = ?`, medID).Scan(&current); err != nil {
		return nil, err
	}
	if !current.Valid {
		if _, err := tx.Exec(`UPDATE medications SET stock_units = 0, low_stock_alerted_at = NULL WHERE id = ?`, medID); err != nil {
			return nil, err
		}
	}
	return changeStock(tx, medID, "", caregiverID, "count", *s.StockUnits-current.Float64, now)
}

// saveStockSettings applies the admin's stock options in their own transaction.
func (h *MedicationHandler) saveStockSettings(medID string, c *models.Caregiver, s StockSettings) (*stockChange, error) {
	if s.StockUnits == nil && s.TrackStock == nil && s.UnitsPerDose == nil && s.RefillThresholdDays == nil {
		return nil, nil
	}
	tx, err := h.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ch, err := applyStockSettings(tx, medID, c.ID, s, time.Now())
	if err == nil {
		err = audit.Record(tx, c.ID, "STOCK_SETTINGS", "medication", medID, fmt.Sprintf("Stock settings: %+v", describeStock(s)))
	}
	if err == nil {
		err = tx.Commit()
	}
	return ch, err
}

func describeStock(s StockSettings) map[string]interface{} {
	out := map[string]interface{}{}
	if s.StockUnits != nil {
		out["stock_units"] = *s.StockUnits
	}
	if s.TrackStock != nil {
		out["track_stock"] = *s.TrackStock
	}
	if s.UnitsPerDose != nil {
		out["units_per_dose"] = *s.UnitsPerDose
	}
	if s.RefillThresholdDays != nil {
		out["refill_threshold_days"] = *s.RefillThresholdDays
	}
	return out
}
