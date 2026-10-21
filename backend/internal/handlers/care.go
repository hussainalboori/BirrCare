package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/push"
	"birrcare/backend/internal/sse"
	"github.com/google/uuid"
)

type CareHandler struct {
	DB     *sql.DB
	Broker *sse.Broker
	Push   *push.Notifier
}

type LogCareEventRequest struct {
	Kind            string  `json:"kind"`
	Value           string  `json:"value"`
	SkinCondition   string  `json:"skin_condition,omitempty"`
	Notes           string  `json:"notes,omitempty"`
	ClientRequestID *string `json:"client_request_id,omitempty"`
}

var validCareKinds = map[string]bool{
	"reposition": true,
	"fluid":      true,
	"bowel":      true,
	"hygiene":    true,
	"physio":     true,
}

var validSkinConditions = map[string]bool{
	"":         true,
	"ok":       true,
	"red_area": true,
}

func parseDBTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse(sqliteTimeLayout, s)
}

func valueLabelAr(val string) string {
	switch val {
	case "left":
		return "الجانب الأيسر"
	case "right":
		return "الجانب الأيمن"
	case "back":
		return "الظهر"
	default:
		return val
	}
}

// GetStatus returns the real-time status of repositioning (and care tasks).
func (h *CareHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "reposition"
	}
	if !validCareKinds[kind] {
		writeJSONError(w, http.StatusBadRequest, "INVALID_KIND", "invalid care kind")
		return
	}

	status, err := FetchCareStatus(r.Context(), h.DB, kind)
	if err != nil {
		internalError(w, "failed fetching care status", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func FetchCareStatus(ctx context.Context, db *sql.DB, kind string) (models.CareStatus, error) {
	intervalMinutes := currentRepositionInterval()

	status := models.CareStatus{
		Kind:              kind,
		IntervalMinutes:   intervalMinutes,
		IsOverdueWarning:  false,
		IsOverdueCritical: false,
	}

	var id, value, caregiverID, caregiverName, avatarColor, createdAtStr string
	var skinCond, notes, clientReqID sql.NullString

	err := db.QueryRowContext(ctx, `
		SELECT 
			e.id, e.value, e.caregiver_id, c.name, c.avatar_color, 
			COALESCE(e.skin_condition, ''), COALESCE(e.notes, ''), e.created_at, e.client_request_id
		FROM care_events e
		JOIN caregivers c ON e.caregiver_id = c.id
		WHERE e.kind = ?
		ORDER BY e.created_at DESC
		LIMIT 1
	`, kind).Scan(&id, &value, &caregiverID, &caregiverName, &avatarColor, &skinCond, &notes, &createdAtStr, &clientReqID)

	now := time.Now().UTC()

	// Find active on-duty caregiver
	if shift, targetDate, err := findCurrentShift(db, now); err == nil && shift != nil {
		var cg models.Caregiver
		var emailStr, createdAtCg sql.NullString
		if err := db.QueryRowContext(ctx, `
			SELECT c.id, c.name, c.email, c.role, c.avatar_color, c.preferred_lang, c.is_active, c.created_at
			FROM shift_assignments a
			JOIN caregivers c ON a.caregiver_id = c.id
			WHERE a.shift_id = ? AND a.date = ?
		`, shift.ID, targetDate).Scan(&cg.ID, &cg.Name, &emailStr, &cg.Role, &cg.AvatarColor, &cg.PreferredLang, &cg.IsActive, &createdAtCg); err == nil {
			if emailStr.Valid {
				cg.Email = emailStr.String
			}
			status.OnDutyCaregiver = &cg
		}
	}

	if err == nil {
		status.LastAt = &createdAtStr
		status.LastValue = &value
		status.LastCaregiverID = &caregiverID
		status.LastCaregiverName = &caregiverName
		status.LastAvatarColor = &avatarColor
		if skinCond.Valid && skinCond.String != "" {
			sc := skinCond.String
			status.LastSkinCondition = &sc
		}

		lastTime, parseErr := parseDBTime(createdAtStr)
		if parseErr == nil {
			minutesSince := int(now.Sub(lastTime.UTC()).Minutes())
			if minutesSince < 0 {
				minutesSince = 0
			}
			status.MinutesSince = &minutesSince

			nextDue := lastTime.UTC().Add(time.Duration(intervalMinutes) * time.Minute)
			nextDueStr := nextDue.Format(time.RFC3339)
			status.NextDueAt = &nextDueStr

			minutesUntilDue := int(nextDue.Sub(now).Minutes())
			status.MinutesUntilDue = &minutesUntilDue

			// Overdue by 15 min: warning
			if minutesSince >= intervalMinutes+15 {
				status.IsOverdueWarning = true
			}
			// Overdue by 45 min: critical
			if minutesSince >= intervalMinutes+45 {
				status.IsOverdueCritical = true
			}
		}
	} else if err != sql.ErrNoRows {
		return status, err
	}

	return status, nil
}

// LogEvent logs a care task (repositioning, fluids, etc.).
func (h *CareHandler) LogEvent(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "viewers cannot log care tasks")
		return
	}

	var req LogCareEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request body")
		return
	}
	req.Kind = strings.TrimSpace(req.Kind)
	req.Value = strings.TrimSpace(req.Value)
	req.SkinCondition = strings.TrimSpace(req.SkinCondition)
	req.Notes = strings.TrimSpace(req.Notes)

	if !validCareKinds[req.Kind] {
		writeJSONError(w, http.StatusBadRequest, "INVALID_KIND", "kind must be reposition, fluid, bowel, hygiene, or physio")
		return
	}
	if req.Value == "" {
		writeJSONError(w, http.StatusBadRequest, "VALUE_REQUIRED", "value is required")
		return
	}
	if !validSkinConditions[req.SkinCondition] {
		writeJSONError(w, http.StatusBadRequest, "INVALID_SKIN_CONDITION", "skin_condition must be ok, red_area, or empty")
		return
	}

	// Idempotency check if client_request_id provided
	if req.ClientRequestID != nil && *req.ClientRequestID != "" {
		var existingID, existingValue, existingCreatedAt string
		if err := h.DB.QueryRowContext(r.Context(), `
			SELECT id, value, created_at FROM care_events WHERE client_request_id = ?
		`, *req.ClientRequestID).Scan(&existingID, &existingValue, &existingCreatedAt); err == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(models.CareEvent{
				ID:              existingID,
				Kind:            req.Kind,
				Value:           existingValue,
				CaregiverID:     me.ID,
				CaregiverName:   me.Name,
				AvatarColor:     me.AvatarColor,
				SkinCondition:   req.SkinCondition,
				Notes:           req.Notes,
				CreatedAt:       existingCreatedAt,
				ClientRequestID: req.ClientRequestID,
			})
			return
		}
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to start transaction", err)
		return
	}
	defer tx.Rollback()

	eventID := uuid.New().String()
	nowUTC := time.Now().UTC().Format(time.RFC3339)

	var clientReq sql.NullString
	if req.ClientRequestID != nil && *req.ClientRequestID != "" {
		clientReq = sql.NullString{String: *req.ClientRequestID, Valid: true}
	}

	_, err = tx.ExecContext(r.Context(), `
		INSERT INTO care_events (id, kind, value, caregiver_id, skin_condition, notes, created_at, client_request_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, eventID, req.Kind, req.Value, me.ID, req.SkinCondition, req.Notes, nowUTC, clientReq)
	if err != nil {
		internalError(w, "failed inserting care event", err)
		return
	}

	// Optional skin-check: "Red area" sets urgent_flag on the next handover
	var handoverID, summaryArNote string
	if req.SkinCondition == "red_area" {
		handoverID = uuid.New().String()
		summaryAr := fmt.Sprintf("تنبيه احمرار الجلد: لوحظت منطقة حمراء أثناء التقليب إلى (%s)", valueLabelAr(req.Value))
		summaryArNote = summaryAr
		// handover_notes uses the SQLite "YYYY-MM-DD HH:MM:SS" format like every
		// other note; an RFC3339 value showed as year 0001 in the shift notes.
		_, err = tx.ExecContext(r.Context(), `
			INSERT INTO handover_notes (id, caregiver_id, summary, urgent_flag, created_at)
			VALUES (?, ?, ?, 1, ?)
		`, handoverID, me.ID, summaryAr, time.Now().UTC().Format(sqliteTimeLayout))
		if err != nil {
			internalError(w, "failed inserting urgent handover note", err)
			return
		}
	}

	if err := audit.Record(tx, me.ID, "LOG_CARE_EVENT", "care_events", eventID,
		fmt.Sprintf("kind: %s, value: %s, skin: %s", req.Kind, req.Value, req.SkinCondition)); err != nil {
		internalError(w, "failed to write audit log", err)
		return
	}

	if err := tx.Commit(); err != nil {
		internalError(w, "failed committing care event", err)
		return
	}

	res := models.CareEvent{
		ID:              eventID,
		Kind:            req.Kind,
		Value:           req.Value,
		CaregiverID:     me.ID,
		CaregiverName:   me.Name,
		AvatarColor:     me.AvatarColor,
		SkinCondition:   req.SkinCondition,
		Notes:           req.Notes,
		CreatedAt:       nowUTC,
		ClientRequestID: req.ClientRequestID,
	}

	// Broadcast SSE to all connected siblings
	if h.Broker != nil && handoverID != "" {
		h.Broker.Broadcast("NOTE_ADDED", models.ActorInfo{ID: me.ID, Name: me.Name, AvatarColor: me.AvatarColor}, map[string]interface{}{
			"id": handoverID, "summary": summaryArNote, "urgent_flag": true,
			"caregiver_name": me.Name, "avatar_color": me.AvatarColor,
		})
	}
	if h.Broker != nil {
		h.Broker.Broadcast("CARE_EVENT_LOGGED", models.ActorInfo{
			ID:          me.ID,
			Name:        me.Name,
			AvatarColor: me.AvatarColor,
		}, res)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(res)
}

// ListEvents returns recent care events.
func (h *CareHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	query := `
		SELECT 
			e.id, e.kind, e.value, e.caregiver_id, c.name, c.avatar_color, 
			COALESCE(e.skin_condition, ''), COALESCE(e.notes, ''), e.created_at, e.client_request_id
		FROM care_events e
		JOIN caregivers c ON e.caregiver_id = c.id
	`
	var args []interface{}
	if kind != "" {
		query += ` WHERE e.kind = ?`
		args = append(args, kind)
	}
	query += ` ORDER BY e.created_at DESC LIMIT 50`

	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		internalError(w, "failed querying care events", err)
		return
	}
	defer rows.Close()

	var events []models.CareEvent
	for rows.Next() {
		var e models.CareEvent
		var skinCond, notes, clientReq sql.NullString
		if err := rows.Scan(&e.ID, &e.Kind, &e.Value, &e.CaregiverID, &e.CaregiverName, &e.AvatarColor, &skinCond, &notes, &e.CreatedAt, &clientReq); err == nil {
			if skinCond.Valid {
				e.SkinCondition = skinCond.String
			}
			if notes.Valid {
				e.Notes = notes.String
			}
			if clientReq.Valid {
				s := clientReq.String
				e.ClientRequestID = &s
			}
			events = append(events, e)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(events)
}

// CheckOverdue runs the overdue checker and returns the result.
func (h *CareHandler) CheckOverdue(w http.ResponseWriter, r *http.Request) {
	err := CheckRepositionOverdue(r.Context(), h.DB, h.Push, h.Broker, time.Now())
	if err != nil {
		internalError(w, "failed checking overdue repositioning", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "checked"})
}

// CheckRepositionOverdue checks if repositioning is overdue by >= 45 min and alerts only the on-duty caregiver.
func CheckRepositionOverdue(ctx context.Context, db *sql.DB, notifier *push.Notifier, broker *sse.Broker, now time.Time) error {
	intervalMinutes := currentRepositionInterval()
	criticalMinutes := intervalMinutes + 45

	// 1. Get last reposition event
	var lastEventID, lastCreatedAtStr, lastValue string
	err := db.QueryRowContext(ctx, `
		SELECT id, value, created_at
		FROM care_events
		WHERE kind = 'reposition'
		ORDER BY created_at DESC
		LIMIT 1
	`).Scan(&lastEventID, &lastValue, &lastCreatedAtStr)
	if err == sql.ErrNoRows {
		// Nothing logged yet, no overdue alert
		return nil
	}
	if err != nil {
		return err
	}

	lastTime, err := parseDBTime(lastCreatedAtStr)
	if err != nil {
		return err
	}

	minutesSince := int(now.UTC().Sub(lastTime.UTC()).Minutes())
	if minutesSince < criticalMinutes {
		return nil
	}

	// 2. Check if an alert for reposition_overdue has already been created for this event
	var existingAlertID string
	err = db.QueryRowContext(ctx, `
		SELECT id FROM alerts
		WHERE kind = 'reposition_overdue' AND ref_id = ?
		LIMIT 1
	`, lastEventID).Scan(&existingAlertID)
	if err == nil {
		// Already alerted for this overdue cycle
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	// 3. Find the caregiver on duty right now
	shift, targetDate, err := findCurrentShift(db, now)
	if err != nil || shift == nil {
		return nil
	}

	var onDutyCaregiverID, onDutyCaregiverName string
	err = db.QueryRowContext(ctx, `
		SELECT c.id, c.name
		FROM shift_assignments a
		JOIN caregivers c ON a.caregiver_id = c.id
		WHERE a.shift_id = ? AND a.date = ?
	`, shift.ID, targetDate).Scan(&onDutyCaregiverID, &onDutyCaregiverName)
	if err != nil {
		// No assigned caregiver on duty right now
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	payload := map[string]interface{}{
		"care_event_id":    lastEventID,
		"last_value":       lastValue,
		"last_at":          lastCreatedAtStr,
		"minutes_since":    minutesSince,
		"interval_minutes": intervalMinutes,
		"on_duty_name":     onDutyCaregiverName,
	}

	// Record alert targeted ONLY to on-duty caregiver
	alertID, err := recordAlertFor(tx, "reposition_overdue", lastEventID, "", onDutyCaregiverID, payload)
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Send push ONLY to the on-duty caregiver's push subscription
	if notifier != nil {
		notifier.NotifyCaregiverAsync(alertID, "reposition_overdue", onDutyCaregiverID)
	}

	if broker != nil {
		broker.Broadcast("CARE_OVERDUE", models.ActorInfo{
			ID:   onDutyCaregiverID,
			Name: onDutyCaregiverName,
		}, map[string]interface{}{
			"alert_id":              alertID,
			"target_caregiver_id":   onDutyCaregiverID,
			"target_caregiver_name": onDutyCaregiverName,
			"minutes_since":         minutesSince,
		})
	}

	return nil
}

// StartRepositionWatcher periodically checks for overdue repositioning every minute.
func StartRepositionWatcher(ctx context.Context, db *sql.DB, notifier *push.Notifier, broker *sse.Broker) {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				if err := CheckRepositionOverdue(ctx, db, notifier, broker, now); err != nil {
					log.Printf("[CARE] overdue check failed: %v", err)
				}
			}
		}
	}()
}
