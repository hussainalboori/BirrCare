package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
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

func (h *MedicationHandler) ListMedications(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
		SELECT id, name_ar, name_en, dosage, instructions_ar, instructions_en, frequency_slots, is_active, created_by, created_at
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
		var instAr, instEn, createdBy sql.NullString
		var slotsJSON string
		var createdAtStr string

		if err := rows.Scan(&m.ID, &m.NameAr, &m.NameEn, &m.Dosage, &instAr, &instEn, &slotsJSON, &m.IsActive, &createdBy, &createdAtStr); err != nil {
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
		json.Unmarshal([]byte(slotsJSON), &m.FrequencySlots)
		m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
		list = append(list, m)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

type CreateMedicationRequest struct {
	NameAr         string   `json:"name_ar"`
	NameEn         string   `json:"name_en"`
	Dosage         string   `json:"dosage"`
	InstructionsAr string   `json:"instructions_ar"`
	InstructionsEn string   `json:"instructions_en"`
	FrequencySlots []string `json:"frequency_slots"`
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

	id := "med_" + uuid.New().String()[:8]
	slotsJSON, _ := json.Marshal(req.FrequencySlots)

	_, err := h.DB.Exec(`
		INSERT INTO medications (id, name_ar, name_en, dosage, instructions_ar, instructions_en, frequency_slots, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
	`, id, req.NameAr, req.NameEn, req.Dosage, req.InstructionsAr, req.InstructionsEn, string(slotsJSON), caregiver.ID)

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
	NameAr         string   `json:"name_ar"`
	NameEn         string   `json:"name_en"`
	Dosage         string   `json:"dosage"`
	InstructionsAr string   `json:"instructions_ar"`
	InstructionsEn string   `json:"instructions_en"`
	FrequencySlots []string `json:"frequency_slots"`
	IsActive       *bool    `json:"is_active,omitempty"`
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

	slotsJSON, _ := json.Marshal(req.FrequencySlots)
	_, err := h.DB.Exec(`
		UPDATE medications
		SET name_ar = COALESCE(NULLIF(?, ''), name_ar),
		    name_en = COALESCE(NULLIF(?, ''), name_en),
		    dosage = COALESCE(NULLIF(?, ''), dosage),
		    instructions_ar = ?,
		    instructions_en = ?,
		    frequency_slots = CASE WHEN ? = '[]' OR ? = 'null' THEN frequency_slots ELSE ? END
		WHERE id = ?
	`, req.NameAr, req.NameEn, req.Dosage, req.InstructionsAr, req.InstructionsEn, string(slotsJSON), string(slotsJSON), string(slotsJSON), medID)

	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to update medication: %v"}`, err), http.StatusInternalServerError)
		return
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
		http.Error(w, fmt.Sprintf(`{"error":"failed to deactivate medication: %v"}`, err), http.StatusInternalServerError)
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
		dateStr = time.Now().UTC().Format("2006-01-02")
	}

	// 1. Fetch all active medications
	medRows, err := h.DB.Query(`
		SELECT id, name_ar, name_en, dosage, instructions_ar, instructions_en, frequency_slots, is_active, created_at
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
		var instAr, instEn sql.NullString
		var slotsJSON, createdAtStr string
		if err := medRows.Scan(&m.ID, &m.NameAr, &m.NameEn, &m.Dosage, &instAr, &instEn, &slotsJSON, &m.IsActive, &createdAtStr); err != nil {
			continue
		}
		if instAr.Valid {
			m.InstructionsAr = instAr.String
		}
		if instEn.Valid {
			m.InstructionsEn = instEn.String
		}
		json.Unmarshal([]byte(slotsJSON), &m.FrequencySlots)
		m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
		meds = append(meds, m)
	}

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
		for _, slot := range m.FrequencySlots {
			key := fmt.Sprintf("%s:%s", m.ID, slot)
			item := models.ScheduledDose{
				Medication: m,
				Slot:       slot,
			}
			if log, exists := logsMap[key]; exists {
				logCopy := log
				item.Log = &logCopy
			}
			schedule = append(schedule, item)
		}
	}

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

// LogDose records medication intake with double-dosing locking.
func (h *MedicationHandler) LogDose(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	if !auth.CanTakeActions(caregiver) {
		http.Error(w, `{"error":"FORBIDDEN","message":"Viewers are not permitted to log medication doses"}`, http.StatusForbidden)
		return
	}

	var req LogDoseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	if req.MedicationID == "" || req.Slot == "" || req.ScheduledDate == "" {
		http.Error(w, `{"error":"missing required fields"}`, http.StatusBadRequest)
		return
	}

	if req.Status != "taken" && req.Status != "skipped" {
		req.Status = "taken"
	}

	logID := uuid.New().String()
	now := time.Now().UTC()

	// Perform write. The SQLite UNIQUE constraint on (medication_id, scheduled_date, slot)
	// guarantees that no two concurrent writes can succeed for the same dose.
	_, err := h.DB.Exec(`
		INSERT INTO medication_logs (id, medication_id, caregiver_id, slot, scheduled_date, taken_at, status, notes)
		VALUES (?, ?, ?, ?, ?, datetime('now'), ?, ?)
	`, logID, req.MedicationID, caregiver.ID, req.Slot, req.ScheduledDate, req.Status, req.Notes)

	if err != nil {
		// Detect SQLite UNIQUE constraint violation
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "constraint") {
			// Fetch the existing log to inform the caller who took it and when
			var existingCaregiverName, existingTakenAt string
			row := h.DB.QueryRow(`
				SELECT c.name, l.taken_at
				FROM medication_logs l
				JOIN caregivers c ON l.caregiver_id = c.id
				WHERE l.medication_id = ? AND l.scheduled_date = ? AND l.slot = ?
			`, req.MedicationID, req.ScheduledDate, req.Slot)
			_ = row.Scan(&existingCaregiverName, &existingTakenAt)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict) // HTTP 409 Conflict
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":          "DOSE_ALREADY_LOGGED",
				"message":        "This dose has already been recorded",
				"caregiver_name": existingCaregiverName,
				"taken_at":       existingTakenAt,
			})
			return
		}

		http.Error(w, fmt.Sprintf(`{"error":"failed to log medication: %v"}`, err), http.StatusInternalServerError)
		return
	}

	// Audit trail
	audit.Log(h.DB, caregiver.ID, "MED_LOGGED", "medication_log", logID,
		fmt.Sprintf("Dose marked as %s for med %s (slot %s, date %s)", req.Status, req.MedicationID, req.Slot, req.ScheduledDate))

	// Fetch medication name for rich notification
	var nameAr, nameEn string
	_ = h.DB.QueryRow("SELECT name_ar, name_en FROM medications WHERE id = ?", req.MedicationID).Scan(&nameAr, &nameEn)

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
