package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"
)

// Updates are offered to the Super Admin and installed only on request
// (issue #43). The backend never deploys anything itself: it has no Docker
// socket and runs no git. It exchanges small JSON files with the host updater
// (scripts/auto-deploy.sh) through the data volume they already share, and the
// host re-checks every request before acting:
//
//	available.json  host → app   what is running, what is offered, the commits
//	policy.json     app  → host  {mode: manual|auto, skipped_sha}
//	request.json    app  → host  {action: install, target_sha, ...}
//	result.json     host → app   how the last install went
const (
	updateAvailableFile = "available.json"
	updatePolicyFile    = "policy.json"
	updateRequestFile   = "request.json"
	updateProcessing    = "request.processing"
	updateResultFile    = "result.json"

	// The host checks every 5 minutes; after 30 minutes without a check the
	// updater is not running.
	updateCheckerStaleAfter = 30 * time.Minute
	// Unacknowledged crisis alerts this recent block an install.
	updateCrisisWindow = 30 * time.Minute
)

type UpdateCommit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
}

type UpdateAvailable struct {
	CurrentSHA      string         `json:"current_sha"`
	TargetSHA       string         `json:"target_sha"`
	CI              string         `json:"ci"`
	CheckedAt       string         `json:"checked_at"`
	Commits         []UpdateCommit `json:"commits"`
	ChangesDatabase bool           `json:"changes_database"`
}

type UpdatePolicy struct {
	Mode       string `json:"mode"` // "manual" (default) or "auto"
	SkippedSHA string `json:"skipped_sha,omitempty"`
}

type UpdateRequest struct {
	Action      string `json:"action"`
	TargetSHA   string `json:"target_sha"`
	RequestedBy string `json:"requested_by"`
	RequestedAt string `json:"requested_at"`
}

type UpdateResult struct {
	SHA        string `json:"sha"`
	Status     string `json:"status"` // success | failed | rejected
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
	LogTail    string `json:"log_tail,omitempty"`
}

type UpdateStatus struct {
	Version         string           `json:"version"`
	Available       *UpdateAvailable `json:"available"`
	UpdateAvailable bool             `json:"update_available"`
	Result          *UpdateResult    `json:"result"`
	Policy          UpdatePolicy     `json:"policy"`
	Pending         bool             `json:"pending"`
	CheckerStale    bool             `json:"checker_stale"`
}

// Version is the running commit (short SHA) baked into the image by deploy.sh,
// or "dev" for a local build.
func Version() string {
	v := strings.TrimSpace(os.Getenv("GIT_SHA"))
	if v == "" {
		return "dev"
	}
	if len(v) > 7 {
		v = v[:7]
	}
	return v
}

// UpdateDirFromEnv is UPDATE_DIR, else "update" next to the database.
func UpdateDirFromEnv(dbPath string) string {
	if d := os.Getenv("UPDATE_DIR"); d != "" {
		return d
	}
	return filepath.Join(filepath.Dir(dbPath), "update")
}

type UpdatesHandler struct {
	DB     *sql.DB
	Broker *sse.Broker
	Dir    string
}

// One install / skip / policy write at a time.
var updateMu sync.Mutex

// writeAtomicHook writes the bytes of a temp file; tests replace it to fail midway.
var writeAtomicHook = func(f *os.File, data []byte) error {
	_, err := f.Write(data)
	return err
}

