package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Insulin safety model
//
//   - The app never computes a dose. An admin copies the doctor's correction scale
//     ("sugar X–Y mmol/L → N units") or fixed dose into a plan; the app only looks it up.
//   - Every injection is logged together with a reading taken right then (it is also
//     written to vitals_logs), so a stale reading can never drive a dose.
//   - Below hypoThreshold, correction insulin is refused outright.
//   - A second injection of the same plan inside min_hours_between is refused with
//     who/when (insulin stacking). Only an admin can override, with a written reason.
//   - Giving anything other than what the scale says requires a reason; the doctor
//     report shows these so the doctor can adjust the scale.
//
// The interval check and the insert run in one transaction. With _txlock=immediate
// and a single pooled connection, two siblings tapping at once are serialized: the
// second one sees the first injection and gets 409.

const (
	hypoThreshold      = 3.9
	maxInsulinUnits    = 100.0
	maxScaleRows       = 20
	minSugarReading    = 1.0
	maxSugarReading    = 45.0
	maxInsulinReason   = 500
	defaultMinHoursGap = 4.0
)

type InsulinHandler struct {
	DB     *sql.DB
	Broker *sse.Broker
}

type ScaleRow struct {
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Units float64 `json:"units"`
}

type InsulinPlan struct {
	ID              string            `json:"id"`
	NameAr          string            `json:"name_ar"`
	NameEn          string            `json:"name_en"`
	Kind            string            `json:"kind"` // 'scale' or 'fixed'
	FixedUnits      *float64          `json:"fixed_units,omitempty"`
	Scale           []ScaleRow        `json:"scale"`
	MinHoursBetween float64           `json:"min_hours_between"`
	FrequencySlots  []string          `json:"frequency_slots"`
	SlotTimes       map[string]string `json:"slot_times,omitempty"`
	PrescribedBy    string            `json:"prescribed_by,omitempty"`
	NotesAr         string            `json:"notes_ar,omitempty"`
	NotesEn         string            `json:"notes_en,omitempty"`
	IsActive        bool              `json:"is_active"`
	UpdatedAt       string            `json:"updated_at,omitempty"`
}

type InsulinLog struct {
	ID            string   `json:"id"`
	PlanID        string   `json:"plan_id"`
	PlanNameAr    string   `json:"plan_name_ar,omitempty"`
	PlanNameEn    string   `json:"plan_name_en,omitempty"`
	CaregiverID   string   `json:"caregiver_id"`
	CaregiverName string   `json:"caregiver_name"`
	AvatarColor   string   `json:"avatar_color,omitempty"`
	GivenAt       string   `json:"given_at"`
	Status        string   `json:"status"`
	Units         float64  `json:"units"`
	ScaleUnits    *float64 `json:"scale_units"`
	BloodSugar    *float64 `json:"blood_sugar"`
	Reason        string   `json:"reason,omitempty"`
}

// LookupScale returns the doctor's units for a reading, or nil when the reading
// falls outside every row (the caregiver must then call the doctor).
func LookupScale(scale []ScaleRow, sugar float64) *float64 {
	for _, r := range scale {
		if sugar >= r.Min && sugar < r.Max {
			u := r.Units
			return &u
		}
	}
	return nil
}

func validUnits(u float64) bool {
	return u >= 0 && u <= maxInsulinUnits && math.Mod(u*2, 1) == 0 // whole or half units
}

// validateScale requires sorted, contiguous rows so no reading falls in a gap or two rows.
func validateScale(rows []ScaleRow) error {
	if len(rows) == 0 {
		return errors.New("the scale needs at least one row")
	}
	if len(rows) > maxScaleRows {
		return fmt.Errorf("the scale can have at most %d rows", maxScaleRows)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Min < rows[j].Min })
	for i, r := range rows {
		if r.Min < 0 || r.Max > maxSugarReading || r.Min >= r.Max {
			return fmt.Errorf("row %d: 'from' must be below 'to' (0–%.0f mmol/L)", i+1, maxSugarReading)
		}
		if !validUnits(r.Units) {
			return fmt.Errorf("row %d: units must be 0–%.0f in whole or half units", i+1, maxInsulinUnits)
		}
		if i > 0 && math.Abs(rows[i-1].Max-r.Min) > 1e-9 {
			return fmt.Errorf("row %d must start where row %d ends (%.1f)", i+1, i, rows[i-1].Max)
		}
	}
	return nil
}

