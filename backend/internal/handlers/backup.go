package handlers

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/models"
)

// Off-site backups (issue #51). The host runs scripts/backup.sh (restic to
// Google Drive / R2 / B2 / USB); the backend never holds any cloud
// credentials. They talk through files in data/backup/, like the updater:
//
//	secret        host → app   shared secret for the internal endpoints
//	staging/      app  → host  the consistent snapshot to upload
//	restore-test/ host → app   the weekly test restore, checked by the app
//	status.json   host → app   how the last backup and restore test went
//	request.json  app  → host  "Back up now" from the Super Admin
//
// The snapshot is taken by the backend itself (VACUUM INTO on the single
// pooled connection), so it always includes the newest WAL content and the
// host needs no sqlite3.
const (
	backupSecretFile  = "secret"
	backupStagingDir  = "staging"
	backupRestoreDir  = "restore-test"
	backupStatusFile  = "status.json"
	backupRequestFile = "request.json"
	backupProcessing  = "request.processing"
	backupDBName      = "birrcare.db"
	backupSecretMin   = 32
	backupStaleAfter  = 24 * time.Hour
	// One reminder a day per reason is enough.
	backupAlertEvery = 24 * time.Hour
)

// BackupDirFromEnv is BACKUP_DIR, else "backup" next to the database.
func BackupDirFromEnv(dbPath string) string {
	if d := os.Getenv("BACKUP_DIR"); d != "" {
		return d
	}
	return filepath.Join(filepath.Dir(dbPath), "backup")
}

type BackupHandler struct {
	DB  *sql.DB
	Dir string
}

