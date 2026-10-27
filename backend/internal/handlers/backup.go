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
	"sort"
	"strings"
	"sync"
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

	mu        sync.Mutex
	postponed *BackupPostponed // the current slot was put off because the app was busy
}

// ---------------------------------------------------------------------------
// Schedule: the Super Admin picks the times; the backend decides when one is
// due, so household time and "is the app in use" come from the app itself.
// ---------------------------------------------------------------------------

// BackupSchedule is stored in app_settings under "backup_schedule".
type BackupSchedule struct {
	Times     []string `json:"times"`      // "HH:MM", household time
	AvoidBusy bool     `json:"avoid_busy"` // wait while the family is using the app
	// Retention: "history" keeps older copies to go back to (last 8, daily 30,
	// weekly 12, monthly 12); "latest" keeps only the newest copy.
	Retention string `json:"retention"`
}

const (
	RetentionHistory = "history"
	RetentionLatest  = "latest"
)

type BackupPostponed struct {
	Slot   string `json:"slot"`   // RFC3339
	Reason string `json:"reason"` // in_use | crisis
	At     string `json:"at"`
}

var defaultBackupSchedule = BackupSchedule{Times: []string{"03:00", "09:00", "15:00", "21:00"}, AvoidBusy: true, Retention: RetentionHistory}

const (
	backupMaxTimes = 6
	// Someone saved something this recently: the app is in use.
	backupBusyWindow = 10 * time.Minute
	// A backup is never put off for longer than this.
	backupMaxDelay = 2 * time.Hour
	// After a failed attempt, wait this long before trying the same slot again.
	backupRetryAfter = 30 * time.Minute
)

