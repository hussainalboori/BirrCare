package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"github.com/go-chi/chi/v5"
)

// Issue #55: the server chases a scheduled dose nobody logged.

func doseWatcherFixture(t *testing.T) (*handlers.MedicationHandler, *time.Location, string, func(hhmm string) time.Time) {
	t.Helper()
	med, _, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	// Household in Riyadh; the fake clock is handed over in UTC, like a server's.
	riyadh, err := time.LoadLocation("Asia/Riyadh")
	if err != nil {
		t.Fatal(err)
	}
	prev := handlers.ClinicalLocation()
	handlers.SetClinicalLocationForTest(riyadh)
	t.Cleanup(func() { handlers.SetClinicalLocationForTest(prev) })

	today := time.Now().In(riyadh).Format("2006-01-02")
	at := func(hhmm string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", today+" "+hhmm, riyadh)
		if err != nil {
			t.Fatal(err)
		}
		return v.UTC()
	}
	mustExec(t, med, `UPDATE medications SET is_active = 0`) // only the medicine under test
	mustExec(t, med, `INSERT INTO medications (id, name_ar, name_en, dosage, frequency_slots, slot_times, is_critical, created_by, created_at)
		VALUES ('med_clop', 'كلوبيدوقريل', 'Clopidogrel', '75 mg', '["morning","evening"]', '{"morning":"08:00","evening":"20:00"}', 1, 'cg_hussain', '2020-01-01 00:00:00')`)
	return med, riyadh, today, at
}

