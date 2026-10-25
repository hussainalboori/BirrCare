package logging_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"birrcare/backend/internal/logging"
)

// mockUserStore records user entries in memory.
type mockUserStore struct {
	mu      sync.Mutex
	entries []logging.UserLogEntry
	failErr error
}

func (m *mockUserStore) WriteUserLog(ctx context.Context, entry logging.UserLogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return m.failErr
	}
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockUserStore) GetUserLogByID(ctx context.Context, id string) (*logging.UserLogEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if e.ID == id {
			return &e, nil
		}
	}
	return nil, nil
}

func (m *mockUserStore) QueryUserLogs(ctx context.Context, filter logging.LogFilter) ([]logging.UserLogEntry, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entries, len(m.entries), nil
}

// mockSystemStore records system entries in memory.
type mockSystemStore struct {
	mu      sync.Mutex
	entries []logging.SystemLogEntry
	failErr error
}

func (m *mockSystemStore) WriteSystemLog(ctx context.Context, entry logging.SystemLogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return m.failErr
	}
	m.entries = append(m.entries, entry)
	return nil
}

func (m *mockSystemStore) GetSystemLogByID(ctx context.Context, id string) (*logging.SystemLogEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if e.ID == id {
			return &e, nil
		}
	}
	return nil, nil
}

func (m *mockSystemStore) QuerySystemLogs(ctx context.Context, filter logging.LogFilter) ([]logging.SystemLogEntry, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entries, len(m.entries), nil
}

func TestLoggerRouting(t *testing.T) {
	userStore := &mockUserStore{}
	systemStore := &mockSystemStore{}
	logger := logging.NewLogger(userStore, systemStore)

	ctx := context.Background()

	// 1. Log User Activity
	err := logger.LogUserActivity(ctx, logging.UserLogEntry{
		CaregiverID:   "cg_1",
		CaregiverName: "Fatima",
		Action:        "log_dose",
		Message:       "Dose recorded successfully",
	})
	if err != nil {
		t.Fatalf("unexpected error logging user activity: %v", err)
	}

	// 2. Log System Event
	err = logger.LogSystemEvent(ctx, logging.SystemLogEntry{
		Subsystem: "backup",
		Event:     "backup_completed",
		Message:   "Restic snapshot succeeded",
	})
	if err != nil {
		t.Fatalf("unexpected error logging system event: %v", err)
	}

	// Verify routing: User entry went to user store, NOT system store
	if len(userStore.entries) != 1 {
		t.Fatalf("expected 1 user store entry, got %d", len(userStore.entries))
	}
	if userStore.entries[0].Action != "log_dose" {
		t.Errorf("expected action 'log_dose', got %s", userStore.entries[0].Action)
	}
	if len(systemStore.entries) != 1 {
		t.Fatalf("expected 1 system store entry, got %d", len(systemStore.entries))
	}
	if systemStore.entries[0].Event != "backup_completed" {
		t.Errorf("expected event 'backup_completed', got %s", systemStore.entries[0].Event)
	}

	// 3. Test generic Log method routing
	err = logger.Log(ctx, logging.StreamUser, logging.UserLogEntry{
		CaregiverID: "cg_2",
		Action:      "login",
		Message:     "PIN entered",
	})
	if err != nil {
		t.Fatalf("unexpected error in generic user log: %v", err)
	}
	if len(userStore.entries) != 2 {
		t.Fatalf("expected 2 user entries, got %d", len(userStore.entries))
	}

	err = logger.Log(ctx, logging.StreamSystem, logging.SystemLogEntry{
		Subsystem: "server",
		Event:     "startup",
		Message:   "Ready",
	})
	if err != nil {
		t.Fatalf("unexpected error in generic system log: %v", err)
	}
	if len(systemStore.entries) != 2 {
		t.Fatalf("expected 2 system entries, got %d", len(systemStore.entries))
	}
}

func TestLoggerSinkFailureHandling(t *testing.T) {
	injectedErr := errors.New("simulated disk I/O failure")
	userStore := &mockUserStore{failErr: injectedErr}
	systemStore := &mockSystemStore{}

	var fallbackBuf bytes.Buffer
	var errorHandlerCalled atomic.Bool

	logger := logging.NewLogger(
		userStore,
		systemStore,
		logging.WithFallbackSink(&fallbackBuf),
		logging.WithErrorHandler(func(stream logging.StreamType, entry any, err error) {
			errorHandlerCalled.Store(true)
			if stream != logging.StreamUser {
				t.Errorf("expected stream %s, got %s", logging.StreamUser, stream)
			}
			if !errors.Is(err, injectedErr) {
				t.Errorf("expected injected error, got %v", err)
			}
		}),
	)

	ctx := context.Background()
	err := logger.LogUserActivity(ctx, logging.UserLogEntry{
		Action:  "emergency_action",
		Message: "Critical action that failed to persist",
	})

	// Error must NOT be silently discarded
	if err == nil {
		t.Fatal("expected error from failed sink, got nil")
	}
	if !strings.Contains(err.Error(), "simulated disk I/O failure") {
		t.Fatalf("expected wrapped error with cause, got: %v", err)
	}

	// Fallback sink must have captured the diagnostics
	fallbackOutput := fallbackBuf.String()
	if !strings.Contains(fallbackOutput, "[LOGGER_SINK_FAILURE]") {
		t.Fatalf("expected fallback output to contain '[LOGGER_SINK_FAILURE]', got: %s", fallbackOutput)
	}
	if !strings.Contains(fallbackOutput, "Critical action that failed to persist") {
		t.Fatalf("expected fallback output to preserve log payload, got: %s", fallbackOutput)
	}

	// Error handler callback must have been executed
	if !errorHandlerCalled.Load() {
		t.Fatal("expected custom error handler to be invoked upon sink failure")
	}
}

func TestLoggerConcurrencyUnderLoad(t *testing.T) {
	userStore := &mockUserStore{}
	systemStore := &mockSystemStore{}
	logger := logging.NewLogger(userStore, systemStore)

	ctx := context.Background()
	const workers = 50
	const logsPerWorker = 100

	var wg sync.WaitGroup
	wg.Add(workers * 2)

	// Goroutines writing user logs
	for i := 0; i < workers; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < logsPerWorker; j++ {
				err := logger.UserActivity(
					ctx,
					"cg_worker",
					"Worker",
					"concurrent_action",
					"/test",
					"Testing concurrent user activity",
					200,
					"127.0.0.1",
					"TestAgent",
					map[string]any{"worker": workerID, "seq": j},
				)
				if err != nil {
					t.Errorf("worker %d failed logging user activity: %v", workerID, err)
				}
			}
		}(i)
	}

	// Goroutines writing system logs
	for i := 0; i < workers; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < logsPerWorker; j++ {
				err := logger.SystemInfo(
					ctx,
					"concurrency_subsystem",
					"worker_tick",
					"Testing concurrent system event",
					map[string]any{"worker": workerID, "seq": j},
				)
				if err != nil {
					t.Errorf("worker %d failed logging system event: %v", workerID, err)
				}
			}
		}(i)
	}

	wg.Wait()

	expectedCount := workers * logsPerWorker
	if len(userStore.entries) != expectedCount {
		t.Fatalf("expected %d user log entries, got %d", expectedCount, len(userStore.entries))
	}
	if len(systemStore.entries) != expectedCount {
		t.Fatalf("expected %d system log entries, got %d", expectedCount, len(systemStore.entries))
	}
}
