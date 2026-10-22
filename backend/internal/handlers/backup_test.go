package handlers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
)

// Issue #51: the backend takes a WAL-consistent snapshot for the host's
// off-site backup, and tells the Super Admin when backups stop working.

const testBackupSecret = "0123456789abcdef0123456789abcdef-test"

func backupFixture(t *testing.T) (*handlers.BackupHandler, *handlers.MedicationHandler, string) {
	t.Helper()
	med, _, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	dir := filepath.Join(t.TempDir(), "backup")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret"), []byte(testBackupSecret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &handlers.BackupHandler{DB: med.DB, Dir: dir}, med, dir
}

func internalCall(fn http.HandlerFunc, remote, secret string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/internal/backup/x", nil)
	req.RemoteAddr = remote
	if secret != "" {
		req.Header.Set("X-Backup-Secret", secret)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	fn(rec, req)
	return rec
}

type snapshotResp struct {
	File      string           `json:"file"`
	Integrity string           `json:"integrity"`
	Counts    map[string]int64 `json:"counts"`
}

func TestBackupSnapshotIncludesLatestWrite(t *testing.T) {
	h, med, dir := backupFixture(t)
	// Committed one statement before the snapshot; still only in the WAL.
	res := postDose(med, siblings[2], handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: today(), Status: "taken"})
	if res.code != http.StatusCreated {
		t.Fatalf("log dose: %d %s", res.code, res.raw)
	}

	rec := internalCall(h.Snapshot, "127.0.0.1:50000", testBackupSecret, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("snapshot: %d %s", rec.Code, rec.Body.String())
	}
	var s snapshotResp
	json.NewDecoder(rec.Body).Decode(&s)
	if s.Integrity != "ok" || s.Counts["medication_logs"] != 1 {
		t.Fatalf("unexpected snapshot result %+v", s)
	}

	// Open the staged file independently: integrity ok and the dose is there.
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, s.File)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var integrity string
	var n int
	db.QueryRow("PRAGMA integrity_check").Scan(&integrity)
	db.QueryRow("SELECT COUNT(*) FROM medication_logs WHERE medication_id = 'med_aspirin'").Scan(&n)
	if integrity != "ok" || n != 1 {
		t.Errorf("staged copy: integrity %q, aspirin rows %d", integrity, n)
	}

	// A second snapshot replaces the first.
	if rec := internalCall(h.Snapshot, "127.0.0.1:50000", testBackupSecret, nil); rec.Code != http.StatusOK {
		t.Errorf("second snapshot: %d %s", rec.Code, rec.Body.String())
	}
}

func TestBackupInternalEndpointsGuard(t *testing.T) {
	h, _, _ := backupFixture(t)
	cases := []struct {
		name, remote, secret string
		headers              map[string]string
		want                 int
	}{
		{"no secret", "127.0.0.1:1", "", nil, http.StatusForbidden},
		{"wrong secret", "127.0.0.1:1", "x" + testBackupSecret, nil, http.StatusForbidden},
		{"public address", "203.0.113.9:1", testBackupSecret, nil, http.StatusForbidden},
		{"came through the tunnel", "127.0.0.1:1", testBackupSecret, map[string]string{"Cf-Connecting-Ip": "203.0.113.9"}, http.StatusForbidden},
		{"came through the proxy", "172.18.0.3:1", testBackupSecret, map[string]string{"X-Forwarded-For": "203.0.113.9"}, http.StatusForbidden},
		{"host via Docker bridge", "172.18.0.1:1", testBackupSecret, nil, http.StatusOK},
		{"loopback", "127.0.0.1:1", testBackupSecret, nil, http.StatusOK},
	}
	for _, c := range cases {
		if rec := internalCall(h.Snapshot, c.remote, c.secret, c.headers); rec.Code != c.want {
			t.Errorf("%s: expected %d, got %d", c.name, c.want, rec.Code)
		}
		if c.want == http.StatusForbidden {
			if rec := internalCall(h.Verify, c.remote, c.secret, c.headers); rec.Code != http.StatusForbidden {
				t.Errorf("verify %s: expected 403, got %d", c.name, rec.Code)
			}
		}
	}

	// A short secret file never unlocks anything.
	os.WriteFile(filepath.Join(h.Dir, "secret"), []byte("short"), 0o600)
	if rec := internalCall(h.Snapshot, "127.0.0.1:1", "short", nil); rec.Code != http.StatusForbidden {
		t.Errorf("short secret: expected 403, got %d", rec.Code)
	}
}