// writeJSONAtomic writes v to dir/name through a temp file and a rename, so the
// host never sees a half-written file.
func writeJSONAtomic(dir, name string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(dir); errors.Is(statErr, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o777); err != nil {
			return err
		}
		// The backend runs as root in Docker; the host updater (a normal user)
		// must still be able to rename and remove files in here.
		_ = os.Chmod(dir, 0o777)
	}
	f, err := os.CreateTemp(dir, "."+name+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	fail := func(err error) error {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := writeAtomicHook(f, append(data, '\n')); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	_ = os.Chmod(tmp, 0o666)
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// readUpdateJSON reads dir/name into v; a missing file returns false, nil.
func readUpdateJSON(dir, name string, v interface{}) (bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, err
	}
	return true, nil
}

func (h *UpdatesHandler) exists(name string) bool {
	_, err := os.Stat(filepath.Join(h.Dir, name))
	return err == nil
}

func (h *UpdatesHandler) readPolicy() UpdatePolicy {
	var p UpdatePolicy
	if ok, _ := readUpdateJSON(h.Dir, updatePolicyFile, &p); !ok || (p.Mode != "auto" && p.Mode != "manual") {
		// No policy file: manual. Nothing installs unless the Super Admin asks.
		p.Mode = "manual"
	}
	return p
}

func (h *UpdatesHandler) readAvailable() *UpdateAvailable {
	var a UpdateAvailable
	if ok, err := readUpdateJSON(h.Dir, updateAvailableFile, &a); !ok || err != nil {
		return nil
	}
	return &a
}

func (h *UpdatesHandler) status() UpdateStatus {
	st := UpdateStatus{Version: Version(), Policy: h.readPolicy()}
	st.Available = h.readAvailable()
	st.Pending = h.exists(updateRequestFile) || h.exists(updateProcessing)
	st.CheckerStale = true
	if st.Available != nil {
		if t, err := time.Parse(time.RFC3339, st.Available.CheckedAt); err == nil && time.Since(t) <= updateCheckerStaleAfter {
			st.CheckerStale = false
		}
		st.UpdateAvailable = st.Available.TargetSHA != "" && st.Available.CI == "success" &&
			!strings.HasPrefix(st.Available.TargetSHA, st.Version)
	}
	var res UpdateResult
	if ok, _ := readUpdateJSON(h.Dir, updateResultFile, &res); ok {
		st.Result = &res
	}
	return st
}

// superAdmin returns the caller, or writes 403 and returns nil.
func superAdmin(w http.ResponseWriter, r *http.Request) *models.Caregiver {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageUsers(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only the Super Admin can manage updates")
		return nil
	}
	return me
}

// GetStatus: GET /admin/update/status
func (h *UpdatesHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	if superAdmin(w, r) == nil {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.status())
}

type installRequest struct {
	TargetSHA          string `json:"target_sha"`
	ConfirmDuringAlert bool   `json:"confirm_during_alert"`
}

// activeCrisisAlerts counts crisis / low-BP / F.A.S.T. alerts from the last 30
// minutes that nobody has acknowledged yet.
func (h *UpdatesHandler) activeCrisisAlerts(ctx context.Context) (int, error) {
	var n int
	err := h.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM alerts a
		WHERE a.kind IN ('bp_crisis', 'bp_low', 'fast_positive')
		  AND datetime(a.created_at) >= datetime('now', ?)
		  AND NOT EXISTS (SELECT 1 FROM alert_acks k WHERE k.alert_id = a.id)
	`, fmt.Sprintf("-%d minutes", int(updateCrisisWindow.Minutes()))).Scan(&n)
	return n, err
}

// Install: POST /admin/update/install {target_sha, confirm_during_alert}
func (h *UpdatesHandler) Install(w http.ResponseWriter, r *http.Request) {
	me := superAdmin(w, r)
	if me == nil {
		return
	}
	var req installRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.TargetSHA) == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "target_sha is required")
		return
	}

	updateMu.Lock()
	defer updateMu.Unlock()

	avail := h.readAvailable()
	if avail == nil || avail.TargetSHA == "" || avail.CI != "success" || req.TargetSHA != avail.TargetSHA {
		writeJSONError(w, http.StatusConflict, "STALE_TARGET", "this version is no longer the one on offer; refresh and try again")
		return
	}
	if h.exists(updateRequestFile) || h.exists(updateProcessing) {
		writeJSONError(w, http.StatusConflict, "IN_PROGRESS", "an update is already being installed")
		return
	}
	n, err := h.activeCrisisAlerts(r.Context())
	if err != nil {
		internalError(w, "failed checking active alerts", err)
		return
	}
	if n > 0 && !req.ConfirmDuringAlert {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":         "ACTIVE_CRISIS",
			"message":       "an emergency alert from the last 30 minutes has not been acknowledged",
			"active_alerts": n,
		})
		return
	}

	if err := writeJSONAtomic(h.Dir, updateRequestFile, UpdateRequest{
		Action:      "install",
		TargetSHA:   req.TargetSHA,
		RequestedBy: me.ID,
		RequestedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		internalError(w, "failed writing update request", err)
		return
	}
	detail := fmt.Sprintf("requested install of %s (%d changes)", short(req.TargetSHA), len(avail.Commits))
	if n > 0 {
		detail += fmt.Sprintf("; confirmed during %d unacknowledged alert(s)", n)
	}
	if err := audit.Record(h.DB, me.ID, "UPDATE_INSTALL_REQUESTED", "update", req.TargetSHA, detail); err != nil {
		// No audit row, no install.
		os.Remove(filepath.Join(h.Dir, updateRequestFile))
		internalError(w, "failed to write audit log", err)
		return
	}

	h.Broker.Broadcast("UPDATE_STARTING", models.ActorInfo{ID: me.ID, Name: me.Name, AvatarColor: me.AvatarColor}, map[string]interface{}{
		"target_sha": short(req.TargetSHA),
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted", "target_sha": req.TargetSHA})
}

// Skip: POST /admin/update/skip {target_sha}
func (h *UpdatesHandler) Skip(w http.ResponseWriter, r *http.Request) {
	me := superAdmin(w, r)
	if me == nil {
		return
	}
	var req struct {
		TargetSHA string `json:"target_sha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.TargetSHA) == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "target_sha is required")
		return
	}

	updateMu.Lock()
	defer updateMu.Unlock()

	avail := h.readAvailable()
	if avail == nil || avail.TargetSHA != req.TargetSHA {
		writeJSONError(w, http.StatusConflict, "STALE_TARGET", "this version is no longer the one on offer; refresh and try again")
		return
	}
	p := h.readPolicy()
	p.SkippedSHA = req.TargetSHA
	h.writePolicy(w, me, p, "UPDATE_SKIPPED", req.TargetSHA, fmt.Sprintf("skipped %s", short(req.TargetSHA)))
}

