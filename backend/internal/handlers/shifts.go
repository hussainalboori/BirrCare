package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type ShiftsHandler struct {
	DB     *sql.DB
	Broker *sse.Broker
}

type CreateShiftRequest struct {
	NameAr        string  `json:"name_ar"`
	NameEn        string  `json:"name_en"`
	StartTime     string  `json:"start_time"` // "HH:MM"
	EndTime       string  `json:"end_time"`   // "HH:MM"
	DurationHours float64 `json:"duration_hours,omitempty"`
	Color         string  `json:"color,omitempty"`
	DisplayOrder  int     `json:"display_order,omitempty"`
}

type UpdateShiftRequest struct {
	NameAr        string  `json:"name_ar"`
	NameEn        string  `json:"name_en"`
	StartTime     string  `json:"start_time"`
	EndTime       string  `json:"end_time"`
	DurationHours float64 `json:"duration_hours,omitempty"`
	Color         string  `json:"color,omitempty"`
	DisplayOrder  int     `json:"display_order,omitempty"`
	IsActive      *bool   `json:"is_active,omitempty"`
}

type ApplyPresetRequest struct {
	Preset string `json:"preset"` // "24h", "4h", "8h", "12h"
}

type AssignShiftRequest struct {
	ShiftID     string `json:"shift_id"`
	CaregiverID string `json:"caregiver_id"`
	Date        string `json:"date"` // "YYYY-MM-DD"
	Notes       string `json:"notes,omitempty"`
}

var timeRegex = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
var dateRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func calculateDuration(start, end string) float64 {
	if start == end {
		return 24.0
	}
	t1, err1 := time.Parse("15:04", start)
	t2, err2 := time.Parse("15:04", end)
	if err1 != nil || err2 != nil {
		return 8.0
	}
	diff := t2.Sub(t1).Hours()
	if diff <= 0 {
		diff += 24.0
	}
	return diff
}

