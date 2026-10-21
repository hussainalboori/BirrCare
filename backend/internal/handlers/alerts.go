package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/push"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Alerts (issue #12): events every sibling must see even if their phone was
// locked or asleep. Each is written in the same transaction as the event, pushed
// to locked phones, and returned by /alerts/unacked on every app open and
// reconnect until that caregiver taps "Seen".

// alertWindow bounds replay: an unacknowledged alert stops nagging after a day.
const alertWindow = 24 * time.Hour

// recordAlert inserts an alert inside the caller's transaction.
func recordAlert(tx *sql.Tx, kind, refID, createdBy string, payload map[string]interface{}) (string, error) {
	return recordAlertFor(tx, kind, refID, createdBy, "", payload)
}

// recordAlertFor addresses the alert to one caregiver (target), or to everyone if "".
func recordAlertFor(tx *sql.Tx, kind, refID, createdBy, target string, payload map[string]interface{}) (string, error) {
	id := uuid.New().String()
	data, _ := json.Marshal(payload)
	_, err := tx.Exec(`INSERT INTO alerts (id, kind, ref_id, created_by, payload, created_at, target_caregiver_id) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, kind, refID, createdBy, string(data), time.Now().UTC().Format(sqliteTimeLayout), nullIfEmpty(target))
	return id, err
}

type Alert struct {
	ID            string          `json:"id"`
	Kind          string          `json:"kind"`
	RefID         string          `json:"ref_id,omitempty"`
	CreatedBy     string          `json:"created_by,omitempty"`
	CreatedByName string          `json:"created_by_name,omitempty"`
	Payload       json.RawMessage `json:"payload"`
	CreatedAt     string          `json:"created_at"` // RFC3339
}

type AlertsHandler struct {
	DB   *sql.DB
	Push *push.Notifier
}

// Unacked returns the alerts this caregiver has not acknowledged, oldest first,
// from the last 24 hours, excluding ones they caused themselves.
func (h *AlertsHandler) Unacked(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	since := time.Now().UTC().Add(-alertWindow).Format(sqliteTimeLayout)
	rows, err := h.DB.QueryContext(r.Context(), `
		SELECT a.id, a.kind, COALESCE(a.ref_id, ''), COALESCE(a.created_by, ''), COALESCE(c.name, ''), a.payload, a.created_at
		FROM alerts a LEFT JOIN caregivers c ON c.id = a.created_by
		WHERE a.created_at >= ? AND COALESCE(a.created_by, '') <> ?
		  AND (a.target_caregiver_id IS NULL OR a.target_caregiver_id = ?)
		  AND NOT EXISTS (SELECT 1 FROM alert_acks k WHERE k.alert_id = a.id AND k.caregiver_id = ?)
		ORDER BY a.created_at ASC`, since, me.ID, me.ID, me.ID)
	if err != nil {
		internalError(w, "failed to load alerts", err)
		return
	}
	defer rows.Close()
	out := []Alert{}
	for rows.Next() {
		var a Alert
		var payload, createdAt string
		if err := rows.Scan(&a.ID, &a.Kind, &a.RefID, &a.CreatedBy, &a.CreatedByName, &payload, &createdAt); err != nil {
			internalError(w, "failed to load alerts", err)
			return
		}
		a.Payload = json.RawMessage(payload)
		a.CreatedAt = sqliteToRFC3339(createdAt)
		out = append(out, a)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// Ack records that this caregiver has seen the alert. Idempotent.
func (h *AlertsHandler) Ack(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	id := chi.URLParam(r, "id")
	res, err := h.DB.ExecContext(r.Context(), `INSERT OR IGNORE INTO alert_acks (alert_id, caregiver_id, acked_at)
		SELECT id, ?, ? FROM alerts WHERE id = ?`, me.ID, time.Now().UTC().Format(sqliteTimeLayout), id)
	if err != nil {
		internalError(w, "failed to acknowledge alert", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		audit.Log(h.DB, me.ID, "ALERT_ACKED", "alert", id, "Acknowledged alert")
	} else {
		var exists int
		if h.DB.QueryRowContext(r.Context(), `SELECT 1 FROM alerts WHERE id = ?`, id).Scan(&exists) != nil {
			writeJSONError(w, http.StatusNotFound, "ALERT_NOT_FOUND", "alert not found")
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"acknowledged"}`))
}

// PushKey returns the VAPID public key the browser needs to subscribe.
func (h *AlertsHandler) PushKey(w http.ResponseWriter, r *http.Request) {
	if h.Push == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "PUSH_UNAVAILABLE", "push notifications are not configured")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"public_key": h.Push.PublicKey})
}

type pushSubscribeRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// Subscribe stores this browser's push subscription for the signed-in caregiver.
// The same endpoint moves to whoever subscribes last (a shared tablet).
func (h *AlertsHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	var req pushSubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !strings.HasPrefix(req.Endpoint, "https://") ||
		req.Keys.P256dh == "" || req.Keys.Auth == "" || len(req.Endpoint) > 2048 {
		writeJSONError(w, http.StatusBadRequest, "INVALID_SUBSCRIPTION", "endpoint (https) and keys.p256dh/auth are required")
		return
	}
	if _, err := h.DB.ExecContext(r.Context(), `
		INSERT INTO push_subscriptions (id, caregiver_id, endpoint, p256dh, auth) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (endpoint) DO UPDATE SET caregiver_id = excluded.caregiver_id, p256dh = excluded.p256dh, auth = excluded.auth`,
		uuid.New().String(), me.ID, req.Endpoint, req.Keys.P256dh, req.Keys.Auth); err != nil {
		internalError(w, "failed to save subscription", err)
		return
	}
	audit.Log(h.DB, me.ID, "PUSH_SUBSCRIBED", "caregiver", me.ID, "Enabled alerts on a device")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(`{"status":"subscribed"}`))
}

// TestPush sends a harmless test notification to the caller's own phones in 10
// seconds, so they can lock the phone and see it arrive. Rate: one per call.
func (h *AlertsHandler) TestPush(w http.ResponseWriter, r *http.Request) {
	if h.Push == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "PUSH_UNAVAILABLE", "push notifications are not configured")
		return
	}
	me := auth.GetCaregiverFromContext(r.Context())
	h.Push.TestAsync(me.ID, 10*time.Second)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"status":"sending in 10 seconds"}`))
}

// Unsubscribe removes this browser's subscription.
func (h *AlertsHandler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	var req pushSubscribeRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if _, err := h.DB.ExecContext(r.Context(), `DELETE FROM push_subscriptions WHERE endpoint = ? AND caregiver_id = ?`, req.Endpoint, me.ID); err != nil {
		internalError(w, "failed to remove subscription", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"unsubscribed"}`))
}
