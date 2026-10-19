package audit

import (
	"database/sql"
	"log"

	"github.com/google/uuid"
)

// Log writes an audit log entry for compliance and accountability.
func Log(database *sql.DB, caregiverID, action, entityType, entityID, details string) {
	id := uuid.New().String()
	_, err := database.Exec(`
		INSERT INTO audit_logs (id, caregiver_id, action, entity_type, entity_id, details, created_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
	`, id, caregiverID, action, entityType, entityID, details)
	if err != nil {
		log.Printf("[AUDIT] Failed to record audit log: %v", err)
	}
}