func (h *InsulinHandler) loadPlans(onlyActive bool) ([]InsulinPlan, error) {
	q := `SELECT id, name_ar, name_en, kind, fixed_units, scale, min_hours_between,
	             COALESCE(frequency_slots, '[]'), COALESCE(slot_times, '{}'),
	             COALESCE(prescribed_by, ''), COALESCE(notes_ar, ''), COALESCE(notes_en, ''), is_active, COALESCE(updated_at, '')
	      FROM insulin_plans`
	if onlyActive {
		q += " WHERE is_active = 1"
	}
	q += " ORDER BY kind DESC, name_en ASC"
	rows, err := h.DB.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	plans := []InsulinPlan{}
	for rows.Next() {
		var p InsulinPlan
		var fixed sql.NullFloat64
		var scaleJSON, slotsJSON, timesJSON string
		if err := rows.Scan(&p.ID, &p.NameAr, &p.NameEn, &p.Kind, &fixed, &scaleJSON, &p.MinHoursBetween,
			&slotsJSON, &timesJSON, &p.PrescribedBy, &p.NotesAr, &p.NotesEn, &p.IsActive, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if fixed.Valid {
			f := fixed.Float64
			p.FixedUnits = &f
		}
		_ = json.Unmarshal([]byte(scaleJSON), &p.Scale)
		if p.Scale == nil {
			p.Scale = []ScaleRow{}
		}
		_ = json.Unmarshal([]byte(slotsJSON), &p.FrequencySlots)
		if p.FrequencySlots == nil {
			p.FrequencySlots = []string{}
		}
		_ = json.Unmarshal([]byte(timesJSON), &p.SlotTimes)
		if p.SlotTimes == nil {
			p.SlotTimes = map[string]string{}
		}
		p.UpdatedAt = sqliteToRFC3339(p.UpdatedAt)
		plans = append(plans, p)
	}
	return plans, rows.Err()
}

func (h *InsulinHandler) ListPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := h.loadPlans(r.URL.Query().Get("all") != "1")
	if err != nil {
		internalError(w, "failed to fetch insulin plans", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(plans)
}

type SavePlanRequest struct {
	NameAr          string            `json:"name_ar"`
	NameEn          string            `json:"name_en"`
	Kind            string            `json:"kind"`
	FixedUnits      *float64          `json:"fixed_units"`
	Scale           []ScaleRow        `json:"scale"`
	MinHoursBetween float64           `json:"min_hours_between"`
	FrequencySlots  []string          `json:"frequency_slots"`
	SlotTimes       map[string]string `json:"slot_times"`
	PrescribedBy    string            `json:"prescribed_by"`
	NotesAr         string            `json:"notes_ar"`
	NotesEn         string            `json:"notes_en"`
	IsActive        *bool             `json:"is_active"`
}

func (req *SavePlanRequest) normalize() (code, msg string) {
	req.NameAr = strings.TrimSpace(req.NameAr)
	req.NameEn = strings.TrimSpace(req.NameEn)
	if req.NameAr == "" && req.NameEn == "" {
		return "MISSING_NAME", "insulin name is required"
	}
	if req.NameAr == "" {
		req.NameAr = req.NameEn
	}
	if req.NameEn == "" {
		req.NameEn = req.NameAr
	}
	if req.FrequencySlots == nil {
		req.FrequencySlots = []string{}
	}
	for _, s := range req.FrequencySlots {
		if !allowedSlots[s] {
			return "INVALID_SLOTS", "frequency_slots must contain valid slots (morning, afternoon, evening, bedtime, night)"
		}
	}
	req.SlotTimes = normalizeSlotTimes(req.SlotTimes, req.FrequencySlots)

	if req.MinHoursBetween <= 0 {
		if len(req.FrequencySlots) > 0 {
			req.MinHoursBetween = intervalHours(nil, req.FrequencySlots, req.SlotTimes)
		} else {
			req.MinHoursBetween = defaultMinHoursGap
		}
	}
	if req.MinHoursBetween < 1 || req.MinHoursBetween > 48 {
		return "INVALID_INTERVAL", "minimum hours between injections must be 1–48"
	}
	switch req.Kind {
	case "scale":
		if err := validateScale(req.Scale); err != nil {
			return "INVALID_SCALE", err.Error()
		}
		req.FixedUnits = nil
	case "fixed":
		if req.FixedUnits == nil || *req.FixedUnits <= 0 || !validUnits(*req.FixedUnits) {
			return "INVALID_UNITS", "fixed dose must be 0.5–100 units in whole or half units"
		}
		req.Scale = []ScaleRow{}
	default:
		return "INVALID_KIND", "kind must be 'scale' or 'fixed'"
	}
	return "", ""
}

func (h *InsulinHandler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	h.savePlan(w, r, "")
}

func (h *InsulinHandler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_ID", "missing plan id")
		return
	}
	h.savePlan(w, r, id)
}

func (h *InsulinHandler) DeletePlan(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageMedications(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only Admins and Super Admins can delete insulin plans")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_ID", "missing plan id")
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to begin transaction", err)
		return
	}
	defer tx.Rollback()

	var nameAr, nameEn string
	err = tx.QueryRow("SELECT name_ar, name_en FROM insulin_plans WHERE id = ?", id).Scan(&nameAr, &nameEn)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "insulin plan not found")
			return
		}
		internalError(w, "failed to query insulin plan", err)
		return
	}

	var logCount int
	if err := tx.QueryRow("SELECT COUNT(*) FROM insulin_logs WHERE plan_id = ?", id).Scan(&logCount); err != nil {
		internalError(w, "failed to check existing insulin logs", err)
		return
	}

	deactivatedOnly := logCount > 0
	if logCount == 0 {
		if _, err := tx.Exec("DELETE FROM insulin_plans WHERE id = ?", id); err != nil {
			internalError(w, "failed to delete insulin plan", err)
			return
		}
	} else {
		if _, err := tx.Exec("UPDATE insulin_plans SET is_active = 0, updated_by = ?, updated_at = datetime('now') WHERE id = ?", caregiver.ID, id); err != nil {
			internalError(w, "failed to deactivate insulin plan", err)
			return
		}
	}

	action := "DEACTIVATE_INSULIN_PLAN"
	desc := fmt.Sprintf("Deactivated insulin plan %s (retained due to existing logs)", nameEn)
	if !deactivatedOnly {
		action = "DELETE_INSULIN_PLAN"
		desc = fmt.Sprintf("Deleted unused insulin plan %s", nameEn)
	}

	if err := audit.Record(tx, caregiver.ID, action, "insulin_plan", id, desc); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}

	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit insulin plan deletion", err)
		return
	}

	h.Broker.Broadcast("INSULIN_PLANS_UPDATED", actorOf(caregiver), map[string]interface{}{"id": id, "action": "deleted"})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "success",
		"message":     "Insulin plan deleted successfully",
		"deactivated": deactivatedOnly,
	})
}

