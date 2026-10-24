package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"

	"github.com/go-chi/chi/v5"
)

// Issue #18: 2-tap handovers with quick tags, and "pass duty".

func postNote(t *testing.T, h *handlers.HandoverHandler, who *models.Caregiver, body map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/handovers", bytes.NewReader(b))
	r = r.WithContext(authContext(r.Context(), who))
	rec := httptest.NewRecorder()
	h.CreateHandover(rec, r)
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestHandoverTags(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	h := &handlers.HandoverHandler{DB: med.DB, Broker: med.Broker}
	fatima := &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}

	if code, out := postNote(t, h, fatima, map[string]interface{}{"tags": []string{"ate_half", "needs_rest"}}); code != http.StatusCreated {
		t.Fatalf("tags only, empty summary: want 201, got %d %v", code, out)
	}
	if code, out := postNote(t, h, fatima, map[string]interface{}{"tags": []string{"ate_half", "danced"}}); code != http.StatusBadRequest || out["error"] != "UNKNOWN_TAG" {
		t.Fatalf("unknown tag: want 400 UNKNOWN_TAG, got %d %v", code, out)
	}
	if code, _ := postNote(t, h, fatima, map[string]interface{}{"summary": "  "}); code != http.StatusBadRequest {
		t.Fatalf("no tags and no text: want 400, got %d", code)
	}
	if code, _ := postNote(t, h, fatima, map[string]interface{}{"tags": []string{"pain", "pain"}}); code != http.StatusCreated {
		t.Fatalf("pain: %d", code)
	}

	var tags string
	var urgent int
	if err := med.DB.QueryRow(`SELECT tags, urgent_flag FROM handover_notes WHERE tags LIKE '%pain%'`).Scan(&tags, &urgent); err != nil {
		t.Fatal(err)
	}
	if tags != `["pain"]` || urgent != 1 {
		t.Fatalf("pain note: tags %s urgent %d; want de-duplicated and urgent", tags, urgent)
	}

	rec := httptest.NewRecorder()
	h.ListHandovers(rec, httptest.NewRequest(http.MethodGet, "/api/v1/handovers", nil))
	var list []models.HandoverNote
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	found := false
	for _, n := range list {
		found = found || (len(n.Tags) == 2 && n.Tags[0] == "ate_half" && n.Tags[1] == "needs_rest")
	}
	if len(list) != 2 || !found {
		t.Fatalf("list does not return tags: %+v", list)
	}
}

