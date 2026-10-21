package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/push"
	"birrcare/backend/internal/sse"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type MedicationHandler struct {
	DB     *sql.DB
	Broker *sse.Broker
	Push   *push.Notifier // nil = no Web Push
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
		SELECT id, name_ar, name_en, dosage, instructions_ar, instructions_en, frequency_slots, image_url, is_active, created_by, created_at, days_of_week, slot_times, min_hours_between, COALESCE(is_critical, 0),
		       stock_units, units_per_dose, refill_threshold_days
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
		var minHours, stock sql.NullFloat64
		var slotsJSON string
		var createdAtStr string

		if err := rows.Scan(&m.ID, &m.NameAr, &m.NameEn, &m.Dosage, &instAr, &instEn, &slotsJSON, &imgURL, &m.IsActive, &createdBy, &createdAtStr, &daysJSON, &timesJSON, &minHours, &m.IsCritical,
			&stock, &m.UnitsPerDose, &m.RefillThresholdDays); err != nil {
			continue
		}
		m.StockUnits = nullableFloat(stock)
		m.MinHoursBetween = nullableFloat(minHours)
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
	// Doctor's minimum hours between doses; omitted/null = automatic.
	MinHoursBetween *float64 `json:"min_hours_between,omitempty"`
	// Anticoagulant / antiplatelet: listed first in the doctor report.
	IsCritical bool `json:"is_critical,omitempty"`
	// Optional stock tracking (issue #20).
	StockSettings
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

	if !validIntervalHours(req.MinHoursBetween) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_INTERVAL", "min_hours_between must be between 0 and 72 hours")
		return
	}

	slotTimes := normalizeSlotTimes(req.SlotTimes, req.FrequencySlots)

	id := "med_" + uuid.New().String()[:8]
	if msg := req.StockSettings.validate(); msg != "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_STOCK", msg)
		return
	}
	slotsJSON, _ := json.Marshal(req.FrequencySlots)
	daysJSON, _ := json.Marshal(days)
	timesJSON, _ := json.Marshal(slotTimes)

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to create medication", err)
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO medications (id, name_ar, name_en, dosage, instructions_ar, instructions_en, frequency_slots, days_of_week, slot_times, image_url, min_hours_between, is_critical, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
	`, id, req.NameAr, req.NameEn, req.Dosage, req.InstructionsAr, req.InstructionsEn, string(slotsJSON), string(daysJSON), string(timesJSON), req.ImageURL, req.MinHoursBetween, req.IsCritical, caregiver.ID)

	if err != nil {
		http.Error(w, `{"error":"failed to create medication"}`, http.StatusInternalServerError)
		return
	}

	auditDetail := fmt.Sprintf("Added prescription: %s (%s)", req.NameAr, req.Dosage)
	if err := audit.Record(tx, caregiver.ID, "CREATE_MEDICATION", "medication", id, auditDetail); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}

	var stockCh *stockChange
	if req.StockSettings.StockUnits != nil || req.StockSettings.TrackStock != nil || req.StockSettings.UnitsPerDose != nil || req.StockSettings.RefillThresholdDays != nil {
		stockCh, err = applyStockSettings(tx, id, caregiver.ID, req.StockSettings, time.Now())
		if err != nil {
			internalError(w, "failed to save stock settings", err)
			return
		}
		if err := audit.Record(tx, caregiver.ID, "STOCK_SETTINGS", "medication", id, fmt.Sprintf("Stock settings: %+v", describeStock(req.StockSettings))); err != nil {
			internalError(w, "failed to record stock audit", err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit medication creation", err)
		return
	}
	if stockCh != nil {
		h.broadcastStock(stockCh, caregiver)
	}

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
	// Absent = unchanged, null = automatic, number = doctor's minimum hours.
	MinHoursBetween json.RawMessage `json:"min_hours_between,omitempty"`
	IsCritical      *bool           `json:"is_critical,omitempty"`
	StockSettings
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
	if msg := req.StockSettings.validate(); msg != "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_STOCK", msg)
		return
	}

	if req.DaysOfWeek != nil {
		if len(req.DaysOfWeek) > 0 && !validDays(req.DaysOfWeek) {
			writeJSONError(w, http.StatusBadRequest, "INVALID_DAYS", "days_of_week must contain valid days (sun, mon, tue, wed, thu, fri, sat)")
			return
		}
	}
	minHours, setMinHours, err := parseOptionalInterval(req.MinHoursBetween)
	if err != nil || !validIntervalHours(minHours) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_INTERVAL", "min_hours_between must be null or between 0 and 72 hours")
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to begin transaction", err)
		return
	}
	defer tx.Rollback()

	var before struct {
		NameAr          string
		NameEn          string
		Dosage          string
		InstructionsAr  sql.NullString
		InstructionsEn  sql.NullString
		FrequencySlots  string
		DaysOfWeek      string
		SlotTimes       string
		ImageURL        string
		MinHoursBetween sql.NullFloat64
		IsCritical      bool
		IsActive        bool
	}
	err = tx.QueryRow(`
		SELECT name_ar, name_en, dosage, instructions_ar, instructions_en, frequency_slots,
		       COALESCE(days_of_week, ''), COALESCE(slot_times, ''), COALESCE(image_url, ''),
		       min_hours_between, is_critical, is_active
		FROM medications WHERE id = ?
	`, medID).Scan(
		&before.NameAr, &before.NameEn, &before.Dosage, &before.InstructionsAr, &before.InstructionsEn, &before.FrequencySlots,
		&before.DaysOfWeek, &before.SlotTimes, &before.ImageURL,
		&before.MinHoursBetween, &before.IsCritical, &before.IsActive,
	)
	if err == sql.ErrNoRows {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "Medication not found")
		return
	} else if err != nil {
		internalError(w, "failed to query medication", err)
		return
	}

	slotsJSON, _ := json.Marshal(req.FrequencySlots)
	_, err = tx.Exec(`
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
		if _, err := tx.Exec("UPDATE medications SET days_of_week = ? WHERE id = ?", string(daysJSON), medID); err != nil {
			internalError(w, "failed to update days of week", err)
			return
		}
	}

	if req.SlotTimes != nil {
		var currentSlotsJSON string
		if err := tx.QueryRow("SELECT frequency_slots FROM medications WHERE id = ?", medID).Scan(&currentSlotsJSON); err != nil {
			internalError(w, "failed to read frequency slots", err)
			return
		}
		var currentSlots []string
		_ = json.Unmarshal([]byte(currentSlotsJSON), &currentSlots)
		normalizedTimes := normalizeSlotTimes(req.SlotTimes, currentSlots)
		timesJSON, _ := json.Marshal(normalizedTimes)
		if _, err := tx.Exec("UPDATE medications SET slot_times = ? WHERE id = ?", string(timesJSON), medID); err != nil {
			internalError(w, "failed to update slot times", err)
			return
		}
	}

	if req.IsCritical != nil {
		if _, err := tx.Exec("UPDATE medications SET is_critical = ? WHERE id = ?", *req.IsCritical, medID); err != nil {
			internalError(w, "failed to update medication", err)
			return
		}
	}

	if setMinHours {
		if _, err := tx.Exec("UPDATE medications SET min_hours_between = ? WHERE id = ?", minHours, medID); err != nil {
			internalError(w, "failed to update medication", err)
			return
		}
	}

	if req.IsActive != nil {
		activeInt := 0
		if *req.IsActive {
			activeInt = 1
		}
		if _, err := tx.Exec("UPDATE medications SET is_active = ? WHERE id = ?", activeInt, medID); err != nil {
			internalError(w, "failed to update active status", err)
			return
		}
	}

	var stockCh *stockChange
	if req.StockSettings.StockUnits != nil || req.StockSettings.TrackStock != nil || req.StockSettings.UnitsPerDose != nil || req.StockSettings.RefillThresholdDays != nil {
		stockCh, err = applyStockSettings(tx, medID, caregiver.ID, req.StockSettings, time.Now())
		if err != nil {
			internalError(w, "failed to save stock settings", err)
			return
		}
		if err := audit.Record(tx, caregiver.ID, "STOCK_SETTINGS", "medication", medID, fmt.Sprintf("Stock settings: %+v", describeStock(req.StockSettings))); err != nil {
			internalError(w, "failed to record stock audit", err)
			return
		}
	}

	var after struct {
		NameAr          string
		NameEn          string
		Dosage          string
		InstructionsAr  sql.NullString
		InstructionsEn  sql.NullString
		FrequencySlots  string
		DaysOfWeek      string
		SlotTimes       string
		ImageURL        string
		MinHoursBetween sql.NullFloat64
		IsCritical      bool
		IsActive        bool
	}
	if err := tx.QueryRow(`
		SELECT name_ar, name_en, dosage, instructions_ar, instructions_en, frequency_slots,
		       COALESCE(days_of_week, ''), COALESCE(slot_times, ''), COALESCE(image_url, ''),
		       min_hours_between, is_critical, is_active
		FROM medications WHERE id = ?
	`, medID).Scan(
		&after.NameAr, &after.NameEn, &after.Dosage, &after.InstructionsAr, &after.InstructionsEn, &after.FrequencySlots,
		&after.DaysOfWeek, &after.SlotTimes, &after.ImageURL,
		&after.MinHoursBetween, &after.IsCritical, &after.IsActive,
	); err != nil {
		internalError(w, "failed to query updated medication", err)
		return
	}

	beforeMap := map[string]any{
		"name_ar":         before.NameAr,
		"name_en":         before.NameEn,
		"dosage":          before.Dosage,
		"instructions_ar": before.InstructionsAr.String,
		"instructions_en": before.InstructionsEn.String,
		"frequency_slots": before.FrequencySlots,
		"days_of_week":    before.DaysOfWeek,
		"slot_times":      before.SlotTimes,
		"image_url":       before.ImageURL,
		"is_critical":     before.IsCritical,
		"is_active":       before.IsActive,
	}
	if before.MinHoursBetween.Valid {
		beforeMap["min_hours_between"] = before.MinHoursBetween.Float64
	}

	afterMap := map[string]any{
		"name_ar":         after.NameAr,
		"name_en":         after.NameEn,
		"dosage":          after.Dosage,
		"instructions_ar": after.InstructionsAr.String,
		"instructions_en": after.InstructionsEn.String,
		"frequency_slots": after.FrequencySlots,
		"days_of_week":    after.DaysOfWeek,
		"slot_times":      after.SlotTimes,
		"image_url":       after.ImageURL,
		"is_critical":     after.IsCritical,
		"is_active":       after.IsActive,
	}
	if after.MinHoursBetween.Valid {
		afterMap["min_hours_between"] = after.MinHoursBetween.Float64
	}

	auditPayload, _ := json.Marshal(map[string]any{
		"action": "Updated prescription",
		"id":     medID,
		"before": beforeMap,
		"after":  afterMap,
	})
	if err := audit.Record(tx, caregiver.ID, "UPDATE_MEDICATION", "medication", medID, string(auditPayload)); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}

	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit medication update", err)
		return
	}

	if stockCh != nil {
		h.broadcastStock(stockCh, caregiver)
	}

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

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to begin transaction", err)
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE medications
		SET days_of_week = ?,
		    slot_times = ?
		WHERE id = ?
	`, string(daysJSON), string(timesJSON), medID)
	if err != nil {
		internalError(w, "failed to update schedule", err)
		return
	}

	if err := audit.Record(tx, caregiver.ID, "UPDATE_SCHEDULE", "medication", medID, fmt.Sprintf("Updated schedule for %s", medID)); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}

	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit schedule update", err)
		return
	}

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

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to begin transaction", err)
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec("UPDATE medications SET is_active = 0 WHERE id = ?", medID)
	if err != nil {
		internalError(w, "failed to deactivate medication", err)
		return
	}

	if err := audit.Record(tx, caregiver.ID, "DEACTIVATE_MEDICATION", "medication", medID, "Deactivated prescription"); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}

	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit deactivation", err)
		return
	}

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
		SELECT id, name_ar, name_en, dosage, instructions_ar, instructions_en, frequency_slots, image_url, is_active, created_at, days_of_week, slot_times, min_hours_between
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
		var minHours sql.NullFloat64
		var slotsJSON, createdAtStr string
		if err := medRows.Scan(&m.ID, &m.NameAr, &m.NameEn, &m.Dosage, &instAr, &instEn, &slotsJSON, &imgURL, &m.IsActive, &createdAtStr, &daysJSON, &timesJSON, &minHours); err != nil {
			continue
		}
		m.MinHoursBetween = nullableFloat(minHours)
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
		       c.name, c.avatar_color, COALESCE(l.recorded_at, ''), COALESCE(l.voided_at, ''), COALESCE(v.name, ''), COALESCE(l.void_reason, '')
		FROM medication_logs l
		JOIN caregivers c ON l.caregiver_id = c.id
		LEFT JOIN caregivers v ON l.voided_by = v.id
		WHERE l.scheduled_date = ?
		ORDER BY l.recorded_at
	`, dateStr)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch medication logs"}`, http.StatusInternalServerError)
		return
	}
	defer logRows.Close()

	// Map key: "medication_id:slot" -> the ACTIVE MedicationLog; voided ones are history.
	logsMap := make(map[string]models.MedicationLog)
	voidedMap := make(map[string][]models.MedicationLog)
	for logRows.Next() {
		var l models.MedicationLog
		var notes sql.NullString
		var takenAtStr sql.NullString
		var recordedAt, voidedAt string
		if err := logRows.Scan(&l.ID, &l.MedicationID, &l.CaregiverID, &l.Slot, &l.ScheduledDate, &takenAtStr, &l.Status, &notes, &l.CaregiverName, &l.AvatarColor,
			&recordedAt, &voidedAt, &l.VoidedByName, &l.VoidReason); err != nil {
			continue
		}
		l.RecordedAt = sqliteToRFC3339(recordedAt)
		l.VoidedAt = sqliteToRFC3339(voidedAt)
		if notes.Valid {
			l.Notes = notes.String
		}
		if takenAtStr.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", takenAtStr.String)
			l.TakenAt = &t
		}
		key := fmt.Sprintf("%s:%s", l.MedicationID, l.Slot)
		if l.VoidedAt != "" {
			voidedMap[key] = append(voidedMap[key], l)
		} else {
			logsMap[key] = l
		}
	}

	logRows.Close()

	events, err := logEventsForDate(h.DB, dateStr)
	if err != nil {
		internalError(w, "failed to fetch dose follow-ups", err)
		return
	}
	for key, l := range logsMap {
		l.Events = events[l.ID]
		logsMap[key] = l
	}

	// 3. Last taken dose of each medication, on any date (interval lock, issue #7).
	now := time.Now()
	lastTaken := make(map[string]*models.LastDose, len(meds))
	for _, m := range meds {
		d, err := nearestTakenDose(h.DB, m.ID, now)
		if err != nil {
			internalError(w, "failed to fetch last doses", err)
			return
		}
		lastTaken[m.ID] = d
	}

	// 4. Assemble scheduled dose cards
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
			if !isScheduledToday && !hasLog && len(voidedMap[key]) == 0 {
				continue
			}

			scheduledTime := m.SlotTimes[slot]
			if scheduledTime == "" {
				scheduledTime = defaultSlotTimes[slot]
			}

			item := models.ScheduledDose{
				Medication:    m,
				Slot:          slot,
				Time:          scheduledTime,
				LastTaken:     lastTaken[m.ID],
				VoidedLogs:    voidedMap[key],
				IntervalHours: intervalHours(m.MinHoursBetween, m.FrequencySlots, m.SlotTimes),
			}
			if last := item.LastTaken; last != nil && item.IntervalHours > 0 {
				if t, err := time.Parse(time.RFC3339, last.TakenAt); err == nil {
					if next := t.Add(time.Duration(item.IntervalHours * float64(time.Hour))); next.After(now) {
						item.NextAllowedAt = next.UTC().Format(time.RFC3339)
					}
				}
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
	Status        string `json:"status"`         // 'taken', 'partial', 'refused' or 'skipped'
	Notes         string `json:"notes,omitempty"`
	// GivenAt (RFC3339, optional) is when the dose was actually given, for a dose
	// ticked late ("given at 08:00", ticked at 20:30). Default: now.
	GivenAt string `json:"given_at,omitempty"`
	// Admin-only escape from the interval lock (e.g. the doctor ordered an extra dose).
	OverrideInterval bool   `json:"override_interval,omitempty"`
	OverrideReason   string `json:"override_reason,omitempty"`
}

