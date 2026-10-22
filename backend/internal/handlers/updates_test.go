package handlers_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
)

// Issue #43: updates are offered to the Super Admin and installed on request.

func updatesFixture(t *testing.T) (*handlers.UpdatesHandler, *sql.DB, string) {
	t.Helper()
	database, broker, cleanup := setupSmartAuthTestDB(t)
	t.Cleanup(cleanup)
	createTestCaregiver(t, database, "sa1", "Hussain", "super_admin", "1234")
	createTestCaregiver(t, database, "ad1", "Fatima", "admin", "1234")
	createTestCaregiver(t, database, "cg1", "Omar", "caregiver", "1234")
	dir := filepath.Join(t.TempDir(), "update")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "available.json", map[string]interface{}{
		"current_sha": "8ecc00c0000000000000000000000000000000aa",
		"target_sha":  "4f2a91c0000000000000000000000000000000bb",
		"ci":          "success",
		"checked_at":  time.Now().UTC().Format(time.RFC3339),
		"commits": []map[string]string{
			{"sha": "4f2a91c", "subject": "fix(vitals): larger numbers", "date": "2026-10-21T10:00:00+03:00"},
		},
		"changes_database": true,
	})
	return &handlers.UpdatesHandler{DB: database, Broker: broker, Dir: dir}, database, dir
}