func (h *InsulinHandler) savePlan(w http.ResponseWriter, r *http.Request, id string) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageMedications(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only Admins and Super Admins can enter the doctor's insulin plan")
		return
	}
	var req SavePlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	if code, msg := req.normalize(); code != "" {
		writeJSONError(w, http.StatusBadRequest, code, msg)
		return
	}
	scaleJSON, _ := json.Marshal(req.Scale)
	slotsJSON, _ := json.Marshal(req.FrequencySlots)
	timesJSON, _ := json.Marshal(req.SlotTimes)
	active := 1
	if req.IsActive != nil && !*req.IsActive {
		active = 0
	}

	var err error
	status := http.StatusOK
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to begin transaction", err)
		return
	}
	defer tx.Rollback()

	if id == "" {
		id = "ins_" + uuid.New().String()[:8]
		status = http.StatusCreated
		_, err = tx.Exec(`
			INSERT INTO insulin_plans (id, name_ar, name_en, kind, fixed_units, scale, min_hours_between, frequency_slots, slot_times, prescribed_by, notes_ar, notes_en, is_active, updated_by, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		`, id, req.NameAr, req.NameEn, req.Kind, req.FixedUnits, string(scaleJSON), req.MinHoursBetween,
			string(slotsJSON), string(timesJSON), req.PrescribedBy, req.NotesAr, req.NotesEn, active, caregiver.ID)
	} else {
		var res sql.Result
		res, err = tx.Exec(`
			UPDATE insulin_plans SET name_ar = ?, name_en = ?, kind = ?, fixed_units = ?, scale = ?, min_hours_between = ?,
			       frequency_slots = ?, slot_times = ?,
			       prescribed_by = ?, notes_ar = ?, notes_en = ?, is_active = ?, updated_by = ?, updated_at = datetime('now')
			WHERE id = ?
		`, req.NameAr, req.NameEn, req.Kind, req.FixedUnits, string(scaleJSON), req.MinHoursBetween,
			string(slotsJSON), string(timesJSON), req.PrescribedBy, req.NotesAr, req.NotesEn, active, caregiver.ID, id)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 0 {
				writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "insulin plan not found")
				return
			}
		}
	}
	if err != nil {
		internalError(w, "failed to save insulin plan", err)
		return
	}

	auditDetail := fmt.Sprintf("%s (%s) kind=%s fixed=%v scale=%s slots=%s gap=%.1fh by=%s active=%d",
		req.NameEn, req.NameAr, req.Kind, req.FixedUnits, scaleJSON, string(slotsJSON), req.MinHoursBetween, req.PrescribedBy, active)
	if err := audit.Record(tx, caregiver.ID, "INSULIN_PLAN_SAVED", "insulin_plan", id, auditDetail); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}

	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit insulin plan", err)
		return
	}

	h.Broker.Broadcast("INSULIN_PLANS_UPDATED", actorOf(caregiver), map[string]interface{}{"id": id})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"id": id})
}

