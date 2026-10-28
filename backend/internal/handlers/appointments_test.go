package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"github.com/go-chi/chi/v5"
)

// Issue #63: appointments with reminders, an .ics file, and questions for the doctor.

var zainabViewer = &models.Caregiver{ID: "cg_zainab", Name: "Zainab", Role: models.RoleViewer}

func callAppt(t *testing.T, fn http.HandlerFunc, who *models.Caregiver, method, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/", bytes.NewReader([]byte(body)))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", id)
	r = r.WithContext(context.WithValue(authContext(r.Context(), who), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	fn(rec, r)
	return rec
}

func decodeAppt(t *testing.T, rec *httptest.ResponseRecorder) handlers.Appointment {
	t.Helper()
	var a handlers.Appointment
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatalf("decode %d %s: %v", rec.Code, rec.Body.String(), err)
	}
	return a
}

func riyadhHousehold(t *testing.T) *time.Location {
	t.Helper()
	riyadh, err := time.LoadLocation("Asia/Riyadh")
	if err != nil {
		t.Fatal(err)
	}
	prev := handlers.ClinicalLocation()
	handlers.SetClinicalLocationForTest(riyadh)
	t.Cleanup(func() { handlers.SetClinicalLocationForTest(prev) })
	return riyadh
}

func TestAppointmentsCRUDAndRoles(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	riyadhHousehold(t)
	h := &handlers.AppointmentHandler{DB: med.DB, Broker: med.Broker}
	day := clinicalDay(3)

	body := `{"kind":"doctor","title":"Neurology follow-up","date":"` + day + `","time":"10:00","location":"King Fahd, clinic 4","assigned_caregiver_id":"cg_hussain","client_request_id":"req-1"}`
	rec := callAppt(t, h.Create, siblings[2], http.MethodPost, "", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	a := decodeAppt(t, rec)
	if a.Date != day || a.Time != "10:00" || a.AssignedName != siblings[0].Name || a.Status != "scheduled" {
		t.Fatalf("created: %+v", a)
	}
	// 10:00 in Riyadh is 07:00 UTC.
	if !strings.HasSuffix(a.StartsAt, "T07:00:00Z") {
		t.Fatalf("starts_at should be 07:00 UTC, got %s", a.StartsAt)
	}
	// A retry returns the same row.
	if again := decodeAppt(t, callAppt(t, h.Create, siblings[2], http.MethodPost, "", body)); again.ID != a.ID {
		t.Fatal("a retried create added a second appointment")
	}

	// Viewers can read but not write.
	if rec := callAppt(t, h.List, zainabViewer, http.MethodGet, "", ""); rec.Code != http.StatusOK {
		t.Fatalf("viewer list: %d", rec.Code)
	} else {
		var out struct{ Upcoming, Recent []handlers.Appointment }
		json.Unmarshal(rec.Body.Bytes(), &out)
		if len(out.Upcoming) != 1 || out.Upcoming[0].ID != a.ID {
			t.Fatalf("upcoming: %+v", out)
		}
	}
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"create": callAppt(t, h.Create, zainabViewer, http.MethodPost, "", body),
		"update": callAppt(t, h.Update, zainabViewer, http.MethodPut, a.ID, body),
		"status": callAppt(t, h.SetStatus, zainabViewer, http.MethodPost, a.ID, `{"status":"cancelled"}`),
	} {
		if rec.Code != http.StatusForbidden {
			t.Errorf("viewer %s: want 403, got %d", name, rec.Code)
		}
	}

	for _, bad := range []string{
		`{"kind":"dentist","title":"x","date":"` + day + `","time":"10:00"}`,
		`{"kind":"lab","title":"","date":"` + day + `","time":"10:00"}`,
		`{"kind":"lab","title":"INR","date":"` + day + `","time":"25:00"}`,
		`{"kind":"lab","title":"INR","date":"` + clinicalDay(-2) + `","time":"10:00"}`,
		`{"kind":"lab","title":"INR","date":"` + day + `","time":"10:00","assigned_caregiver_id":"cg_nobody"}`,
	} {
		if rec := callAppt(t, h.Create, siblings[1], http.MethodPost, "", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", bad, rec.Code)
		}
	}

	// Edit, then mark done: it leaves the upcoming list and is audited.
	edited := decodeAppt(t, callAppt(t, h.Update, siblings[1], http.MethodPut, a.ID,
		`{"kind":"doctor","title":"Neurology follow-up","date":"`+day+`","time":"11:30","assigned_caregiver_id":"cg_ali"}`))
	if edited.Time != "11:30" || edited.AssignedID != "cg_ali" {
		t.Fatalf("edited: %+v", edited)
	}
	if done := decodeAppt(t, callAppt(t, h.SetStatus, siblings[1], http.MethodPost, a.ID, `{"status":"done"}`)); done.Status != "done" {
		t.Fatalf("done: %+v", done)
	}
	if rec := callAppt(t, h.Update, siblings[1], http.MethodPut, a.ID, body); rec.Code != http.StatusConflict {
		t.Fatalf("editing a done appointment: want 409, got %d", rec.Code)
	}
	var n int
	med.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE entity_type = 'appointment' AND entity_id = ?`, a.ID).Scan(&n)
	if n != 3 {
		t.Fatalf("want 3 audit rows (added, changed, done), got %d", n)
	}
}

func TestAppointmentRemindersOncePerMoment(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	riyadh := riyadhHousehold(t)

	// Sunday 10:00 at home, added a few days before.
	start := time.Date(2030, 3, 10, 10, 0, 0, 0, riyadh)
	mustExec(t, med, `INSERT INTO appointments (id, kind, title, starts_at, assigned_caregiver_id, created_by, created_at, updated_at)
		VALUES ('ap1', 'doctor', 'Neurology', ?, 'cg_fatima', 'cg_hussain', '2030-03-01 09:00:00', '2030-03-01 09:00:00')`,
		start.UTC().Format("2006-01-02 15:04:05"))
	check := func(at time.Time) {
		t.Helper()
		if err := handlers.CheckAppointmentReminders(context.Background(), med.DB, nil, med.Broker, at.UTC()); err != nil {
			t.Fatal(err)
		}
	}
	reminders := func() []string {
		rows, err := med.DB.Query(`SELECT ref_id || '>' || COALESCE(target_caregiver_id, 'all') FROM alerts WHERE kind = 'appointment_reminder' ORDER BY created_at, ref_id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			rows.Scan(&s)
			out = append(out, s)
		}
		return out
	}

	check(time.Date(2030, 3, 9, 19, 59, 0, 0, riyadh)) // Saturday 19:59: too early
	if got := reminders(); len(got) != 0 {
		t.Fatalf("before 20:00 the evening before: %v", got)
	}
	check(time.Date(2030, 3, 9, 20, 0, 0, 0, riyadh)) // Saturday 20:00 at home
	check(time.Date(2030, 3, 9, 20, 5, 0, 0, riyadh))
	check(time.Date(2030, 3, 10, 7, 59, 0, 0, riyadh))
	if got := reminders(); len(got) != 1 || !strings.HasPrefix(got[0], "ap1:evening:") || !strings.HasSuffix(got[0], ">cg_fatima") {
		t.Fatalf("one evening reminder to Fatima, got %v", got)
	}
	check(time.Date(2030, 3, 10, 8, 0, 0, 0, riyadh)) // 2 hours before
	check(time.Date(2030, 3, 10, 9, 30, 0, 0, riyadh))
	if got := reminders(); len(got) != 2 || !strings.HasPrefix(got[1], "ap1:2h:") {
		t.Fatalf("then one 2-hour reminder, got %v", got)
	}
	check(time.Date(2030, 3, 10, 10, 30, 0, 0, riyadh))
	if got := reminders(); len(got) != 2 {
		t.Fatalf("nothing after it started, got %v", got)
	}

	// Moved to 16:00 the same day: reminded again for the new time. Unassigned → everyone.
	mustExec(t, med, `UPDATE appointments SET starts_at = ?, assigned_caregiver_id = NULL WHERE id = 'ap1'`,
		time.Date(2030, 3, 10, 16, 0, 0, 0, riyadh).UTC().Format("2006-01-02 15:04:05"))
	check(time.Date(2030, 3, 10, 14, 0, 0, 0, riyadh))
	if got := reminders(); len(got) != 3 || !strings.HasPrefix(got[2], "ap1:2h:") || !strings.HasSuffix(got[2], ">all") {
		t.Fatalf("rescheduled: a new 2-hour reminder to everyone, got %v", got)
	}

	// Cancelled appointments and ones added after the reminder moment stay quiet.
	mustExec(t, med, `INSERT INTO appointments (id, kind, title, starts_at, status, created_by, created_at, updated_at)
		VALUES ('ap2', 'lab', 'INR', ?, 'cancelled', 'cg_hussain', '2030-03-01 09:00:00', '2030-03-01 09:00:00'),
		       ('ap3', 'physio', 'Physio', ?, 'scheduled', 'cg_hussain', ?, ?)`,
		time.Date(2030, 3, 12, 9, 0, 0, 0, riyadh).UTC().Format("2006-01-02 15:04:05"),
		time.Date(2030, 3, 12, 9, 0, 0, 0, riyadh).UTC().Format("2006-01-02 15:04:05"),
		time.Date(2030, 3, 11, 21, 0, 0, 0, riyadh).UTC().Format("2006-01-02 15:04:05"),
		time.Date(2030, 3, 11, 21, 0, 0, 0, riyadh).UTC().Format("2006-01-02 15:04:05"))
	check(time.Date(2030, 3, 11, 22, 0, 0, 0, riyadh))
	if got := reminders(); len(got) != 3 {
		t.Fatalf("cancelled / added after 20:00: no evening reminder, got %v", got)
	}
	check(time.Date(2030, 3, 12, 7, 0, 0, 0, riyadh))
	if got := reminders(); len(got) != 4 || !strings.HasPrefix(got[3], "ap3:2h:") {
		t.Fatalf("ap3 still gets its 2-hour reminder, got %v", got)
	}
}