func writeFile(t *testing.T, dir, name string, v interface{}) {
	t.Helper()
	b, _ := json.Marshal(v)
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func asRole(id, role string) context.Context {
	return context.WithValue(context.Background(), auth.CaregiverContextKey, &models.Caregiver{ID: id, Name: id, Role: role})
}

func callUpdate(h http.HandlerFunc, ctx context.Context, method, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/admin/update/x", bytes.NewBufferString(body)).WithContext(ctx)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func auditCount(t *testing.T, db *sql.DB, action string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = ?`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

const target = "4f2a91c0000000000000000000000000000000bb"

func TestUpdateEndpointsAreSuperAdminOnly(t *testing.T) {
	h, db, dir := updatesFixture(t)
	endpoints := []struct {
		name   string
		fn     http.HandlerFunc
		method string
		body   string
	}{
		{"status", h.GetStatus, http.MethodGet, ""},
		{"install", h.Install, http.MethodPost, `{"target_sha":"` + target + `"}`},
		{"skip", h.Skip, http.MethodPost, `{"target_sha":"` + target + `"}`},
		{"policy", h.SetPolicy, http.MethodPut, `{"mode":"auto"}`},
	}
	for _, role := range []struct{ id, role string }{{"cg1", "caregiver"}, {"ad1", "admin"}} {
		for _, e := range endpoints {
			if rec := callUpdate(e.fn, asRole(role.id, role.role), e.method, e.body); rec.Code != http.StatusForbidden {
				t.Errorf("%s calling %s: expected 403, got %d", role.role, e.name, rec.Code)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "request.json")); err == nil {
		t.Fatal("a non-Super Admin must not be able to write request.json")
	}

	sa := asRole("sa1", "super_admin")
	rec := callUpdate(h.GetStatus, sa, http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: expected 200, got %d", rec.Code)
	}
	var st handlers.UpdateStatus
	json.NewDecoder(rec.Body).Decode(&st)
	if !st.UpdateAvailable || st.Policy.Mode != "manual" || st.CheckerStale || st.Available == nil || len(st.Available.Commits) != 1 {
		t.Errorf("unexpected status %+v", st)
	}

	if rec := callUpdate(h.SetPolicy, sa, http.MethodPut, `{"mode":"auto"}`); rec.Code != http.StatusOK {
		t.Fatalf("policy: expected 200, got %d", rec.Code)
	}
	if auditCount(t, db, "UPDATE_POLICY_CHANGED") != 1 {
		t.Error("policy change must be audited")
	}
	if rec := callUpdate(h.Skip, sa, http.MethodPost, `{"target_sha":"`+target+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("skip: expected 200, got %d", rec.Code)
	}
	if auditCount(t, db, "UPDATE_SKIPPED") != 1 {
		t.Error("skip must be audited")
	}
	var p handlers.UpdatePolicy
	b, _ := os.ReadFile(filepath.Join(dir, "policy.json"))
	json.Unmarshal(b, &p)
	if p.Mode != "auto" || p.SkippedSHA != target {
		t.Errorf("policy.json should keep mode and record the skip, got %+v", p)
	}

	if rec := callUpdate(h.Install, sa, http.MethodPost, `{"target_sha":"`+target+`"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("install: expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	if auditCount(t, db, "UPDATE_INSTALL_REQUESTED") != 1 {
		t.Error("install must be audited")
	}
}

func TestInstallRefusedDuringCrisisUnlessConfirmed(t *testing.T) {
	h, db, dir := updatesFixture(t)
	if _, err := db.Exec(`INSERT INTO alerts (id, kind, created_by, created_at) VALUES ('al1', 'bp_crisis', 'cg1', datetime('now', '-10 minutes'))`); err != nil {
		t.Fatal(err)
	}
	sa := asRole("sa1", "super_admin")

	rec := callUpdate(h.Install, sa, http.MethodPost, `{"target_sha":"`+target+`"}`)
	var body map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&body)
	if rec.Code != http.StatusConflict || body["error"] != "ACTIVE_CRISIS" {
		t.Fatalf("expected 409 ACTIVE_CRISIS, got %d %v", rec.Code, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "request.json")); err == nil {
		t.Fatal("request.json must not be written during an active crisis")
	}

	rec = callUpdate(h.Install, sa, http.MethodPost, `{"target_sha":"`+target+`","confirm_during_alert":true}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 with confirm_during_alert, got %d (%s)", rec.Code, rec.Body.String())
	}
	var req handlers.UpdateRequest
	b, err := os.ReadFile(filepath.Join(dir, "request.json"))
	if err != nil {
		t.Fatalf("request.json not written: %v", err)
	}
	json.Unmarshal(b, &req)
	if req.Action != "install" || req.TargetSHA != target || req.RequestedBy != "sa1" {
		t.Errorf("unexpected request.json %+v", req)
	}

	// A second install while that one is pending.
	rec = callUpdate(h.Install, sa, http.MethodPost, `{"target_sha":"`+target+`","confirm_during_alert":true}`)
	json.NewDecoder(rec.Body).Decode(&body)
	if rec.Code != http.StatusConflict || body["error"] != "IN_PROGRESS" {
		t.Errorf("expected 409 IN_PROGRESS, got %d %v", rec.Code, body)
	}
}

func TestInstallAcknowledgedOrOldAlertDoesNotBlock(t *testing.T) {
	h, db, _ := updatesFixture(t)
	db.Exec(`INSERT INTO alerts (id, kind, created_by, created_at) VALUES ('old', 'bp_crisis', 'cg1', datetime('now', '-45 minutes'))`)
	db.Exec(`INSERT INTO alerts (id, kind, created_by, created_at) VALUES ('ack', 'fast_positive', 'cg1', datetime('now', '-5 minutes'))`)
	db.Exec(`INSERT INTO alert_acks (alert_id, caregiver_id) VALUES ('ack', 'cg1')`)
	if rec := callUpdate(h.Install, asRole("sa1", "super_admin"), http.MethodPost, `{"target_sha":"`+target+`"}`); rec.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestInstallStaleTarget(t *testing.T) {
	h, _, _ := updatesFixture(t)
	rec := callUpdate(h.Install, asRole("sa1", "super_admin"), http.MethodPost, `{"target_sha":"deadbeef"}`)
	var body map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&body)
	if rec.Code != http.StatusConflict || body["error"] != "STALE_TARGET" {
		t.Errorf("expected 409 STALE_TARGET, got %d %v", rec.Code, body)
	}
}

func TestRequestJSONWrittenAtomically(t *testing.T) {
	h, _, dir := updatesFixture(t)
	restore := handlers.SetWriteAtomicHookForTest(func(f *os.File, data []byte) error {
		f.Write(data[:len(data)/2]) // half the file, then the disk "fails"
		return errors.New("disk full")
	})
	defer restore()

	rec := callUpdate(h.Install, asRole("sa1", "super_admin"), http.MethodPost, `{"target_sha":"`+target+`"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 when the write fails, got %d", rec.Code)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "available.json" {
			t.Errorf("no partial or temp file may be left behind, found %s", e.Name())
		}
	}
}

func TestHealthVersion(t *testing.T) {
	t.Setenv("GIT_SHA", "")
	if v := handlers.Version(); v != "dev" {
		t.Errorf("expected dev without GIT_SHA, got %q", v)
	}
	t.Setenv("GIT_SHA", "8ecc00c1a2b3c4d5e6f7")
	if v := handlers.Version(); v != "8ecc00c" {
		t.Errorf("expected short sha 8ecc00c, got %q", v)
	}
}

func TestInstallRefusedWhenUpdateNeedsASetting(t *testing.T) {
	h, _, dir := updatesFixture(t)
	b, _ := os.ReadFile(filepath.Join(dir, "available.json"))
	var a map[string]interface{}
	json.Unmarshal(b, &a)
	a["needs_config"] = []map[string]string{{"key": "WARD_NAME", "question_en": "Name of the care ward", "question_ar": "اسم جناح الرعاية"}}
	writeFile(t, dir, "available.json", a)

	rec := callUpdate(h.GetStatus, asRole("sa1", "super_admin"), http.MethodGet, "")
	var st handlers.UpdateStatus
	json.NewDecoder(rec.Body).Decode(&st)
	if !st.UpdateAvailable || len(st.Available.NeedsConfig) != 1 || st.Available.NeedsConfig[0].QuestionEn != "Name of the care ward" {
		t.Fatalf("status should offer the update with its needed setting: %+v", st.Available)
	}

	rec = callUpdate(h.Install, asRole("sa1", "super_admin"), http.MethodPost, `{"target_sha":"`+target+`"}`)
	var body map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&body)
	if rec.Code != http.StatusConflict || body["error"] != "NEEDS_CONFIG" || !strings.Contains(body["message"].(string), "WARD_NAME") {
		t.Fatalf("expected 409 NEEDS_CONFIG naming the key, got %d %v", rec.Code, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "request.json")); err == nil {
		t.Error("no install request may be written while a setting is missing")
	}
}
