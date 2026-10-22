package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
)

// The Super Admin chooses the backup times; a backup waits while the family
// is using the app, but never more than 2 hours.

func TestDecideBackup(t *testing.T) {
	bah, _ := time.LoadLocation("Asia/Bahrain")
	at := func(hm string) time.Time {
		p, _ := time.Parse("15:04", hm)
		return time.Date(2026, 10, 22, p.Hour(), p.Minute(), 0, 0, bah)
	}
	rfc := func(tm time.Time) string { return tm.UTC().Format(time.RFC3339) }
	sch := handlers.BackupSchedule{Times: []string{"03:00", "15:00"}, AvoidBusy: true}
	okAt := func(tm time.Time) *handlers.BackupStatusFile {
		return &handlers.BackupStatusFile{Status: "success", LastSuccessAt: rfc(tm), LastAttemptAt: rfc(tm)}
	}

	cases := []struct {
		name   string
		now    time.Time
		sch    handlers.BackupSchedule
		st     *handlers.BackupStatusFile
		busy   string
		due    bool
		reason string
	}{
		{"first ever, at 03:05", at("03:05"), sch, nil, "", true, "due"},
		{"03:00 slot already done", at("10:00"), sch, okAt(at("03:01")), "", false, "done"},
		{"15:00 reached", at("15:02"), sch, okAt(at("03:01")), "", true, "due"},
		{"15:00 but someone is logging a dose", at("15:02"), sch, okAt(at("03:01")), "in_use", false, "in_use"},
		{"15:00 during an open crisis alert", at("15:20"), sch, okAt(at("03:01")), "crisis", false, "crisis"},
		{"still busy 2 hours later: runs anyway", at("17:01"), sch, okAt(at("03:01")), "in_use", true, "due"},
		{"busy, but avoid_busy is off", at("15:02"), handlers.BackupSchedule{Times: sch.Times}, okAt(at("03:01")), "in_use", true, "due"},
		{"failed 10 min ago: wait before retrying", at("15:12"), sch, &handlers.BackupStatusFile{Status: "failed", LastSuccessAt: rfc(at("03:01")), LastAttemptAt: rfc(at("15:02"))}, "", false, "retry_later"},
		{"failed 40 min ago: retry", at("15:42"), sch, &handlers.BackupStatusFile{Status: "failed", LastSuccessAt: rfc(at("03:01")), LastAttemptAt: rfc(at("15:02"))}, "", true, "due"},
		{"just after midnight: yesterday's 15:00 was missed", at("00:30"), sch, okAt(at("03:01").AddDate(0, 0, -1)), "", true, "due"},
	}
	for _, c := range cases {
		got := handlers.DecideBackupForTest(c.now, bah, c.sch, c.st, c.busy)
		if got.Due != c.due || got.Reason != c.reason {
			t.Errorf("%s: got due=%v reason=%s, want due=%v reason=%s", c.name, got.Due, got.Reason, c.due, c.reason)
		}
	}

	next := handlers.NextBackupSlotForTest(at("16:00"), bah, sch.Times)
	if want := at("03:00").AddDate(0, 0, 1); !next.Equal(want) {
		t.Errorf("next after 16:00: got %v, want %v", next, want)
	}
}

