package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/push"
)

// Issue #67: the daily family summary.

const summaryDay = "2025-03-10"

func summaryFixture(t *testing.T) (*handlers.MedicationHandler, *time.Location) {
	t.Helper()
	med, _, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	riyadh := riyadhHousehold(t)
	at := func(hhmm string) string {
		v, _ := time.ParseInLocation("2006-01-02 15:04", summaryDay+" "+hhmm, riyadh)
		return v.UTC().Format("2006-01-02 15:04:05")
	}
	mustExec(t, med, `UPDATE medications SET is_active = 0`)
	mustExec(t, med, `DELETE FROM shift_assignments`)
	mustExec(t, med, `INSERT INTO medications (id, name_ar, name_en, dosage, frequency_slots, slot_times, is_critical, created_by, created_at) VALUES
		('med_clop', 'كلوبيدوقريل', 'Clopidogrel', '75 mg', '["morning"]', '{"morning":"08:00"}', 1, 'cg_hussain', '2020-01-01 00:00:00'),
		('med_ator', 'أتورفاستاتين', 'Atorvastatin', '20 mg', '["evening"]', '{"evening":"20:00"}', 0, 'cg_hussain', '2020-01-01 00:00:00')`)
	mustExec(t, med, `INSERT INTO medication_logs (id, medication_id, caregiver_id, slot, scheduled_date, taken_at, recorded_at, status)
		VALUES ('l1', 'med_clop', 'cg_fatima', 'morning', ?, ?, ?, 'taken')`, summaryDay, at("08:05"), at("08:05"))
	mustExec(t, med, `INSERT INTO vitals_logs (id, caregiver_id, recorded_at, systolic, diastolic, vital_type) VALUES
		('v1', 'cg_fatima', ?, 128, 80, 'bp'), ('v2', 'cg_fatima', ?, 141, 88, 'bp')`, at("09:00"), at("18:00"))
	mustExec(t, med, `INSERT INTO handover_notes (id, caregiver_id, created_at, shift_type, summary, urgent_flag, tags)
		VALUES ('n1', 'cg_fatima', ?, 'shift_24h', 'Walked to the kitchen twice.', 0, '["ate_well","good_mood"]')`, at("19:00"))
	mustExec(t, med, `INSERT INTO shift_assignments (id, shift_id, caregiver_id, date) VALUES ('a1', 'shift_24h', 'cg_fatima', ?)`, summaryDay)
	return med, riyadh
}

func todaySummary(t *testing.T, med *handlers.MedicationHandler, lang string, now time.Time) handlers.DailySummary {
	t.Helper()
	s, err := handlers.BuildDailySummaryForTest(med.DB, summaryDay, lang, now)
	if err != nil {
		t.Fatal(err)
	}
	return *s
}

func TestDailySummaryContent(t *testing.T) {
	med, riyadh := summaryFixture(t)
	end := time.Date(2025, 3, 10, 23, 0, 0, 0, riyadh)

	en := todaySummary(t, med, "en", end)
	if en.MedsExpected != 2 || en.MedsGiven != 1 || len(en.Missed) != 1 || en.Missed[0] != "Atorvastatin" || en.BloodThinnerOK == nil || !*en.BloodThinnerOK {
		t.Fatalf("medicines: %+v", en)
	}
	if en.BPMinSys != 128 || en.BPMaxSys != 141 || en.BPCount != 2 {
		t.Fatalf("BP: %+v", en)
	}
	if en.Headline != "Today: 1 of 2 doses given, BP 128–141, ate well" {
		t.Fatalf("en headline: %q", en.Headline)
	}
	joined := strings.Join(en.Lines, "\n")
	for _, want := range []string{"Medicines: 1 of 2 given (not recorded: Atorvastatin)", "Blood thinner: given ✓", "Meals and mood: ate well, good mood",
		"On duty: ", "Last note: «Walked to the kitchen twice.»"} {
		if !strings.Contains(joined, want) {
			t.Errorf("en lines missing %q:\n%s", want, joined)
		}
	}

	ar := todaySummary(t, med, "ar", end)
	if ar.Headline != "اليوم: أُعطيت 1 من 2 جرعات، الضغط 128–141، أكلت جيداً" {
		t.Fatalf("ar headline: %q", ar.Headline)
	}
	if j := strings.Join(ar.Lines, "\n"); !strings.Contains(j, "لم تُسجَّل: أتورفاستاتين") || !strings.Contains(j, "مميّع الدم: أُعطي ✓") || !strings.Contains(j, "مزاج جيد") {
		t.Fatalf("ar lines:\n%s", j)
	}

	// In hospital: only that.
	mustExec(t, med, `INSERT INTO care_pauses (id, reason, started_at, started_by) VALUES ('p1', 'hospital', ?, 'cg_ali')`,
		time.Date(2025, 3, 9, 14, 0, 0, 0, riyadh).UTC().Format("2006-01-02 15:04:05"))
	p := todaySummary(t, med, "en", end)
	if p.Headline != "In hospital since 9 Mar" || len(p.Lines) != 1 || p.MedsExpected != 0 {
		t.Fatalf("paused: %+v", p)
	}
}