func TestLabTestDueReminder(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	riyadh := riyadhHousehold(t)
	mustExec(t, med, `INSERT INTO lab_results (id, kind, value, taken_on, next_test_on, caregiver_id, created_at)
		VALUES ('lab1', 'inr', 2.4, '2030-03-01', '2030-03-15', 'cg_hussain', '2030-03-01 08:00:00')`)
	check := func(at time.Time) int {
		t.Helper()
		if err := handlers.CheckAppointmentReminders(context.Background(), med.DB, nil, med.Broker, at.UTC()); err != nil {
			t.Fatal(err)
		}
		var n int
		med.DB.QueryRow(`SELECT COUNT(*) FROM alerts WHERE kind = 'lab_test_due' AND target_caregiver_id IS NULL`).Scan(&n)
		return n
	}
	if n := check(time.Date(2030, 3, 14, 19, 0, 0, 0, riyadh)); n != 0 {
		t.Fatalf("before the evening: %d", n)
	}
	check(time.Date(2030, 3, 14, 20, 0, 0, 0, riyadh))
	if n := check(time.Date(2030, 3, 15, 9, 0, 0, 0, riyadh)); n != 1 {
		t.Fatalf("one reminder for everyone the evening before, got %d", n)
	}
	// A newer result (the test was done) replaces the old next date.
	mustExec(t, med, `INSERT INTO lab_results (id, kind, value, taken_on, next_test_on, caregiver_id, created_at)
		VALUES ('lab2', 'inr', 2.6, '2030-03-15', '2030-03-29', 'cg_hussain', '2030-03-15 08:00:00')`)
	if n := check(time.Date(2030, 3, 28, 20, 30, 0, 0, riyadh)); n != 2 {
		t.Fatalf("the next one follows the newest result, got %d", n)
	}
}

