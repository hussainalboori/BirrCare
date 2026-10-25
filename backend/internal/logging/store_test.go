package logging_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"birrcare/backend/internal/db"
	"birrcare/backend/internal/logging"
)

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "logging-test.db")

	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed initializing test db: %v", err)
	}

	if err := db.Migrate(database); err != nil {
		t.Fatalf("failed migrating test db: %v", err)
	}

	_, _ = database.Exec(`
		INSERT OR IGNORE INTO caregivers (id, name, email, role, pin_hash)
		VALUES
			('cg_1', 'Fatima', 'fatima@test.local', 'caregiver', 'hash'),
			('cg_2', 'Ali', 'ali@test.local', 'admin', 'hash')
	`)

	cleanup := func() {
		database.Close()
		_ = os.Remove(dbPath)
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
	}
	return database, cleanup
}

func TestStoreSeparation(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	userStore := logging.NewSQLiteUserLogStore(database)
	systemStore := logging.NewSQLiteSystemLogStore(database)
	ctx := context.Background()

	// Write 1 entry to UserLogStore
	err := userStore.WriteUserLog(ctx, logging.UserLogEntry{
		CaregiverName: "Fatima",
		Action:        "log_dose",
		Message:       "Patient took morning dose",
		Level:         logging.LevelInfo,
	})
	if err != nil {
		t.Fatalf("failed writing user log: %v", err)
	}

	// Verify user store has 1 entry
	uLogs, uCount, err := userStore.QueryUserLogs(ctx, logging.LogFilter{})
	if err != nil {
		t.Fatalf("failed querying user logs: %v", err)
	}
	if uCount != 1 || len(uLogs) != 1 {
		t.Fatalf("expected 1 user log, got count=%d len=%d", uCount, len(uLogs))
	}

	// Verify system store is COMPLETELY empty (separation of stores!)
	sLogs, sCount, err := systemStore.QuerySystemLogs(ctx, logging.LogFilter{})
	if err != nil {
		t.Fatalf("failed querying system logs: %v", err)
	}
	if sCount != 0 || len(sLogs) != 0 {
		t.Fatalf("store separation breached: expected 0 system logs, got count=%d len=%d", sCount, len(sLogs))
	}

	// Write 1 entry to SystemLogStore
	err = systemStore.WriteSystemLog(ctx, logging.SystemLogEntry{
		Subsystem: "backup",
		Event:     "snapshot_created",
		Message:   "WAL consistent snapshot completed",
		Level:     logging.LevelInfo,
	})
	if err != nil {
		t.Fatalf("failed writing system log: %v", err)
	}

	// Verify system store has 1 entry
	sLogs, sCount, err = systemStore.QuerySystemLogs(ctx, logging.LogFilter{})
	if err != nil {
		t.Fatalf("failed querying system logs: %v", err)
	}
	if sCount != 1 || len(sLogs) != 1 {
		t.Fatalf("expected 1 system log, got count=%d len=%d", sCount, len(sLogs))
	}

	// Verify user store STILL has only 1 entry (no cross-contamination)
	uLogs, uCount, err = userStore.QueryUserLogs(ctx, logging.LogFilter{})
	if err != nil {
		t.Fatalf("failed querying user logs: %v", err)
	}
	if uCount != 1 || len(uLogs) != 1 {
		t.Fatalf("store separation breached: expected 1 user log, got count=%d len=%d", uCount, len(uLogs))
	}
}