func (h *ShiftsHandler) ListShifts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
		SELECT id, name_ar, name_en, start_time, end_time, duration_hours, color, display_order, is_active, created_at
		FROM shifts
		WHERE is_active = 1
		ORDER BY display_order ASC, start_time ASC
	`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "DB_ERROR", "failed fetching shifts")
		return
	}
	defer rows.Close()

	var shifts []models.Shift
	for rows.Next() {
		var s models.Shift
		var createdAtStr string
		var isActInt int
		if err := rows.Scan(&s.ID, &s.NameAr, &s.NameEn, &s.StartTime, &s.EndTime, &s.DurationHours, &s.Color, &s.DisplayOrder, &isActInt, &createdAtStr); err == nil {
			s.IsActive = (isActInt == 1)
			s.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
			shifts = append(shifts, s)
		}
	}
	if shifts == nil {
		shifts = []models.Shift{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(shifts)
}

func (h *ShiftsHandler) CreateShift(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageShifts(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only Super Admin can configure work shifts")
		return
	}

	var req CreateShiftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid shift payload")
		return
	}

	req.NameAr = strings.TrimSpace(req.NameAr)
	req.NameEn = strings.TrimSpace(req.NameEn)
	req.StartTime = strings.TrimSpace(req.StartTime)
	req.EndTime = strings.TrimSpace(req.EndTime)

	if req.NameAr == "" || req.NameEn == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_NAME", "Arabic and English shift names are required")
		return
	}
	if !timeRegex.MatchString(req.StartTime) || !timeRegex.MatchString(req.EndTime) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_TIME", "start_time and end_time must be HH:MM in 24h format")
		return
	}

	if req.DurationHours <= 0 {
		req.DurationHours = calculateDuration(req.StartTime, req.EndTime)
	}
	if req.Color == "" {
		req.Color = "#2563eb"
	}

	shiftID := "shift_" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")
	_, err := h.DB.Exec(`
		INSERT INTO shifts (id, name_ar, name_en, start_time, end_time, duration_hours, color, display_order, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
	`, shiftID, req.NameAr, req.NameEn, req.StartTime, req.EndTime, req.DurationHours, req.Color, req.DisplayOrder)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "DB_ERROR", "failed creating shift: "+err.Error())
		return
	}

	shift := models.Shift{
		ID:            shiftID,
		NameAr:        req.NameAr,
		NameEn:        req.NameEn,
		StartTime:     req.StartTime,
		EndTime:       req.EndTime,
		DurationHours: req.DurationHours,
		Color:         req.Color,
		DisplayOrder:  req.DisplayOrder,
		IsActive:      true,
		CreatedAt:     time.Now().UTC(),
	}

	if h.Broker != nil {
		h.Broker.Broadcast("SHIFT_UPDATED", models.ActorInfo{ID: caregiver.ID, Name: caregiver.Name, AvatarColor: caregiver.AvatarColor}, shift)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(shift)
}

func (h *ShiftsHandler) UpdateShift(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageShifts(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only Super Admin can configure work shifts")
		return
	}

	shiftID := chi.URLParam(r, "id")
	if shiftID == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_ID", "shift id is required")
		return
	}

	var req UpdateShiftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid shift payload")
		return
	}

	req.NameAr = strings.TrimSpace(req.NameAr)
	req.NameEn = strings.TrimSpace(req.NameEn)
	req.StartTime = strings.TrimSpace(req.StartTime)
	req.EndTime = strings.TrimSpace(req.EndTime)

	if req.NameAr == "" || req.NameEn == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_NAME", "Arabic and English shift names are required")
		return
	}
	if !timeRegex.MatchString(req.StartTime) || !timeRegex.MatchString(req.EndTime) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_TIME", "start_time and end_time must be HH:MM")
		return
	}

	if req.DurationHours <= 0 {
		req.DurationHours = calculateDuration(req.StartTime, req.EndTime)
	}

	isActiveVal := 1
	if req.IsActive != nil && !*req.IsActive {
		isActiveVal = 0
	}

	_, err := h.DB.Exec(`
		UPDATE shifts
		SET name_ar = ?, name_en = ?, start_time = ?, end_time = ?, duration_hours = ?, color = ?, display_order = ?, is_active = ?
		WHERE id = ?
	`, req.NameAr, req.NameEn, req.StartTime, req.EndTime, req.DurationHours, req.Color, req.DisplayOrder, isActiveVal, shiftID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "DB_ERROR", "failed updating shift")
		return
	}

	if h.Broker != nil {
		h.Broker.Broadcast("SHIFT_UPDATED", models.ActorInfo{ID: caregiver.ID, Name: caregiver.Name, AvatarColor: caregiver.AvatarColor}, map[string]interface{}{"id": shiftID, "status": "updated"})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (h *ShiftsHandler) DeleteShift(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageShifts(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only Super Admin can delete work shifts")
		return
	}

	shiftID := chi.URLParam(r, "id")
	if shiftID == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_ID", "shift id is required")
		return
	}

	// Soft delete to preserve historical shift assignment records
	_, err := h.DB.Exec(`UPDATE shifts SET is_active = 0 WHERE id = ?`, shiftID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "DB_ERROR", "failed deactivating shift")
		return
	}

	if h.Broker != nil {
		h.Broker.Broadcast("SHIFT_UPDATED", models.ActorInfo{ID: caregiver.ID, Name: caregiver.Name, AvatarColor: caregiver.AvatarColor}, map[string]string{"id": shiftID, "status": "deactivated"})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func (h *ShiftsHandler) ApplyPreset(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageShifts(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only Super Admin can apply shift presets")
		return
	}

	var req ApplyPresetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid preset payload")
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "DB_ERROR", "failed initiating preset transaction")
		return
	}
	defer tx.Rollback()

	// Deactivate existing active shifts
	if _, err := tx.Exec("UPDATE shifts SET is_active = 0"); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "DB_ERROR", "failed clearing current shifts")
		return
	}

	type presetShift struct {
		id, nameAr, nameEn, start, end, color string
		duration                              float64
		order                                 int
	}

	var items []presetShift
	switch req.Preset {
	case "24h":
		items = []presetShift{
			{"shift_24h", "وردية 24 ساعة (يوم كامل)", "24-Hour Full Day Shift", "08:00", "08:00", "#059669", 24.0, 1},
		}
	case "4h":
		// User specifically requested support for 4-hour shifts:
		items = []presetShift{
			{"shift_4h_1", "الفترة الأولى (الصباح الباكر)", "Shift 1 (Early Morning)", "00:00", "04:00", "#6366f1", 4.0, 1},
			{"shift_4h_2", "الفترة الثانية (الصباح)", "Shift 2 (Morning)", "04:00", "08:00", "#0ea5e9", 4.0, 2},
			{"shift_4h_3", "الفترة الثالثة (الظهيرة)", "Shift 3 (Midday)", "08:00", "12:00", "#10b981", 4.0, 3},
			{"shift_4h_4", "الفترة الرابعة (العصر)", "Shift 4 (Afternoon)", "12:00", "16:00", "#f59e0b", 4.0, 4},
			{"shift_4h_5", "الفترة الخامسة (المساء)", "Shift 5 (Evening)", "16:00", "20:00", "#ec4899", 4.0, 5},
			{"shift_4h_6", "الفترة السادسة (الليل)", "Shift 6 (Night)", "20:00", "00:00", "#8b5cf6", 4.0, 6},
		}
	case "8h":
		items = []presetShift{
			{"shift_8h_1", "وردية الصباح", "Morning Shift", "08:00", "16:00", "#0ea5e9", 8.0, 1},
			{"shift_8h_2", "وردية المساء", "Evening Shift", "16:00", "00:00", "#f59e0b", 8.0, 2},
			{"shift_8h_3", "وردية الليل", "Night Shift", "00:00", "08:00", "#8b5cf6", 8.0, 3},
		}
	case "12h":
		items = []presetShift{
			{"shift_12h_day", "وردية النهار", "Day Shift", "08:00", "20:00", "#f59e0b", 12.0, 1},
			{"shift_12h_night", "وردية الليل", "Night Shift", "20:00", "08:00", "#6366f1", 12.0, 2},
		}
	default:
		writeJSONError(w, http.StatusBadRequest, "INVALID_PRESET", "preset must be '24h', '4h', '8h', or '12h'")
		return
	}

	for _, it := range items {
		_, err := tx.Exec(`
			INSERT INTO shifts (id, name_ar, name_en, start_time, end_time, duration_hours, color, display_order, is_active)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
			ON CONFLICT(id) DO UPDATE SET
				name_ar = excluded.name_ar,
				name_en = excluded.name_en,
				start_time = excluded.start_time,
				end_time = excluded.end_time,
				duration_hours = excluded.duration_hours,
				color = excluded.color,
				display_order = excluded.display_order,
				is_active = 1
		`, it.id, it.nameAr, it.nameEn, it.start, it.end, it.duration, it.color, it.order)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "DB_ERROR", "failed applying preset: "+err.Error())
			return
		}
	}

	if err := tx.Commit(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "DB_ERROR", "failed committing preset transaction")
		return
	}

	if h.Broker != nil {
		h.Broker.Broadcast("SHIFT_UPDATED", models.ActorInfo{ID: caregiver.ID, Name: caregiver.Name, AvatarColor: caregiver.AvatarColor}, map[string]string{"preset": req.Preset})
	}

	// Return the new active shifts
	h.ListShifts(w, r)
}

func (h *ShiftsHandler) ListAssignments(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().Format("2006-01-02")
	} else if !dateRegex.MatchString(date) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_DATE", "date must be YYYY-MM-DD")
		return
	}

	rows, err := h.DB.Query(`
		SELECT 
			a.id, a.shift_id, a.caregiver_id, a.date, COALESCE(a.notes, ''), a.created_at,
			c.name, c.avatar_color, c.role,
			s.name_ar, s.name_en, s.start_time, s.end_time, s.color
		FROM shift_assignments a
		JOIN caregivers c ON a.caregiver_id = c.id
		JOIN shifts s ON a.shift_id = s.id
		WHERE a.date = ? AND s.is_active = 1
		ORDER BY s.display_order ASC, s.start_time ASC
	`, date)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "DB_ERROR", "failed fetching shift assignments")
		return
	}
	defer rows.Close()

	var assignments []models.ShiftAssignment
	for rows.Next() {
		var a models.ShiftAssignment
		var createdAtStr string
		if err := rows.Scan(
			&a.ID, &a.ShiftID, &a.CaregiverID, &a.Date, &a.Notes, &createdAtStr,
			&a.CaregiverName, &a.AvatarColor, &a.CaregiverRole,
			&a.ShiftNameAr, &a.ShiftNameEn, &a.ShiftStartTime, &a.ShiftEndTime, &a.ShiftColor,
		); err == nil {
			a.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
			assignments = append(assignments, a)
		}
	}
	if assignments == nil {
		assignments = []models.ShiftAssignment{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(assignments)
}

func (h *ShiftsHandler) AssignShift(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageShifts(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only Super Admin can assign caregivers to shifts")
		return
	}

	var req AssignShiftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid assignment payload")
		return
	}

	if req.ShiftID == "" || req.CaregiverID == "" || req.Date == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_FIELDS", "shift_id, caregiver_id, and date are required")
		return
	}
	if !dateRegex.MatchString(req.Date) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_DATE", "date must be YYYY-MM-DD")
		return
	}

	// Verify shift and caregiver exist
	var shiftCount, cgCount int
	_ = h.DB.QueryRow("SELECT COUNT(*) FROM shifts WHERE id = ?", req.ShiftID).Scan(&shiftCount)
	_ = h.DB.QueryRow("SELECT COUNT(*) FROM caregivers WHERE id = ? AND is_active = 1", req.CaregiverID).Scan(&cgCount)
	if shiftCount == 0 || cgCount == 0 {
		writeJSONError(w, http.StatusBadRequest, "NOT_FOUND", "active shift or caregiver not found")
		return
	}

	assignID := "assign_" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")
	_, err := h.DB.Exec(`
		INSERT INTO shift_assignments (id, shift_id, caregiver_id, date, notes)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(shift_id, date) DO UPDATE SET
			caregiver_id = excluded.caregiver_id,
			notes = excluded.notes,
			created_at = datetime('now')
	`, assignID, req.ShiftID, req.CaregiverID, req.Date, req.Notes)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "DB_ERROR", "failed assigning shift: "+err.Error())
		return
	}

	if h.Broker != nil {
		h.Broker.Broadcast("DUTY_UPDATED", models.ActorInfo{ID: caregiver.ID, Name: caregiver.Name, AvatarColor: caregiver.AvatarColor}, map[string]string{"date": req.Date, "shift_id": req.ShiftID, "caregiver_id": req.CaregiverID})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "assigned", "date": req.Date})
}

func (h *ShiftsHandler) DeleteAssignment(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageShifts(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only Super Admin can remove shift assignments")
		return
	}

	assignID := chi.URLParam(r, "id")
	if assignID == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_ID", "assignment id is required")
		return
	}

	_, err := h.DB.Exec("DELETE FROM shift_assignments WHERE id = ?", assignID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "DB_ERROR", "failed removing assignment")
		return
	}

	if h.Broker != nil {
		h.Broker.Broadcast("DUTY_UPDATED", models.ActorInfo{ID: caregiver.ID, Name: caregiver.Name, AvatarColor: caregiver.AvatarColor}, map[string]string{"id": assignID, "status": "deleted"})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func isTimeInShift(currentTime, start, end string) bool {
	if start == end {
		return true // 24-hour shift covers all hours
	}
	if start < end {
		return currentTime >= start && currentTime < end
	}
	// Overnight shift (e.g. 20:00 to 04:00 or 20:00 to 08:00)
	return currentTime >= start || currentTime < end
}

func (h *ShiftsHandler) GetCurrentDuty(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	currentTime := now.Format("15:04")
	todayDate := now.Format("2006-01-02")
	yesterdayDate := now.AddDate(0, 0, -1).Format("2006-01-02")

	// 1. Fetch active shifts
	rows, err := h.DB.Query(`
		SELECT id, name_ar, name_en, start_time, end_time, duration_hours, color, display_order, is_active
		FROM shifts
		WHERE is_active = 1
		ORDER BY display_order ASC, start_time ASC
	`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "DB_ERROR", "failed fetching shifts")
		return
	}
	defer rows.Close()

	var activeShifts []models.Shift
	for rows.Next() {
		var s models.Shift
		var isAct int
		if err := rows.Scan(&s.ID, &s.NameAr, &s.NameEn, &s.StartTime, &s.EndTime, &s.DurationHours, &s.Color, &s.DisplayOrder, &isAct); err == nil {
			s.IsActive = (isAct == 1)
			activeShifts = append(activeShifts, s)
		}
	}

	var currentShift *models.Shift
	targetDate := todayDate

	for _, s := range activeShifts {
		if isTimeInShift(currentTime, s.StartTime, s.EndTime) {
			currentShift = &s
			// If overnight shift and currentTime < end_time, it started yesterday
			if s.StartTime > s.EndTime && currentTime < s.EndTime {
				targetDate = yesterdayDate
			}
			break
		}
	}

	// If no match found (e.g. edge case or single shift), fallback to first shift
	if currentShift == nil && len(activeShifts) > 0 {
		currentShift = &activeShifts[0]
	}

	status := models.CurrentDutyStatus{
		ActiveShift: currentShift,
		CurrentTime: currentTime,
		Date:        targetDate,
		IsOnDuty:    false,
	}

	if currentShift != nil {
		// Find assignment for this shift on targetDate
		var a models.ShiftAssignment
		var c models.Caregiver
		var createdAtStr, emailStr sql.NullString
		err := h.DB.QueryRow(`
			SELECT 
				a.id, a.shift_id, a.caregiver_id, a.date, COALESCE(a.notes, ''),
				c.id, c.name, c.email, c.role, c.avatar_color, c.preferred_lang, c.is_active, c.created_at
			FROM shift_assignments a
			JOIN caregivers c ON a.caregiver_id = c.id
			WHERE a.shift_id = ? AND a.date = ?
		`, currentShift.ID, targetDate).Scan(
			&a.ID, &a.ShiftID, &a.CaregiverID, &a.Date, &a.Notes,
			&c.ID, &c.Name, &emailStr, &c.Role, &c.AvatarColor, &c.PreferredLang, &c.IsActive, &createdAtStr,
		)
		if err == nil {
			if emailStr.Valid {
				c.Email = emailStr.String
			}
			a.CaregiverName = c.Name
			a.AvatarColor = c.AvatarColor
			a.CaregiverRole = c.Role
			a.ShiftNameAr = currentShift.NameAr
			a.ShiftNameEn = currentShift.NameEn
			a.ShiftStartTime = currentShift.StartTime
			a.ShiftEndTime = currentShift.EndTime
			a.ShiftColor = currentShift.Color

			status.ActiveAssignment = &a
			status.ActiveCaregiver = &c
			status.IsOnDuty = true
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
