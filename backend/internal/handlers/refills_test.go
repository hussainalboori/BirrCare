package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
)

// Issue #65: "I'll get it" — an owner for each low-stock warning, and follow-ups.

func claimRefill(t *testing.T, h *handlers.MedicationHandler, who *models.Caregiver, medID string) (int, handlers.RefillTask) {
	t.Helper()
	rec := medCall(t, h.ClaimRefill, who, medID, map[string]interface{}{})
	var task handlers.RefillTask
	json.Unmarshal(rec.Body.Bytes(), &task)
	return rec.Code, task
}

func stockList(t *testing.T, h *handlers.MedicationHandler) map[string]handlers.StockStatus {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ListStock(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var list []handlers.StockStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("stock: %d %s", rec.Code, rec.Body.String())
	}
	out := map[string]handlers.StockStatus{}
	for _, s := range list {
		out[s.MedicationID] = s
	}
	return out
}

func TestRefillClaimAndDone(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	alerts := &handlers.AlertsHandler{DB: med.DB}
	trackAspirin(t, med, 30)
	mustExec(t, med, `UPDATE medications SET pharmacy_name = 'Al-Nahdi', pharmacy_phone = '+966 11 000 0000', needs_prescription = 1 WHERE id = 'med_aspirin'`)

	// Counting 3 pills (3 days, threshold 4) raises the low-stock alert and opens a task.
	if code, _ := postStock(t, med, siblings[0], map[string]interface{}{"action": "count", "units": 3}); code != http.StatusOK {
		t.Fatalf("count: %d", code)
	}
	st := stockList(t, med)["med_aspirin"]
	if !st.Low || st.Refill == nil || st.Refill.ClaimedBy != "" || !st.NeedsPrescription || st.PharmacyPhone != "+966 11 000 0000" {
		t.Fatalf("low with an open, unclaimed task and the pharmacy: %+v %+v", st, st.Refill)
	}
	kinds := func(who *models.Caregiver) string {
		var k []string
		for _, a := range unacked(t, alerts, who) {
			k = append(k, a.Kind)
		}
		return strings.Join(k, ",")
	}
	if kinds(siblings[1]) != "low_stock" || kinds(siblings[2]) != "low_stock" {
		t.Fatalf("Ali and Fatima see the low-stock alert: %q %q", kinds(siblings[1]), kinds(siblings[2]))
	}

	// Fatima: "I'll get it". The alert goes quiet for everyone; all see who and by when.
	if code, _ := claimRefill(t, med, &models.Caregiver{ID: "cg_zainab", Role: models.RoleViewer}, "med_aspirin"); code != http.StatusForbidden {
		t.Fatalf("viewer claim: want 403, got %d", code)
	}
	code, task := claimRefill(t, med, siblings[2], "med_aspirin")
	if code != http.StatusOK || task.ClaimedBy != "cg_fatima" || task.DueBy == "" {
		t.Fatalf("claim: %d %+v", code, task)
	}
	due, _ := time.Parse(time.RFC3339, task.DueBy)
	if h := due.In(handlers.ClinicalLocation()).Hour(); h != 12 || !due.After(time.Now()) {
		t.Fatalf("due by the next noon at home, got %s", task.DueBy)
	}
	if k := kinds(siblings[1]); k != "" {
		t.Fatalf("after the claim Ali's low-stock alert is quiet, got %q", k)
	}
	if st := stockList(t, med)["med_aspirin"]; st.Refill == nil || st.Refill.ClaimedByName != siblings[2].Name {
		t.Fatalf("everyone sees Fatima is getting it: %+v", st.Refill)
	}
	var n int
	med.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'REFILL_CLAIMED' AND entity_id = 'med_aspirin'`).Scan(&n)
	if n != 1 {
		t.Fatalf("claim audited once, got %d", n)
	}

	// A new box (even one that leaves it under the threshold) closes the task.
	if code, _ := postStock(t, med, siblings[2], map[string]interface{}{"action": "refill", "units": 0.5}); code != http.StatusOK {
		t.Fatalf("refill: %d", code)
	}
	var doneBy string
	med.DB.QueryRow(`SELECT COALESCE(done_by, '') FROM refill_tasks WHERE id = ? AND done_at IS NOT NULL`, task.ID).Scan(&doneBy)
	if doneBy != "cg_fatima" {
		t.Fatalf("task done by Fatima, got %q", doneBy)
	}
	if st := stockList(t, med)["med_aspirin"]; st.Refill != nil {
		t.Fatalf("no open task after the refill: %+v", st.Refill)
	}
	// ...and the watcher doesn't reopen it although the medicine is still low.
	if err := handlers.CheckRefillTasks(context.Background(), med.DB, nil, med.Broker, time.Now()); err != nil {
		t.Fatal(err)
	}
	if st := stockList(t, med)["med_aspirin"]; st.Refill != nil {
		t.Fatalf("watcher reopened a task just closed by a refill: %+v", st.Refill)
	}
	// Not low: nothing to claim.
	postStock(t, med, siblings[2], map[string]interface{}{"action": "refill", "units": 60})
	if code, _ := claimRefill(t, med, siblings[1], "med_aspirin"); code != http.StatusConflict {
		t.Fatalf("claiming a medicine that isn't low: want 409, got %d", code)
	}
}

func TestRefillFollowUps(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	trackAspirin(t, med, 30)
	mustExec(t, med, `UPDATE medications SET is_critical = 1 WHERE id = 'med_aspirin'`)
	postStock(t, med, siblings[0], map[string]interface{}{"action": "count", "units": 2}) // 2 days left
	start := time.Now()
	check := func(at time.Time) {
		t.Helper()
		if err := handlers.CheckRefillTasks(context.Background(), med.DB, nil, med.Broker, at); err != nil {
			t.Fatal(err)
		}
	}
	followups := func() []string {
		rows, err := med.DB.Query(`SELECT json_extract(payload, '$.stage') || '>' || COALESCE(target_caregiver_id, 'all') || ':' || json_extract(payload, '$.critical')
			FROM alerts WHERE kind = 'refill_followup' ORDER BY created_at`)
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

	check(start.Add(11 * time.Hour))
	if got := followups(); len(got) != 0 {
		t.Fatalf("before 12 hours: %v", got)
	}
	check(start.Add(12*time.Hour + time.Minute))
	check(start.Add(13 * time.Hour))
	if got := followups(); len(got) != 1 || got[0] != "unclaimed>all:1" {
		t.Fatalf("unclaimed for 12 h: one red follow-up to everyone, got %v", got)
	}

	// Ali claims; nothing until the due time, then the owner, then everyone 4 h later.
	_, task := claimRefill(t, med, siblings[1], "med_aspirin")
	due, _ := time.Parse(time.RFC3339, task.DueBy)
	var resolved int
	med.DB.QueryRow(`SELECT COUNT(*) FROM alerts WHERE kind IN ('low_stock', 'refill_followup') AND resolved_at IS NULL`).Scan(&resolved)
	if resolved != 0 {
		t.Fatalf("the claim quiets the low-stock alert and the follow-up, %d still open", resolved)
	}
	check(due.Add(-time.Minute))
	if got := followups(); len(got) != 1 {
		t.Fatalf("before the due time: %v", got)
	}
	check(due)
	check(due.Add(time.Hour))
	if got := followups(); len(got) != 2 || got[1] != "owner>cg_ali:1" {
		t.Fatalf("past the due time: one reminder to Ali, got %v", got)
	}
	check(due.Add(4 * time.Hour))
	check(due.Add(5 * time.Hour))
	if got := followups(); len(got) != 3 || got[2] != "everyone>all:1" {
		t.Fatalf("4 h later: everyone, once, got %v", got)
	}
}

func TestPharmacyFieldsValidate(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	base := map[string]interface{}{"name_ar": "س", "name_en": "S", "dosage": "1", "frequency_slots": []string{"morning"}}
	bad := map[string]interface{}{}
	for k, v := range base {
		bad[k] = v
	}
	bad["pharmacy_phone"] = "call me"
	if code, out := createMedication(t, h, bad); code != http.StatusBadRequest {
		t.Fatalf("bad phone: want 400, got %d %v", code, out)
	}
	base["pharmacy_name"], base["pharmacy_phone"], base["needs_prescription"] = " Al-Nahdi ", "0112345678", true
	code, out := createMedication(t, h, base)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, out)
	}
	var name, phone string
	var rx bool
	h.DB.QueryRow(`SELECT pharmacy_name, pharmacy_phone, needs_prescription FROM medications WHERE id = ?`, out["id"]).Scan(&name, &phone, &rx)
	if name != "Al-Nahdi" || phone != "0112345678" || !rx {
		t.Fatalf("saved: %q %q %v", name, phone, rx)
	}
	if rec := medCall(t, h.UpdateMedication, siblings[1], out["id"].(string), map[string]interface{}{"needs_prescription": false}); rec.Code != http.StatusOK {
		t.Fatalf("update: %d", rec.Code)
	}
	h.DB.QueryRow(`SELECT pharmacy_name, needs_prescription FROM medications WHERE id = ?`, out["id"]).Scan(&name, &rx)
	if name != "Al-Nahdi" || rx {
		t.Fatalf("update changes only what was sent: %q %v", name, rx)
	}
}
