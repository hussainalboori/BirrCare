package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// InitDB initializes SQLite in WAL mode with serialized write safety.
func InitDB(dbPath string) (*sql.DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	// _txlock=immediate makes every BEGIN acquire the write lock up front (honouring
	// busy_timeout), so a write transaction can never fail mid-way with SQLITE_BUSY
	// when a second process (backup job, sqlite3 CLI) touches the same WAL file.
	dsn := fmt.Sprintf("%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)&_txlock=immediate", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Mandatory for SQLite in concurrent web environments to eliminate race conditions
	// IMPORTANT: with a single connection, never issue a query on *sql.DB while a
	// *sql.Tx or an un-exhausted *sql.Rows is held - it will block forever.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	// Fail fast if WAL could not be enabled (e.g. unsupported filesystem mount).
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		db.Close()
		return nil, fmt.Errorf("sqlite journal_mode is %q, expected wal (err: %v)", mode, err)
	}

	return db, nil
}
