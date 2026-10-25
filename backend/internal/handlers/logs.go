package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/logging"
	"github.com/go-chi/chi/v5"
)

// AdminLogsHandler serves log inspection, search, and export endpoints restricted exclusively
// to the Super Admin role.
type AdminLogsHandler struct {
	UserStore   logging.UserLogStore
	SystemStore logging.SystemLogStore
	AuditStore  logging.AccessAuditStore
}

// NewAdminLogsHandler creates a new AdminLogsHandler from logger and audit store.
func NewAdminLogsHandler(l *logging.Logger, auditStore logging.AccessAuditStore) *AdminLogsHandler {
	var uStore logging.UserLogStore
	var sStore logging.SystemLogStore
	if l != nil {
		uStore = l.UserStore()
		sStore = l.SystemStore()
	}
	return &AdminLogsHandler{
		UserStore:   uStore,
		SystemStore: sStore,
		AuditStore:  auditStore,
	}
}

// requireSuperAdmin checks that the request is authenticated as a Super Admin.
// Returns the Caregiver if authorized; otherwise writes 401 or 403 and returns nil.
func (h *AdminLogsHandler) requireSuperAdmin(w http.ResponseWriter, r *http.Request) *authCaregiverInfo {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		writeJSONError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Select your profile and enter your PIN")
		return nil
	}
	if !auth.CanManageUsers(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only Super Admins can access logs")
		return nil
	}
	return &authCaregiverInfo{
		ID:   caregiver.ID,
		Name: caregiver.Name,
	}
}

type authCaregiverInfo struct {
	ID   string
	Name string
}

func (h *AdminLogsHandler) recordAccessAudit(r *http.Request, admin *authCaregiverInfo, stream, operation string, filter any, count int) {
	if h.AuditStore == nil || admin == nil {
		return
	}
	filterBytes, _ := json.Marshal(filter)
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	} else {
		ip = strings.Split(ip, ",")[0]
	}

	_ = h.AuditStore.RecordAccess(r.Context(), logging.AccessAuditEntry{
		CaregiverID:   admin.ID,
		CaregiverName: admin.Name,
		Stream:        stream,
		Operation:     operation,
		FilterQuery:   string(filterBytes),
		RecordsCount:  count,
		IPAddress:     strings.TrimSpace(ip),
		UserAgent:     r.UserAgent(),
		CreatedAt:     time.Now().UTC(),
	})
}

// ListUserLogs handles GET /api/v1/admin/logs/user.
func (h *AdminLogsHandler) ListUserLogs(w http.ResponseWriter, r *http.Request) {
	admin := h.requireSuperAdmin(w, r)
	if admin == nil {
		return
	}
	if h.UserStore == nil {
		internalError(w, "user log store is not initialized", nil)
		return
	}

	filter := parseLogFilter(r, logging.StreamUser)
	entries, total, err := h.UserStore.QueryUserLogs(r.Context(), filter)
	if err != nil {
		internalError(w, "failed to query user logs", err)
		return
	}

	h.recordAccessAudit(r, admin, "user", "list", filter, len(entries))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"entries": entries,
		"total":   total,
		"limit":   filter.Limit,
		"offset":  filter.Offset,
	})
}

// GetUserLog handles GET /api/v1/admin/logs/user/{id}.
func (h *AdminLogsHandler) GetUserLog(w http.ResponseWriter, r *http.Request) {
	admin := h.requireSuperAdmin(w, r)
	if admin == nil {
		return
	}
	if h.UserStore == nil {
		internalError(w, "user log store is not initialized", nil)
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "BAD_REQUEST", "Log ID is required")
		return
	}

	entry, err := h.UserStore.GetUserLogByID(r.Context(), id)
	if err != nil {
		internalError(w, "failed to fetch user log", err)
		return
	}
	if entry == nil {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "User log not found")
		return
	}

	h.recordAccessAudit(r, admin, "user", "read", map[string]string{"id": id}, 1)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entry)
}

// ListSystemLogs handles GET /api/v1/admin/logs/system.
func (h *AdminLogsHandler) ListSystemLogs(w http.ResponseWriter, r *http.Request) {
	admin := h.requireSuperAdmin(w, r)
	if admin == nil {
		return
	}
	if h.SystemStore == nil {
		internalError(w, "system log store is not initialized", nil)
		return
	}

	filter := parseLogFilter(r, logging.StreamSystem)
	entries, total, err := h.SystemStore.QuerySystemLogs(r.Context(), filter)
	if err != nil {
		internalError(w, "failed to query system logs", err)
		return
	}

	h.recordAccessAudit(r, admin, "system", "list", filter, len(entries))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"entries": entries,
		"total":   total,
		"limit":   filter.Limit,
		"offset":  filter.Offset,
	})
}

// GetSystemLog handles GET /api/v1/admin/logs/system/{id}.
func (h *AdminLogsHandler) GetSystemLog(w http.ResponseWriter, r *http.Request) {
	admin := h.requireSuperAdmin(w, r)
	if admin == nil {
		return
	}
	if h.SystemStore == nil {
		internalError(w, "system log store is not initialized", nil)
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "BAD_REQUEST", "Log ID is required")
		return
	}

	entry, err := h.SystemStore.GetSystemLogByID(r.Context(), id)
	if err != nil {
		internalError(w, "failed to fetch system log", err)
		return
	}
	if entry == nil {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "System log not found")
		return
	}

	h.recordAccessAudit(r, admin, "system", "read", map[string]string{"id": id}, 1)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entry)
}

