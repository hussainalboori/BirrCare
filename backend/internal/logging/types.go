package logging

import (
	"encoding/json"
	"time"
)

// StreamType designates the distinct target log stream.
type StreamType string

const (
	// StreamUser represents user interactions, clinical actions, and user-facing HTTP errors.
	StreamUser StreamType = "user"
	// StreamSystem represents low-level server, database, worker, and subsystem events.
	StreamSystem StreamType = "system"
)

// LogLevel defines the severity level of an event.
type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
	LevelFatal LogLevel = "fatal"
)

// UserLogEntry records an action or error triggered by an authenticated or anonymous user.
type UserLogEntry struct {
	ID            string         `json:"id"`
	CaregiverID   string         `json:"caregiver_id,omitempty"`
	CaregiverName string         `json:"caregiver_name,omitempty"`
	Level         LogLevel       `json:"level"`
	Action        string         `json:"action"`
	Resource      string         `json:"resource,omitempty"`
	StatusCode    int            `json:"status_code,omitempty"`
	IPAddress     string         `json:"ip_address,omitempty"`
	UserAgent     string         `json:"user_agent,omitempty"`
	Message       string         `json:"message"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

// SystemLogEntry records an infrastructure, background task, or system lifecycle event.
type SystemLogEntry struct {
	ID           string         `json:"id"`
	Level        LogLevel       `json:"level"`
	Subsystem    string         `json:"subsystem"`
	Event        string         `json:"event"`
	Message      string         `json:"message"`
	ErrorDetails string         `json:"error_details,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

// LogFilter defines criteria for searching and filtering log entries.
type LogFilter struct {
	Stream      StreamType `json:"stream,omitempty"`
	Query       string     `json:"query,omitempty"`
	Level       LogLevel   `json:"level,omitempty"`
	CaregiverID string     `json:"caregiver_id,omitempty"`
	Action      string     `json:"action,omitempty"`
	Subsystem   string     `json:"subsystem,omitempty"`
	Event       string     `json:"event,omitempty"`
	From        *time.Time `json:"from,omitempty"`
	To          *time.Time `json:"to,omitempty"`
	Limit       int        `json:"limit,omitempty"`
	Offset      int        `json:"offset,omitempty"`
}

// AccessAuditEntry records access to log entries by administrators.
type AccessAuditEntry struct {
	ID           string    `json:"id"`
	CaregiverID  string    `json:"caregiver_id"`
	CaregiverName string   `json:"caregiver_name"`
	Stream       string    `json:"stream"`    // "user", "system", "both", "access_audit"
	Operation    string    `json:"operation"` // "list", "read", "search", "export"
	FilterQuery  string    `json:"filter_query,omitempty"`
	RecordsCount int       `json:"records_count"`
	IPAddress    string    `json:"ip_address,omitempty"`
	UserAgent    string    `json:"user_agent,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// AccessAuditFilter defines criteria for viewing log access audits.
type AccessAuditFilter struct {
	CaregiverID string     `json:"caregiver_id,omitempty"`
	Stream      string     `json:"stream,omitempty"`
	Operation   string     `json:"operation,omitempty"`
	From        *time.Time `json:"from,omitempty"`
	To          *time.Time `json:"to,omitempty"`
	Limit       int        `json:"limit,omitempty"`
	Offset      int        `json:"offset,omitempty"`
}

// MarshalMetadata serializes a metadata map to a JSON string.
func MarshalMetadata(m map[string]any) string {
	if len(m) == 0 {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// UnmarshalMetadata deserializes a JSON string into a metadata map.
func UnmarshalMetadata(raw string) map[string]any {
	if raw == "" || raw == "{}" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	return m
}