func TestBackupScheduleEndpoints(t *testing.T) {
	h, med, _ := backupFixture(t)
	put := func(body string, c int) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/backup/schedule", bytes.NewBufferString(body))
		req = req.WithContext(authContext(req.Context(), siblings[c]))
		rec := httptest.NewRecorder()
		h.SetSchedule(rec, req)
		return rec
	}

	// Default: every 6 hours, avoiding busy times.
	rec := adminCall(h.GetStatus, siblings[0], http.MethodGet)
	var st handlers.BackupStatus
	json.NewDecoder(rec.Body).Decode(&st)
	if len(st.Schedule.Times) != 4 || !st.Schedule.AvoidBusy || st.NextAt == "" {
		t.Fatalf("unexpected default schedule %+v next %q", st.Schedule, st.NextAt)
	}

	if rec := put(`{"times":["03:00"],"avoid_busy":true}`, 1); rec.Code != http.StatusForbidden {
		t.Errorf("admin: expected 403, got %d", rec.Code)
	}
	for _, bad := range []string{`{"times":[]}`, `{"times":["25:00"]}`, `{"times":["1","2","3","4","5","6","7"]}`} {
		if rec := put(bad, 0); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", bad, rec.Code)
		}
	}

	rec = put(`{"times":["15:30","02:00","02:00"],"avoid_busy":false}`, 0)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	json.NewDecoder(rec.Body).Decode(&st)
	if len(st.Schedule.Times) != 2 || st.Schedule.Times[0] != "02:00" || st.Schedule.Times[1] != "15:30" || st.Schedule.AvoidBusy {
		t.Errorf("saved schedule should be sorted and de-duplicated: %+v", st.Schedule)
	}
	var n int
	med.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'BACKUP_SCHEDULE_CHANGED'`).Scan(&n)
	if n != 1 {
		t.Errorf("expected one audit row, got %d", n)
	}
}

func TestBackupDueEndpointPostponesWhileInUse(t *testing.T) {
	h, med, _ := backupFixture(t)
	// A backup time a minute ago, avoiding busy times.
	now := time.Now().In(handlers.ClinicalLocation())
	slot := now.Add(-time.Minute).Format("15:04")
	raw, _ := json.Marshal(handlers.BackupSchedule{Times: []string{slot}, AvoidBusy: true})
	med.DB.Exec(`INSERT INTO app_settings (key, value) VALUES ('backup_schedule', ?)`, string(raw))

	if rec := internalCall(h.Due, "203.0.113.9:1", testBackupSecret, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("public caller: expected 403, got %d", rec.Code)
	}

	// Nobody is using the app: due.
	var d handlers.BackupDueForTest
	json.NewDecoder(internalCall(h.Due, "127.0.0.1:1", testBackupSecret, nil).Body).Decode(&d)
	if !d.Due {
		t.Fatalf("expected due when idle, got %+v", d)
	}

	// Someone just logged a dose: postponed, and the card says so.
	if res := postDose(med, siblings[2], handlers.LogDoseRequest{MedicationID: "med_aspirin", Slot: "morning", ScheduledDate: today(), Status: "skipped"}); res.code != http.StatusCreated {
		t.Fatalf("log dose: %d %s", res.code, res.raw)
	}
	json.NewDecoder(internalCall(h.Due, "127.0.0.1:1", testBackupSecret, nil).Body).Decode(&d)
	if d.Due || d.Reason != "in_use" {
		t.Fatalf("expected postponed while in use, got %+v", d)
	}
	var st handlers.BackupStatus
	json.NewDecoder(adminCall(h.GetStatus, siblings[0], http.MethodGet).Body).Decode(&st)
	if st.Postponed == nil || st.Postponed.Reason != "in_use" {
		t.Errorf("status should show the postponement, got %+v", st.Postponed)
	}
}

func TestBackupRetentionChoice(t *testing.T) {
	h, _, _ := backupFixture(t)
	put := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/backup/schedule", bytes.NewBufferString(body))
		req = req.WithContext(authContext(req.Context(), siblings[0]))
		rec := httptest.NewRecorder()
		h.SetSchedule(rec, req)
		return rec
	}
	snapRetention := func() string {
		var res struct {
			Retention string `json:"retention"`
		}
		json.NewDecoder(internalCall(h.Snapshot, "127.0.0.1:1", testBackupSecret, nil).Body).Decode(&res)
		return res.Retention
	}

	// Default: keep history.
	var st handlers.BackupStatus
	json.NewDecoder(adminCall(h.GetStatus, siblings[0], http.MethodGet).Body).Decode(&st)
	if st.Schedule.Retention != "history" || snapRetention() != "history" {
		t.Fatalf("default retention should be history, got %q / %q", st.Schedule.Retention, snapRetention())
	}

	if rec := put(`{"times":["03:00"],"avoid_busy":true,"retention":"everything"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown retention: expected 400, got %d", rec.Code)
	}

	// Latest only: the host is told on the next snapshot.
	rec := put(`{"times":["03:00"],"avoid_busy":true,"retention":"latest"}`)
	json.NewDecoder(rec.Body).Decode(&st)
	if rec.Code != http.StatusOK || st.Schedule.Retention != "latest" || snapRetention() != "latest" {
		t.Errorf("latest: %d %+v / %q", rec.Code, st.Schedule, snapRetention())
	}

	// Older clients that don't send it keep history.
	put(`{"times":["03:00"],"avoid_busy":true}`)
	if r := snapRetention(); r != "history" {
		t.Errorf("missing retention should mean history, got %q", r)
	}
}
