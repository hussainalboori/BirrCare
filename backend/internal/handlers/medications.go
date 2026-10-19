package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type MedicationHandler struct {
	DB     *sql.DB
	Broker *sse.Broker
}

var allowedSlots = map[string]bool{"morning": true, "afternoon": true, "evening": true, "bedtime": true, "night": true}
var allowedDays = map[string]bool{"sun": true, "mon": true, "tue": true, "wed": true, "thu": true, "fri": true, "sat": true}

var defaultSlotTimes = map[string]string{
	"morning":   "08:00",
	"afternoon": "14:00",
	"evening":   "20:00",
	"bedtime":   "22:30",
	"night":     "02:00",
}

var allDaysOfWeek = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func validSlots(slots []string) bool {
	if len(slots) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, s := range slots {
		if !allowedSlots[s] || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}

func validDays(days []string) bool {
	if len(days) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, d := range days {
		dl := strings.ToLower(strings.TrimSpace(d))
		if !allowedDays[dl] || seen[dl] {
			return false
		}
		seen[dl] = true
	}
	return true
}

func isValidTimeHHMM(t string) bool {
	t = strings.TrimSpace(t)
	if len(t) != 5 || t[2] != ':' {
		return false
	}
	var h, m int
	if n, _ := fmt.Sscanf(t, "%02d:%02d", &h, &m); n != 2 {
		return false
	}
	return h >= 0 && h <= 23 && m >= 0 && m <= 59
}

func normalizeSlotTimes(times map[string]string, slots []string) map[string]string {
	res := make(map[string]string)
	for _, s := range slots {
		if times != nil {
			if t, ok := times[s]; ok && isValidTimeHHMM(t) {
				res[s] = strings.TrimSpace(t)
				continue
			}
		}
		if def, exists := defaultSlotTimes[s]; exists {
			res[s] = def
		} else {
			res[s] = "08:00"
		}
	}
	return res
}

func (h *MedicationHandler) ListMedications(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
		SELECT id, name_ar, name_en, dosage, instructions_ar, instructions_en, frequency_slots, image_url, is_active, created_by, created_at, days_of_week, slot_times
		FROM medications WHERE is_active = 1 ORDER BY name_ar ASC
	`)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch medications"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var list []models.Medication
	for rows.Next() {
		var m models.Medication
		var instAr, instEn, createdBy, imgURL, daysJSON, timesJSON sql.NullString
		var slotsJSON string
		var createdAtStr string

		if err := rows.Scan(&m.ID, &m.NameAr, &m.NameEn, &m.Dosage, &instAr, &instEn, &slotsJSON, &imgURL, &m.IsActive, &createdBy, &createdAtStr, &daysJSON, &timesJSON); err != nil {
			continue
		}
		if instAr.Valid {
			m.InstructionsAr = instAr.String
		}
		if instEn.Valid {
			m.InstructionsEn = instEn.String
		}
		if createdBy.Valid {
			m.CreatedBy = createdBy.String
		}
		if imgURL.Valid {
			m.ImageURL = imgURL.String
		}
		json.Unmarshal([]byte(slotsJSON), &m.FrequencySlots)

		if daysJSON.Valid && daysJSON.String != "" {
			json.Unmarshal([]byte(daysJSON.String), &m.DaysOfWeek)
		}
		if len(m.DaysOfWeek) == 0 {
			m.DaysOfWeek = allDaysOfWeek
		}

		if timesJSON.Valid && timesJSON.String != "" {
			json.Unmarshal([]byte(timesJSON.String), &m.SlotTimes)
		}
		m.SlotTimes = normalizeSlotTimes(m.SlotTimes, m.FrequencySlots)

		m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
		list = append(list, m)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

type CreateMedicationRequest struct {
	NameAr         string            `json:"name_ar"`
	NameEn         string            `json:"name_en"`
	Dosage         string            `json:"dosage"`
	InstructionsAr string            `json:"instructions_ar"`
	InstructionsEn string            `json:"instructions_en"`
	FrequencySlots []string          `json:"frequency_slots"`
	DaysOfWeek     []string          `json:"days_of_week,omitempty"`
	SlotTimes      map[string]string `json:"slot_times,omitempty"`
	ImageURL       string            `json:"image_url,omitempty"`
}

func (h *MedicationHandler) CreateMedication(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	if !auth.CanManageMedications(caregiver) {
		http.Error(w, `{"error":"FORBIDDEN","message":"Only Admins and Super Admins can add medications"}`, http.StatusForbidden)
		return
	}

	var req CreateMedicationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NameAr == "" || req.Dosage == "" {
		http.Error(w, `{"error":"invalid medication payload"}`, http.StatusBadRequest)
		return
	}

	if !validSlots(req.FrequencySlots) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_SLOTS", "frequency_slots must be a non-empty list of morning, afternoon, evening, bedtime, night")
		return
	}

	days := req.DaysOfWeek
	if len(days) == 0 {
		days = allDaysOfWeek
	} else if !validDays(days) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_DAYS", "days_of_week must contain valid days (sun, mon, tue, wed, thu, fri, sat)")
		return
	}

	slotTimes := normalizeSlotTimes(req.SlotTimes, req.FrequencySlots)

	id := "med_" + uuid.New().String()[:8]
	slotsJSON, _ := json.Marshal(req.FrequencySlots)
	daysJSON, _ := json.Marshal(days)
	timesJSON, _ := json.Marshal(slotTimes)

	_, err := h.DB.Exec(`
		INSERT INTO medications (id, name_ar, name_en, dosage, instructions_ar, instructions_en, frequency_slots, days_of_week, slot_times, image_url, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
	`, id, req.NameAr, req.NameEn, req.Dosage, req.InstructionsAr, req.InstructionsEn, string(slotsJSON), string(daysJSON), string(timesJSON), req.ImageURL, caregiver.ID)

	if err != nil {
		http.Error(w, `{"error":"failed to create medication"}`, http.StatusInternalServerError)
		return
	}

	audit.Log(h.DB, caregiver.ID, "CREATE_MEDICATION", "medication", id, fmt.Sprintf("Added prescription: %s (%s)", req.NameAr, req.Dosage))

	h.Broker.Broadcast("MEDICATIONS_UPDATED", models.ActorInfo{
		ID:          caregiver.ID,
		Name:        caregiver.Name,
		AvatarColor: caregiver.AvatarColor,
	}, map[string]interface{}{
		"action": "created",
		"id":     id,
		"name":   req.NameAr,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      id,
		"message": "Prescription created",
	})
}

type UpdateMedicationRequest struct {
	NameAr         string            `json:"name_ar"`
	NameEn         string            `json:"name_en"`
	Dosage         string            `json:"dosage"`
	InstructionsAr string            `json:"instructions_ar"`
	InstructionsEn string            `json:"instructions_en"`
	FrequencySlots []string          `json:"frequency_slots"`
	DaysOfWeek     []string          `json:"days_of_week,omitempty"`
	SlotTimes      map[string]string `json:"slot_times,omitempty"`
	ImageURL       string            `json:"image_url,omitempty"`
	IsActive       *bool             `json:"is_active,omitempty"`
}

func (h *MedicationHandler) UpdateMedication(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	if !auth.CanManageMedications(caregiver) {
		http.Error(w, `{"error":"FORBIDDEN","message":"Only Admins and Super Admins can edit medications"}`, http.StatusForbidden)
		return
	}

	medID := chi.URLParam(r, "id")
	if medID == "" {
		http.Error(w, `{"error":"missing medication id"}`, http.StatusBadRequest)
		return
	}

	var req UpdateMedicationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	if len(req.FrequencySlots) > 0 && !validSlots(req.FrequencySlots) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_SLOTS", "frequency_slots must be a list of morning, afternoon, evening, bedtime, night")
		return
	}

	if req.DaysOfWeek != nil {
		if len(req.DaysOfWeek) > 0 && !validDays(req.DaysOfWeek) {
			writeJSONError(w, http.StatusBadRequest, "INVALID_DAYS", "days_of_week must contain valid days (sun, mon, tue, wed, thu, fri, sat)")
			return
		}
	}

	slotsJSON, _ := json.Marshal(req.FrequencySlots)
	_, err := h.DB.Exec(`
		UPDATE medications
		SET name_ar = COALESCE(NULLIF(?, ''), name_ar),
		    name_en = COALESCE(NULLIF(?, ''), name_en),
		    dosage = COALESCE(NULLIF(?, ''), dosage),
		    instructions_ar = ?,
		    instructions_en = ?,
		    frequency_slots = CASE WHEN ? = '[]' OR ? = 'null' THEN frequency_slots ELSE ? END,
		    image_url = CASE WHEN ? != '' THEN ? ELSE image_url END
		WHERE id = ?
	`, req.NameAr, req.NameEn, req.Dosage, req.InstructionsAr, req.InstructionsEn, string(slotsJSON), string(slotsJSON), string(slotsJSON), req.ImageURL, req.ImageURL, medID)

	if err != nil {
		internalError(w, "failed to update medication", err)
		return
	}

	if req.DaysOfWeek != nil && len(req.DaysOfWeek) > 0 {
		daysJSON, _ := json.Marshal(req.DaysOfWeek)
		_, _ = h.DB.Exec("UPDATE medications SET days_of_week = ? WHERE id = ?", string(daysJSON), medID)
	}

	if req.SlotTimes != nil {
		var currentSlotsJSON string
		_ = h.DB.QueryRow("SELECT frequency_slots FROM medications WHERE id = ?", medID).Scan(&currentSlotsJSON)
		var currentSlots []string
		_ = json.Unmarshal([]byte(currentSlotsJSON), &currentSlots)
		normalizedTimes := normalizeSlotTimes(req.SlotTimes, currentSlots)
		timesJSON, _ := json.Marshal(normalizedTimes)
		_, _ = h.DB.Exec("UPDATE medications SET slot_times = ? WHERE id = ?", string(timesJSON), medID)
	}

	if req.IsActive != nil {
		activeInt := 0
		if *req.IsActive {
			activeInt = 1
		}
		_, _ = h.DB.Exec("UPDATE medications SET is_active = ? WHERE id = ?", activeInt, medID)
	}

	audit.Log(h.DB, caregiver.ID, "UPDATE_MEDICATION", "medication", medID, fmt.Sprintf("Updated prescription %s", medID))

	h.Broker.Broadcast("MEDICATIONS_UPDATED", models.ActorInfo{
		ID:          caregiver.ID,
		Name:        caregiver.Name,
		AvatarColor: caregiver.AvatarColor,
	}, map[string]interface{}{
		"action": "updated",
		"id":     medID,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "Medication updated successfully",
	})
}

type UpdateScheduleRequest struct {
	DaysOfWeek []string          `json:"days_of_week"`
	SlotTimes  map[string]string `json:"slot_times"`
}

// UpdateSchedule allows Admins and Super Admins to configure the times and days of a medication
func (h *MedicationHandler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	if !auth.CanManageMedications(caregiver) {
		http.Error(w, `{"error":"FORBIDDEN","message":"Only Admins and Super Admins can manage medication schedules"}`, http.StatusForbidden)
		return
	}

	medID := chi.URLParam(r, "id")
	if medID == "" {
		http.Error(w, `{"error":"missing medication id"}`, http.StatusBadRequest)
		return
	}

	var req UpdateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	if len(req.DaysOfWeek) > 0 && !validDays(req.DaysOfWeek) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_DAYS", "days_of_week must be a subset of sun, mon, tue, wed, thu, fri, sat")
		return
	}

	// Fetch existing slots
	var slotsJSON string
	err := h.DB.QueryRow("SELECT frequency_slots FROM medications WHERE id = ?", medID).Scan(&slotsJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, `{"error":"medication not found"}`, http.StatusNotFound)
			return
		}
		internalError(w, "database query error", err)
		return
	}

	var slots []string
	json.Unmarshal([]byte(slotsJSON), &slots)
	normalizedTimes := normalizeSlotTimes(req.SlotTimes, slots)

	daysToSave := req.DaysOfWeek
	if len(daysToSave) == 0 {
		daysToSave = allDaysOfWeek
	}

	daysJSON, _ := json.Marshal(daysToSave)
	timesJSON, _ := json.Marshal(normalizedTimes)

	_, err = h.DB.Exec(`
		UPDATE medications
		SET days_of_week = ?,
		    slot_times = ?
		WHERE id = ?
	`, string(daysJSON), string(timesJSON), medID)
	if err != nil {
		internalError(w, "failed to update schedule", err)
		return
	}

	audit.Log(h.DB, caregiver.ID, "UPDATE_SCHEDULE", "medication", medID, fmt.Sprintf("Updated schedule for %s", medID))

	h.Broker.Broadcast("MEDICATIONS_UPDATED", models.ActorInfo{
		ID:          caregiver.ID,
		Name:        caregiver.Name,
		AvatarColor: caregiver.AvatarColor,
	}, map[string]interface{}{
		"action": "schedule_updated",
		"id":     medID,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "Medication schedule updated successfully",
	})
}

func (h *MedicationHandler) DeleteMedication(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	if !auth.CanManageMedications(caregiver) {
		http.Error(w, `{"error":"FORBIDDEN","message":"Only Admins and Super Admins can deactivate medications"}`, http.StatusForbidden)
		return
	}

	medID := chi.URLParam(r, "id")
	if medID == "" {
		http.Error(w, `{"error":"missing medication id"}`, http.StatusBadRequest)
		return
	}

	_, err := h.DB.Exec("UPDATE medications SET is_active = 0 WHERE id = ?", medID)
	if err != nil {
		internalError(w, "failed to deactivate medication", err)
		return
	}

	audit.Log(h.DB, caregiver.ID, "DEACTIVATE_MEDICATION", "medication", medID, "Deactivated prescription")

	h.Broker.Broadcast("MEDICATIONS_UPDATED", models.ActorInfo{
		ID:          caregiver.ID,
		Name:        caregiver.Name,
		AvatarColor: caregiver.AvatarColor,
	}, map[string]interface{}{
		"action": "deactivated",
		"id":     medID,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "Medication deactivated successfully",
	})
}

// GetSchedule returns all doses scheduled for a specific date with their logged status
func (h *MedicationHandler) GetSchedule(w http.ResponseWriter, r *http.Request) {
	dateStr := r.URL.Query().Get("date")
	if dateStr == "" {
		// Household-local day, not UTC: otherwise late-evening doses land on the wrong date.
		dateStr = clinicalToday()
	} else if canon, _, ok := parseClinicalDate(dateStr); ok {
		dateStr = canon
	} else {
		writeJSONError(w, http.StatusBadRequest, "INVALID_DATE", "date must be YYYY-MM-DD")
		return
	}

	// Determine day of the week for target date (e.g., "sun", "mon", "tue", "wed", "thu", "fri", "sat")
	var targetDayKey string
	if targetDate, err := time.Parse("2006-01-02", dateStr); err == nil {
		switch targetDate.Weekday() {
		case time.Sunday:
			targetDayKey = "sun"
		case time.Monday:
			targetDayKey = "mon"
		case time.Tuesday:
			targetDayKey = "tue"
		case time.Wednesday:
			targetDayKey = "wed"
		case time.Thursday:
			targetDayKey = "thu"
		case time.Friday:
			targetDayKey = "fri"
		case time.Saturday:
			targetDayKey = "sat"
		}
	}

	// 1. Fetch all active medications including days_of_week and slot_times
	medRows, err := h.DB.Query(`
		SELECT id, name_ar, name_en, dosage, instructions_ar, instructions_en, frequency_slots, image_url, is_active, created_at, days_of_week, slot_times
		FROM medications WHERE is_active = 1
	`)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch medications"}`, http.StatusInternalServerError)
		return
	}
	defer medRows.Close()

	var meds []models.Medication
	for medRows.Next() {
		var m models.Medication
		var instAr, instEn, imgURL, daysJSON, timesJSON sql.NullString
		var slotsJSON, createdAtStr string
		if err := medRows.Scan(&m.ID, &m.NameAr, &m.NameEn, &m.Dosage, &instAr, &instEn, &slotsJSON, &imgURL, &m.IsActive, &createdAtStr, &daysJSON, &timesJSON); err != nil {
			continue
		}
		if instAr.Valid {
			m.InstructionsAr = instAr.String
		}
		if instEn.Valid {
			m.InstructionsEn = instEn.String
		}
		if imgURL.Valid {
			m.ImageURL = imgURL.String
		}
		json.Unmarshal([]byte(slotsJSON), &m.FrequencySlots)

		if daysJSON.Valid && daysJSON.String != "" {
			json.Unmarshal([]byte(daysJSON.String), &m.DaysOfWeek)
		}
		if len(m.DaysOfWeek) == 0 {
			m.DaysOfWeek = allDaysOfWeek
		}

		if timesJSON.Valid && timesJSON.String != "" {
			json.Unmarshal([]byte(timesJSON.String), &m.SlotTimes)
		}
		m.SlotTimes = normalizeSlotTimes(m.SlotTimes, m.FrequencySlots)

		m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
		meds = append(meds, m)
	}
	medRows.Close() // release the single pooled connection before the next query

	// 2. Fetch all logs for this date
	logRows, err := h.DB.Query(`
		SELECT l.id, l.medication_id, l.caregiver_id, l.slot, l.scheduled_date, l.taken_at, l.status, l.notes,
		       c.name, c.avatar_color
		FROM medication_logs l
		JOIN caregivers c ON l.caregiver_id = c.id
		WHERE l.scheduled_date = ?
	`, dateStr)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch medication logs"}`, http.StatusInternalServerError)
		return
	}
	defer logRows.Close()

	// Map key: "medication_id:slot" -> MedicationLog
	logsMap := make(map[string]models.MedicationLog)
	for logRows.Next() {
		var l models.MedicationLog
		var notes sql.NullString
		var takenAtStr sql.NullString
		if err := logRows.Scan(&l.ID, &l.MedicationID, &l.CaregiverID, &l.Slot, &l.ScheduledDate, &takenAtStr, &l.Status, &notes, &l.CaregiverName, &l.AvatarColor); err != nil {
			continue
		}
		if notes.Valid {
			l.Notes = notes.String
		}
		if takenAtStr.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", takenAtStr.String)
			l.TakenAt = &t
		}
		logsMap[fmt.Sprintf("%s:%s", l.MedicationID, l.Slot)] = l
	}

	// 3. Assemble scheduled dose cards
	var schedule []models.ScheduledDose
	for _, m := range meds {
		// Filter by day of week
		isScheduledToday := true
		if targetDayKey != "" && len(m.DaysOfWeek) > 0 {
			isScheduledToday = slices.Contains(m.DaysOfWeek, targetDayKey)
		}

		for _, slot := range m.FrequencySlots {
			key := fmt.Sprintf("%s:%s", m.ID, slot)
			log, hasLog := logsMap[key]

			// Only show if scheduled for today OR if there's already a logged dose on this day
			if !isScheduledToday && !hasLog {
				continue
			}

			scheduledTime := m.SlotTimes[slot]
			if scheduledTime == "" {
				scheduledTime = defaultSlotTimes[slot]
			}

			item := models.ScheduledDose{
				Medication: m,
				Slot:       slot,
				Time:       scheduledTime,
			}
			if hasLog {
				logCopy := log
				item.Log = &logCopy
			}
			schedule = append(schedule, item)
		}
	}

	// Sort chronologically by dose time
	slices.SortStableFunc(schedule, func(a, b models.ScheduledDose) int {
		if a.Time != b.Time {
			return strings.Compare(a.Time, b.Time)
		}
		return strings.Compare(a.Slot, b.Slot)
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"date":     dateStr,
		"schedule": schedule,
	})
}