func TestUserLogStoreQueryAndFilters(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	store := logging.NewSQLiteUserLogStore(database)
	ctx := context.Background()

	now := time.Now().UTC()
	entries := []logging.UserLogEntry{
		{
			ID:            "u1",
			CaregiverID:   "cg_1",
			CaregiverName: "Fatima",
			Level:         logging.LevelInfo,
			Action:        "log_dose",
			Resource:      "/api/v1/medications/log",
			StatusCode:    201,
			Message:       "Aspirin dose recorded",
			CreatedAt:     now.Add(-2 * time.Hour),
		},
		{
			ID:            "u2",
			CaregiverID:   "cg_2",
			CaregiverName: "Ali",
			Level:         logging.LevelWarn,
			Action:        "auth_pin",
			Resource:      "/api/v1/auth/verify-pin",
			StatusCode:    401,
			Message:       "Invalid PIN attempt",
			CreatedAt:     now.Add(-1 * time.Hour),
		},
		{
			ID:            "u3",
			CaregiverID:   "cg_1",
			CaregiverName: "Fatima",
			Level:         logging.LevelError,
			Action:        "dose_override",
			Resource:      "/api/v1/medications/log",
			StatusCode:    409,
			Message:       "Double-dose lock triggered",
			CreatedAt:     now,
		},
	}

	for _, e := range entries {
		if err := store.WriteUserLog(ctx, e); err != nil {
			t.Fatalf("failed inserting entry %s: %v", e.ID, err)
		}
	}

	// 1. Get by ID
	got, err := store.GetUserLogByID(ctx, "u2")
	if err != nil {
		t.Fatalf("failed getting log by id: %v", err)
	}
	if got == nil || got.ID != "u2" || got.CaregiverName != "Ali" {
		t.Fatalf("unexpected entry fetched: %+v", got)
	}

	// 2. Filter by Level
	res, count, err := store.QueryUserLogs(ctx, logging.LogFilter{Level: logging.LevelWarn})
	if err != nil {
		t.Fatalf("failed querying by level: %v", err)
	}
	if count != 1 || res[0].ID != "u2" {
		t.Fatalf("expected 1 warn entry (u2), got count=%d res=%+v", count, res)
	}

	// 3. Filter by CaregiverID
	res, count, err = store.QueryUserLogs(ctx, logging.LogFilter{CaregiverID: "cg_1"})
	if err != nil {
		t.Fatalf("failed querying by caregiver: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 entries for cg_1, got %d", count)
	}

	// 4. Text search query
	res, count, err = store.QueryUserLogs(ctx, logging.LogFilter{Query: "lock"})
	if err != nil {
		t.Fatalf("failed text search: %v", err)
	}
	if count != 1 || res[0].ID != "u3" {
		t.Fatalf("expected 1 match for 'lock' (u3), got count=%d", count)
	}

	// 5. Pagination (Limit 1, Offset 1)
	res, count, err = store.QueryUserLogs(ctx, logging.LogFilter{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("failed pagination query: %v", err)
	}
	if count != 3 || len(res) != 1 {
		t.Fatalf("expected total count 3, page size 1, got total=%d len=%d", count, len(res))
	}
	if res[0].ID != "u2" {
		t.Fatalf("expected middle item u2 (since order is DESC: u3, u2, u1), got %s", res[0].ID)
	}
}

func TestSystemLogStoreQueryAndFilters(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	store := logging.NewSQLiteSystemLogStore(database)
	ctx := context.Background()

	entries := []logging.SystemLogEntry{
		{
			ID:        "s1",
			Level:     logging.LevelInfo,
			Subsystem: "server",
			Event:     "startup",
			Message:   "Birrcare backend listening on :8080",
		},
		{
			ID:           "s2",
			Level:        logging.LevelError,
			Subsystem:    "push",
			Event:        "send_failed",
			Message:      "Failed to deliver Web Push notification",
			ErrorDetails: "endpoint returned 410 Gone",
		},
		{
			ID:        "s3",
			Level:     logging.LevelInfo,
			Subsystem: "backup",
			Event:     "backup_completed",
			Message:   "Encrypted snapshot uploaded",
		},
	}

	for _, e := range entries {
		if err := store.WriteSystemLog(ctx, e); err != nil {
			t.Fatalf("failed inserting system log %s: %v", e.ID, err)
		}
	}

	// 1. Get by ID
	got, err := store.GetSystemLogByID(ctx, "s2")
	if err != nil {
		t.Fatalf("failed getting system log by id: %v", err)
	}
	if got == nil || got.ErrorDetails != "endpoint returned 410 Gone" {
		t.Fatalf("unexpected system log fetched: %+v", got)
	}

	// 2. Filter by Subsystem
	res, count, err := store.QuerySystemLogs(ctx, logging.LogFilter{Subsystem: "push"})
	if err != nil {
		t.Fatalf("failed querying by subsystem: %v", err)
	}
	if count != 1 || res[0].ID != "s2" {
		t.Fatalf("expected 1 match for push subsystem, got %d", count)
	}

	// 3. Text search
	res, count, err = store.QuerySystemLogs(ctx, logging.LogFilter{Query: "encrypted"})
	if err != nil {
		t.Fatalf("failed querying text: %v", err)
	}
	if count != 1 || res[0].ID != "s3" {
		t.Fatalf("expected 1 match for 'encrypted', got %d", count)
	}
}

func TestAccessAuditStore(t *testing.T) {
	database, cleanup := setupTestDB(t)
	defer cleanup()

	store := logging.NewSQLiteAccessAuditStore(database)
	ctx := context.Background()

	err := store.RecordAccess(ctx, logging.AccessAuditEntry{
		CaregiverID:   "cg_superadmin",
		CaregiverName: "Hussain",
		Stream:        "user",
		Operation:     "list",
		FilterQuery:   `{"limit":50}`,
		RecordsCount:  25,
		IPAddress:     "192.168.1.100",
		UserAgent:     "Mozilla/5.0",
		CreatedAt:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("failed recording access audit: %v", err)
	}

	audits, total, err := store.QueryAccess(ctx, logging.AccessAuditFilter{})
	if err != nil {
		t.Fatalf("failed querying access audits: %v", err)
	}
	if total != 1 || len(audits) != 1 {
		t.Fatalf("expected 1 audit record, got total=%d len=%d", total, len(audits))
	}
	record := audits[0]
	if record.CaregiverID != "cg_superadmin" || record.Stream != "user" || record.Operation != "list" {
		t.Fatalf("unexpected audit entry values: %+v", record)
	}
}
