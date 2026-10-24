package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/push"
	"birrcare/backend/internal/sse"
	"github.com/google/uuid"
)

// Bruises and bleeding signs. A patient on a strong blood thinner bruises
// easily, so the family records where a bruise is, how big, whether it is
// growing, and a photo to compare with later. Bleeding signs (gums, nose,
// urine, stool, vomit, a fall or knock to the head, a cut that won't stop)
// alert every sibling at once; the app never says what to do with the
// medicine, only to call the doctor.

// BruisePhotoURLPrefix is what bruise_logs.photo_url holds for an uploaded photo.
const BruisePhotoURLPrefix = "/api/v1/bruises/photos/"

var validBodyParts = map[string]bool{
	"arm_left": true, "arm_right": true, "hand_left": true, "hand_right": true,
	"leg_left": true, "leg_right": true, "belly": true, "chest": true,
	"back": true, "face_head": true, "other": true,
}

var validBruiseSizes = map[string]bool{"small": true, "medium": true, "large": true}
var validBruiseChanges = map[string]bool{"new": true, "same": true, "bigger": true, "smaller": true}

var validBleedingSigns = map[string]bool{
	"gums": true, "nose": true, "urine": true, "stool": true,
	"vomit": true, "head_knock": true, "cut_wont_stop": true,
}

type BruiseHandler struct {
	DB       *sql.DB
	Broker   *sse.Broker
	Push     *push.Notifier
	PhotoDir string // medicine photo folder; bruise photos go in its "bruises" subfolder
}

func (h *BruiseHandler) dir() string { return filepath.Join(h.PhotoDir, "bruises") }

type BruiseLog struct {
	ID            string   `json:"id"`
	CaregiverID   string   `json:"caregiver_id"`
	CaregiverName string   `json:"caregiver_name"`
	AvatarColor   string   `json:"avatar_color,omitempty"`
	CreatedAt     string   `json:"created_at"` // RFC3339
	BodyPart      string   `json:"body_part,omitempty"`
	Size          string   `json:"size,omitempty"`
	Change        string   `json:"change,omitempty"`
	BleedingSigns []string `json:"bleeding_signs"`
	PhotoURL      string   `json:"photo_url,omitempty"`
	Notes         string   `json:"notes,omitempty"`
}

type CreateBruiseRequest struct {
	BodyPart        string   `json:"body_part"`
	Size            string   `json:"size"`
	Change          string   `json:"change"`
	BleedingSigns   []string `json:"bleeding_signs"`
	PhotoURL        string   `json:"photo_url"`
	Notes           string   `json:"notes"`
	ClientRequestID string   `json:"client_request_id,omitempty"`
}

// UploadPhoto: POST /bruises/photos with the image as the body. Anyone who
// may record care can add one.
func (h *BruiseHandler) UploadPhoto(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if me == nil {
		writeJSONError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "view-only accounts cannot add photos")
		return
	}
	p, ok := savePhoto(w, r, h.dir())
	if !ok {
		return
	}
	audit.Log(h.DB, me.ID, "UPLOAD_BRUISE_PHOTO", "bruise_photo", p.Name, p.describe())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"photo_url": BruisePhotoURLPrefix + p.Name})
}

// ServePhoto: GET /bruises/photos/{name}, signed-in family only.
func (h *BruiseHandler) ServePhoto(w http.ResponseWriter, r *http.Request) {
	servePhoto(w, r, h.dir())
}

const bruiseSelect = `
	SELECT b.id, b.caregiver_id, c.name, COALESCE(c.avatar_color, ''), b.created_at,
	       COALESCE(b.body_part, ''), COALESCE(b.size, ''), COALESCE(b.change, ''),
	       b.bleeding_signs, COALESCE(b.photo_url, ''), COALESCE(b.notes, '')
	FROM bruise_logs b JOIN caregivers c ON c.id = b.caregiver_id`

func scanBruise(sc interface{ Scan(...any) error }) (BruiseLog, error) {
	var b BruiseLog
	var created, signs string
	if err := sc.Scan(&b.ID, &b.CaregiverID, &b.CaregiverName, &b.AvatarColor, &created,
		&b.BodyPart, &b.Size, &b.Change, &signs, &b.PhotoURL, &b.Notes); err != nil {
		return b, err
	}
	if t, err := time.Parse(sqliteTimeLayout, created); err == nil {
		b.CreatedAt = t.UTC().Format(time.RFC3339)
	} else {
		b.CreatedAt = created
	}
	_ = json.Unmarshal([]byte(signs), &b.BleedingSigns)
	if b.BleedingSigns == nil {
		b.BleedingSigns = []string{}
	}
	return b, nil
}