// ExportLogs handles GET /api/v1/admin/logs/export.
// Query params:
// - stream: "user" (default), "system", or "both"
// - format: "json" (default) or "csv"
func (h *AdminLogsHandler) ExportLogs(w http.ResponseWriter, r *http.Request) {
	admin := h.requireSuperAdmin(w, r)
	if admin == nil {
		return
	}

	stream := strings.ToLower(r.URL.Query().Get("stream"))
	if stream == "" {
		stream = "user"
	}
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "json"
	}

	timestamp := time.Now().UTC().Format("20060102-150405")
	filter := parseLogFilter(r, logging.StreamType(stream))
	filter.Limit = 5000 // cap export limit to a safe batch size
	filter.Offset = 0

	switch stream {
	case "user":
		if h.UserStore == nil {
			internalError(w, "user log store not initialized", nil)
			return
		}
		entries, _, err := h.UserStore.QueryUserLogs(r.Context(), filter)
		if err != nil {
			internalError(w, "failed to query user logs for export", err)
			return
		}
		h.recordAccessAudit(r, admin, "user", "export", map[string]any{"format": format, "filter": filter}, len(entries))

		if format == "csv" {
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="birrcare-user-logs-%s.csv"`, timestamp))
			writer := csv.NewWriter(w)
			_ = writer.Write([]string{"ID", "Created At", "Level", "Caregiver ID", "Caregiver Name", "Action", "Resource", "Status Code", "IP", "User Agent", "Message"})
			for _, e := range entries {
				_ = writer.Write([]string{
					e.ID, e.CreatedAt.Format(time.RFC3339), string(e.Level), e.CaregiverID,
					e.CaregiverName, e.Action, e.Resource, strconv.Itoa(e.StatusCode),
					e.IPAddress, e.UserAgent, e.Message,
				})
			}
			writer.Flush()
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="birrcare-user-logs-%s.json"`, timestamp))
		_ = json.NewEncoder(w).Encode(entries)

	case "system":
		if h.SystemStore == nil {
			internalError(w, "system log store not initialized", nil)
			return
		}
		entries, _, err := h.SystemStore.QuerySystemLogs(r.Context(), filter)
		if err != nil {
			internalError(w, "failed to query system logs for export", err)
			return
		}
		h.recordAccessAudit(r, admin, "system", "export", map[string]any{"format": format, "filter": filter}, len(entries))

		if format == "csv" {
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="birrcare-system-logs-%s.csv"`, timestamp))
			writer := csv.NewWriter(w)
			_ = writer.Write([]string{"ID", "Created At", "Level", "Subsystem", "Event", "Message", "Error Details"})
			for _, e := range entries {
				_ = writer.Write([]string{
					e.ID, e.CreatedAt.Format(time.RFC3339), string(e.Level), e.Subsystem,
					e.Event, e.Message, e.ErrorDetails,
				})
			}
			writer.Flush()
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="birrcare-system-logs-%s.json"`, timestamp))
		_ = json.NewEncoder(w).Encode(entries)

	default:
		writeJSONError(w, http.StatusBadRequest, "BAD_REQUEST", "stream must be 'user' or 'system'")
	}
}

// ListAccessAudits handles GET /api/v1/admin/logs/access-audits.
func (h *AdminLogsHandler) ListAccessAudits(w http.ResponseWriter, r *http.Request) {
	admin := h.requireSuperAdmin(w, r)
	if admin == nil {
		return
	}
	if h.AuditStore == nil {
		internalError(w, "access audit store is not initialized", nil)
		return
	}

	filter := logging.AccessAuditFilter{
		CaregiverID: r.URL.Query().Get("caregiver_id"),
		Stream:      r.URL.Query().Get("stream"),
		Operation:   r.URL.Query().Get("operation"),
		From:        parseTimeParam(r.URL.Query().Get("from")),
		To:          parseTimeParam(r.URL.Query().Get("to")),
		Limit:       parseIntParam(r.URL.Query().Get("limit"), 50),
		Offset:      parseIntParam(r.URL.Query().Get("offset"), 0),
	}

	entries, total, err := h.AuditStore.QueryAccess(r.Context(), filter)
	if err != nil {
		internalError(w, "failed to query access audits", err)
		return
	}

	h.recordAccessAudit(r, admin, "access_audit", "list", filter, len(entries))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"entries": entries,
		"total":   total,
		"limit":   filter.Limit,
		"offset":  filter.Offset,
	})
}

func parseLogFilter(r *http.Request, stream logging.StreamType) logging.LogFilter {
	q := r.URL.Query()
	return logging.LogFilter{
		Stream:      stream,
		Query:       q.Get("query"),
		Level:       logging.LogLevel(q.Get("level")),
		CaregiverID: q.Get("caregiver_id"),
		Action:      q.Get("action"),
		Subsystem:   q.Get("subsystem"),
		Event:       q.Get("event"),
		From:        parseTimeParam(q.Get("from")),
		To:          parseTimeParam(q.Get("to")),
		Limit:       parseIntParam(q.Get("limit"), 50),
		Offset:      parseIntParam(q.Get("offset"), 0),
	}
}

func parseTimeParam(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			utc := t.UTC()
			return &utc
		}
	}
	return nil
}

func parseIntParam(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
