package logging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Logger provides a centralized capture point for both user-facing activity/errors
// and internal system events. It guarantees thread safety and explicit sink error handling.
type Logger struct {
	userStore    UserLogStore
	systemStore  SystemLogStore
	fallbackSink io.Writer
	errorHandler func(stream StreamType, entry any, err error)
	mu           sync.RWMutex
}

// Option configures a Logger instance.
type Option func(*Logger)

// WithFallbackSink configures an alternate destination for emergency diagnostics
// if the primary storage sinks fail. Default is os.Stderr.
func WithFallbackSink(w io.Writer) Option {
	return func(l *Logger) {
		if w != nil {
			l.fallbackSink = w
		}
	}
}

// WithErrorHandler configures a callback invoked whenever a storage sink fails.
func WithErrorHandler(fn func(stream StreamType, entry any, err error)) Option {
	return func(l *Logger) {
		l.errorHandler = fn
	}
}

// NewLogger constructs a centralized Logger routing to the provided stores.
func NewLogger(userStore UserLogStore, systemStore SystemLogStore, opts ...Option) *Logger {
	l := &Logger{
		userStore:    userStore,
		systemStore:  systemStore,
		fallbackSink: os.Stderr,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// LogUserActivity records a user-facing event or action into the user store.
func (l *Logger) LogUserActivity(ctx context.Context, entry UserLogEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.Level == "" {
		entry.Level = LevelInfo
	}

	l.mu.RLock()
	store := l.userStore
	l.mu.RUnlock()

	if store == nil {
		err := errors.New("user log store is nil")
		l.handleSinkFailure(StreamUser, entry, err)
		return err
	}

	if err := store.WriteUserLog(ctx, entry); err != nil {
		l.handleSinkFailure(StreamUser, entry, err)
		return fmt.Errorf("user log sink write failed: %w", err)
	}
	return nil
}

// LogUserError records an error encountered during user interaction into the user store.
func (l *Logger) LogUserError(ctx context.Context, entry UserLogEntry) error {
	if entry.Level == "" {
		entry.Level = LevelError
	}
	return l.LogUserActivity(ctx, entry)
}

// LogSystemEvent records an infrastructure or background lifecycle event into the system store.
func (l *Logger) LogSystemEvent(ctx context.Context, entry SystemLogEntry) error {
	if entry.ID == "" {
		entry.ID = uuid.New().String()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.Level == "" {
		entry.Level = LevelInfo
	}

	l.mu.RLock()
	store := l.systemStore
	l.mu.RUnlock()

	if store == nil {
		err := errors.New("system log store is nil")
		l.handleSinkFailure(StreamSystem, entry, err)
		return err
	}

	if err := store.WriteSystemLog(ctx, entry); err != nil {
		l.handleSinkFailure(StreamSystem, entry, err)
		return fmt.Errorf("system log sink write failed: %w", err)
	}
	return nil
}

// LogSystemError records a system-level failure into the system store.
func (l *Logger) LogSystemError(ctx context.Context, entry SystemLogEntry) error {
	if entry.Level == "" {
		entry.Level = LevelError
	}
	return l.LogSystemEvent(ctx, entry)
}

// Log routes an arbitrary entry to the designated stream.
func (l *Logger) Log(ctx context.Context, stream StreamType, entry any) error {
	switch stream {
	case StreamUser:
		switch e := entry.(type) {
		case UserLogEntry:
			return l.LogUserActivity(ctx, e)
		case *UserLogEntry:
			if e == nil {
				return errors.New("user log entry is nil")
			}
			return l.LogUserActivity(ctx, *e)
		default:
			return fmt.Errorf("invalid entry type %T for StreamUser", entry)
		}
	case StreamSystem:
		switch e := entry.(type) {
		case SystemLogEntry:
			return l.LogSystemEvent(ctx, e)
		case *SystemLogEntry:
			if e == nil {
				return errors.New("system log entry is nil")
			}
			return l.LogSystemEvent(ctx, *e)
		default:
			return fmt.Errorf("invalid entry type %T for StreamSystem", entry)
		}
	default:
		return fmt.Errorf("unknown stream type %q", stream)
	}
}

// SystemInfo logs an informational system event.
func (l *Logger) SystemInfo(ctx context.Context, subsystem, event, message string, metadata map[string]any) error {
	return l.LogSystemEvent(ctx, SystemLogEntry{
		Level:     LevelInfo,
		Subsystem: subsystem,
		Event:     event,
		Message:   message,
		Metadata:  metadata,
	})
}

// SystemWarn logs a warning system event.
func (l *Logger) SystemWarn(ctx context.Context, subsystem, event, message string, metadata map[string]any) error {
	return l.LogSystemEvent(ctx, SystemLogEntry{
		Level:     LevelWarn,
		Subsystem: subsystem,
		Event:     event,
		Message:   message,
		Metadata:  metadata,
	})
}

// SystemError logs an error system event.
func (l *Logger) SystemError(ctx context.Context, subsystem, event, message string, err error, metadata map[string]any) error {
	var errDetails string
	if err != nil {
		errDetails = err.Error()
	}
	return l.LogSystemError(ctx, SystemLogEntry{
		Level:        LevelError,
		Subsystem:    subsystem,
		Event:        event,
		Message:      message,
		ErrorDetails: errDetails,
		Metadata:     metadata,
	})
}

// UserActivity logs a user activity entry.
func (l *Logger) UserActivity(ctx context.Context, caregiverID, caregiverName, action, resource, message string, statusCode int, ip, ua string, metadata map[string]any) error {
	return l.LogUserActivity(ctx, UserLogEntry{
		CaregiverID:   caregiverID,
		CaregiverName: caregiverName,
		Level:         LevelInfo,
		Action:        action,
		Resource:      resource,
		StatusCode:    statusCode,
		IPAddress:     ip,
		UserAgent:     ua,
		Message:       message,
		Metadata:      metadata,
	})
}

// UserError logs a user error entry.
func (l *Logger) UserError(ctx context.Context, caregiverID, caregiverName, action, resource, message string, statusCode int, ip, ua string, err error, metadata map[string]any) error {
	msg := message
	if err != nil {
		if msg != "" {
			msg = fmt.Sprintf("%s: %v", msg, err)
		} else {
			msg = err.Error()
		}
	}
	level := LevelError
	if statusCode >= 400 && statusCode < 500 {
		level = LevelWarn
	}
	return l.LogUserError(ctx, UserLogEntry{
		CaregiverID:   caregiverID,
		CaregiverName: caregiverName,
		Level:         level,
		Action:        action,
		Resource:      resource,
		StatusCode:    statusCode,
		IPAddress:     ip,
		UserAgent:     ua,
		Message:       msg,
		Metadata:      metadata,
	})
}

// UserStore returns the underlying user log store for read/query operations.
func (l *Logger) UserStore() UserLogStore {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.userStore
}

// SystemStore returns the underlying system log store for read/query operations.
func (l *Logger) SystemStore() SystemLogStore {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.systemStore
}

// handleSinkFailure is invoked when a primary store write fails.
// It ensures that no event is silently dropped by recording it to the fallback sink
// and notifying any configured error handler.
func (l *Logger) handleSinkFailure(stream StreamType, entry any, sinkErr error) {
	l.mu.RLock()
	sink := l.fallbackSink
	handler := l.errorHandler
	l.mu.RUnlock()

	now := time.Now().UTC().Format(time.RFC3339)
	payloadBytes, _ := json.Marshal(entry)

	if sink != nil {
		_, _ = fmt.Fprintf(sink, "[%s] [LOGGER_SINK_FAILURE] stream=%s error=%v payload=%s\n",
			now, stream, sinkErr, string(payloadBytes))
	}

	if handler != nil {
		handler(stream, entry, sinkErr)
	}
}