func actorOf(c *models.Caregiver) models.ActorInfo {
	return models.ActorInfo{ID: c.ID, Name: c.Name, AvatarColor: c.AvatarColor}
}

const insulinLogSelect = `
	SELECT l.id, l.plan_id, p.name_ar, p.name_en, l.caregiver_id, c.name, COALESCE(c.avatar_color, ''),
	       COALESCE(l.given_at, ''), l.status, l.units, l.scale_units, l.blood_sugar, COALESCE(l.reason, '')
	FROM insulin_logs l
	JOIN insulin_plans p ON p.id = l.plan_id
	JOIN caregivers c ON c.id = l.caregiver_id`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanInsulinLog(s rowScanner) (InsulinLog, error) {
	var l InsulinLog
	var scaleUnits, sugar sql.NullFloat64
	err := s.Scan(&l.ID, &l.PlanID, &l.PlanNameAr, &l.PlanNameEn, &l.CaregiverID, &l.CaregiverName, &l.AvatarColor,
		&l.GivenAt, &l.Status, &l.Units, &scaleUnits, &sugar, &l.Reason)
	if err != nil {
		return l, err
	}
	if scaleUnits.Valid {
		v := scaleUnits.Float64
		l.ScaleUnits = &v
	}
	if sugar.Valid {
		v := sugar.Float64
		l.BloodSugar = &v
	}
	l.GivenAt = sqliteToRFC3339(l.GivenAt)
	return l, nil
}

type PlanStatus struct {
	Plan          InsulinPlan  `json:"plan"`
	LastGiven     *InsulinLog  `json:"last_given"`
	NextAllowedAt string       `json:"next_allowed_at,omitempty"` // set only while locked
	Recent        []InsulinLog `json:"recent"`
}