const (
	maxNotesLen          = 2000
	maxOverrideReasonLen = 500
	// A late tick may be backdated at most this far (yesterday's bedtime dose
	// ticked the next morning).
	maxGivenAtAge = 36 * time.Hour
)

// LogDose records medication intake with double-dosing locking.
//
// Safety model, two locks:
//  1. Same dose card: SQLite's UNIQUE(medication_id, scheduled_date, slot) allows at
//     most one row per dose no matter how many siblings tap at once (409
//     DOSE_ALREADY_LOGGED with who/when).
//  2. Same medication across cards, e.g. bedtime on D at 23:30 and bedtime on D+1 at
//     03:45 (issue #7): a "taken" dose within the minimum interval of any other
//     taken dose is refused (409 DOSE_TOO_SOON). This one IS check-then-insert, and
//     it is race-free only because the DSN sets _txlock=immediate: BEGIN takes the
//     write lock, so no other transaction can insert between the check and the
//     INSERT. Do not change the transaction mode without keeping that guarantee
//     (TestDoseIntervalConcurrent covers it).
//
// The dose row and its audit rows are committed in one transaction.
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
	case "taken", "skipped", "partial", "refused":
	case "vomited":
		writeJSONError(w, http.StatusBadRequest, "USE_EVENT", "vomiting is recorded on the given dose: POST /medications/log/{id}/event")
		return
	default:
		// Never silently coerce an unknown status into "taken".
		writeJSONError(w, http.StatusBadRequest, "INVALID_STATUS", "status must be 'taken', 'partial', 'refused' or 'skipped'")
		return
	}
	if len(req.Notes) > maxNotesLen {
		writeJSONError(w, http.StatusBadRequest, "NOTES_TOO_LONG", "notes are too long")
		return
	}

	now := time.Now().UTC().Truncate(time.Second)
	givenAt := now
	if req.GivenAt != "" {
		t, err := time.Parse(time.RFC3339, req.GivenAt)
		if err != nil || t.After(now.Add(time.Minute)) || now.Sub(t) > maxGivenAtAge {
			writeJSONError(w, http.StatusBadRequest, "INVALID_GIVEN_AT", "given_at must be an RFC3339 time within the last 36 hours")
			return
		}
		if t.Before(now) {
			givenAt = t.UTC().Truncate(time.Second)
		}
	}

	req.OverrideReason = strings.TrimSpace(req.OverrideReason)
	if req.OverrideInterval {
		if !auth.CanManageMedications(caregiver) {
			writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only an admin can give a dose inside the minimum interval")
			return
		}
		if req.OverrideReason == "" {
			writeJSONError(w, http.StatusBadRequest, "REASON_REQUIRED", "Giving a dose inside the minimum interval needs a reason")
			return
		}
		if len(req.OverrideReason) > maxOverrideReasonLen {
			writeJSONError(w, http.StatusBadRequest, "REASON_TOO_LONG", "reason is too long")
			return
		}
	}

	// Medication must exist, be active, and actually be scheduled in this slot.
	var nameAr, nameEn, slotsJSON, dosage string
	var timesJSON sql.NullString
	var minHoursCol sql.NullFloat64
	err := h.DB.QueryRowContext(r.Context(),
		"SELECT name_ar, name_en, frequency_slots, slot_times, min_hours_between, dosage FROM medications WHERE id = ? AND is_active = 1",
		req.MedicationID).Scan(&nameAr, &nameEn, &slotsJSON, &timesJSON, &minHoursCol, &dosage)
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

	var slotTimes map[string]string
	if timesJSON.Valid {
		_ = json.Unmarshal([]byte(timesJSON.String), &slotTimes)
	}
	minHours := intervalHours(nullableFloat(minHoursCol), slots, normalizeSlotTimes(slotTimes, slots))

	logID := uuid.New().String()

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to log medication", err)
		return
	}
	// From here until Commit/Rollback the single pooled connection belongs to tx:
	// every statement MUST go through tx, never h.DB, or it deadlocks.

	// Same card already recorded: report that, not the interval.
	var existing int
	if err := tx.QueryRow(`SELECT 1 FROM medication_logs WHERE medication_id = ? AND scheduled_date = ? AND slot = ? AND voided_at IS NULL`,
		req.MedicationID, req.ScheduledDate, req.Slot).Scan(&existing); err == nil {
		tx.Rollback()
		h.writeDoseConflict(w, r, req)
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		tx.Rollback()
		internalError(w, "failed to log medication", err)
		return
	}

	// Interval lock. Compared against the nearest taken dose in either direction, so
	// a backdated given_at cannot slip a dose in before a later one.
	var overridden *models.LastDose
	// A partial dose is still medicine in the body: it counts for the interval too.
	if (req.Status == "taken" || req.Status == "partial") && minHours > 0 {
		other, err := nearestTakenDose(tx, req.MedicationID, givenAt)
		if err != nil {
			tx.Rollback()
			internalError(w, "failed to check the last dose", err)
			return
		}
		if other != nil {
			t, _ := time.Parse(time.RFC3339, other.TakenAt)
			if math.Abs(givenAt.Sub(t).Hours()) < minHours {
				if !req.OverrideInterval {
					tx.Rollback()
					writeDoseTooSoon(w, req, other, minHours)
					return
				}
				overridden = other
			}
		}
	}

	_, err = tx.Exec(`
		INSERT INTO medication_logs (id, medication_id, caregiver_id, slot, scheduled_date, taken_at, recorded_at, status, notes, dosage_snapshot)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, logID, req.MedicationID, caregiver.ID, req.Slot, req.ScheduledDate, givenAt.Format(sqliteTimeLayout), now.Format(sqliteTimeLayout), req.Status, req.Notes, dosage)
	if err != nil {
		tx.Rollback() // release the connection BEFORE querying the existing dose
		if isUniqueViolation(err) {
			h.writeDoseConflict(w, r, req)
			return
		}
		internalError(w, "failed to log medication", err)
		return
	}

	details := fmt.Sprintf("Dose marked as %s for med %s (slot %s, date %s)", req.Status, req.MedicationID, req.Slot, req.ScheduledDate)
	if !givenAt.Equal(now) {
		details += fmt.Sprintf(", given at %s (recorded %s)", givenAt.Format(time.RFC3339), now.Format(time.RFC3339))
	}
	if err := audit.Record(tx, caregiver.ID, "MED_LOGGED", "medication_log", logID, details); err != nil {
		tx.Rollback()
		internalError(w, "failed to write audit trail", err)
		return
	}
	// Issue #20: the dose comes out of the box in the same transaction.
	stockCh, err := takeDoseFromStock(tx, req.MedicationID, logID, caregiver.ID, req.Status, now)
	if err != nil {
		tx.Rollback()
		internalError(w, "failed to update stock", err)
		return
	}
	var alertID string
	if overridden != nil {
		if alertID, err = recordAlert(tx, "dose_override", logID, caregiver.ID, map[string]interface{}{
			"medication_id": req.MedicationID, "name_ar": nameAr, "name_en": nameEn, "slot": req.Slot, "reason": req.OverrideReason,
		}); err != nil {
			tx.Rollback()
			internalError(w, "failed to record alert", err)
			return
		}
		if err := audit.Record(tx, caregiver.ID, "MED_INTERVAL_OVERRIDE", "medication_log", logID,
			fmt.Sprintf("Gave %s inside the %.1f h minimum interval; previous dose %s by %s (%s %s). Reason: %s",
				req.MedicationID, minHours, overridden.TakenAt, overridden.CaregiverName, overridden.ScheduledDate, overridden.Slot, req.OverrideReason)); err != nil {
			tx.Rollback()
			internalError(w, "failed to write audit trail", err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit medication log", err)
		return
	}

	// Broadcast SSE event to all connected sibling devices
	medPayload := map[string]interface{}{
		"alert_id":          alertID,
		"log_id":            logID,
		"medication_id":     req.MedicationID,
		"name_ar":           nameAr,
		"name_en":           nameEn,
		"slot":              req.Slot,
		"scheduled_date":    req.ScheduledDate,
		"status":            req.Status,
		"taken_at":          givenAt.Format(time.RFC3339),
		"notes":             req.Notes,
		"interval_override": overridden != nil,
		"override_reason":   overrideReasonIf(overridden != nil, req.OverrideReason),
		"caregiver_id":      caregiver.ID,
		"caregiver_name":    caregiver.Name,
		"avatar_color":      caregiver.AvatarColor,
	}
	if alertID != "" {
		h.Broker.BroadcastAlert(alertID, "MED_LOGGED", actorOf(caregiver), medPayload)
		h.Push.NotifyAsync(alertID, "dose_override", caregiver.ID)
	} else {
		h.Broker.Broadcast("MED_LOGGED", actorOf(caregiver), medPayload)
	}
	h.broadcastStock(stockCh, caregiver)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "success",
		"log_id":         logID,
		"taken_at":       givenAt.Format(time.RFC3339),
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
		WHERE l.medication_id = ? AND l.scheduled_date = ? AND l.slot = ? AND l.voided_at IS NULL
	`, req.MedicationID, req.ScheduledDate, req.Slot).Scan(&logID, &caregiverID, &name, &avatar, &takenAt, &status)
	if err != nil {
		log.Printf("[ERROR] dose conflict lookup failed: %v", err)
	}

	// Issue #16: a retry of my own tap (a lost 201, an offline outbox resend) is not
	// a sibling conflict: same caregiver, same status → 200, already logged by you.
	if me := auth.GetCaregiverFromContext(r.Context()); me != nil && caregiverID == me.ID && status == req.Status {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":                "success",
			"already_logged_by_you": true,
			"log_id":                logID,
			"taken_at":              sqliteToRFC3339(takenAt),
			"caregiver_name":        name,
			"avatar_color":          avatar,
		})
		return
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

