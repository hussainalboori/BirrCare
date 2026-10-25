package handlers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/db"
	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/logging"
	"birrcare/backend/internal/models"
	"github.com/go-chi/chi/v5"
)

func setupLogsTestDB(t *testing.T) (*sql.DB, *handlers.AdminLogsHandler, *logging.Logger, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "logs-handler-test.db")

	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed initializing test db: %v", err)
	}

	if err := db.Migrate(database); err != nil {
		t.Fatalf("failed migrating test db: %v", err)
	}

	// Insert test caregivers
	_, err = database.Exec(`
		INSERT OR IGNORE INTO caregivers (id, name, email, role, pin_hash)
		VALUES
			('cg_superadmin', 'Hussain Super', 'hussain@test.local', 'super_admin', 'hash'),
			('cg_admin', 'Ali Admin', 'ali@test.local', 'admin', 'hash'),
			('cg_caregiver', 'Fatima Caregiver', 'fatima@test.local', 'caregiver', 'hash'),
			('cg_viewer', 'Zainab Viewer', 'zainab@test.local', 'viewer', 'hash')
	`)
	if err != nil {
		t.Fatalf("failed seeding test caregivers: %v", err)
	}

	uStore := logging.NewSQLiteUserLogStore(database)
	sStore := logging.NewSQLiteSystemLogStore(database)
	auditStore := logging.NewSQLiteAccessAuditStore(database)
	logger := logging.NewLogger(uStore, sStore)

	handler := handlers.NewAdminLogsHandler(logger, auditStore)

	cleanup := func() {
		database.Close()
		_ = os.Remove(dbPath)
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
	}

	return database, handler, logger, cleanup
}

func contextWithUser(c *models.Caregiver) context.Context {
	if c == nil {
		return context.Background()
	}
	return context.WithValue(context.Background(), auth.CaregiverContextKey, c)
}

func withRouteParam(ctx context.Context, key, val string) context.Context {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return context.WithValue(ctx, chi.RouteCtxKey, rctx)
}

