package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"
	"github.com/google/uuid"
)

type HandoverHandler struct {
	DB     *sql.DB
	Broker *sse.Broker
}

func (h *HandoverHandler) ListHandovers(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
		limit = l
	}

	rows, err := h.DB.Query(`
		SELECT n.id, n.caregiver_id, n.created_at, n.shift_type, n.summary, n.urgent_flag,
		       c.name, c.avatar_color
		FROM handover_notes n
		JOIN caregivers c ON n.caregiver_id = c.id
		ORDER BY n.created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		http.Error(w, `{"error":"failed to fetch handovers"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var list []models.HandoverNote
	for rows.Next() {
		var n models.HandoverNote
		var shiftType sql.NullString
		var createdAtStr string
		var urgentInt int

		if err := rows.Scan(&n.ID, &n.CaregiverID, &createdAtStr, &shiftType, &n.Summary, &urgentInt, &n.CaregiverName, &n.AvatarColor); err != nil {
			continue
		}
		if shiftType.Valid {
			n.ShiftType = shiftType.String
		}
		n.UrgentFlag = urgentInt == 1
		n.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
		list = append(list, n)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

type CreateHandoverRequest struct {
	ShiftType  string `json:"shift_type"` // 'morning', 'evening', 'night'
	Summary    string `json:"summary"`
	UrgentFlag bool   `json:"urgent_flag"`
}

func (h *HandoverHandler) CreateHandover(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	if !auth.CanTakeActions(caregiver) {
		http.Error(w, `{"error":"FORBIDDEN","message":"Viewers are not permitted to create handover notes"}`, http.StatusForbidden)
		return
	}

	var req CreateHandoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Summary == "" {
		http.Error(w, `{"error":"invalid handover note payload"}`, http.StatusBadRequest)
		return
	}

	validShifts := map[string]bool{"morning": true, "evening": true, "night": true}
	if !validShifts[req.ShiftType] {
		req.ShiftType = "morning"
	}

	id := uuid.New().String()
	urgentVal := 0
	if req.UrgentFlag {
		urgentVal = 1
	}
	now := time.Now().UTC()

	_, err := h.DB.Exec(`
		INSERT INTO handover_notes (id, caregiver_id, created_at, shift_type, summary, urgent_flag)
		VALUES (?, ?, datetime('now'), ?, ?, ?)
	`, id, caregiver.ID, req.ShiftType, req.Summary, urgentVal)

	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"failed to create handover: %v"}`, err), http.StatusInternalServerError)
		return
	}

	audit.Log(h.DB, caregiver.ID, "NOTE_ADDED", "handover_note", id,
		fmt.Sprintf("Handover (%s, urgent=%t): %s", req.ShiftType, req.UrgentFlag, req.Summary))

	// Broadcast SSE event
	h.Broker.Broadcast("NOTE_ADDED", models.ActorInfo{
		ID:          caregiver.ID,
		Name:        caregiver.Name,
		AvatarColor: caregiver.AvatarColor,
	}, map[string]interface{}{
		"id":             id,
		"created_at":     now.Format(time.RFC3339),
		"shift_type":     req.ShiftType,
		"summary":        req.Summary,
		"urgent_flag":    req.UrgentFlag,
		"caregiver_name": caregiver.Name,
		"avatar_color":   caregiver.AvatarColor,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":         id,
		"created_at": now.Format(time.RFC3339),
	})
}