// GetStatus returns, per active plan, the last injection and whether a new one is locked.
func (h *InsulinHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	plans, err := h.loadPlans(true)
	if err != nil {
		internalError(w, "failed to fetch insulin plans", err)
		return
	}
	now := time.Now().UTC()
	out := make([]PlanStatus, 0, len(plans))
	for _, p := range plans {
		st := PlanStatus{Plan: p, Recent: []InsulinLog{}}
		rows, err := h.DB.Query(insulinLogSelect+` WHERE l.plan_id = ? ORDER BY l.given_at DESC LIMIT 6`, p.ID)
		if err != nil {
			internalError(w, "failed to fetch insulin logs", err)
			return
		}
		for rows.Next() {
			l, err := scanInsulinLog(rows)
			if err != nil {
				continue
			}
			st.Recent = append(st.Recent, l)
			if st.LastGiven == nil && l.Status == "given" {
				lc := l
				st.LastGiven = &lc
			}
		}
		rows.Close() // release the single pooled connection before the next query
		if st.LastGiven != nil {
			if t, err := time.Parse(time.RFC3339, st.LastGiven.GivenAt); err == nil {
				next := t.Add(time.Duration(p.MinHoursBetween * float64(time.Hour)))
				if next.After(now) {
					st.NextAllowedAt = next.Format(time.RFC3339)
				}
			}
		}
		out = append(out, st)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

type LogInsulinRequest struct {
	PlanID           string   `json:"plan_id"`
	Status           string   `json:"status"` // 'given' or 'held'
	Units            float64  `json:"units"`
	BloodSugar       *float64 `json:"blood_sugar"`
	Reason           string   `json:"reason"`
	OverrideInterval bool     `json:"override_interval"`
}

func (h *InsulinHandler) LogInsulin(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		writeJSONError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Select your profile and enter your PIN")
		return
	}
	if !auth.CanTakeActions(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers cannot record insulin")
		return
	}

	var req LogInsulinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if len(req.Reason) > maxInsulinReason {
		writeJSONError(w, http.StatusBadRequest, "REASON_TOO_LONG", "reason is too long")
		return
	}
	if req.Status != "given" && req.Status != "held" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_STATUS", "status must be 'given' or 'held'")
		return
	}
	if req.BloodSugar != nil && (*req.BloodSugar < minSugarReading || *req.BloodSugar > maxSugarReading) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_SUGAR", "blood sugar must be 1–45 mmol/L")
		return
	}

	plans, err := h.loadPlans(true)
	if err != nil {
		internalError(w, "failed to fetch insulin plan", err)
		return
	}
	var plan *InsulinPlan
	for i := range plans {
		if plans[i].ID == req.PlanID {
			plan = &plans[i]
		}
	}
	if plan == nil {
		writeJSONError(w, http.StatusNotFound, "PLAN_NOT_FOUND", "insulin plan not found or inactive")
		return
	}

	// What the doctor's plan says for this reading.
	var expected *float64
	switch plan.Kind {
	case "scale":
		if req.BloodSugar == nil {
			writeJSONError(w, http.StatusBadRequest, "SUGAR_REQUIRED", "measure blood sugar now; the scale needs the current reading")
			return
		}
		expected = LookupScale(plan.Scale, *req.BloodSugar)
	case "fixed":
		expected = plan.FixedUnits
	}

	if req.Status == "held" {
		req.Units = 0
	} else {
		if req.Units <= 0 || !validUnits(req.Units) {
			writeJSONError(w, http.StatusBadRequest, "INVALID_UNITS", "units must be 0.5–100 in whole or half units")
			return
		}
		if req.BloodSugar != nil && *req.BloodSugar < hypoThreshold {
			if plan.Kind == "scale" {
				writeJSONError(w, http.StatusUnprocessableEntity, "HYPO_NO_INSULIN",
					"Blood sugar is below 3.9: do not give correction insulin. Give fast sugar now and recheck in 15 minutes.")
				return
			}
			if req.Reason == "" {
				writeJSONError(w, http.StatusUnprocessableEntity, "HYPO_REASON_REQUIRED",
					"Blood sugar is below 3.9: only give this dose if the doctor said so, and write that as the reason.")
				return
			}
		}
		if (expected == nil || *expected != req.Units) && req.Reason == "" {
			writeJSONError(w, http.StatusUnprocessableEntity, "REASON_REQUIRED",
				"This dose is different from the doctor's plan: write the reason (e.g. the doctor's instruction).")
			return
		}
	}
	if req.OverrideInterval {
		if !auth.CanManageMedications(caregiver) {
			writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only an admin can override the insulin interval lock")
			return
		}
		if req.Reason == "" {
			writeJSONError(w, http.StatusUnprocessableEntity, "REASON_REQUIRED", "Overriding the interval lock needs a reason")
			return
		}
	}

	snapshot, _ := json.Marshal(plan)
	now := time.Now().UTC().Truncate(time.Second)
	logID := uuid.New().String()

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to record insulin", err)
		return
	}
	// The single pooled connection belongs to tx until Commit/Rollback: use tx only.
	if req.Status == "given" && !req.OverrideInterval {
		last, err := scanInsulinLog(tx.QueryRow(insulinLogSelect+
			` WHERE l.plan_id = ? AND l.status = 'given' ORDER BY l.given_at DESC LIMIT 1`, plan.ID))
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			tx.Rollback()
			internalError(w, "failed to check last insulin", err)
			return
		}
		if err == nil {
			if t, perr := time.Parse(time.RFC3339, last.GivenAt); perr == nil {
				next := t.Add(time.Duration(plan.MinHoursBetween * float64(time.Hour)))
				if next.After(now) {
					tx.Rollback()
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusConflict)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"error":           "INSULIN_TOO_SOON",
						"message":         "Insulin from this plan was already given recently",
						"last":            last,
						"next_allowed_at": next.Format(time.RFC3339),
					})
					return
				}
			}
		}
	}

	var vitalID *string
	if req.BloodSugar != nil {
		id := uuid.New().String()
		vitalID = &id
		if _, err := tx.Exec(`
			INSERT INTO vitals_logs (id, caregiver_id, recorded_at, blood_sugar, alertness_level, vital_type, notes)
			VALUES (?, ?, ?, ?, NULL, 'sugar', ?)
		`, id, caregiver.ID, now.Format(sqliteTimeLayout), *req.BloodSugar, "[Insulin check] "+plan.NameEn); err != nil {
			tx.Rollback()
			internalError(w, "failed to record blood sugar", err)
			return
		}
	}

	if _, err := tx.Exec(`
		INSERT INTO insulin_logs (id, plan_id, caregiver_id, given_at, status, units, scale_units, blood_sugar, vital_id, reason, scale_snapshot)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, logID, plan.ID, caregiver.ID, now.Format(sqliteTimeLayout), req.Status, req.Units, expected, req.BloodSugar, vitalID, req.Reason, string(snapshot)); err != nil {
		tx.Rollback()
		internalError(w, "failed to record insulin", err)
		return
	}

	details := fmt.Sprintf("%s %s: %.1f units (plan said %s), sugar %s", plan.NameEn, req.Status, req.Units, fmtUnits(expected), fmtSugar(req.BloodSugar))
	if req.OverrideInterval {
		details += " [INTERVAL OVERRIDE]"
	}
	if req.Reason != "" {
		details += ", reason: " + req.Reason
	}
	if err := audit.Record(tx, caregiver.ID, "INSULIN_LOGGED", "insulin_log", logID, details); err != nil {
		tx.Rollback()
		internalError(w, "failed to write audit trail", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit insulin log", err)
		return
	}

	payload := map[string]interface{}{
		"log_id":       logID,
		"plan_id":      plan.ID,
		"name_ar":      plan.NameAr,
		"name_en":      plan.NameEn,
		"status":       req.Status,
		"units":        req.Units,
		"scale_units":  expected,
		"blood_sugar":  req.BloodSugar,
		"sugar_status": CalculateSugarStatus(req.BloodSugar),
		"given_at":     now.Format(time.RFC3339),
		"reason":       req.Reason,
	}
	h.Broker.Broadcast("INSULIN_LOGGED", actorOf(caregiver), payload)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(payload)
}

func fmtUnits(u *float64) string {
	if u == nil {
		return "outside scale"
	}
	return fmt.Sprintf("%.1f", *u)
}

func fmtSugar(s *float64) string {
	if s == nil {
		return "not measured"
	}
	return fmt.Sprintf("%.1f mmol/L", *s)
}

// insulinLogsSince is used by the doctor export.
func insulinLogsSince(database *sql.DB, since string) ([]InsulinLog, error) {
	rows, err := database.Query(insulinLogSelect+` WHERE l.given_at >= ? ORDER BY l.given_at DESC LIMIT 200`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InsulinLog{}
	for rows.Next() {
		l, err := scanInsulinLog(rows)
		if err != nil {
			continue
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