func TestAdminLogsRBAC(t *testing.T) {
	_, handler, logger, cleanup := setupLogsTestDB(t)
	defer cleanup()

	// Seed one user log and one system log
	ctx := context.Background()
	_ = logger.UserActivity(ctx, "cg_caregiver", "Fatima", "test_action", "/test", "Test user entry", 200, "127.0.0.1", "Agent", nil)
	_ = logger.SystemInfo(ctx, "test_subsystem", "test_event", "Test system entry", nil)

	superAdmin := &models.Caregiver{ID: "cg_superadmin", Name: "Hussain", Role: models.RoleSuperAdmin}
	admin := &models.Caregiver{ID: "cg_admin", Name: "Ali", Role: models.RoleAdmin}
	caregiver := &models.Caregiver{ID: "cg_caregiver", Name: "Fatima", Role: models.RoleCaregiver}
	viewer := &models.Caregiver{ID: "cg_viewer", Name: "Zainab", Role: models.RoleViewer}

	endpoints := []struct {
		name    string
		method  string
		url     string
		handler func(w http.ResponseWriter, r *http.Request)
		paramID string
	}{
		{
			name:    "ListUserLogs",
			method:  "GET",
			url:     "/api/v1/admin/logs/user",
			handler: handler.ListUserLogs,
		},
		{
			name:    "GetUserLog",
			method:  "GET",
			url:     "/api/v1/admin/logs/user/dummy",
			handler: handler.GetUserLog,
			paramID: "dummy",
		},
		{
			name:    "ListSystemLogs",
			method:  "GET",
			url:     "/api/v1/admin/logs/system",
			handler: handler.ListSystemLogs,
		},
		{
			name:    "GetSystemLog",
			method:  "GET",
			url:     "/api/v1/admin/logs/system/dummy",
			handler: handler.GetSystemLog,
			paramID: "dummy",
		},
		{
			name:    "ExportLogs",
			method:  "GET",
			url:     "/api/v1/admin/logs/export?stream=user&format=json",
			handler: handler.ExportLogs,
		},
		{
			name:    "ListAccessAudits",
			method:  "GET",
			url:     "/api/v1/admin/logs/access-audits",
			handler: handler.ListAccessAudits,
		},
	}

	for _, ep := range endpoints {
		t.Run(ep.name+"_Unauthenticated_Rejected_401", func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.url, nil)
			if ep.paramID != "" {
				req = req.WithContext(withRouteParam(req.Context(), "id", ep.paramID))
			}
			rec := httptest.NewRecorder()
			ep.handler(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("[%s] expected 401 for unauthenticated request, got %d (body: %s)", ep.name, rec.Code, rec.Body.String())
			}
		})

		for _, nonSuper := range []*models.Caregiver{admin, caregiver, viewer} {
			t.Run(ep.name+"_NonSuperAdmin_"+nonSuper.Role+"_Rejected_403", func(t *testing.T) {
				reqCtx := contextWithUser(nonSuper)
				if ep.paramID != "" {
					reqCtx = withRouteParam(reqCtx, "id", ep.paramID)
				}
				req := httptest.NewRequest(ep.method, ep.url, nil).WithContext(reqCtx)
				rec := httptest.NewRecorder()
				ep.handler(rec, req)

				if rec.Code != http.StatusForbidden {
					t.Errorf("[%s] expected 403 for role %s, got %d (body: %s)", ep.name, nonSuper.Role, rec.Code, rec.Body.String())
				}
			})
		}

		t.Run(ep.name+"_SuperAdmin_Allowed_200", func(t *testing.T) {
			reqCtx := contextWithUser(superAdmin)
			// For single GET, retrieve an existing ID if available or check that authorization passes
			if ep.paramID != "" {
				reqCtx = withRouteParam(reqCtx, "id", "dummy")
			}
			req := httptest.NewRequest(ep.method, ep.url, nil).WithContext(reqCtx)
			rec := httptest.NewRecorder()
			ep.handler(rec, req)

			// If dummy ID not found, it returns 404 Not Found, but NOT 401 or 403!
			if ep.paramID != "" {
				if rec.Code != http.StatusNotFound && rec.Code != http.StatusOK {
					t.Errorf("[%s] expected 200 or 404 for super admin, got %d", ep.name, rec.Code)
				}
			} else {
				if rec.Code != http.StatusOK {
					t.Errorf("[%s] expected 200 for super admin, got %d (body: %s)", ep.name, rec.Code, rec.Body.String())
				}
			}
		})
	}
}