// normalizeBackupTimes validates, sorts and de-duplicates "HH:MM" times.
func normalizeBackupTimes(times []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, t := range times {
		t = strings.TrimSpace(t)
		parsed, err := time.Parse("15:04", t)
		if err != nil {
			return nil, fmt.Errorf("%q is not a time like 03:00", t)
		}
		t = parsed.Format("15:04")
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	if len(out) == 0 || len(out) > backupMaxTimes {
		return nil, fmt.Errorf("choose between 1 and %d backup times a day", backupMaxTimes)
	}
	sort.Strings(out)
	return out, nil
}

func (h *BackupHandler) loadSchedule(ctx context.Context) BackupSchedule {
	var raw string
	if err := h.DB.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = 'backup_schedule'`).Scan(&raw); err != nil {
		return defaultBackupSchedule
	}
	var sch BackupSchedule
	if json.Unmarshal([]byte(raw), &sch) != nil {
		return defaultBackupSchedule
	}
	if sch.Retention != RetentionLatest {
		sch.Retention = RetentionHistory
	}
	if times, err := normalizeBackupTimes(sch.Times); err == nil {
		sch.Times = times
		return sch
	}
	return defaultBackupSchedule
}

// backupSlots returns the scheduled moments of the given day.
func backupSlots(day time.Time, loc *time.Location, times []string) []time.Time {
	y, m, d := day.In(loc).Date()
	var out []time.Time
	for _, t := range times {
		hm, err := time.Parse("15:04", t)
		if err != nil {
			continue
		}
		out = append(out, time.Date(y, m, d, hm.Hour(), hm.Minute(), 0, 0, loc))
	}
	return out
}

// lastBackupSlot is the latest scheduled moment at or before now.
func lastBackupSlot(now time.Time, loc *time.Location, times []string) time.Time {
	var best time.Time
	for _, day := range []time.Time{now.AddDate(0, 0, -1), now} {
		for _, s := range backupSlots(day, loc, times) {
			if !s.After(now) && s.After(best) {
				best = s
			}
		}
	}
	return best
}

// nextBackupSlot is the first scheduled moment after now.
func nextBackupSlot(now time.Time, loc *time.Location, times []string) time.Time {
	var best time.Time
	for _, day := range []time.Time{now, now.AddDate(0, 0, 1)} {
		for _, s := range backupSlots(day, loc, times) {
			if s.After(now) && (best.IsZero() || s.Before(best)) {
				best = s
			}
		}
	}
	return best
}

// busyReason says why now is a bad moment: someone saved something in the
// last few minutes, or an emergency alert is still open.
func (h *BackupHandler) busyReason(ctx context.Context) string {
	var n int
	_ = h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_logs WHERE datetime(created_at) >= datetime('now', ?)`,
		fmt.Sprintf("-%d minutes", int(backupBusyWindow.Minutes()))).Scan(&n)
	if n > 0 {
		return "in_use"
	}
	_ = h.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM alerts a
		WHERE `+models.EmergencyAlertWhereSQL()+`
		  AND datetime(a.created_at) >= datetime('now', '-30 minutes')
		  AND NOT EXISTS (SELECT 1 FROM alert_acks k WHERE k.alert_id = a.id)`).Scan(&n)
	if n > 0 {
		return "crisis"
	}
	return ""
}

type backupDue struct {
	Due    bool   `json:"due"`
	Slot   string `json:"slot,omitempty"`
	Reason string `json:"reason"` // due | no_slot | done | retry_later | in_use | crisis
}

// decideBackup: is a scheduled backup due now? Pure, for tests.
func decideBackup(now time.Time, loc *time.Location, sch BackupSchedule, st *BackupStatusFile, busy string) backupDue {
	slot := lastBackupSlot(now, loc, sch.Times)
	if slot.IsZero() {
		return backupDue{Reason: "no_slot"}
	}
	res := backupDue{Slot: slot.UTC().Format(time.RFC3339)}
	if st != nil {
		if t, err := time.Parse(time.RFC3339, st.LastSuccessAt); err == nil && !t.Before(slot) {
			res.Reason = "done"
			return res
		}
		if t, err := time.Parse(time.RFC3339, st.LastAttemptAt); err == nil && !t.Before(slot) && now.Sub(t) < backupRetryAfter {
			res.Reason = "retry_later"
			return res
		}
	}
	if sch.AvoidBusy && busy != "" && now.Sub(slot) < backupMaxDelay {
		res.Reason = busy
		return res
	}
	res.Due, res.Reason = true, "due"
	return res
}

// Due: POST /internal/backup/due — the host asks every few minutes.
func (h *BackupHandler) Due(w http.ResponseWriter, r *http.Request) {
	if !h.internalGuard(w, r) {
		return
	}
	sch := h.loadSchedule(r.Context())
	busy := ""
	if sch.AvoidBusy {
		busy = h.busyReason(r.Context())
	}
	st, _ := h.readStatus()
	res := decideBackup(time.Now(), ClinicalLocation(), sch, st, busy)
	h.mu.Lock()
	switch res.Reason {
	case "in_use", "crisis":
		h.postponed = &BackupPostponed{Slot: res.Slot, Reason: res.Reason, At: time.Now().UTC().Format(time.RFC3339)}
	case "due", "done":
		h.postponed = nil
	}
	h.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

type BackupDestination struct {
	Name          string `json:"name"`
	LastSuccessAt string `json:"last_success_at,omitempty"`
	SnapshotID    string `json:"snapshot_id,omitempty"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	Snapshots     int    `json:"snapshots,omitempty"` // copies kept there, to go back to
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
	Schedule   BackupSchedule    `json:"schedule"`
	NextAt     string            `json:"next_at,omitempty"`
	Postponed  *BackupPostponed  `json:"postponed,omitempty"`
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
		// What the host keeps after this backup (Menu → Server).
		"retention":  h.loadSchedule(r.Context()).Retention,
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
	res := BackupStatus{Configured: ok, Stale: stale, Reason: reason, Pending: pending, Status: st}
	res.Schedule = h.loadSchedule(context.Background())
	if next := nextBackupSlot(time.Now(), ClinicalLocation(), res.Schedule.Times); !next.IsZero() {
		res.NextAt = next.UTC().Format(time.RFC3339)
	}
	h.mu.Lock()
	if p := h.postponed; p != nil {
		// Only while that slot is still the current one and not yet backed up.
		cur := lastBackupSlot(time.Now(), ClinicalLocation(), res.Schedule.Times)
		done := false
		if st != nil {
			if t, err := time.Parse(time.RFC3339, st.LastSuccessAt); err == nil && !cur.IsZero() && !t.Before(cur) {
				done = true
			}
		}
		if !done && !cur.IsZero() && p.Slot == cur.UTC().Format(time.RFC3339) {
			res.Postponed = p
		}
	}
	h.mu.Unlock()
	return res
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

// SetSchedule: PUT /admin/backup/schedule {times, avoid_busy}
func (h *BackupHandler) SetSchedule(w http.ResponseWriter, r *http.Request) {
	me := superAdmin(w, r)
	if me == nil {
		return
	}
	var req BackupSchedule
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	times, err := normalizeBackupTimes(req.Times)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_TIMES", err.Error())
		return
	}
	req.Times = times
	switch req.Retention {
	case "":
		req.Retention = RetentionHistory
	case RetentionHistory, RetentionLatest:
	default:
		writeJSONError(w, http.StatusBadRequest, "INVALID_RETENTION", `retention must be "history" or "latest"`)
		return
	}
	raw, _ := json.Marshal(req)
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to begin transaction", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO app_settings (key, value) VALUES ('backup_schedule', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, string(raw)); err != nil {
		internalError(w, "failed saving backup schedule", err)
		return
	}
	if err := audit.Record(tx, me.ID, "BACKUP_SCHEDULE_CHANGED", "backup", "schedule",
		fmt.Sprintf("times %s, avoid busy %v, keep %s", strings.Join(times, " "), req.AvoidBusy, req.Retention)); err != nil {
		internalError(w, "failed to write audit log", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed saving backup schedule", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
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