func TestPassDuty(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	database := med.DB
	broker := sse.InitBroker()
	defer broker.Close()
	shifts := &handlers.ShiftsHandler{DB: database, Broker: broker}
	alerts := &handlers.AlertsHandler{DB: database}
	hussain := &models.Caregiver{ID: "cg_hussain", Name: "Hussain", Role: models.RoleSuperAdmin}
	fatima := &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}
	ali := &models.Caregiver{ID: "cg_ali", Name: "Ali", Role: models.RoleAdmin}

	// One 24-hour shift, Hussain on duty today.
	today := time.Now().Format("2006-01-02")
	if _, err := database.Exec(`UPDATE shifts SET is_active = 0`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO shifts (id, name_ar, name_en, start_time, end_time, duration_hours, display_order) VALUES ('s24', 'يوم', 'Day', '08:00', '08:00', 24, -1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO shift_assignments (id, shift_id, caregiver_id, date) VALUES ('a1', 's24', 'cg_hussain', ?)`, today); err != nil {
		t.Fatal(err)
	}

	pass := func(who *models.Caregiver, body map[string]interface{}) (int, map[string]interface{}) {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPost, "/api/v1/shifts/handover", bytes.NewReader(b))
		r = r.WithContext(authContext(r.Context(), who))
		rec := httptest.NewRecorder()
		shifts.PassDuty(rec, r)
		var out map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	accept := func(who *models.Caregiver, id string) int {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/shifts/handover/"+id+"/accept", nil)
		rc := chi.NewRouteContext()
		rc.URLParams.Add("id", id)
		r = r.WithContext(context.WithValue(authContext(r.Context(), who), chi.RouteCtxKey, rc))
		rec := httptest.NewRecorder()
		shifts.AcceptDuty(rec, r)
		return rec.Code
	}
	count := func(q string, args ...interface{}) int {
		var n int
		if err := database.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Not on duty → 403, and nothing changed.
	if code, out := pass(ali, map[string]interface{}{"to_caregiver_id": "cg_fatima", "tags": []string{"ate_half"}}); code != http.StatusForbidden || out["error"] != "NOT_ON_DUTY" {
		t.Fatalf("not on duty: want 403 NOT_ON_DUTY, got %d %v", code, out)
	}
	if code, _ := pass(hussain, map[string]interface{}{"to_caregiver_id": "cg_zainab"}); code != http.StatusBadRequest {
		t.Fatalf("passing to a viewer: want 400, got %d", code)
	}
	if count(`SELECT COUNT(*) FROM handover_notes`)+count(`SELECT COUNT(*) FROM duty_handovers`) != 0 {
		t.Fatal("a refused pass left rows behind")
	}

	// On duty → note + moved assignment + duty record + audit + alert for Fatima only.
	code, out := pass(hussain, map[string]interface{}{"to_caregiver_id": "cg_fatima", "tags": []string{"ate_half", "needs_rest"}})
	if code != http.StatusCreated {
		t.Fatalf("pass duty: want 201, got %d %v", code, out)
	}
	dutyID := out["duty_handover_id"].(string)
	if n := count(`SELECT COUNT(*) FROM shift_assignments WHERE id = 'a1' AND caregiver_id = 'cg_fatima'`); n != 1 {
		t.Fatal("assignment not moved to Fatima")
	}
	if n := count(`SELECT COUNT(*) FROM handover_notes n JOIN duty_handovers d ON d.note_id = n.id WHERE d.id = ? AND n.caregiver_id = 'cg_hussain' AND n.tags = '["ate_half","needs_rest"]'`, dutyID); n != 1 {
		t.Fatal("handover note missing or not linked")
	}
	if n := count(`SELECT COUNT(*) FROM audit_logs WHERE action = 'DUTY_PASSED' AND entity_id = 'a1'`); n != 1 {
		t.Fatal("no audit row")
	}
	if got := unacked(t, alerts, fatima); len(got) != 1 || got[0].Kind != "duty_handover" {
		t.Fatalf("Fatima should have one duty alert, got %+v", got)
	}
	if got := unacked(t, alerts, ali); len(got) != 0 {
		t.Fatalf("the alert is for Fatima only, Ali got %+v", got)
	}
	// Hussain is no longer on duty: he can't pass it again.
	if code, _ := pass(hussain, map[string]interface{}{"to_caregiver_id": "cg_ali"}); code != http.StatusForbidden {
		t.Fatalf("second pass by the old holder: want 403, got %d", code)
	}

	// Not accepted after 15 minutes → Hussain is alerted, once.
	if err := handlers.RemindUnacceptedDuties(context.Background(), database, nil, broker, time.Now().Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got := unacked(t, alerts, hussain); len(got) != 0 {
		t.Fatalf("reminded before 15 minutes: %+v", got)
	}
	for i := 0; i < 2; i++ {
		if err := handlers.RemindUnacceptedDuties(context.Background(), database, nil, broker, time.Now().Add(16*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if got := unacked(t, alerts, hussain); len(got) != 1 || got[0].Kind != "duty_unaccepted" {
		t.Fatalf("Hussain should get one 'not accepted' alert, got %+v", got)
	}

	// Only Fatima can accept; accepting clears both alerts and is idempotent.
	if code := accept(ali, dutyID); code != http.StatusForbidden {
		t.Fatalf("Ali accepting Fatima's duty: want 403, got %d", code)
	}
	for i := 0; i < 2; i++ {
		if code := accept(fatima, dutyID); code != http.StatusOK {
			t.Fatalf("accept: %d", code)
		}
	}
	if len(unacked(t, alerts, fatima))+len(unacked(t, alerts, hussain)) != 0 {
		t.Fatal("alerts still pending after accepting")
	}
	if n := count(`SELECT COUNT(*) FROM audit_logs WHERE action = 'DUTY_ACCEPTED'`); n != 1 {
		t.Fatalf("want one DUTY_ACCEPTED audit row, got %d", n)
	}
}

// Reports during the day: ?date= lists every note of that household day.
func TestHandoversByDay(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	h := &handlers.HandoverHandler{DB: med.DB, Broker: med.Broker}
	fatima := &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}
	for _, s := range []string{"vomited after breakfast", "complained of a headache", "refused lunch"} {
		if code, out := postNote(t, h, fatima, map[string]interface{}{"shift_type": "report", "summary": s}); code != http.StatusCreated {
			t.Fatalf("report %q: %d %v", s, code, out)
		}
	}
	// One from two days ago must not show up today.
	if _, err := med.DB.Exec(`UPDATE handover_notes SET created_at = datetime('now', '-2 days') WHERE summary = 'refused lunch'`); err != nil {
		t.Fatal(err)
	}

	list := func(q string) (int, []models.HandoverNote) {
		rec := httptest.NewRecorder()
		h.ListHandovers(rec, httptest.NewRequest(http.MethodGet, "/api/v1/handovers"+q, nil))
		var out []models.HandoverNote
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	today := time.Now().In(handlers.ClinicalLocation()).Format("2006-01-02")
	if code, got := list("?date=" + today); code != http.StatusOK || len(got) != 2 || got[0].ShiftType != "report" {
		t.Fatalf("today: want 2 reports, got %d %+v", code, got)
	}
	twoDaysAgo := time.Now().In(handlers.ClinicalLocation()).AddDate(0, 0, -2).Format("2006-01-02")
	if _, got := list("?date=" + twoDaysAgo); len(got) != 1 || got[0].Summary != "refused lunch" {
		t.Fatalf("two days ago: want the one report, got %+v", got)
	}
	if code, _ := list("?date=yesterday"); code != http.StatusBadRequest {
		t.Fatalf("bad date: want 400, got %d", code)
	}
}