func TestAdminLogsAuditableAccess(t *testing.T) {
	_, handler, logger, cleanup := setupLogsTestDB(t)
	defer cleanup()

	ctx := context.Background()
	_ = logger.UserActivity(ctx, "cg_caregiver", "Fatima", "med_taken", "/api/v1/medications/log", "Aspirin taken", 200, "127.0.0.1", "Safari", nil)
	_ = logger.SystemInfo(ctx, "db", "checkpoint", "WAL checkpoint completed", nil)

	superAdmin := &models.Caregiver{ID: "cg_superadmin", Name: "Hussain Super", Role: models.RoleSuperAdmin}

	// 1. Super Admin lists user logs
	req := httptest.NewRequest("GET", "/api/v1/admin/logs/user?query=Aspirin", nil).WithContext(contextWithUser(superAdmin))
	rec := httptest.NewRecorder()
	handler.ListUserLogs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// 2. Super Admin exports system logs
	exportReq := httptest.NewRequest("GET", "/api/v1/admin/logs/export?stream=system&format=json", nil).WithContext(contextWithUser(superAdmin))
	exportRec := httptest.NewRecorder()
	handler.ExportLogs(exportRec, exportReq)

	if exportRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", exportRec.Code)
	}

	// 3. Inspect log_access_audits via ListAccessAudits
	auditReq := httptest.NewRequest("GET", "/api/v1/admin/logs/access-audits", nil).WithContext(contextWithUser(superAdmin))
	auditRec := httptest.NewRecorder()
	handler.ListAccessAudits(auditRec, auditReq)

	if auditRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", auditRec.Code)
	}

	var resp struct {
		Entries []logging.AccessAuditEntry `json:"entries"`
		Total   int                        `json:"total"`
	}
	if err := json.Unmarshal(auditRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed unmarshaling audit response: %v", err)
	}

	// We executed:
	// 1) ListUserLogs (stream=user, operation=list)
	// 2) ExportLogs (stream=system, operation=export)
	if resp.Total != 2 {
		t.Fatalf("expected 2 audit entries on first fetch, got total=%d", resp.Total)
	}

	// Verify all records captured the super admin ID
	for _, entry := range resp.Entries {
		if entry.CaregiverID != "cg_superadmin" {
			t.Errorf("expected caregiver_id cg_superadmin, got %s", entry.CaregiverID)
		}
		if entry.Operation == "" || entry.Stream == "" {
			t.Errorf("empty operation or stream in audit entry: %+v", entry)
		}
	}

	// Fetching access audits a second time must show 3 entries, because the first
	// call to ListAccessAudits itself was audited!
	auditReq2 := httptest.NewRequest("GET", "/api/v1/admin/logs/access-audits", nil).WithContext(contextWithUser(superAdmin))
	auditRec2 := httptest.NewRecorder()
	handler.ListAccessAudits(auditRec2, auditReq2)

	var resp2 struct {
		Entries []logging.AccessAuditEntry `json:"entries"`
		Total   int                        `json:"total"`
	}
	_ = json.Unmarshal(auditRec2.Body.Bytes(), &resp2)
	if resp2.Total != 3 {
		t.Fatalf("expected 3 audit entries on second fetch, got total=%d", resp2.Total)
	}
	if resp2.Entries[0].Stream != "access_audit" || resp2.Entries[0].Operation != "list" {
		t.Errorf("expected latest audit to record access_audit list, got %+v", resp2.Entries[0])
	}
}

func TestAdminLogsExportFormats(t *testing.T) {
	_, handler, logger, cleanup := setupLogsTestDB(t)
	defer cleanup()

	ctx := context.Background()
	_ = logger.UserActivity(ctx, "cg_caregiver", "Fatima", "dose_taken", "/api/v1/med", "Dose logged", 200, "10.0.0.1", "Chrome", nil)
	_ = logger.SystemInfo(ctx, "updater", "update_check", "No update available", nil)

	superAdmin := &models.Caregiver{ID: "cg_superadmin", Name: "Hussain", Role: models.RoleSuperAdmin}

	// 1. Export User Logs as JSON
	reqJSON := httptest.NewRequest("GET", "/api/v1/admin/logs/export?stream=user&format=json", nil).WithContext(contextWithUser(superAdmin))
	recJSON := httptest.NewRecorder()
	handler.ExportLogs(recJSON, reqJSON)

	if recJSON.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recJSON.Code)
	}
	if !strings.Contains(recJSON.Header().Get("Content-Type"), "application/json") {
		t.Errorf("expected application/json, got %s", recJSON.Header().Get("Content-Type"))
	}
	var userEntries []logging.UserLogEntry
	if err := json.Unmarshal(recJSON.Body.Bytes(), &userEntries); err != nil {
		t.Fatalf("invalid JSON export: %v", err)
	}
	if len(userEntries) != 1 || userEntries[0].Action != "dose_taken" {
		t.Fatalf("unexpected user export entries: %+v", userEntries)
	}

	// 2. Export User Logs as CSV
	reqCSV := httptest.NewRequest("GET", "/api/v1/admin/logs/export?stream=user&format=csv", nil).WithContext(contextWithUser(superAdmin))
	recCSV := httptest.NewRecorder()
	handler.ExportLogs(recCSV, reqCSV)

	if recCSV.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recCSV.Code)
	}
	if !strings.Contains(recCSV.Header().Get("Content-Type"), "text/csv") {
		t.Errorf("expected text/csv, got %s", recCSV.Header().Get("Content-Type"))
	}
	csvBody := recCSV.Body.String()
	if !strings.Contains(csvBody, "ID,Created At,Level,Caregiver ID") {
		t.Errorf("expected CSV header in user export, got: %s", csvBody)
	}
	if !strings.Contains(csvBody, "dose_taken") {
		t.Errorf("expected CSV row with action in user export, got: %s", csvBody)
	}

	// 3. Export System Logs as CSV
	reqSysCSV := httptest.NewRequest("GET", "/api/v1/admin/logs/export?stream=system&format=csv", nil).WithContext(contextWithUser(superAdmin))
	recSysCSV := httptest.NewRecorder()
	handler.ExportLogs(recSysCSV, reqSysCSV)

	if recSysCSV.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recSysCSV.Code)
	}
	sysCSV := recSysCSV.Body.String()
	if !strings.Contains(sysCSV, "ID,Created At,Level,Subsystem,Event") {
		t.Errorf("expected CSV header in system export, got: %s", sysCSV)
	}
	if !strings.Contains(sysCSV, "update_check") {
		t.Errorf("expected CSV row with event in system export, got: %s", sysCSV)
	}

	// 4. Bad stream parameter
	reqBad := httptest.NewRequest("GET", "/api/v1/admin/logs/export?stream=invalid", nil).WithContext(contextWithUser(superAdmin))
	recBad := httptest.NewRecorder()
	handler.ExportLogs(recBad, reqBad)

	if recBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for invalid stream, got %d", recBad.Code)
	}
}

