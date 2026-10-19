package audit

import (
	"database/sql"
	"log"

	"github.com/google/uuid"
)

// Execer is satisfied by both *sql.DB and *sql.Tx. Passing the active *sql.Tx is
// REQUIRED when auditing inside a transaction: the pool has a single connection
// (SetMaxOpenConns(1)), so writing through *sql.DB while a Tx is open deadlocks.
type Execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// Record writes an audit log entry and returns any error, so callers can make the
// audit row part of the same atomic transaction as the clinical write.
func Record(database Execer, caregiverID, action, entityType, entityID, details string) error {
	id := uuid.New().String()
	_, err := database.Exec(`
		INSERT INTO audit_logs (id, caregiver_id, action, entity_type, entity_id, details, created_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
	`, id, caregiverID, action, entityType, entityID, details)
	return err
}

// Log writes an audit log entry for compliance and accountability (best effort).
func Log(database Execer, caregiverID, action, entityType, entityID, details string) {
	if err := Record(database, caregiverID, action, entityType, entityID, details); err != nil {
		log.Printf("[AUDIT] Failed to record audit log: %v", err)
	}
}
