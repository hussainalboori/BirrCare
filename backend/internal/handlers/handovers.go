package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"
	"github.com/go-chi/chi/v5"
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

	// ?date=YYYY-MM-DD: every note of that household day (the Report page lists
	// all of a day's reports, not only the latest).
	where, args := "", []interface{}{}
	if d := strings.TrimSpace(r.URL.Query().Get("date")); d != "" {
		_, start, ok := parseClinicalDate(d)
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "INVALID_DATE", "date must be YYYY-MM-DD")
			return
		}
		where = "WHERE datetime(n.created_at) >= datetime(?) AND datetime(n.created_at) < datetime(?)"
		args = append(args, start.UTC().Format(sqliteTimeLayout), start.AddDate(0, 0, 1).UTC().Format(sqliteTimeLayout))
		limit = 100
	}
	rows, err := h.DB.Query(handoverNoteSelect+`
		`+where+`
		ORDER BY n.created_at DESC
		LIMIT ?
	`, append(args, limit)...)
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

type UpdateHandoverRequest struct {
	Summary    string   `json:"summary"`
	UrgentFlag bool     `json:"urgent_flag"`
	Tags       []string `json:"tags,omitempty"`
}

func (h *HandoverHandler) UpdateHandover(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		writeJSONError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "unauthenticated")
		return
	}
	if !auth.CanTakeActions(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers are not permitted to edit handover notes")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_ID", "missing handover note id")
		return
	}

	var req UpdateHandoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid handover note payload")
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
	req.UrgentFlag = req.UrgentFlag || tagUrgent
	if len(req.Summary) > 10000 {
		writeJSONError(w, http.StatusBadRequest, "SUMMARY_TOO_LONG", "summary is too long")
		return
	}

	var existingAuthor, shiftType string
	err = h.DB.QueryRowContext(r.Context(), `SELECT caregiver_id, shift_type FROM handover_notes WHERE id = ?`, id).Scan(&existingAuthor, &shiftType)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "NOTE_NOT_FOUND", "handover note not found")
		return
	} else if err != nil {
		internalError(w, "failed to check handover note", err)
		return
	}

	// Only original author or admin/super_admin can edit
	if existingAuthor != caregiver.ID && !auth.CanManageMedications(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "You can only edit your own notes, or ask an admin")
		return
	}

	urgentVal := 0
	if req.UrgentFlag {
		urgentVal = 1
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to begin transaction", err)
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		UPDATE handover_notes
		SET summary = ?, urgent_flag = ?, tags = ?
		WHERE id = ?
	`, req.Summary, urgentVal, tagsJSON(tags), id)
	if err != nil {
		internalError(w, "failed to update handover", err)
		return
	}

	auditDetail := fmt.Sprintf("Updated handover note %s (%s, urgent=%t, tags=%v): %s", id, shiftType, req.UrgentFlag, tags, req.Summary)
	if err := audit.Record(tx, caregiver.ID, "NOTE_UPDATED", "handover_note", id, auditDetail); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}

	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit update", err)
		return
	}

	h.Broker.Broadcast("NOTE_UPDATED", models.ActorInfo{
		ID:          caregiver.ID,
		Name:        caregiver.Name,
		AvatarColor: caregiver.AvatarColor,
	}, map[string]interface{}{
		"id":          id,
		"summary":     req.Summary,
		"urgent_flag": req.UrgentFlag,
		"tags":        tags,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "Handover note updated successfully",
		"id":      id,
	})
}

func (h *HandoverHandler) DeleteHandover(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		writeJSONError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "unauthenticated")
		return
	}
	if !auth.CanTakeActions(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers are not permitted to delete handover notes")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "MISSING_ID", "missing handover note id")
		return
	}

	var existingAuthor, shiftType, summary string
	var urgentVal int
	err := h.DB.QueryRowContext(r.Context(), `SELECT caregiver_id, shift_type, summary, urgent_flag FROM handover_notes WHERE id = ?`, id).Scan(&existingAuthor, &shiftType, &summary, &urgentVal)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "NOTE_NOT_FOUND", "handover note not found")
		return
	} else if err != nil {
		internalError(w, "failed to check handover note", err)
		return
	}

	// Only original author or admin/super_admin can delete
	if existingAuthor != caregiver.ID && !auth.CanManageMedications(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "You can only delete your own notes, or ask an admin")
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to begin transaction", err)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM duty_handovers WHERE note_id = ?", id); err != nil {
		internalError(w, "failed to delete attached duty handover", err)
		return
	}

	if _, err := tx.Exec("DELETE FROM handover_notes WHERE id = ?", id); err != nil {
		internalError(w, "failed to delete handover note", err)
		return
	}

	auditDetail := fmt.Sprintf("Deleted handover note %s by %s (%s, urgent=%t): %s", id, existingAuthor, shiftType, urgentVal == 1, summary)
	if err := audit.Record(tx, caregiver.ID, "NOTE_DELETED", "handover_note", id, auditDetail); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}

	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit deletion", err)
		return
	}

	h.Broker.Broadcast("NOTE_DELETED", models.ActorInfo{
		ID:          caregiver.ID,
		Name:        caregiver.Name,
		AvatarColor: caregiver.AvatarColor,
	}, map[string]interface{}{
		"id": id,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "Handover note deleted successfully",
		"id":      id,
	})
}
