package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

var allowedReasonTags = map[string]bool{
	"headache": true,
	"pain":     true,
	"fever":    true,
	"other":    true,
}

// GetPRNMedications returns all active PRN medications with their 24h rolling dose counts,
// interval locks, last administered details, and recent logs.
func (h *MedicationHandler) GetPRNMedications(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT id, name_ar, name_en, dosage, instructions_ar, instructions_en, image_url,
		       prn_min_hours, prn_max_per_24h, stock_units, units_per_dose, refill_threshold_days,
		       COALESCE(is_critical, 0), created_at, COALESCE(photo_urls, '[]')
		FROM medications
		WHERE is_active = 1 AND is_prn = 1
		ORDER BY name_ar ASC
	`)
	if err != nil {
		internalError(w, "failed to fetch PRN medications", err)
		return
	}
	defer rows.Close()

	now := time.Now().UTC()
	list := []models.PRNMedicationStatus{} // [] not null when there are none

	type prnBase struct {
		med      models.Medication
		minHours *float64
		max24h   *int
	}
	var prnMeds []prnBase

	for rows.Next() {
		var m models.Medication
		var instAr, instEn, imgURL sql.NullString
		var minHours, stock sql.NullFloat64
		var max24h sql.NullInt64
		var createdAtStr, photosJSON string

		if err := rows.Scan(&m.ID, &m.NameAr, &m.NameEn, &m.Dosage, &instAr, &instEn, &imgURL,
			&minHours, &max24h, &stock, &m.UnitsPerDose, &m.RefillThresholdDays,
			&m.IsCritical, &createdAtStr, &photosJSON); err != nil {
			continue
		}
		m.PhotoURLs = parsePhotoURLs(photosJSON)
		m.IsPRN = true
		m.FrequencySlots = []string{}
		m.DaysOfWeek = allDaysOfWeek
		if instAr.Valid {
			m.InstructionsAr = instAr.String
		}
		if instEn.Valid {
			m.InstructionsEn = instEn.String
		}
		if imgURL.Valid {
			m.ImageURL = imgURL.String
		}
		m.StockUnits = nullableFloat(stock)
		m.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)

		var pMin *float64
		if minHours.Valid {
			val := minHours.Float64
			pMin = &val
			m.PRNMinHours = pMin
		}
		var pMax *int
		if max24h.Valid {
			val := int(max24h.Int64)
			pMax = &val
			m.PRNMaxPer24h = pMax
		}

		prnMeds = append(prnMeds, prnBase{med: m, minHours: pMin, max24h: pMax})
	}
	rows.Close() // Release pooled connection before queries inside the loop

	for _, pb := range prnMeds {
		med := pb.med
		status := models.PRNMedicationStatus{
			Medication:   med,
			MaxPer24h:    pb.max24h,
			MinHours:     pb.minHours,
			CanGive:      true,
			RecentLogs:   []models.PRNLog{},
		}

		// 1. Count active doses in rolling 24h window
		var count24h int
		since24h := now.Add(-24 * time.Hour).Format(sqliteTimeLayout)
		_ = h.DB.QueryRowContext(r.Context(), `
			SELECT COUNT(*) FROM prn_logs
			WHERE medication_id = ? AND voided_at IS NULL AND given_at >= ?
		`, med.ID, since24h).Scan(&count24h)
		status.CountLast24h = count24h

		// 2. Query most recent active dose
		var last models.PRNLastGiven
		var lastGivenAtStr string
		err := h.DB.QueryRowContext(r.Context(), `
			SELECT l.id, l.given_at, l.caregiver_id, c.name, COALESCE(c.avatar_color, ''), l.reason_tag
			FROM prn_logs l
			JOIN caregivers c ON l.caregiver_id = c.id
			WHERE l.medication_id = ? AND l.voided_at IS NULL
			ORDER BY l.given_at DESC
			LIMIT 1
		`, med.ID).Scan(&last.LogID, &lastGivenAtStr, &last.CaregiverID, &last.CaregiverName, &last.AvatarColor, &last.ReasonTag)

		var lastGivenTime time.Time
		var hasLastGiven bool
		if err == nil {
			hasLastGiven = true
			if t, err := time.Parse(sqliteTimeLayout, lastGivenAtStr); err == nil {
				lastGivenTime = t.UTC()
				last.GivenAt = lastGivenTime.Format(time.RFC3339)
				last.HoursSince = math.Round(now.Sub(lastGivenTime).Hours()*100) / 100
			} else {
				last.GivenAt = lastGivenAtStr
			}
			status.LastGiven = &last
		}

		// 3. Clinical Lock Evaluation
		var nextAllowed time.Time

		// Check 24-hour cap
		if pb.max24h != nil && *pb.max24h > 0 && count24h >= *pb.max24h {
			status.CanGive = false
			status.LockReason = "limit_reached"

			// Find earliest active dose in the rolling 24h window: next slot opens 24h after that dose
			var earliestIn24hStr string
			if err := h.DB.QueryRowContext(r.Context(), `
				SELECT given_at FROM prn_logs
				WHERE medication_id = ? AND voided_at IS NULL AND given_at >= ?
				ORDER BY given_at ASC
				LIMIT 1
			`, med.ID, since24h).Scan(&earliestIn24hStr); err == nil {
				if t, err := time.Parse(sqliteTimeLayout, earliestIn24hStr); err == nil {
					slotOpensAt := t.UTC().Add(24 * time.Hour)
					if slotOpensAt.After(nextAllowed) {
						nextAllowed = slotOpensAt
					}
				}
			}
		}

		// Check minimum interval
		if pb.minHours != nil && *pb.minHours > 0 && hasLastGiven {
			intervalDuration := time.Duration(*pb.minHours * float64(time.Hour))
			intervalOpensAt := lastGivenTime.Add(intervalDuration)
			if intervalOpensAt.After(now) {
				status.CanGive = false
				if status.LockReason == "" {
					status.LockReason = "too_soon"
				}
				if intervalOpensAt.After(nextAllowed) {
					nextAllowed = intervalOpensAt
				}
			}
		}

		if !nextAllowed.IsZero() && nextAllowed.After(now) {
			status.NextAllowedAt = nextAllowed.Format(time.RFC3339)
		}

		// 4. Fetch recent logs for this PRN medication
		logRows, err := h.DB.QueryContext(r.Context(), `
			SELECT l.id, l.medication_id, l.caregiver_id, c.name, COALESCE(c.avatar_color, ''),
			       l.given_at, l.reason_tag, COALESCE(l.notes, ''), COALESCE(l.voided_at, ''),
			       COALESCE(v.name, ''), COALESCE(l.void_reason, ''), COALESCE(l.dosage_snapshot, ''), COALESCE(l.recorded_at, '')
			FROM prn_logs l
			JOIN caregivers c ON l.caregiver_id = c.id
			LEFT JOIN caregivers v ON l.voided_by = v.id
			WHERE l.medication_id = ?
			ORDER BY l.given_at DESC
			LIMIT 15
		`, med.ID)
		if err == nil {
			for logRows.Next() {
				var pl models.PRNLog
				var givenAt, voidedAt, recordedAt string
				if err := logRows.Scan(&pl.ID, &pl.MedicationID, &pl.CaregiverID, &pl.CaregiverName, &pl.AvatarColor,
					&givenAt, &pl.ReasonTag, &pl.Notes, &voidedAt, &pl.VoidedByName, &pl.VoidReason, &pl.DosageSnapshot, &recordedAt); err == nil {
					if recordedAt != "" {
						pl.RecordedAt = sqliteToRFC3339(recordedAt)
					}
					pl.GivenAt = sqliteToRFC3339(givenAt)
					if voidedAt != "" {
						pl.VoidedAt = sqliteToRFC3339(voidedAt)
					}
					status.RecentLogs = append(status.RecentLogs, pl)
				}
			}
			logRows.Close()
		}

		list = append(list, status)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

type LogPRNDoseRequest struct {
	MedicationID     string `json:"medication_id"`
	ReasonTag        string `json:"reason_tag"` // 'headache', 'pain', 'fever', 'other'
	Notes            string `json:"notes,omitempty"`
	GivenAt          string `json:"given_at,omitempty"` // RFC3339, default now
	OverrideLimit    bool   `json:"override_limit,omitempty"`
	OverrideInterval bool   `json:"override_interval,omitempty"`
	OverrideReason   string `json:"override_reason,omitempty"`
}

// LogPRNDose records an as-needed medication intake with transactional 24h limit
// and minimum interval enforcement. Zero race conditions under concurrent submissions.
func (h *MedicationHandler) LogPRNDose(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		writeJSONError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Select your profile and enter your PIN")
		return
	}
	if !auth.CanTakeActions(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers are not permitted to log medication doses")
		return
	}

	var req LogPRNDoseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}

	req.MedicationID = strings.TrimSpace(req.MedicationID)
	req.ReasonTag = strings.ToLower(strings.TrimSpace(req.ReasonTag))
	req.Notes = strings.TrimSpace(req.Notes)
	req.OverrideReason = strings.TrimSpace(req.OverrideReason)

	if req.MedicationID == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_MEDICATION_ID", "medication_id is required")
		return
	}
	if !allowedReasonTags[req.ReasonTag] {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REASON", "reason_tag must be one of: headache, pain, fever, other")
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
			writeJSONError(w, http.StatusBadRequest, "INVALID_GIVEN_AT", "given_at must be an RFC3339 timestamp within the last 36 hours")
			return
		}
		givenAt = t.UTC().Truncate(time.Second)
	}

	isOverride := req.OverrideLimit || req.OverrideInterval
	if isOverride {
		if !auth.CanManageMedications(caregiver) {
			writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only Admins and Super Admins can override clinical PRN dose limits")
			return
		}
		if req.OverrideReason == "" {
			writeJSONError(w, http.StatusBadRequest, "REASON_REQUIRED", "Overriding PRN limit or interval requires an explanation")
			return
		}
		if len(req.OverrideReason) > maxOverrideReasonLen {
			writeJSONError(w, http.StatusBadRequest, "REASON_TOO_LONG", "override reason is too long")
			return
		}
	}

	// Begin transactional write lock (_txlock=immediate guarantees exclusive write safety)
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to start transaction", err)
		return
	}
	defer tx.Rollback()

	// 1. Verify medication exists, is active and is PRN
	var nameAr, nameEn, dosage string
	var isPRN int
	var minHoursCol sql.NullFloat64
	var max24hCol sql.NullInt64

	err = tx.QueryRow(`
		SELECT name_ar, name_en, dosage, is_prn, prn_min_hours, prn_max_per_24h
		FROM medications
		WHERE id = ? AND is_active = 1
	`, req.MedicationID).Scan(&nameAr, &nameEn, &dosage, &isPRN, &minHoursCol, &max24hCol)

	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "MEDICATION_NOT_FOUND", "medication not found or inactive")
		return
	} else if err != nil {
		internalError(w, "failed to query medication", err)
		return
	}

	if isPRN != 1 {
		writeJSONError(w, http.StatusBadRequest, "NOT_PRN", "this medication is not marked as an as-needed (PRN) medication")
		return
	}

	var prnMinHours *float64
	if minHoursCol.Valid {
		val := minHoursCol.Float64
		prnMinHours = &val
	}
	var prnMax24h *int
	if max24hCol.Valid {
		val := int(max24hCol.Int64)
		prnMax24h = &val
	}

	// 2. Check 24-hour rolling cap
	since24h := givenAt.Add(-24 * time.Hour).Format(sqliteTimeLayout)
	givenAtStr := givenAt.Format(sqliteTimeLayout)

	var count24h int
	err = tx.QueryRow(`
		SELECT COUNT(*) FROM prn_logs
		WHERE medication_id = ? AND voided_at IS NULL AND given_at >= ? AND given_at <= datetime(?, '+1 minute')
	`, req.MedicationID, since24h, givenAtStr).Scan(&count24h)
	if err != nil {
		internalError(w, "failed to count 24h PRN doses", err)
		return
	}

	// Query last given dose for conflict reporting
	var lastGivenID, lastGivenAt, lastGiverName, lastAvatar, lastReason string
	// Last dose, named in the 24h-limit reply.
	_ = tx.QueryRow(`
		SELECT l.id, l.given_at, c.name, COALESCE(c.avatar_color, ''), l.reason_tag
		FROM prn_logs l
		JOIN caregivers c ON l.caregiver_id = c.id
		WHERE l.medication_id = ? AND l.voided_at IS NULL
		ORDER BY l.given_at DESC
		LIMIT 1
	`, req.MedicationID).Scan(&lastGivenID, &lastGivenAt, &lastGiverName, &lastAvatar, &lastReason)

	// Enforce 24-hour limit
	if prnMax24h != nil && *prnMax24h > 0 && count24h >= *prnMax24h {
		if !req.OverrideLimit {
			// Find when the oldest active dose in the rolling 24h expires
			var earliestDoseStr string
			var nextAllowed string
			if err := tx.QueryRow(`
				SELECT given_at FROM prn_logs
				WHERE medication_id = ? AND voided_at IS NULL AND given_at >= ?
				ORDER BY given_at ASC LIMIT 1
			`, req.MedicationID, since24h).Scan(&earliestDoseStr); err == nil {
				if t, err := time.Parse(sqliteTimeLayout, earliestDoseStr); err == nil {
					nextAllowed = t.UTC().Add(24 * time.Hour).Format(time.RFC3339)
				}
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":           "PRN_LIMIT",
				"message":         "Maximum 24-hour PRN dose limit reached",
				"medication_id":   req.MedicationID,
				"count_last_24h":  count24h,
				"max_per_24h":     *prnMax24h,
				"last_given_at":   sqliteToRFC3339(lastGivenAt),
				"caregiver_name":  lastGiverName,
				"avatar_color":    lastAvatar,
				"next_allowed_at": nextAllowed,
			})
			return
		}
	}

	// Enforce minimum hours gap against the nearest dose in either direction, so a
	// late entry with an earlier given_at can't land next to an older dose (#7).
	var intervalOverridden bool
	var nearAt, nearName, nearAvatar string
	nearErr := tx.QueryRow(`
		SELECT l.given_at, c.name, COALESCE(c.avatar_color, '')
		FROM prn_logs l JOIN caregivers c ON l.caregiver_id = c.id
		WHERE l.medication_id = ? AND l.voided_at IS NULL
		ORDER BY ABS(julianday(l.given_at) - julianday(?)) ASC
		LIMIT 1
	`, req.MedicationID, givenAtStr).Scan(&nearAt, &nearName, &nearAvatar)
	if prnMinHours != nil && *prnMinHours > 0 && nearErr == nil {
		lastGivenAt, lastGiverName, lastAvatar := nearAt, nearName, nearAvatar
		if t, err := time.Parse(sqliteTimeLayout, lastGivenAt); err == nil {
			hoursSince := math.Abs(givenAt.Sub(t.UTC()).Hours())
			if hoursSince < *prnMinHours {
				if !req.OverrideInterval {
					nextAllowed := t.UTC().Add(time.Duration(*prnMinHours * float64(time.Hour))).Format(time.RFC3339)
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusConflict)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"error":           "PRN_TOO_SOON",
						"message":         "PRN medication given too recently",
						"medication_id":   req.MedicationID,
						"last_given_at":   sqliteToRFC3339(lastGivenAt),
						"caregiver_name":  lastGiverName,
						"avatar_color":    lastAvatar,
						"hours_since":     math.Round(hoursSince*100) / 100,
						"min_hours":       *prnMinHours,
						"next_allowed_at": nextAllowed,
					})
					return
				}
				intervalOverridden = true
			}
		}
	}

	// 3. Insert PRN log
	logID := "prn_" + uuid.New().String()[:8]
	_, err = tx.Exec(`
		INSERT INTO prn_logs (id, medication_id, caregiver_id, given_at, reason_tag, notes, dosage_snapshot, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, logID, req.MedicationID, caregiver.ID, givenAtStr, req.ReasonTag, req.Notes, dosage, now.Format(sqliteTimeLayout))
	if err != nil {
		internalError(w, "failed to insert PRN dose log", err)
		return
	}

	// 4. Update medicine stock if tracked
	stockCh, err := takeDoseFromStock(tx, req.MedicationID, logID, caregiver.ID, "taken", now)
	if err != nil {
		internalError(w, "failed to update medicine stock", err)
		return
	}

	// 5. Audit trail & alert recording
	details := fmt.Sprintf("PRN dose (%s) given for %s (%s, reason: %s)", dosage, nameAr, req.MedicationID, req.ReasonTag)
	if err := audit.Record(tx, caregiver.ID, "PRN_LOGGED", "prn_log", logID, details); err != nil {
		internalError(w, "failed to write audit log", err)
		return
	}

	var alertID string
	if isOverride {
		overrideType := "PRN 24h limit"
		if intervalOverridden && req.OverrideLimit {
			overrideType = "PRN 24h limit & minimum interval"
		} else if intervalOverridden {
			overrideType = "PRN minimum interval"
		}

		if alertID, err = recordAlert(tx, "dose_override", logID, caregiver.ID, map[string]interface{}{
			"medication_id": req.MedicationID,
			"name_ar":       nameAr,
			"name_en":       nameEn,
			"is_prn":        true,
			"reason":        req.OverrideReason,
			"override_type": overrideType,
		}); err != nil {
			internalError(w, "failed to record dose override alert", err)
			return
		}

		auditMsg := fmt.Sprintf("Overrode %s for %s (%s). Reason: %s", overrideType, nameAr, req.MedicationID, req.OverrideReason)
		if err := audit.Record(tx, caregiver.ID, "PRN_OVERRIDE", "prn_log", logID, auditMsg); err != nil {
			internalError(w, "failed to write audit log", err)
			return
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit PRN dose log", err)
		return
	}

	// 6. Broadcast SSE events to all connected sibling devices
	prnPayload := map[string]interface{}{
		"log_id":            logID,
		"medication_id":     req.MedicationID,
		"name_ar":           nameAr,
		"name_en":           nameEn,
		"dosage":            dosage,
		"reason_tag":        req.ReasonTag,
		"notes":             req.Notes,
		"given_at":          givenAt.Format(time.RFC3339),
		"caregiver_id":      caregiver.ID,
		"caregiver_name":    caregiver.Name,
		"avatar_color":      caregiver.AvatarColor,
		"is_override":       isOverride,
		"override_reason":   req.OverrideReason,
	}

	if alertID != "" {
		h.Broker.BroadcastAlert(alertID, "PRN_LOGGED", actorOf(caregiver), prnPayload)
		h.Push.NotifyAsync(alertID, "dose_override", caregiver.ID)
	} else {
		h.Broker.Broadcast("PRN_LOGGED", actorOf(caregiver), prnPayload)
	}
	h.broadcastStock(stockCh, caregiver)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "success",
		"id":             logID,
		"medication_id":  req.MedicationID,
		"given_at":       givenAt.Format(time.RFC3339),
		"caregiver_name": caregiver.Name,
		"avatar_color":   caregiver.AvatarColor,
		"reason_tag":     req.ReasonTag,
	})
}