func mustExec(t *testing.T, med *handlers.MedicationHandler, q string, args ...interface{}) {
	t.Helper()
	if _, err := med.DB.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

func runWatcher(t *testing.T, med *handlers.MedicationHandler, now time.Time) {
	t.Helper()
	if err := handlers.CheckDoseOverdue(context.Background(), med.DB, nil, med.Broker, now); err != nil {
		t.Fatal(err)
	}
}

type overdueRow struct {
	ref, target string
	resolved    bool
}

func overdueAlerts(t *testing.T, med *handlers.MedicationHandler) []overdueRow {
	t.Helper()
	rows, err := med.DB.Query(`SELECT ref_id, COALESCE(target_caregiver_id, ''), resolved_at IS NOT NULL FROM alerts WHERE kind = 'dose_overdue' ORDER BY ref_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []overdueRow
	for rows.Next() {
		var r overdueRow
		rows.Scan(&r.ref, &r.target, &r.resolved)
		out = append(out, r)
	}
	return out
}

func unackedKinds(t *testing.T, med *handlers.MedicationHandler, who string) []string {
	t.Helper()
	h := &handlers.AlertsHandler{DB: med.DB}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(authContext(r.Context(), &models.Caregiver{ID: who, Role: models.RoleCaregiver}))
	rec := httptest.NewRecorder()
	h.Unacked(rec, r)
	var got []handlers.Alert
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, a := range got {
		kinds = append(kinds, a.Kind)
	}
	return kinds
}

func TestCriticalDoseOverdueEscalatesAndResolves(t *testing.T) {
	med, _, today, at := doseWatcherFixture(t)
	putOnDuty(t, med, "cg_fatima")
	ref := "med_clop:" + today + ":morning"

	runWatcher(t, med, at("08:29"))
	if got := overdueAlerts(t, med); len(got) != 0 {
		t.Fatalf("08:29: too early for a blood thinner (30 min), got %v", got)
	}
	runWatcher(t, med, at("08:30"))
	if got := overdueAlerts(t, med); !reflect.DeepEqual(got, []overdueRow{{ref, "cg_fatima", false}}) {
		t.Fatalf("08:30: want one alert for the on-duty caregiver, got %v", got)
	}
	if k := unackedKinds(t, med, "cg_fatima"); !reflect.DeepEqual(k, []string{"dose_overdue"}) {
		t.Fatalf("on-duty caregiver should see it, got %v", k)
	}
	if k := unackedKinds(t, med, "cg_ali"); len(k) != 0 {
		t.Fatalf("others must not see it before escalation, got %v", k)
	}
	runWatcher(t, med, at("08:45")) // run again: still one row
	runWatcher(t, med, at("09:00"))
	if got := overdueAlerts(t, med); !reflect.DeepEqual(got, []overdueRow{{ref, "", false}}) {
		t.Fatalf("09:00: want the same alert escalated to everyone, got %v", got)
	}
	if k := unackedKinds(t, med, "cg_ali"); !reflect.DeepEqual(k, []string{"dose_overdue"}) {
		t.Fatalf("after escalation everyone sees it, got %v", k)
	}

	// 09:05: Ali logs it. Resolved for everyone, and it stays resolved.
	if res := postDose(med, siblings[1], handlers.LogDoseRequest{MedicationID: "med_clop", Slot: "morning", ScheduledDate: today, Status: "taken"}); res.code != http.StatusCreated {
		t.Fatalf("log dose: %d %s", res.code, res.raw)
	}
	if got := overdueAlerts(t, med); !reflect.DeepEqual(got, []overdueRow{{ref, "", true}}) {
		t.Fatalf("logged: want the alert resolved, got %v", got)
	}
	for _, who := range []string{"cg_fatima", "cg_ali", "cg_hussain"} {
		if k := unackedKinds(t, med, who); len(k) != 0 {
			t.Fatalf("%s still sees %v after the dose was logged", who, k)
		}
	}
	runWatcher(t, med, at("09:10"))
	if got := overdueAlerts(t, med); len(got) != 1 || !got[0].resolved {
		t.Fatalf("a logged dose must not be chased again, got %v", got)
	}
}

func TestDoseOverdueNormalWindowsAndNobodyOnDuty(t *testing.T) {
	med, _, today, at := doseWatcherFixture(t)
	mustExec(t, med, `UPDATE medications SET is_critical = 0 WHERE id = 'med_clop'`)
	runWatcher(t, med, at("08:44"))
	if got := overdueAlerts(t, med); len(got) != 0 {
		t.Fatalf("08:44: before the 45-minute window, got %v", got)
	}
	runWatcher(t, med, at("08:45"))
	if got := overdueAlerts(t, med); !reflect.DeepEqual(got, []overdueRow{{"med_clop:" + today + ":morning", "", false}}) {
		t.Fatalf("nobody on duty: want the alert for everyone, got %v", got)
	}
}

func TestDoseOverdueRespectsRemindAndIntervalLock(t *testing.T) {
	med, riyadh, today, at := doseWatcherFixture(t)
	mustExec(t, med, `UPDATE medications SET remind = 0 WHERE id = 'med_clop'`)
	runWatcher(t, med, at("10:00"))
	if got := overdueAlerts(t, med); len(got) != 0 {
		t.Fatalf("remind=false: want no alert, got %v", got)
	}

	// Last night's 20:00 dose was given late, at 01:00: the 9-hour lock (¾ of
	// 08:00↔20:00) refuses the morning dose until 10:00, so nothing is chased.
	mustExec(t, med, `UPDATE medications SET remind = 1 WHERE id = 'med_clop'`)
	yesterday := time.Now().In(riyadh).AddDate(0, 0, -1).Format("2006-01-02")
	mustExec(t, med, `INSERT INTO medication_logs (id, medication_id, caregiver_id, slot, scheduled_date, taken_at, recorded_at, status)
		VALUES ('log_late', 'med_clop', 'cg_ali', 'evening', ?, ?, ?, 'taken')`, yesterday, at("01:00").Format("2006-01-02 15:04:05"), at("01:00").Format("2006-01-02 15:04:05"))
	runWatcher(t, med, at("09:00"))
	if got := overdueAlerts(t, med); len(got) != 0 {
		t.Fatalf("inside the DOSE_TOO_SOON window: want no alert, got %v", got)
	}
	runWatcher(t, med, at("10:01"))
	if got := overdueAlerts(t, med); !reflect.DeepEqual(got, []overdueRow{{"med_clop:" + today + ":morning", "", false}}) {
		t.Fatalf("once the lock has passed the dose is chased, got %v", got)
	}
}

func TestDoseOverdueUsesHouseholdTimeZone(t *testing.T) {
	// 05:30 UTC is 08:30 in Riyadh: the 08:00 dose there is 30 minutes late.
	med, _, today, at := doseWatcherFixture(t)
	now := at("08:30")
	if now.Hour() != 5 || now.Minute() != 30 {
		t.Fatalf("fixture clock should be 05:30 UTC, got %v", now)
	}
	runWatcher(t, med, now)
	if got := overdueAlerts(t, med); len(got) != 1 || got[0].ref != "med_clop:"+today+":morning" {
		t.Fatalf("want the Riyadh-morning dose overdue at 05:30 UTC, got %v", got)
	}
}

func TestVoidingTheDoseReopensItsAlert(t *testing.T) {
	med, _, today, at := doseWatcherFixture(t)
	runWatcher(t, med, at("08:30"))
	res := postDose(med, siblings[2], handlers.LogDoseRequest{MedicationID: "med_clop", Slot: "morning", ScheduledDate: today, Status: "taken"})
	if res.code != http.StatusCreated {
		t.Fatalf("log: %d %s", res.code, res.raw)
	}
	mustExec(t, med, `UPDATE medication_logs SET voided_at = datetime('now'), voided_by = 'cg_ali', void_reason = 'wrong medicine' WHERE id = ?`, res.body["log_id"])
	runWatcher(t, med, at("08:40"))
	if got := overdueAlerts(t, med); len(got) != 1 || got[0].resolved {
		t.Fatalf("a cancelled entry leaves the dose missing: want the alert open again, got %v", got)
	}
}

func TestLogDoseOnBehalfOfSomeoneElse(t *testing.T) {
	med, _, today, _ := doseWatcherFixture(t)
	res := postDose(med, siblings[2], handlers.LogDoseRequest{MedicationID: "med_clop", Slot: "morning", ScheduledDate: today, Status: "taken", GivenBy: "cg_ali"})
	if res.code != http.StatusCreated || res.body["caregiver_name"] != siblings[1].Name {
		t.Fatalf("on behalf: %d %s", res.code, res.raw)
	}
	var giver, recorder string
	med.DB.QueryRow(`SELECT caregiver_id, recorded_by FROM medication_logs WHERE id = ?`, res.body["log_id"]).Scan(&giver, &recorder)
	if giver != "cg_ali" || recorder != "cg_fatima" {
		t.Fatalf("want given by Ali, recorded by Fatima; got %s / %s", giver, recorder)
	}
	var details string
	med.DB.QueryRow(`SELECT details FROM audit_logs WHERE action = 'MED_LOGGED' AND entity_id = ?`, res.body["log_id"]).Scan(&details)
	if want := "recorded on their behalf by " + siblings[2].Name; !strings.Contains(details, want) {
		t.Fatalf("audit should say who recorded it, got %q", details)
	}
	// The person who tapped can still undo their own mis-tap.
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", res.body["log_id"].(string))
	r = r.WithContext(context.WithValue(authContext(r.Context(), siblings[2]), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	med.VoidDose(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("recorder's undo: %d %s", rec.Code, rec.Body.String())
	}
	// A viewer can't be named as the giver.
	if res := postDose(med, siblings[2], handlers.LogDoseRequest{MedicationID: "med_clop", Slot: "evening", ScheduledDate: today, Status: "taken", GivenBy: "cg_zainab"}); res.code != http.StatusBadRequest {
		t.Fatalf("viewer as giver: want 400, got %d", res.code)
	}
}
