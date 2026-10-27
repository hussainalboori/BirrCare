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
	"github.com/google/uuid"
)

// Falls and seizures (issue #60), saved as care events with the answers of the
// guided check, and an alert to every sibling: red ("escalate") for a fall with a
// head knock, not awake and talking normally, or new pain / a limb that won't move
// / bleeding; and for a seizure of 5 minutes or more, or a second one within a day.

const seizureEmergencySeconds = 300

type IncidentRequest struct {
	Kind            string `json:"kind"` // fall | seizure
	HitHead         *bool  `json:"hit_head,omitempty"`
	AwakeNormal     *bool  `json:"awake_normal,omitempty"`
	PainOrBleeding  *bool  `json:"pain_or_bleeding,omitempty"`
	DurationSeconds *int   `json:"duration_seconds,omitempty"`
	Notes           string `json:"notes,omitempty"`
	ClientRequestID string `json:"client_request_id,omitempty"`
}

// Incident is a fall or seizure as stored in care_events.value.
type Incident struct {
	ID              string   `json:"id"`
	Kind            string   `json:"kind"`
	At              string   `json:"at"` // RFC3339
	HitHead         *bool    `json:"hit_head,omitempty"`
	AwakeNormal     *bool    `json:"awake_normal,omitempty"`
	PainOrBleeding  *bool    `json:"pain_or_bleeding,omitempty"`
	DurationSeconds *int     `json:"duration_seconds,omitempty"`
	Repeat          bool     `json:"repeat,omitempty"` // a second seizure within 24 h
	BloodThinners   []string `json:"blood_thinners,omitempty"`
	Escalate        bool     `json:"escalate"`
	Notes           string   `json:"notes,omitempty"`
	AlertID         string   `json:"alert_id,omitempty"`
	By              string   `json:"by,omitempty"`
}

// LogIncident: POST /care/incidents.
func (h *CareHandler) LogIncident(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers cannot record a fall or seizure")
		return
	}
	var req IncidentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	req.Notes = strings.TrimSpace(req.Notes)
	if len(req.Notes) > maxNotesLen {
		writeJSONError(w, http.StatusBadRequest, "NOTES_TOO_LONG", "notes are too long")
		return
	}
	switch req.Kind {
	case "fall":
		if req.HitHead == nil || req.AwakeNormal == nil || req.PainOrBleeding == nil {
			writeJSONError(w, http.StatusBadRequest, "ANSWERS_REQUIRED", "a fall needs hit_head, awake_normal and pain_or_bleeding")
			return
		}
	case "seizure":
		if req.DurationSeconds == nil || *req.DurationSeconds < 1 || *req.DurationSeconds > 6*3600 {
			writeJSONError(w, http.StatusBadRequest, "DURATION_REQUIRED", "a seizure needs duration_seconds (1 s to 6 h)")
			return
		}
	default:
		writeJSONError(w, http.StatusBadRequest, "INVALID_KIND", "kind must be fall or seizure")
		return
	}
	if req.ClientRequestID != "" {
		if inc, err := incidentByRequest(h.DB, req.ClientRequestID); err == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(inc) // a resend of the same tap
			return
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	inc := Incident{ID: uuid.New().String(), Kind: req.Kind, At: now.Format(time.RFC3339), HitHead: req.HitHead, AwakeNormal: req.AwakeNormal,
		PainOrBleeding: req.PainOrBleeding, DurationSeconds: req.DurationSeconds, Notes: req.Notes, By: me.Name}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to record", err)
		return
	}
	defer tx.Rollback()
	// The single pooled connection belongs to tx: use tx only.
	rows, err := tx.Query(`SELECT name_en FROM medications WHERE is_active = 1 AND COALESCE(is_critical, 0) = 1 ORDER BY name_en`)
	if err != nil {
		internalError(w, "failed to read medicines", err)
		return
	}
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			inc.BloodThinners = append(inc.BloodThinners, n)
		}
	}
	rows.Close()

	if inc.Kind == "fall" {
		inc.Escalate = *req.HitHead || !*req.AwakeNormal || *req.PainOrBleeding
	} else {
		var earlier int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM care_events WHERE kind = 'seizure' AND created_at >= ?`, now.Add(-24*time.Hour).Format(time.RFC3339)).Scan(&earlier); err != nil {
			internalError(w, "failed to check earlier seizures", err)
			return
		}
		inc.Repeat = earlier > 0
		inc.Escalate = *req.DurationSeconds >= seizureEmergencySeconds || inc.Repeat
	}

	value, _ := json.Marshal(inc)
	if _, err := tx.Exec(`INSERT INTO care_events (id, kind, value, caregiver_id, notes, created_at, client_request_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		inc.ID, inc.Kind, string(value), me.ID, req.Notes, inc.At, nullIfEmpty(req.ClientRequestID)); err != nil {
		if isUniqueViolation(err) {
			tx.Rollback()
			if dup, err := incidentByRequest(h.DB, req.ClientRequestID); err == nil {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(dup)
				return
			}
		}
		internalError(w, "failed to record", err)
		return
	}
	var payload map[string]interface{}
	_ = json.Unmarshal(value, &payload)
	if inc.AlertID, err = recordAlert(tx, inc.Kind, inc.ID, me.ID, payload); err != nil {
		internalError(w, "failed to record alert", err)
		return
	}
	if err := audit.Record(tx, me.ID, strings.ToUpper(inc.Kind)+"_LOGGED", "care_event", inc.ID, incidentAuditText(inc)); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to record", err)
		return
	}
	out, _ := json.Marshal(inc)
	var ssePayload map[string]interface{}
	_ = json.Unmarshal(out, &ssePayload)
	h.Broker.BroadcastAlert(inc.AlertID, "CARE_EVENT_LOGGED", actorOf(me), ssePayload)
	h.Push.NotifyAsync(inc.AlertID, inc.Kind, me.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write(out)
}