// SetPolicy: PUT /admin/update/policy {mode}
func (h *UpdatesHandler) SetPolicy(w http.ResponseWriter, r *http.Request) {
	me := superAdmin(w, r)
	if me == nil {
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Mode != "manual" && req.Mode != "auto") {
		writeJSONError(w, http.StatusBadRequest, "INVALID_MODE", `mode must be "manual" or "auto"`)
		return
	}

	updateMu.Lock()
	defer updateMu.Unlock()

	p := h.readPolicy()
	p.Mode = req.Mode
	h.writePolicy(w, me, p, "UPDATE_POLICY_CHANGED", "policy", fmt.Sprintf("automatic updates: %s", req.Mode))
}

func (h *UpdatesHandler) writePolicy(w http.ResponseWriter, me *models.Caregiver, p UpdatePolicy, action, entityID, detail string) {
	if err := audit.Record(h.DB, me.ID, action, "update", entityID, detail); err != nil {
		internalError(w, "failed to write audit log", err)
		return
	}
	if err := writeJSONAtomic(h.Dir, updatePolicyFile, p); err != nil {
		internalError(w, "failed writing update policy", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.status())
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// StartUpdateWatcher broadcasts UPDATE_AVAILABLE when the host offers a new
// version. Only the Super Admin's screen shows it.
func StartUpdateWatcher(ctx context.Context, dir string, broker *sse.Broker) {
	h := &UpdatesHandler{Dir: dir}
	last := ""
	if a := h.readAvailable(); a != nil {
		last = a.TargetSHA
	}
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a := h.readAvailable()
				if a == nil || a.TargetSHA == last {
					continue
				}
				last = a.TargetSHA
				if a.TargetSHA == "" || a.CI != "success" {
					continue
				}
				log.Printf("[UPDATE] %s is available (%d changes)", short(a.TargetSHA), len(a.Commits))
				broker.Broadcast("UPDATE_AVAILABLE", models.ActorInfo{}, map[string]interface{}{
					"target_sha": short(a.TargetSHA),
					"changes":    len(a.Commits),
				})
			}
		}
	}()
}
