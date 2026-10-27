package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
)

// Issue #59: hospital stay / away mode.

func pauseCall(t *testing.T, fn http.HandlerFunc, who *models.Caregiver, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r = r.WithContext(authContext(r.Context(), who))
	rec := httptest.NewRecorder()
	fn(rec, r)
	return rec
}

func householdUTC(t *testing.T, date, hhmm string) string {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02 15:04", date+" "+hhmm, handlers.ClinicalLocation())
	if err != nil {
		t.Fatal(err)
	}
	return v.UTC().Format("2006-01-02 15:04:05")
}

func TestPauseAndResume(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	h := &handlers.CarePauseHandler{DB: med.DB, Broker: med.Broker}
	viewer := &models.Caregiver{ID: "cg_zainab", Role: models.RoleViewer}

	if rec := pauseCall(t, h.Pause, viewer, `{"reason":"hospital"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer pause: want 403, got %d", rec.Code)
	}
	if rec := pauseCall(t, h.Pause, siblings[2], `{"reason":"party"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad reason: want 400, got %d", rec.Code)
	}
	if rec := pauseCall(t, h.Pause, siblings[2], `{"reason":"hospital","notes":"King Fahd, ward 3"}`); rec.Code != http.StatusCreated {
		t.Fatalf("pause: %d %s", rec.Code, rec.Body.String())
	}
	if rec := pauseCall(t, h.Pause, siblings[1], `{"reason":"travel"}`); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "PAUSE_OPEN") {
		t.Fatalf("second open pause: want 409 PAUSE_OPEN, got %d %s", rec.Code, rec.Body.String())
	}
	rec := httptest.NewRecorder()
	h.Status(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(rec.Body.String(), `"paused":true`) || !strings.Contains(rec.Body.String(), "King Fahd") {
		t.Fatalf("status while paused: %s", rec.Body.String())
	}
	if rec := pauseCall(t, h.Resume, siblings[1], ``); rec.Code != http.StatusOK {
		t.Fatalf("resume: %d %s", rec.Code, rec.Body.String())
	}
	if rec := pauseCall(t, h.Resume, siblings[1], ``); rec.Code != http.StatusConflict {
		t.Fatalf("resume twice: want 409, got %d", rec.Code)
	}
	var audits int
	med.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action IN ('CARE_PAUSED', 'CARE_RESUMED')`).Scan(&audits)
	if audits != 2 {
		t.Fatalf("pause and resume must be audited, got %d rows", audits)
	}
	// A new pause after resuming is fine.
	if rec := pauseCall(t, h.Pause, siblings[2], `{"reason":"travel"}`); rec.Code != http.StatusCreated {
		t.Fatalf("pause again: %d", rec.Code)
	}
}

func TestReportCountsPausedDoses(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	h.DB.Exec(`UPDATE medications SET is_active = 0`)
	addMed(t, h, "med_daily", `["sun","mon","tue","wed","thu","fri","sat"]`, "00:00", false)
	// In hospital from day 5 08:00 to day 7 18:00: the 00:00 doses of days 6 and 7.
	day := func(n int) string { return clinicalDay(n - 14) }
	if _, err := h.DB.Exec(`INSERT INTO care_pauses (id, reason, started_at, ended_at, started_by) VALUES ('p1', 'hospital', ?, ?, 'cg_ali')`,
		householdUTC(t, day(5), "08:00"), householdUTC(t, day(7), "18:00")); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 14; n++ {
		if n == 6 || n == 7 {
			continue
		}
		logTaken(t, h, "med_daily", day(n), "5mg")
	}
	_, out := runExport(t, &handlers.ExportHandler{DB: h.DB}, 14)
	m := medByID(out, "med_daily")
	if m.Paused != 2 || m.NotRecorded != 0 || m.Expected != 12 || m.Rate != 100 {
		t.Fatalf("want paused 2, not_recorded 0, expected 12, rate 100%%; got %+v", m)
	}
	if out.Adherence.TotalPaused != 2 || out.Adherence.AdherenceRate != 100 {
		t.Fatalf("totals: %+v", out.Adherence)
	}
}

func TestNoDoseOrRepositionAlertsWhilePaused(t *testing.T) {
	med, _, today, at := doseWatcherFixture(t)
	putOnDuty(t, med, "cg_fatima")
	// Admitted at 07:00, before the 08:00 blood thinner.
	mustExec(t, med, `INSERT INTO care_pauses (id, reason, started_at, started_by) VALUES ('p1', 'hospital', ?, 'cg_ali')`, at("07:00").Format("2006-01-02 15:04:05"))
	runWatcher(t, med, at("08:30"))
	runWatcher(t, med, at("09:30"))
	if got := overdueAlerts(t, med); len(got) != 0 {
		t.Fatalf("paused: no dose alerts, got %v", got)
	}
	// Home again at 10:00: the 08:00 dose was due in hospital, so it isn't chased.
	mustExec(t, med, `UPDATE care_pauses SET ended_at = ?, ended_by = 'cg_ali' WHERE id = 'p1'`, at("10:00").Format("2006-01-02 15:04:05"))
	runWatcher(t, med, at("10:01"))
	if got := overdueAlerts(t, med); len(got) != 0 {
		t.Fatalf("a dose due during the pause must not be chased after it, got %v", got)
	}
	_ = today

	// Repositioning: last turn 5 h ago, but she has been in hospital.
	mustExec(t, med, `INSERT INTO care_events (id, kind, value, caregiver_id, created_at) VALUES ('ev1', 'reposition', 'left', 'cg_ali', ?)`,
		time.Now().UTC().Add(-5*time.Hour).Format(time.RFC3339))
	mustExec(t, med, `INSERT INTO care_pauses (id, reason, started_at, started_by) VALUES ('p2', 'hospital', ?, 'cg_ali')`,
		time.Now().UTC().Add(-4*time.Hour).Format("2006-01-02 15:04:05"))
	if err := handlers.CheckRepositionOverdue(context.Background(), med.DB, nil, med.Broker, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Back home 10 minutes ago: the repositioning clock starts again from now.
	mustExec(t, med, `UPDATE care_pauses SET ended_at = ? WHERE id = 'p2'`, time.Now().UTC().Add(-10*time.Minute).Format("2006-01-02 15:04:05"))
	if err := handlers.CheckRepositionOverdue(context.Background(), med.DB, nil, med.Broker, time.Now()); err != nil {
		t.Fatal(err)
	}
	var n int
	med.DB.QueryRow(`SELECT COUNT(*) FROM alerts WHERE kind = 'reposition_overdue'`).Scan(&n)
	if n != 0 {
		t.Fatalf("no repositioning alarm for an empty bed or right after coming home, got %d", n)
	}
}