// List: GET /bruises?days=30 (1–365), newest first.
func (h *BruiseHandler) List(w http.ResponseWriter, r *http.Request) {
	days := 30
	if d, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && d >= 1 && d <= 365 {
		days = d
	}
	since := time.Now().UTC().AddDate(0, 0, -days).Format(sqliteTimeLayout)
	rows, err := h.DB.Query(bruiseSelect+`
		WHERE datetime(b.created_at) >= datetime(?)
		ORDER BY b.created_at DESC
		LIMIT 200`, since)
	if err != nil {
		internalError(w, "failed to load bruises", err)
		return
	}
	list := []BruiseLog{}
	for rows.Next() {
		b, err := scanBruise(rows)
		if err != nil {
			rows.Close()
			internalError(w, "failed to read bruises", err)
			return
		}
		list = append(list, b)
	}
	rows.Close()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (h *BruiseHandler) replyIfDuplicate(w http.ResponseWriter, requestID string) bool {
	if requestID == "" {
		return false
	}
	b, err := scanBruise(h.DB.QueryRow(bruiseSelect+` WHERE b.client_request_id = ?`, requestID))
	if err != nil {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(b)
	return true
}

// Create: POST /bruises. A bruise needs a body part; bleeding signs alone are
// allowed (e.g. a nosebleed). Any bleeding sign alerts everyone.
func (h *BruiseHandler) Create(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if me == nil {
		writeJSONError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "view-only accounts cannot record bruises")
		return
	}
	var req CreateBruiseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	req.Notes = strings.TrimSpace(req.Notes)
	if req.BodyPart != "" && !validBodyParts[req.BodyPart] {
		writeJSONError(w, http.StatusBadRequest, "INVALID_BODY_PART", "unknown body part")
		return
	}
	if req.Size != "" && !validBruiseSizes[req.Size] {
		writeJSONError(w, http.StatusBadRequest, "INVALID_SIZE", "size must be small, medium or large")
		return
	}
	if req.Change != "" && !validBruiseChanges[req.Change] {
		writeJSONError(w, http.StatusBadRequest, "INVALID_CHANGE", "change must be new, same, bigger or smaller")
		return
	}
	seen := map[string]bool{}
	signs := []string{}
	for _, s := range req.BleedingSigns {
		if !validBleedingSigns[s] {
			writeJSONError(w, http.StatusBadRequest, "INVALID_BLEEDING_SIGN", fmt.Sprintf("unknown bleeding sign %q", s))
			return
		}
		if !seen[s] {
			seen[s] = true
			signs = append(signs, s)
		}
	}
	if req.BodyPart == "" && len(signs) == 0 {
		writeJSONError(w, http.StatusBadRequest, "EMPTY_REPORT", "choose where the bruise is, or a bleeding sign")
		return
	}
	if req.PhotoURL != "" {
		name := strings.TrimPrefix(req.PhotoURL, BruisePhotoURLPrefix)
		if name == req.PhotoURL || !photoNameRe.MatchString(name) {
			writeJSONError(w, http.StatusBadRequest, "INVALID_PHOTO", "photo must be one uploaded to /bruises/photos")
			return
		}
	}
	if len(req.Notes) > 2000 {
		writeJSONError(w, http.StatusBadRequest, "NOTES_TOO_LONG", "notes are too long")
		return
	}
	if len(req.ClientRequestID) > 64 {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST_ID", "client_request_id is too long")
		return
	}
	if h.replyIfDuplicate(w, req.ClientRequestID) {
		return
	}

	id := uuid.New().String()
	signsJSON, _ := json.Marshal(signs)
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to begin transaction", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		INSERT INTO bruise_logs (id, caregiver_id, created_at, body_part, size, change, bleeding_signs, photo_url, notes, client_request_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, me.ID, time.Now().UTC().Format(sqliteTimeLayout), nullIfEmpty(req.BodyPart), nullIfEmpty(req.Size), nullIfEmpty(req.Change),
		string(signsJSON), nullIfEmpty(req.PhotoURL), nullIfEmpty(req.Notes), nullIfEmpty(req.ClientRequestID)); err != nil {
		if isUniqueViolation(err) && h.replyIfDuplicate(w, req.ClientRequestID) {
			return
		}
		internalError(w, "failed to record bruise", err)
		return
	}
	alertID := ""
	if len(signs) > 0 {
		alertID, err = recordAlert(tx, "bleeding", id, me.ID, map[string]interface{}{
			"bleeding_signs": signs, "body_part": req.BodyPart, "notes": req.Notes,
		})
		if err != nil {
			internalError(w, "failed to record alert", err)
			return
		}
	}
	if err := audit.Record(tx, me.ID, "LOG_BRUISE", "bruise_log", id,
		fmt.Sprintf("part=%s size=%s change=%s bleeding=%v photo=%t: %s", req.BodyPart, req.Size, req.Change, signs, req.PhotoURL != "", req.Notes)); err != nil {
		internalError(w, "failed to write audit trail", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to commit", err)
		return
	}

	saved, err := scanBruise(h.DB.QueryRow(bruiseSelect+` WHERE b.id = ?`, id))
	if err != nil {
		internalError(w, "failed to read the saved bruise", err)
		return
	}
	payload := map[string]interface{}{"bruise": saved}
	if alertID != "" {
		payload["alert_id"] = alertID
		h.Broker.BroadcastAlert(alertID, "BRUISE_LOGGED", actorOf(me), payload)
		h.Push.NotifyAsync(alertID, "bleeding", me.ID)
	} else {
		h.Broker.Broadcast("BRUISE_LOGGED", actorOf(me), payload)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(saved)
}