func TestAdminLogsGetSingleEntry(t *testing.T) {
	_, handler, logger, cleanup := setupLogsTestDB(t)
	defer cleanup()

	ctx := context.Background()
	_ = logger.UserActivity(ctx, "cg_caregiver", "Fatima", "vitals_logged", "/vitals", "BP recorded", 200, "127.0.0.1", "Agent", nil)
	_ = logger.SystemInfo(ctx, "server", "cert_renewed", "TLS certificate renewed", nil)

	superAdmin := &models.Caregiver{ID: "cg_superadmin", Name: "Hussain", Role: models.RoleSuperAdmin}

	// Retrieve user logs list to get the generated UUID
	listReq := httptest.NewRequest("GET", "/api/v1/admin/logs/user", nil).WithContext(contextWithUser(superAdmin))
	listRec := httptest.NewRecorder()
	handler.ListUserLogs(listRec, listReq)

	var userListResp struct {
		Entries []logging.UserLogEntry `json:"entries"`
	}
	_ = json.Unmarshal(listRec.Body.Bytes(), &userListResp)
	if len(userListResp.Entries) != 1 {
		t.Fatalf("expected 1 user log entry, got %d", len(userListResp.Entries))
	}
	userLogID := userListResp.Entries[0].ID

	// Get existing user log by ID
	getReq := httptest.NewRequest("GET", "/api/v1/admin/logs/user/"+userLogID, nil).WithContext(
		withRouteParam(contextWithUser(superAdmin), "id", userLogID),
	)
	getRec := httptest.NewRecorder()
	handler.GetUserLog(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for GetUserLog, got %d (body: %s)", getRec.Code, getRec.Body.String())
	}
	var singleUser logging.UserLogEntry
	_ = json.Unmarshal(getRec.Body.Bytes(), &singleUser)
	if singleUser.ID != userLogID || singleUser.Action != "vitals_logged" {
		t.Fatalf("unexpected user entry: %+v", singleUser)
	}

	// Get non-existent user log by ID -> 404
	get404Req := httptest.NewRequest("GET", "/api/v1/admin/logs/user/non-existent-id", nil).WithContext(
		withRouteParam(contextWithUser(superAdmin), "id", "non-existent-id"),
	)
	get404Rec := httptest.NewRecorder()
	handler.GetUserLog(get404Rec, get404Req)

	if get404Rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-existent user log, got %d", get404Rec.Code)
	}
}
