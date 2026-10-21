package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
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

	rows, err := h.DB.Query(handoverNoteSelect+`
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
		n, err := scanHandoverNote(rows)
		if err != nil {
			continue
		}
		list = append(list, n)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

type CreateHandoverRequest struct {
	ShiftType  string `json:"shift_type"` // 'morning', 'evening', 'night' or a shift id (e.g. "shift_4h_1")
	Summary    string `json:"summary"`
	UrgentFlag bool   `json:"urgent_flag"`
	// Quick-tap tags (issue #18); with tags, the summary may be empty.
	Tags []string `json:"tags,omitempty"`
	// Set by the offline outbox (issue #16): resending it returns the first note.
	ClientRequestID string `json:"client_request_id,omitempty"`
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid handover note payload"}`, http.StatusBadRequest)
		return
	}
	req.Summary = strings.TrimSpace(req.Summary)
	tags, tagUrgent, err := validateTags(req.Tags)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "UNKNOWN_TAG", err.Error())
		return
	}
	if req.Summary == "" && len(tags) == 0 {
		writeJSONError(w, http.StatusBadRequest, "EMPTY_NOTE", "tap at least one tag or write a note")
		return
	}
	// Swallowing difficulty and pain are always urgent.
	req.UrgentFlag = req.UrgentFlag || tagUrgent

	req.ShiftType = strings.TrimSpace(req.ShiftType)
	if req.ShiftType == "" {
		req.ShiftType = "morning"
	} else if len(req.ShiftType) > 64 {
		writeJSONError(w, http.StatusBadRequest, "INVALID_SHIFT", "shift_type is too long")
		return
	}
	if len(req.Summary) > 10000 {
		writeJSONError(w, http.StatusBadRequest, "SUMMARY_TOO_LONG", "summary is too long")
		return
	}

	if len(req.ClientRequestID) > 64 {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST_ID", "client_request_id is too long")
		return
	}
	if h.replyIfDuplicateNote(w, req.ClientRequestID) {
		return
	}

	id := uuid.New().String()
	urgentVal := 0
	if req.UrgentFlag {
		urgentVal = 1
	}
	now := time.Now().UTC()

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to begin transaction", err)
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO handover_notes (id, caregiver_id, created_at, shift_type, summary, urgent_flag, client_request_id, tags)
		VALUES (?, ?, datetime('now'), ?, ?, ?, ?, ?)
	`, id, caregiver.ID, req.ShiftType, req.Summary, urgentVal, nullIfEmpty(req.ClientRequestID), tagsJSON(tags))

	if err != nil {
		if isUniqueViolation(err) && h.replyIfDuplicateNote(w, req.ClientRequestID) {
			return
		}
		internalError(w, "failed to create handover", err)
		return
	}

	auditDetail := fmt.Sprintf("Handover (%s, urgent=%t, tags=%v): %s", req.ShiftType, req.UrgentFlag, tags, req.Summary)
	if err := audit.Record(tx, caregiver.ID, "NOTE_ADDED", "handover_note", id, auditDetail); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}

	if err := tx.Commit(); err != nil {
		internalError(w, "failed to create handover", err)
		return
	}

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
		"tags":           tags,
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

// replyIfDuplicateNote answers 200 with the first note for a repeated client_request_id.
func (h *HandoverHandler) replyIfDuplicateNote(w http.ResponseWriter, clientRequestID string) bool {
	if clientRequestID == "" {
		return false
	}
	var id, createdAt string
	if err := h.DB.QueryRow(`SELECT id, created_at FROM handover_notes WHERE client_request_id = ?`, clientRequestID).Scan(&id, &createdAt); err != nil {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"id": id, "created_at": sqliteToRFC3339(createdAt), "duplicate": true})
	return true
}
