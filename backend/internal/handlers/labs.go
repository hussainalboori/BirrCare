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
	"birrcare/backend/internal/push"
	"birrcare/backend/internal/sse"
	"github.com/google/uuid"
)

// Lab results (issue #62): INR for warfarin first. A result outside the doctor's
// range (INRTarget) alerts every admin: "tell the doctor before the next dose".
// The app never suggests a new dose.

type LabHandler struct {
	DB     *sql.DB
	Broker *sse.Broker
	Push   *push.Notifier
}

type LabResult struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"` // inr | hba1c | other
	Value      float64 `json:"value"`
	Unit       string  `json:"unit,omitempty"`
	TakenOn    string  `json:"taken_on"`               // household YYYY-MM-DD
	NextTestOn string  `json:"next_test_on,omitempty"` // household YYYY-MM-DD
	OutOfRange bool    `json:"out_of_range"`
	TargetLow  float64 `json:"target_low,omitempty"`
	TargetHigh float64 `json:"target_high,omitempty"`
	Notes      string  `json:"notes,omitempty"`
	By         string  `json:"by,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

type labRequest struct {
	Kind            string  `json:"kind"`
	Value           float64 `json:"value"`
	Unit            string  `json:"unit"`
	TakenOn         string  `json:"taken_on"`
	NextTestOn      string  `json:"next_test_on"`
	Notes           string  `json:"notes"`
	ClientRequestID string  `json:"client_request_id"`
}

// Create: POST /labs.
func (h *LabHandler) Create(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers cannot record lab results")
		return
	}
	var req labRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	req.Notes, req.Unit = strings.TrimSpace(req.Notes), strings.TrimSpace(req.Unit)
	switch req.Kind {
	case "inr":
		if req.Value < 0.5 || req.Value > 10 {
			writeJSONError(w, http.StatusBadRequest, "INVALID_VALUE", "INR must be between 0.5 and 10")
			return
		}
	case "hba1c":
		if req.Value < 3 || req.Value > 20 {
			writeJSONError(w, http.StatusBadRequest, "INVALID_VALUE", "HbA1c must be between 3 and 20 %")
			return
		}
		if req.Unit == "" {
			req.Unit = "%"
		}
	case "other":
		if req.Unit == "" {
			writeJSONError(w, http.StatusBadRequest, "UNIT_REQUIRED", "write the unit of the result")
			return
		}
	default:
		writeJSONError(w, http.StatusBadRequest, "INVALID_KIND", "kind must be inr, hba1c or other")
		return
	}
	if len(req.Notes) > maxNotesLen || len(req.Unit) > 20 {
		writeJSONError(w, http.StatusBadRequest, "NOTES_TOO_LONG", "notes or unit are too long")
		return
	}
	today := clinicalToday()
	if req.TakenOn == "" {
		req.TakenOn = today
	}
	if canon, _, ok := parseClinicalDate(req.TakenOn); !ok || canon > today {
		writeJSONError(w, http.StatusBadRequest, "INVALID_DATE", "taken_on must be a date, not in the future")
		return
	} else {
		req.TakenOn = canon
	}
	if req.NextTestOn != "" {
		canon, _, ok := parseClinicalDate(req.NextTestOn)
		if !ok || canon < req.TakenOn {
			writeJSONError(w, http.StatusBadRequest, "INVALID_DATE", "next_test_on must be a date after the test")
			return
		}
		req.NextTestOn = canon
	}
	if req.ClientRequestID != "" {
		if dup, err := labByRequest(h.DB, req.ClientRequestID); err == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(dup)
			return
		}
	}

	res := LabResult{ID: uuid.New().String(), Kind: req.Kind, Value: req.Value, Unit: req.Unit, TakenOn: req.TakenOn, NextTestOn: req.NextTestOn,
		Notes: req.Notes, By: me.Name, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if res.Kind == "inr" {
		t := currentINRTarget()
		res.TargetLow, res.TargetHigh = t.Low, t.High
		res.OutOfRange = res.Value < t.Low || res.Value > t.High
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to save the result", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO lab_results (id, kind, value, unit, taken_on, next_test_on, out_of_range, notes, caregiver_id, created_at, client_request_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, res.ID, res.Kind, res.Value, res.Unit, res.TakenOn, nullIfEmpty(res.NextTestOn), res.OutOfRange,
		res.Notes, me.ID, time.Now().UTC().Format(sqliteTimeLayout), nullIfEmpty(req.ClientRequestID)); err != nil {
		internalError(w, "failed to save the result", err)
		return
	}
	if err := audit.Record(tx, me.ID, "LAB_RESULT_LOGGED", "lab_result", res.ID,
		fmt.Sprintf("%s %.2f %s on %s (out of range %t), next test %s", strings.ToUpper(res.Kind), res.Value, res.Unit, res.TakenOn, res.OutOfRange, res.NextTestOn)); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}
	// Out of range: every admin is told, on their own phone.
	var raised []raisedAlert
	if res.OutOfRange {
		rows, err := tx.Query(`SELECT id FROM caregivers WHERE is_active = 1 AND role IN ('admin', 'super_admin')`)
		if err != nil {
			internalError(w, "failed to find admins", err)
			return
		}
		var admins []string
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				admins = append(admins, id)
			}
		}
		rows.Close()
		payload := map[string]interface{}{"value": res.Value, "target_low": res.TargetLow, "target_high": res.TargetHigh, "taken_on": res.TakenOn}
		for _, a := range admins {
			id, err := recordAlertFor(tx, "inr_out_of_range", res.ID, me.ID, a, payload)
			if err != nil {
				internalError(w, "failed to record alert", err)
				return
			}
			raised = append(raised, raisedAlert{ID: id, Kind: "inr_out_of_range", Target: a})
		}
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to save the result", err)
		return
	}
	for _, a := range raised {
		pushRaised(h.Push, a, me.ID)
	}
	payload := map[string]interface{}{"id": res.ID, "kind": res.Kind, "value": res.Value, "out_of_range": res.OutOfRange}
	if len(raised) > 0 {
		payload["alert_id"] = raised[0].ID
	}
	h.Broker.Broadcast("LAB_RESULT_LOGGED", actorOf(me), payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(res)
}

const labSelect = `SELECT l.id, l.kind, l.value, l.unit, l.taken_on, COALESCE(l.next_test_on, ''), l.out_of_range, l.notes, COALESCE(c.name, ''), l.created_at
	FROM lab_results l LEFT JOIN caregivers c ON c.id = l.caregiver_id`

func scanLabs(rows *sql.Rows) ([]LabResult, error) {
	defer rows.Close()
	out := []LabResult{}
	for rows.Next() {
		var l LabResult
		if err := rows.Scan(&l.ID, &l.Kind, &l.Value, &l.Unit, &l.TakenOn, &l.NextTestOn, &l.OutOfRange, &l.Notes, &l.By, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.CreatedAt = sqliteToRFC3339(l.CreatedAt)
		out = append(out, l)
	}
	return out, rows.Err()
}

func labByRequest(db *sql.DB, clientRequestID string) (LabResult, error) {
	rows, err := db.Query(labSelect+` WHERE l.client_request_id = ?`, clientRequestID)
	if err != nil {
		return LabResult{}, err
	}
	list, err := scanLabs(rows)
	if err != nil || len(list) == 0 {
		return LabResult{}, sql.ErrNoRows
	}
	return list[0], nil
}

// labsSince lists results of a kind (or all, kind "") taken on or after a household date.
func labsSince(q querier, kind, since string) ([]LabResult, error) {
	rows, err := q.Query(labSelect+` WHERE (? = '' OR l.kind = ?) AND l.taken_on >= ? ORDER BY l.taken_on DESC, l.created_at DESC`, kind, kind, since)
	if err != nil {
		return nil, err
	}
	return scanLabs(rows)
}

// List: GET /labs?kind=inr&days=180 → {results, inr_target}.
func (h *LabHandler) List(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	days := 180
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v > 0 && v <= 3650 {
		days = v
	}
	since := time.Now().In(ClinicalLocation()).AddDate(0, 0, -days).Format("2006-01-02")
	list, err := labsSince(h.DB, kind, since)
	if err != nil {
		internalError(w, "failed to load lab results", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"results": list, "inr_target": currentINRTarget()})
}