func overrideReasonIf(ok bool, reason string) string {
	if ok {
		return reason
	}
	return ""
}

type VoidDoseRequest struct {
	Reason string `json:"reason"`
}

// undoWindow is how long the caregiver who tapped a dose may cancel it on their
// own (a sleepy mis-tap). After that only an admin can, with a reason.
const undoWindow = 10 * time.Minute

// VoidDose cancels a dose entry (issue #8). Nothing is deleted: the row is marked
// voided and stays as history, and the partial unique index lets the real dose be
// logged on that card. The void and its audit row commit in one transaction.
func (h *MedicationHandler) VoidDose(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		writeJSONError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Select your profile and enter your PIN")
		return
	}
	if !auth.CanTakeActions(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers cannot change dose entries")
		return
	}
	logID := chi.URLParam(r, "id")

	var req VoidDoseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if len(req.Reason) > maxOverrideReasonLen {
		writeJSONError(w, http.StatusBadRequest, "REASON_TOO_LONG", "reason is too long")
		return
	}

	var loggedBy, recordedAt, medID, slot, date, status, nameAr, nameEn string
	var voidedAt sql.NullString
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT l.caregiver_id, COALESCE(l.recorded_at, l.taken_at, ''), l.voided_at, l.medication_id, l.slot, l.scheduled_date, l.status, m.name_ar, m.name_en
		FROM medication_logs l JOIN medications m ON m.id = l.medication_id
		WHERE l.id = ?`, logID).Scan(&loggedBy, &recordedAt, &voidedAt, &medID, &slot, &date, &status, &nameAr, &nameEn)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "LOG_NOT_FOUND", "dose entry not found")
		return
	} else if err != nil {
		internalError(w, "failed to look up dose entry", err)
		return
	}
	if voidedAt.Valid {
		writeJSONError(w, http.StatusConflict, "ALREADY_VOIDED", "this entry was already cancelled")
		return
	}

	now := time.Now().UTC().Truncate(time.Second)
	tapped, err := time.Parse(sqliteTimeLayout, recordedAt)
	ownUndo := err == nil && loggedBy == caregiver.ID && now.Sub(tapped) <= undoWindow
	if !ownUndo {
		if !auth.CanManageMedications(caregiver) {
			writeJSONError(w, http.StatusForbidden, "VOID_NOT_ALLOWED",
				"Only the person who recorded it can cancel within 10 minutes; after that, ask an admin")
			return
		}
		if req.Reason == "" {
			writeJSONError(w, http.StatusBadRequest, "REASON_REQUIRED", "Write why this entry is being cancelled")
			return
		}
	}
	if req.Reason == "" {
		req.Reason = "Undo"
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to cancel dose entry", err)
		return
	}
	// The single pooled connection belongs to tx until Commit/Rollback: use tx only.
	res, err := tx.Exec(`UPDATE medication_logs SET voided_at = ?, voided_by = ?, void_reason = ? WHERE id = ? AND voided_at IS NULL`,
		now.Format(sqliteTimeLayout), caregiver.ID, req.Reason, logID)
	if err != nil {
		tx.Rollback()
		internalError(w, "failed to cancel dose entry", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		tx.Rollback()
		writeJSONError(w, http.StatusConflict, "ALREADY_VOIDED", "this entry was already cancelled")
		return
	}
	// Issue #20: put back what the cancelled dose took out of the box.
	stockCh, err := returnDoseToStock(tx, medID, logID, caregiver.ID, now)
	if err != nil {
		tx.Rollback()
		internalError(w, "failed to update stock", err)
		return
	}
	if err := audit.Record(tx, caregiver.ID, "MED_VOIDED", "medication_log", logID,
		fmt.Sprintf("Cancelled %s entry for med %s (slot %s, date %s, recorded by %s). Reason: %s", status, medID, slot, date, loggedBy, req.Reason)); err != nil {
		tx.Rollback()
		internalError(w, "failed to write audit trail", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit", err)
		return
	}

	h.Broker.Broadcast("MED_VOIDED", models.ActorInfo{
		ID:          caregiver.ID,
		Name:        caregiver.Name,
		AvatarColor: caregiver.AvatarColor,
	}, map[string]interface{}{
		"log_id":         logID,
		"medication_id":  medID,
		"name_ar":        nameAr,
		"name_en":        nameEn,
		"slot":           slot,
		"scheduled_date": date,
		"status":         status,
		"reason":         req.Reason,
	})
	h.broadcastStock(stockCh, caregiver)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "voided",
		"log_id":    logID,
		"voided_at": now.Format(time.RFC3339),
	})
}
