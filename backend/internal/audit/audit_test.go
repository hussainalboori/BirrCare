package audit_test

import (
	"database/sql"
	"testing"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/db"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatalf("failed to open memory db: %v", err)
	}
	if err := db.Migrate(database); err != nil {
		t.Fatalf("failed to migrate db: %v", err)
	}
	return database
}

func TestAuditHashChainAndVerify(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// 1. Empty table verifies cleanly
	res, err := audit.Verify(database)
	if err != nil {
		t.Fatalf("Verify on empty table failed: %v", err)
	}
	if !res.Valid || res.VerifiedRows != 0 {
		t.Fatalf("want valid empty chain, got %+v", res)
	}

	// 2. Add 3 sequential audit entries
	if err := audit.Record(database, "cg_superadmin", "TEST_ACTION_1", "test", "id1", "details 1"); err != nil {
		t.Fatalf("failed to record entry 1: %v", err)
	}
	if err := audit.Record(database, "cg_superadmin", "TEST_ACTION_2", "test", "id2", "details 2"); err != nil {
		t.Fatalf("failed to record entry 2: %v", err)
	}
	if err := audit.Record(database, "cg_superadmin", "TEST_ACTION_3", "test", "id3", "details 3"); err != nil {
		t.Fatalf("failed to record entry 3: %v", err)
	}

	// 3. Verify intact chain
	res, err = audit.Verify(database)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if !res.Valid || res.VerifiedRows != 3 {
		t.Fatalf("want valid chain with 3 rows, got %+v", res)
	}

	// 4. Manually tamper with the second row (drop trigger to simulate DB-level tampering)
	if _, err := database.Exec("DROP TRIGGER audit_no_update"); err != nil {
		t.Fatalf("failed to drop trigger: %v", err)
	}

	var row2ID string
	err = database.QueryRow("SELECT id FROM audit_logs WHERE action = 'TEST_ACTION_2'").Scan(&row2ID)
	if err != nil {
		t.Fatalf("failed to get row 2 id: %v", err)
	}

	if _, err := database.Exec("UPDATE audit_logs SET details = 'tampered details' WHERE id = ?", row2ID); err != nil {
		t.Fatalf("failed to tamper row: %v", err)
	}

	// 5. Verify reports the tampered row id
	res, err = audit.Verify(database)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if res.Valid {
		t.Fatalf("expected invalid chain after tampering, got valid")
	}
	if res.BrokenID != row2ID {
		t.Fatalf("want broken row id %q, got %q (res: %+v)", row2ID, res.BrokenID, res)
	}
}

func TestComputeHashDeterminism(t *testing.T) {
	h1 := audit.ComputeHash("prev", "id1", "cg1", "ACTION", "ent1", "details", "2026-10-21 12:00:00")
	h2 := audit.ComputeHash("prev", "id1", "cg1", "ACTION", "ent1", "details", "2026-10-21 12:00:00")
	if h1 != h2 {
		t.Fatalf("expected deterministic hash: %s != %s", h1, h2)
	}

	h3 := audit.ComputeHash("prev", "id1", "cg1", "ACTION", "ent1", "different details", "2026-10-21 12:00:00")
	if h1 == h3 {
		t.Fatalf("expected different hash for different details")
	}
}