type VoidPRNDoseRequest struct {
	Reason string `json:"reason"`
}

// VoidPRNDose voids a mis-tapped PRN dose, returning units to stock and restoring the 24h capacity.
func (h *MedicationHandler) VoidPRNDose(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		writeJSONError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Select your profile and enter your PIN")
		return
	}
	if !auth.CanTakeActions(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers are not permitted to void doses")
		return
	}

	logID := chi.URLParam(r, "id")
	if logID == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_ID", "log id is required")
		return
	}

	var req VoidPRNDoseRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	req.Reason = strings.TrimSpace(req.Reason)
	if len(req.Reason) > maxOverrideReasonLen {
		writeJSONError(w, http.StatusBadRequest, "REASON_TOO_LONG", "reason is too long")
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to start transaction", err)
		return
	}
	defer tx.Rollback()

	var medID, voidedAt, loggedBy, recordedAt sql.NullString
	err = tx.QueryRow(`SELECT medication_id, voided_at, caregiver_id, recorded_at FROM prn_logs WHERE id = ?`, logID).Scan(&medID, &voidedAt, &loggedBy, &recordedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "LOG_NOT_FOUND", "PRN dose log not found")
		return
	} else if err != nil {
		internalError(w, "failed to query PRN log", err)
		return
	}

	if voidedAt.Valid && voidedAt.String != "" {
		writeJSONError(w, http.StatusBadRequest, "ALREADY_VOIDED", "This PRN dose was already voided")
		return
	}

	now := time.Now().UTC()
	nowStr := now.Format(sqliteTimeLayout)

	// Cancelling frees a place in the 24h cap, so the rule of #8 applies: your own
	// entry within 10 minutes; otherwise only an admin, with a reason.
	tapped, terr := time.Parse(sqliteTimeLayout, recordedAt.String)
	ownUndo := recordedAt.Valid && terr == nil && loggedBy.String == caregiver.ID && now.Sub(tapped) <= undoWindow
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

	_, err = tx.Exec(`
		UPDATE prn_logs
		SET voided_at = ?, voided_by = ?, void_reason = ?
		WHERE id = ?
	`, nowStr, caregiver.ID, req.Reason, logID)
	if err != nil {
		internalError(w, "failed to void PRN dose", err)
		return
	}

	// Restore stock if tracked
	stockCh, err := returnDoseToStock(tx, medID.String, logID, caregiver.ID, now)
	if err != nil {
		internalError(w, "failed to restore stock", err)
		return
	}

	// Record audit trail
	auditDetails := fmt.Sprintf("Voided PRN dose %s for med %s (recorded by %s). Reason: %s", logID, medID.String, loggedBy.String, req.Reason)
	if err := audit.Record(tx, caregiver.ID, "PRN_VOIDED", "prn_log", logID, auditDetails); err != nil {
		internalError(w, "failed to write audit log", err)
		return
	}

	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit PRN void", err)
		return
	}

	h.Broker.Broadcast("PRN_VOIDED", actorOf(caregiver), map[string]interface{}{
		"log_id":        logID,
		"medication_id": medID.String,
		"voided_by":     caregiver.Name,
		"void_reason":   req.Reason,
		"voided_at":     now.Format(time.RFC3339),
	})
	h.broadcastStock(stockCh, caregiver)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "PRN dose voided successfully",
		"log_id":  logID,
	})
}