func TestAppointmentICS(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	riyadhHousehold(t)
	h := &handlers.AppointmentHandler{DB: med.DB, Broker: med.Broker}
	a := decodeAppt(t, callAppt(t, h.Create, siblings[0], http.MethodPost, "",
		`{"kind":"doctor","title":"متابعة الأعصاب, Neurology; follow-up","date":"`+clinicalDay(5)+`","time":"10:00","location":"مستشفى الملك فهد — عيادة الأعصاب رقم ٤، الدور الثاني","notes":"Bring the medicine list\nand the BP log","assigned_caregiver_id":"cg_ali"}`))

	rec := callAppt(t, h.ICS, zainabViewer, http.MethodGet, a.ID, "")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/calendar") {
		t.Fatalf("ics: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	ics := rec.Body.String()
	if !strings.HasSuffix(ics, "\r\n") || strings.Contains(strings.ReplaceAll(ics, "\r\n", ""), "\n") {
		t.Fatal("every line must end in CRLF")
	}
	lines := strings.Split(strings.TrimSuffix(ics, "\r\n"), "\r\n")
	var unfolded []string
	for _, l := range lines {
		if len(l) > 75 {
			t.Errorf("line longer than 75 octets: %q", l)
		}
		if !utf8.ValidString(l) {
			t.Errorf("folding split a letter: %q", l)
		}
		if strings.HasPrefix(l, " ") {
			unfolded[len(unfolded)-1] += l[1:]
		} else {
			unfolded = append(unfolded, l)
		}
	}
	props := map[string]string{}
	for _, l := range unfolded {
		k, v, _ := strings.Cut(l, ":")
		if _, seen := props[k]; !seen {
			props[k] = v
		}
	}
	day := strings.ReplaceAll(a.Date, "-", "")
	for k, want := range map[string]string{
		"BEGIN":   "VCALENDAR",
		"VERSION": "2.0",
		"UID":     a.ID + "@birrcare",
		"DTSTART": day + "T070000Z",
		"DTEND":   day + "T080000Z",
		"SUMMARY": `متابعة الأعصاب\, Neurology\; follow-up`,
		"STATUS":  "CONFIRMED",
	} {
		if props[k] != want {
			t.Errorf("%s: want %q, got %q", k, want, props[k])
		}
	}
	if !strings.Contains(props["DESCRIPTION"], `Takes her: علي (Ali)\nBring the medicine list\nand the BP log`) {
		t.Errorf("description: %q", props["DESCRIPTION"])
	}
	if unfolded[len(unfolded)-1] != "END:VCALENDAR" || props["PRODID"] == "" || props["DTSTAMP"] == "" {
		t.Error("missing calendar wrapper or required properties")
	}
}

func TestDoctorQuestions(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	h := &handlers.AppointmentHandler{DB: med.DB, Broker: med.Broker}

	add := func(who *models.Caregiver, text string) (int, handlers.DoctorQuestion) {
		rec := callAppt(t, h.AddQuestion, who, http.MethodPost, "", `{"text":`+jsonString(text)+`}`)
		var q handlers.DoctorQuestion
		json.Unmarshal(rec.Body.Bytes(), &q)
		return rec.Code, q
	}
	code, q1 := add(siblings[2], "Can she start walking with the frame alone?")
	if code != http.StatusCreated {
		t.Fatalf("add: %d", code)
	}
	_, q2 := add(siblings[1], "Should the evening BP dose move to bedtime?")
	if code, _ := add(zainabViewer, "x"); code != http.StatusForbidden {
		t.Fatalf("viewer add: want 403, got %d", code)
	}
	if code, _ := add(siblings[2], "   "); code != http.StatusBadRequest {
		t.Fatalf("empty question: want 400, got %d", code)
	}

	// The report lists open questions first.
	report := func() (open, answered []handlers.DoctorQuestion) {
		rec := httptest.NewRecorder()
		(&handlers.ExportHandler{DB: med.DB}).ExportDoctorSummary(rec, httptest.NewRequest(http.MethodGet, "/api/v1/export/summary?days=14", nil))
		var out struct {
			Open     []handlers.DoctorQuestion `json:"open_questions"`
			Answered []handlers.DoctorQuestion `json:"answered_questions"`
		}
		json.Unmarshal(rec.Body.Bytes(), &out)
		return out.Open, out.Answered
	}
	if open, answered := report(); len(open) != 2 || open[0].ID != q1.ID || len(answered) != 0 {
		t.Fatalf("report before the visit: open %+v answered %+v", open, answered)
	}

	// "Doctor said…" moves it to the history.
	if rec := callAppt(t, h.AnswerQuestion, zainabViewer, http.MethodPost, q1.ID, `{"answer":"yes"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer answer: want 403, got %d", rec.Code)
	}
	if rec := callAppt(t, h.AnswerQuestion, siblings[0], http.MethodPost, q1.ID, `{"answer":""}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty answer: want 400, got %d", rec.Code)
	}
	if rec := callAppt(t, h.AnswerQuestion, siblings[0], http.MethodPost, q1.ID, `{"answer":"Yes, indoors only, with someone near."}`); rec.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", rec.Code, rec.Body.String())
	}
	if rec := callAppt(t, h.AnswerQuestion, siblings[0], http.MethodPost, q1.ID, `{"answer":"again"}`); rec.Code != http.StatusConflict {
		t.Fatalf("answering twice: want 409, got %d", rec.Code)
	}
	open, answered := report()
	if len(open) != 1 || open[0].ID != q2.ID || len(answered) != 1 || answered[0].Answer != "Yes, indoors only, with someone near." {
		t.Fatalf("report after the visit: open %+v answered %+v", open, answered)
	}

	// Only the writer or an admin removes an open question.
	if rec := callAppt(t, h.RemoveQuestion, siblings[2], http.MethodDelete, q2.ID, ""); rec.Code != http.StatusForbidden {
		t.Fatalf("Fatima removing Ali's question: want 403, got %d", rec.Code)
	}
	if rec := callAppt(t, h.RemoveQuestion, siblings[1], http.MethodDelete, q2.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("Ali removing his own: %d", rec.Code)
	}
	if open, _ := report(); len(open) != 0 {
		t.Fatalf("removed question still open: %+v", open)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