func TestDailySummarySentOnceAtHomeTime(t *testing.T) {
	med, riyadh := summaryFixture(t)
	mustExec(t, med, `UPDATE caregivers SET preferred_lang = 'en' WHERE id = 'cg_fatima'`)
	mustExec(t, med, `UPDATE caregivers SET preferred_lang = 'ar' WHERE id = 'cg_ali'`)
	mustExec(t, med, `INSERT INTO daily_summary_prefs (caregiver_id, enabled, lock_details, updated_at) VALUES
		('cg_ali', 1, 0, '2030-01-01 00:00:00'), ('cg_fatima', 1, 1, '2030-01-01 00:00:00'), ('cg_hussain', 0, 1, '2030-01-01 00:00:00')`)
	type sent struct {
		who string
		msg map[string]push.Message
	}
	var got []sent
	send := func(cg string, msg func(lang string) push.Message) {
		got = append(got, sent{cg, map[string]push.Message{"ar": msg("ar"), "en": msg("en")}})
	}
	check := func(at time.Time) {
		t.Helper()
		if err := handlers.CheckDailySummaries(context.Background(), med.DB, send, at); err != nil {
			t.Fatal(err)
		}
	}
	check(time.Date(2025, 3, 10, 20, 59, 0, 0, riyadh))
	if len(got) != 0 {
		t.Fatalf("before 21:00 at home: %v", got)
	}
	check(time.Date(2025, 3, 10, 21, 0, 0, 0, riyadh))
	check(time.Date(2025, 3, 10, 21, 5, 0, 0, riyadh))
	check(time.Date(2025, 3, 10, 23, 0, 0, 0, riyadh))
	if len(got) != 2 {
		t.Fatalf("once each to Ali and Fatima (not Hussain, who turned it off): %+v", got)
	}
	digits := regexp.MustCompile(`[0-9٠-٩]`)
	for _, s := range got {
		switch s.who {
		case "cg_ali": // no lock-screen details: no values at all
			for _, m := range s.msg {
				if digits.MatchString(m.Body) {
					t.Fatalf("Ali's lock screen shows values: %q", m.Body)
				}
			}
		case "cg_fatima":
			if s.msg["en"].Body != "Today: 1 of 2 doses given, BP 128–141, ate well" {
				t.Fatalf("Fatima chose details: %q", s.msg["en"].Body)
			}
		default:
			t.Fatalf("unexpected recipient %s", s.who)
		}
	}
	// The next day, again (once); in hospital it says only that.
	mustExec(t, med, `INSERT INTO care_pauses (id, reason, started_at, started_by) VALUES ('p1', 'hospital', ?, 'cg_ali')`,
		time.Date(2025, 3, 11, 9, 0, 0, 0, riyadh).UTC().Format("2006-01-02 15:04:05"))
	check(time.Date(2025, 3, 11, 21, 30, 0, 0, riyadh))
	if len(got) != 4 {
		t.Fatalf("next day: %d sends", len(got))
	}
	for _, s := range got[2:] {
		if s.who == "cg_fatima" && s.msg["en"].Body != "In hospital since 11 Mar" {
			t.Fatalf("paused day: %q", s.msg["en"].Body)
		}
	}
}

func TestDailySummaryPrefs(t *testing.T) {
	med, _ := summaryFixture(t)
	h := &handlers.DailySummaryHandler{DB: med.DB}
	viewer := &models.Caregiver{ID: "cg_hussain", Role: models.RoleViewer, PreferredLang: "en"}
	put := func(who *models.Caregiver, body map[string]interface{}) (int, handlers.SummaryPrefs) {
		rec := medCall(t, h.PutPrefs, who, "", body)
		var p handlers.SummaryPrefs
		json.Unmarshal(rec.Body.Bytes(), &p)
		return rec.Code, p
	}
	if code, p := put(viewer, map[string]interface{}{"enabled": true}); code != http.StatusOK || !p.Enabled || p.LockDetails || p.Time != "21:00" {
		t.Fatalf("a viewer opts in (no lock-screen details by default): %d %+v", code, p)
	}
	if code, _ := put(siblings[1], map[string]interface{}{"time": "20:30"}); code != http.StatusForbidden {
		t.Fatalf("an admin changing the time: want 403, got %d", code)
	}
	if code, p := put(siblings[0], map[string]interface{}{"time": "20:30"}); code != http.StatusOK || p.Time != "20:30" {
		t.Fatalf("Super Admin sets the time: %d %+v", code, p)
	}
	if code, _ := put(siblings[0], map[string]interface{}{"time": "8pm"}); code != http.StatusBadRequest {
		t.Fatalf("bad time: want 400, got %d", code)
	}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/?date="+summaryDay, nil)
	h.Today(rec, r.WithContext(authContext(r.Context(), viewer)))
	var s handlers.DailySummary
	json.Unmarshal(rec.Body.Bytes(), &s)
	if rec.Code != http.StatusOK || s.Lang != "en" || s.BPMaxSys != 141 {
		t.Fatalf("viewer reads the Today card: %d %+v", rec.Code, s)
	}
}