func TestBackupSnapshotWhileDosesAreLogged(t *testing.T) {
	h, med, _ := backupFixture(t)
	// 20 different doses (2 slots x 10 days) logged while snapshots run.
	var wg sync.WaitGroup
	results := make([]doseResult, 20)
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			// Skipped doses on past days: 20 separate rows, no interval lock.
			d := time.Now().In(handlers.ClinicalLocation()).AddDate(0, 0, -(i/2 + 1))
			slot := []string{"morning", "evening"}[i%2]
			results[i] = postDose(med, siblings[i%3], handlers.LogDoseRequest{MedicationID: "med_citicoline", Slot: slot, ScheduledDate: d.Format("2006-01-02"), Status: "skipped"})
		}(i)
	}
	snaps := make([]int, 3)
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := range snaps {
			snaps[i] = internalCall(h.Snapshot, "127.0.0.1:1", testBackupSecret, nil).Code
		}
	}()
	close(start)
	wg.Wait()

	for i, r := range results {
		low := strings.ToLower(r.raw)
		if strings.Contains(low, "busy") || strings.Contains(low, "locked") || r.code != http.StatusCreated {
			t.Errorf("dose %d: %d %s", i, r.code, r.raw)
		}
	}
	for i, c := range snaps {
		if c != http.StatusOK {
			t.Errorf("snapshot %d: %d", i, c)
		}
	}
	var n int
	med.DB.QueryRow("SELECT COUNT(*) FROM medication_logs").Scan(&n)
	if n != 20 {
		t.Errorf("expected 20 doses, got %d", n)
	}
}

func TestBackupVerifyRestoredCopy(t *testing.T) {
	h, _, dir := backupFixture(t)
	rec := internalCall(h.Snapshot, "127.0.0.1:1", testBackupSecret, nil)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	// No restored copy yet: a clear refusal, not a crash.
	if rec := internalCall(h.Verify, "127.0.0.1:1", testBackupSecret, nil); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("missing restore: expected 422, got %d", rec.Code)
	}
	os.MkdirAll(filepath.Join(dir, "restore-test"), 0o755)
	src, _ := os.Open(filepath.Join(dir, "staging", "birrcare.db"))
	dst, _ := os.Create(filepath.Join(dir, "restore-test", "birrcare.db"))
	io.Copy(dst, src)
	src.Close()
	dst.Close()
	rec = internalCall(h.Verify, "127.0.0.1:1", testBackupSecret, nil)
	var v snapshotResp
	json.NewDecoder(rec.Body).Decode(&v)
	if rec.Code != http.StatusOK || v.Integrity != "ok" {
		t.Errorf("verify: %d %+v", rec.Code, v)
	}
}

func writeBackupStatus(t *testing.T, dir string, st map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(dir, "status.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func adminCall(fn http.HandlerFunc, c *models.Caregiver, method string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/admin/backup/x", nil)
	req = req.WithContext(authContext(req.Context(), c))
	rec := httptest.NewRecorder()
	fn(rec, req)
	return rec
}