func incidentAuditText(inc Incident) string {
	if inc.Kind == "fall" {
		return fmt.Sprintf("Fall: hit head %t, awake and talking normally %t, new pain/limb/bleeding %t, escalate %t", *inc.HitHead, *inc.AwakeNormal, *inc.PainOrBleeding, inc.Escalate)
	}
	return fmt.Sprintf("Seizure lasting %d s, second within 24 h %t, escalate %t", *inc.DurationSeconds, inc.Repeat, inc.Escalate)
}

func incidentByRequest(db *sql.DB, clientRequestID string) (Incident, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM care_events WHERE client_request_id = ? AND kind IN ('fall', 'seizure')`, clientRequestID).Scan(&value)
	if err != nil {
		return Incident{}, err
	}
	var inc Incident
	return inc, json.Unmarshal([]byte(value), &inc)
}

// incidentsSince lists falls and seizures at or after since, newest first.
func incidentsSince(q querier, since time.Time) ([]Incident, error) {
	rows, err := q.Query(`SELECT e.value, COALESCE(c.name, '') FROM care_events e LEFT JOIN caregivers c ON c.id = e.caregiver_id
		WHERE e.kind IN ('fall', 'seizure') AND e.created_at >= ? ORDER BY e.created_at DESC`, since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Incident{}
	for rows.Next() {
		var value, by string
		if err := rows.Scan(&value, &by); err != nil {
			return nil, err
		}
		var inc Incident
		if json.Unmarshal([]byte(value), &inc) == nil {
			inc.By, inc.AlertID = by, ""
			out = append(out, inc)
		}
	}
	return out, rows.Err()
}

// ListIncidents: GET /care/incidents?hours=24 (default 24, at most 31 days).
func (h *CareHandler) ListIncidents(w http.ResponseWriter, r *http.Request) {
	hours := 24
	if v, err := strconv.Atoi(r.URL.Query().Get("hours")); err == nil && v > 0 && v <= 744 {
		hours = v
	}
	list, err := incidentsSince(h.DB, time.Now().Add(-time.Duration(hours)*time.Hour))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		internalError(w, "failed to load falls and seizures", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}