type LogDoseRequest struct {
	MedicationID  string `json:"medication_id"`
	Slot          string `json:"slot"`
	ScheduledDate string `json:"scheduled_date"` // 'YYYY-MM-DD'
	Status        string `json:"status"`         // 'taken' or 'skipped'
	Notes         string `json:"notes,omitempty"`
}

const maxNotesLen = 2000

// LogDose records medication intake with double-dosing locking.
//
// Safety model: there is NO check-then-insert. The INSERT itself is the lock -
// SQLite's UNIQUE(medication_id, scheduled_date, slot) guarantees at most one row
// per dose no matter how many siblings tap at once. The dose row and its audit row
// are committed in one transaction. A losing writer gets 409 with who/when.
func (h *MedicationHandler) LogDose(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		writeJSONError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Select your profile and enter your PIN")
		return
	}
	if !auth.CanTakeActions(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers are not permitted to log medication doses")
		return
	}

	var req LogDoseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}

	req.MedicationID = strings.TrimSpace(req.MedicationID)
	req.Slot = strings.TrimSpace(req.Slot)
	if req.MedicationID == "" || req.Slot == "" || req.ScheduledDate == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_FIELDS", "medication_id, slot and scheduled_date are required")
		return
	}

	// Canonicalise the date: a non-canonical spelling ("2026-10-19T00:00", "2026-10-19 ")
	// would otherwise produce a distinct key and bypass the UNIQUE double-dose lock.
	date, day, ok := parseClinicalDate(strings.TrimSpace(req.ScheduledDate))
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "INVALID_DATE", "scheduled_date must be YYYY-MM-DD")
		return
	}
	if day.After(time.Now().In(ClinicalLocation()).AddDate(0, 0, 1)) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_DATE", "cannot log a dose for a future date")
		return
	}
	req.ScheduledDate = date

	switch req.Status {
	case "":
		req.Status = "taken"
	case "taken", "skipped":
	default:
		// Never silently coerce an unknown status into "taken".
		writeJSONError(w, http.StatusBadRequest, "INVALID_STATUS", "status must be 'taken' or 'skipped'")
		return
	}
	if len(req.Notes) > maxNotesLen {
		writeJSONError(w, http.StatusBadRequest, "NOTES_TOO_LONG", "notes are too long")
		return
	}

	// Medication must exist, be active, and actually be scheduled in this slot.
	var nameAr, nameEn, slotsJSON string
	err := h.DB.QueryRowContext(r.Context(),
		"SELECT name_ar, name_en, frequency_slots FROM medications WHERE id = ? AND is_active = 1",
		req.MedicationID).Scan(&nameAr, &nameEn, &slotsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "MEDICATION_NOT_FOUND", "medication not found or inactive")
		return
	} else if err != nil {
		internalError(w, "failed to look up medication", err)
		return
	}
	var slots []string
	_ = json.Unmarshal([]byte(slotsJSON), &slots)
	if !slices.Contains(slots, req.Slot) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_SLOT", "medication is not scheduled for this slot")
		return
	}

	logID := uuid.New().String()
	now := time.Now().UTC().Truncate(time.Second)

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to log medication", err)
		return
	}
	// From here until Commit/Rollback the single pooled connection belongs to tx:
	// every statement MUST go through tx, never h.DB, or it deadlocks.
	_, err = tx.Exec(`
		INSERT INTO medication_logs (id, medication_id, caregiver_id, slot, scheduled_date, taken_at, status, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, logID, req.MedicationID, caregiver.ID, req.Slot, req.ScheduledDate, now.Format(sqliteTimeLayout), req.Status, req.Notes)
	if err != nil {
		tx.Rollback() // release the connection BEFORE querying the existing dose
		if isUniqueViolation(err) {
			h.writeDoseConflict(w, r, req)
			return
		}
		internalError(w, "failed to log medication", err)
		return
	}

	if err := audit.Record(tx, caregiver.ID, "MED_LOGGED", "medication_log", logID,
		fmt.Sprintf("Dose marked as %s for med %s (slot %s, date %s)", req.Status, req.MedicationID, req.Slot, req.ScheduledDate)); err != nil {
		tx.Rollback()
		internalError(w, "failed to write audit trail", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit medication log", err)
		return
	}

	// Broadcast SSE event to all connected sibling devices
	h.Broker.Broadcast("MED_LOGGED", models.ActorInfo{
		ID:          caregiver.ID,
		Name:        caregiver.Name,
		AvatarColor: caregiver.AvatarColor,
	}, map[string]interface{}{
		"log_id":         logID,
		"medication_id":  req.MedicationID,
		"name_ar":        nameAr,
		"name_en":        nameEn,
		"slot":           req.Slot,
		"scheduled_date": req.ScheduledDate,
		"status":         req.Status,
		"taken_at":       now.Format(time.RFC3339),
		"notes":          req.Notes,
		"caregiver_id":   caregiver.ID,
		"caregiver_name": caregiver.Name,
		"avatar_color":   caregiver.AvatarColor,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "success",
		"log_id":         logID,
		"taken_at":       now.Format(time.RFC3339),
		"caregiver_name": caregiver.Name,
		"avatar_color":   caregiver.AvatarColor,
	})
}

// writeDoseConflict responds 409 with the identity of whoever already recorded the dose.
func (h *MedicationHandler) writeDoseConflict(w http.ResponseWriter, r *http.Request, req LogDoseRequest) {
	var logID, caregiverID, name, avatar, takenAt, status string
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT l.id, l.caregiver_id, c.name, COALESCE(c.avatar_color, ''), COALESCE(l.taken_at, ''), l.status
		FROM medication_logs l
		JOIN caregivers c ON l.caregiver_id = c.id
		WHERE l.medication_id = ? AND l.scheduled_date = ? AND l.slot = ?
	`, req.MedicationID, req.ScheduledDate, req.Slot).Scan(&logID, &caregiverID, &name, &avatar, &takenAt, &status)
	if err != nil {
		log.Printf("[ERROR] dose conflict lookup failed: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict) // HTTP 409 Conflict
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":          "DOSE_ALREADY_LOGGED",
		"message":        "This dose has already been recorded",
		"log_id":         logID,
		"medication_id":  req.MedicationID,
		"slot":           req.Slot,
		"scheduled_date": req.ScheduledDate,
		"status":         status,
		"caregiver_id":   caregiverID,
		"caregiver_name": name,
		"avatar_color":   avatar,
		"taken_at":       sqliteToRFC3339(takenAt),
	})
}