type BackupDestination struct {
	Name          string `json:"name"`
	LastSuccessAt string `json:"last_success_at,omitempty"`
	SnapshotID    string `json:"snapshot_id,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
}

// BackupStatusFile is what scripts/backup.sh writes.
type BackupStatusFile struct {
	LastSuccessAt     string              `json:"last_success_at,omitempty"`
	LastAttemptAt     string              `json:"last_attempt_at,omitempty"`
	Status            string              `json:"status"` // success | failed | running
	Destinations      []BackupDestination `json:"destinations"`
	LastRestoreTestAt string              `json:"last_restore_test_at,omitempty"`
	LastRestoreTestOK *bool               `json:"last_restore_test_ok,omitempty"`
	Error             string              `json:"error,omitempty"`
	ErrorCode         string              `json:"error_code,omitempty"` // e.g. invalid_grant
}

type BackupStatus struct {
	Configured bool              `json:"configured"`
	Stale      bool              `json:"stale"`
	Reason     string            `json:"reason"` // ok | not_set_up | stale | failed | restore_failed | invalid_grant
	Pending    bool              `json:"pending"`
	Status     *BackupStatusFile `json:"status"`
}

// ---------------------------------------------------------------------------
// Internal endpoints for the host script
// ---------------------------------------------------------------------------

// internalAllowed: only a direct call from this machine (loopback, or the
// Docker bridge when the host calls the published 127.0.0.1 port), never one
// that came through the Next.js proxy or the Cloudflare tunnel.
func internalAllowed(r *http.Request) bool {
	for _, h := range []string{"Cf-Connecting-Ip", "Cf-Access-Jwt-Assertion", "X-Forwarded-For", "X-Real-Ip"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func (h *BackupHandler) secretOK(r *http.Request) bool {
	want, err := os.ReadFile(filepath.Join(h.Dir, backupSecretFile))
	if err != nil {
		return false
	}
	w := strings.TrimSpace(string(want))
	got := strings.TrimSpace(r.Header.Get("X-Backup-Secret"))
	return len(w) >= backupSecretMin && subtle.ConstantTimeCompare([]byte(w), []byte(got)) == 1
}

func (h *BackupHandler) internalGuard(w http.ResponseWriter, r *http.Request) bool {
	if !internalAllowed(r) || !h.secretOK(r) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "not allowed")
		return false
	}
	return true
}

type backupCheck struct {
	Integrity string           `json:"integrity"`
	Counts    map[string]int64 `json:"counts"`
}

// checkBackupFile opens a database file read-only and reports its integrity
// and the row counts the restore test compares.
func checkBackupFile(ctx context.Context, path string) (backupCheck, error) {
	res := backupCheck{Counts: map[string]int64{}}
	if _, err := os.Stat(path); err != nil {
		return res, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return res, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&res.Integrity); err != nil {
		return res, err
	}
	for _, t := range []string{"medication_logs", "vitals_logs"} {
		var n int64
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+t).Scan(&n); err != nil {
			return res, err
		}
		res.Counts[t] = n
	}
	return res, nil
}

// ensureDir creates dir writable by the host user too (the backend runs as
// root in Docker; the host script must write status.json and restore-test/).
func ensureDir(dir string) error {
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o777); err != nil {
			return err
		}
		_ = os.Chmod(dir, 0o777)
	}
	return nil
}

// Snapshot: POST /internal/backup/snapshot → data/backup/staging/birrcare.db
func (h *BackupHandler) Snapshot(w http.ResponseWriter, r *http.Request) {
	if !h.internalGuard(w, r) {
		return
	}
	staging := filepath.Join(h.Dir, backupStagingDir)
	if err := ensureDir(h.Dir); err != nil {
		internalError(w, "failed creating backup folder", err)
		return
	}
	if err := ensureDir(staging); err != nil {
		internalError(w, "failed creating staging folder", err)
		return
	}
	out := filepath.Join(staging, backupDBName)
	for _, f := range []string{out, out + "-wal", out + "-shm", out + "-journal"} {
		_ = os.Remove(f)
	}
	// One pooled connection: writers wait in database/sql's queue for the
	// fraction of a second this takes, and never see SQLITE_BUSY.
	if _, err := h.DB.ExecContext(r.Context(), "VACUUM INTO ?", out); err != nil {
		internalError(w, "snapshot failed", err)
		return
	}
	_ = os.Chmod(out, 0o644)
	check, err := checkBackupFile(r.Context(), out)
	if err != nil {
		internalError(w, "snapshot check failed", err)
		return
	}
	info, _ := os.Stat(out)
	var size int64
	if info != nil {
		size = info.Size()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"file":       filepath.Join(backupStagingDir, backupDBName),
		"size_bytes": size,
		"integrity":  check.Integrity,
		"counts":     check.Counts,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	})
}

// Verify: POST /internal/backup/verify checks data/backup/restore-test/birrcare.db
// (the host restores the latest snapshot there for the weekly restore test).
func (h *BackupHandler) Verify(w http.ResponseWriter, r *http.Request) {
	if !h.internalGuard(w, r) {
		return
	}
	check, err := checkBackupFile(r.Context(), filepath.Join(h.Dir, backupRestoreDir, backupDBName))
	if err != nil {
		writeJSONError(w, http.StatusUnprocessableEntity, "UNREADABLE", "the restored file could not be opened: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(check)
}

// ---------------------------------------------------------------------------
// Super Admin endpoints
// ---------------------------------------------------------------------------

func (h *BackupHandler) readStatus() (*BackupStatusFile, bool) {
	var st BackupStatusFile
	ok, err := readUpdateJSON(h.Dir, backupStatusFile, &st)
	if !ok || err != nil {
		return nil, false
	}
	return &st, true
}

// evaluateBackup turns status.json into a reason. Only configured backups
// raise alerts; "not set up" is shown on the Backups card instead.
func evaluateBackup(st *BackupStatusFile, now time.Time) (stale bool, reason string) {
	if st == nil {
		return true, "not_set_up"
	}
	last, err := time.Parse(time.RFC3339, st.LastSuccessAt)
	stale = err != nil || now.Sub(last) > backupStaleAfter
	switch {
	case st.ErrorCode == "invalid_grant":
		return stale, "invalid_grant"
	case st.LastRestoreTestOK != nil && !*st.LastRestoreTestOK:
		return stale, "restore_failed"
	case stale:
		return true, "stale"
	case st.Status == "failed":
		return false, "failed"
	}
	return false, "ok"
}

func (h *BackupHandler) status() BackupStatus {
	st, ok := h.readStatus()
	stale, reason := evaluateBackup(st, time.Now())
	pending := false
	for _, f := range []string{backupRequestFile, backupProcessing} {
		if _, err := os.Stat(filepath.Join(h.Dir, f)); err == nil {
			pending = true
		}
	}
	return BackupStatus{Configured: ok, Stale: stale, Reason: reason, Pending: pending, Status: st}
}

// alertingReasons raise a Super-Admin-only alert.
var alertingReasons = map[string]bool{"stale": true, "restore_failed": true, "invalid_grant": true}

// ensureBackupAlert addresses one alert per Super Admin, at most once a day.
func ensureBackupAlert(ctx context.Context, db *sql.DB, s BackupStatus) error {
	if !s.Configured || !alertingReasons[s.Reason] {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT id FROM caregivers WHERE role = ? AND is_active = 1`, models.RoleSuperAdmin)
	if err != nil {
		return err
	}
	var admins []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			admins = append(admins, id)
		}
	}
	rows.Close()

	since := time.Now().UTC().Add(-backupAlertEvery).Format(sqliteTimeLayout)
	payload := map[string]interface{}{"reason": s.Reason}
	if s.Status != nil {
		payload["last_success_at"] = s.Status.LastSuccessAt
	}
	for _, id := range admins {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM alerts WHERE kind = 'backup_failed' AND target_caregiver_id = ? AND created_at >= ?`, id, since).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := recordAlertFor(tx, "backup_failed", "", "", id, payload); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// GetStatus: GET /admin/backup/status
func (h *BackupHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	if superAdmin(w, r) == nil {
		return
	}
	s := h.status()
	if err := ensureBackupAlert(r.Context(), h.DB, s); err != nil {
		log.Printf("[BACKUP] alert check failed: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

// Run: POST /admin/backup/run asks the host for a backup now.
func (h *BackupHandler) Run(w http.ResponseWriter, r *http.Request) {
	me := superAdmin(w, r)
	if me == nil {
		return
	}
	if h.status().Pending {
		writeJSONError(w, http.StatusConflict, "IN_PROGRESS", "a backup is already running")
		return
	}
	if err := ensureDir(h.Dir); err != nil {
		internalError(w, "failed creating backup folder", err)
		return
	}
	if err := writeJSONAtomic(h.Dir, backupRequestFile, map[string]string{
		"action":       "backup",
		"requested_by": me.ID,
		"requested_at": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		internalError(w, "failed writing backup request", err)
		return
	}
	if err := audit.Record(h.DB, me.ID, "BACKUP_REQUESTED", "backup", "", "Back up now"); err != nil {
		os.Remove(filepath.Join(h.Dir, backupRequestFile))
		internalError(w, "failed to write audit log", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(h.status())
}

// StartBackupWatcher raises the Super Admin's alert even when nobody opens the
// Backups card.
func StartBackupWatcher(ctx context.Context, db *sql.DB, dir string) {
	h := &BackupHandler{DB: db, Dir: dir}
	go func() {
		t := time.NewTicker(15 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := ensureBackupAlert(ctx, db, h.status()); err != nil {
					log.Printf("[BACKUP] alert check failed: %v", err)
				}
			}
		}
	}()
}

// String helps logs and tests.
func (s BackupStatus) String() string {
	return fmt.Sprintf("configured=%v stale=%v reason=%s pending=%v", s.Configured, s.Stale, s.Reason, s.Pending)
}
