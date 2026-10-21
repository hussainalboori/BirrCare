package audit

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

// DBTX is satisfied by both *sql.DB and *sql.Tx. Passing the active *sql.Tx is
// REQUIRED when auditing inside a transaction: the pool has a single connection
// (SetMaxOpenConns(1)), so writing through *sql.DB while a Tx is open deadlocks.
type DBTX interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

// Execer is an alias for DBTX for backwards compatibility.
type Execer = DBTX

// ComputeHash calculates the sha256 hex digest for an audit log entry:
// row_hash = sha256(prev_hash || id || caregiver_id || action || entity_id || details || created_at)
func ComputeHash(prevHash, id, caregiverID, action, entityID, details, createdAt string) string {
	data := prevHash + id + caregiverID + action + entityID + details + createdAt
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

// Record writes an audit log entry with deterministic hash chaining and returns any
// error, so callers can make the audit row part of the same atomic transaction as the clinical write.
//
// Reading the last hash and inserting the new row must not interleave with another
// writer, or two rows chain from the same parent and the chain looks tampered. A
// *sql.Tx already holds the single pooled connection; a bare *sql.DB gets its own
// short transaction here.
func Record(database DBTX, caregiverID, action, entityType, entityID, details string) error {
	if db, ok := database.(*sql.DB); ok {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed starting audit transaction: %w", err)
		}
		if err := record(tx, caregiverID, action, entityType, entityID, details); err != nil {
			tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	return record(database, caregiverID, action, entityType, entityID, details)
}

func record(database DBTX, caregiverID, action, entityType, entityID, details string) error {
	id := uuid.New().String()
	now := time.Now().UTC().Format("2006-01-02 15:04:05")

	var prevHash string
	var lastHash sql.NullString
	// Under SQLite's single writer, getting the last row_hash is deterministic.
	err := database.QueryRow(`SELECT row_hash FROM audit_logs ORDER BY rowid DESC LIMIT 1`).Scan(&lastHash)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed fetching previous audit hash: %w", err)
	}
	if lastHash.Valid {
		prevHash = lastHash.String
	}

	rowHash := ComputeHash(prevHash, id, caregiverID, action, entityID, details, now)

	var cgArg any = caregiverID
	if caregiverID == "" {
		cgArg = nil
	}

	_, err = database.Exec(`
		INSERT INTO audit_logs (id, caregiver_id, action, entity_type, entity_id, details, created_at, prev_hash, row_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, cgArg, action, entityType, entityID, details, now, prevHash, rowHash)
	return err
}

// Log writes an audit log entry for compliance and accountability (best effort).
func Log(database DBTX, caregiverID, action, entityType, entityID, details string) {
	if err := Record(database, caregiverID, action, entityType, entityID, details); err != nil {
		log.Printf("[AUDIT] Failed to record audit log: %v", err)
	}
}