func TestBackupStatusStaleRaisesSuperAdminAlert(t *testing.T) {
	h, med, dir := backupFixture(t)
	writeBackupStatus(t, dir, map[string]interface{}{
		"status": "success", "last_success_at": time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339),
		"destinations": []map[string]string{{"name": "gdrive"}},
	})

	// Caregiver and admin: no access to /admin/backup/*.
	for _, c := range []*models.Caregiver{siblings[1], siblings[2]} {
		for _, fn := range []http.HandlerFunc{h.GetStatus, h.Run} {
			if rec := adminCall(fn, c, http.MethodGet); rec.Code != http.StatusForbidden {
				t.Errorf("%s: expected 403, got %d", c.Role, rec.Code)
			}
		}
	}

	rec := adminCall(h.GetStatus, siblings[0], http.MethodGet)
	var st handlers.BackupStatus
	json.NewDecoder(rec.Body).Decode(&st)
	if rec.Code != http.StatusOK || !st.Stale || st.Reason != "stale" || !st.Configured {
		t.Fatalf("expected stale status, got %d %+v", rec.Code, st)
	}

	var target string
	var n int
	med.DB.QueryRow(`SELECT COUNT(*), COALESCE(MAX(target_caregiver_id), '') FROM alerts WHERE kind = 'backup_failed'`).Scan(&n, &target)
	if n != 1 || target != "cg_hussain" {
		t.Fatalf("expected one alert for the Super Admin, got %d for %q", n, target)
	}
	// Checking again the same day does not repeat it.
	adminCall(h.GetStatus, siblings[0], http.MethodGet)
	med.DB.QueryRow(`SELECT COUNT(*) FROM alerts WHERE kind = 'backup_failed'`).Scan(&n)
	if n != 1 {
		t.Errorf("alert repeated: %d", n)
	}
}

func TestBackupStatusReasons(t *testing.T) {
	h, med, dir := backupFixture(t)
	now := time.Now().UTC().Format(time.RFC3339)
	f := false
	for _, c := range []struct {
		st     map[string]interface{}
		reason string
		alert  bool
	}{
		{nil, "not_set_up", false},
		{map[string]interface{}{"status": "success", "last_success_at": now}, "ok", false},
		{map[string]interface{}{"status": "failed", "last_success_at": now, "error": "network"}, "failed", false},
		{map[string]interface{}{"status": "success", "last_success_at": now, "last_restore_test_ok": f}, "restore_failed", true},
		{map[string]interface{}{"status": "failed", "last_success_at": now, "error_code": "invalid_grant"}, "invalid_grant", true},
	} {
		os.Remove(filepath.Join(dir, "status.json"))
		med.DB.Exec(`DELETE FROM alerts`)
		if c.st != nil {
			writeBackupStatus(t, dir, c.st)
		}
		rec := adminCall(h.GetStatus, siblings[0], http.MethodGet)
		var st handlers.BackupStatus
		json.NewDecoder(rec.Body).Decode(&st)
		var n int
		med.DB.QueryRow(`SELECT COUNT(*) FROM alerts WHERE kind = 'backup_failed'`).Scan(&n)
		if st.Reason != c.reason || (n == 1) != c.alert {
			t.Errorf("%v: reason %q (want %q), alerts %d (want alert %v)", c.st, st.Reason, c.reason, n, c.alert)
		}
	}
}

func TestBackupRunWritesRequestOnce(t *testing.T) {
	h, med, dir := backupFixture(t)
	if rec := adminCall(h.Run, siblings[0], http.MethodPost); rec.Code != http.StatusAccepted {
		t.Fatalf("run: %d %s", rec.Code, rec.Body.String())
	}
	b, err := os.ReadFile(filepath.Join(dir, "request.json"))
	if err != nil || !strings.Contains(string(b), `"action": "backup"`) {
		t.Fatalf("request.json: %v %s", err, b)
	}
	if rec := adminCall(h.Run, siblings[0], http.MethodPost); rec.Code != http.StatusConflict {
		t.Errorf("second run while pending: expected 409, got %d", rec.Code)
	}
	var n int
	med.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'BACKUP_REQUESTED'`).Scan(&n)
	if n != 1 {
		t.Errorf("expected one audit row, got %d", n)
	}
}

var _ = context.Background
