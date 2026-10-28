package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
)

// Issue #64: crush, split and food guidance per medicine, and a recent
// "swallowing difficulty" handover tag.

func TestSwallowGuidanceFields(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()
	base := func(name string) map[string]interface{} {
		return map[string]interface{}{"name_ar": name, "name_en": name, "dosage": "10 mg", "frequency_slots": []string{"morning"}}
	}

	// Default: ask the pharmacist, split not known, any time.
	code, out := createMedication(t, h, base("Plain"))
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, out)
	}
	plainID := out["id"].(string)

	slow := base("Nifedipine XL")
	slow["crush"], slow["split_ok"], slow["with_food"] = "do_not_crush", false, "with_food"
	code, out = createMedication(t, h, slow)
	if code != http.StatusCreated {
		t.Fatalf("create do_not_crush: %d %v", code, out)
	}
	slowID := out["id"].(string)

	for field, bad := range map[string]interface{}{"crush": "maybe", "with_food": "after_tea", "split_ok": "yes"} {
		m := base("Bad " + field)
		m[field] = bad
		if code, out := createMedication(t, h, m); code != http.StatusBadRequest {
			t.Errorf("%s=%v: want 400, got %d %v", field, bad, code, out)
		}
	}
	if rec := medCall(t, h.UpdateMedication, siblings[1], plainID, map[string]interface{}{"crush": "never"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("update with a bad crush: want 400, got %d", rec.Code)
	}

	// Update: crush ok, split yes; then split back to "not known" (null) leaves crush.
	if rec := medCall(t, h.UpdateMedication, siblings[1], plainID, map[string]interface{}{"crush": "ok", "split_ok": true, "with_food": "empty_stomach"}); rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if rec := medCall(t, h.UpdateMedication, siblings[1], plainID, map[string]interface{}{"split_ok": nil}); rec.Code != http.StatusOK {
		t.Fatalf("update split null: %d", rec.Code)
	}

	byID := map[string]models.Medication{}
	rec := httptest.NewRecorder()
	h.ListMedications(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var list []models.Medication
	json.Unmarshal(rec.Body.Bytes(), &list)
	for _, m := range list {
		byID[m.ID] = m
	}
	if m := byID[plainID]; m.Crush != "ok" || m.SplitOK != nil || m.WithFood != "empty_stomach" {
		t.Fatalf("plain after updates: %+v", m)
	}
	if m := byID[slowID]; m.Crush != "do_not_crush" || m.SplitOK == nil || *m.SplitOK || m.WithFood != "with_food" {
		t.Fatalf("slow-release: %+v", m)
	}

	// The schedule carries the fields, and no swallowing difficulty yet.
	schedule := func() (map[string]models.Medication, *handlers.SwallowNote) {
		rec := httptest.NewRecorder()
		h.GetSchedule(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		var out struct {
			Schedule []models.ScheduledDose `json:"schedule"`
			Swallow  *handlers.SwallowNote  `json:"swallowing_difficulty"`
		}
		json.Unmarshal(rec.Body.Bytes(), &out)
		meds := map[string]models.Medication{}
		for _, d := range out.Schedule {
			meds[d.Medication.ID] = d.Medication
		}
		return meds, out.Swallow
	}
	meds, sw := schedule()
	if meds[slowID].Crush != "do_not_crush" || meds[slowID].WithFood != "with_food" || sw != nil {
		t.Fatalf("schedule: %+v swallow %+v", meds[slowID], sw)
	}

	// A handover tagged "swallowing difficulty" 8 days ago doesn't count; 2 days ago does.
	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	recent := time.Now().UTC().Add(-48 * time.Hour).Format("2006-01-02 15:04:05")
	for i, at := range []string{old, recent} {
		if _, err := h.DB.Exec(`INSERT INTO handover_notes (id, caregiver_id, created_at, shift_type, summary, urgent_flag, tags)
			VALUES (?, 'cg_fatima', ?, 'morning', 'Coughed with water', 1, '["swallowing_difficulty","pain"]')`, []string{"n_old", "n_new"}[i], at); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if _, sw := schedule(); sw != nil {
				t.Fatalf("an 8-day-old tag must not count: %+v", sw)
			}
		}
	}
	if _, sw := schedule(); sw == nil || sw.By == "" {
		t.Fatalf("a tag 2 days ago: want swallowing_difficulty with who, got %+v", sw)
	}
}

func TestEmergencyCardSwallowing(t *testing.T) {
	ins, _ := setupInsulin(t)
	db := ins.DB
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT OR IGNORE INTO patient_profile (id) VALUES (1)`)
	exec(`UPDATE patient_profile SET emergency_token_hash = ? WHERE id = 1`, handlers.EmergencyTokenHashForTest(emergencyTestToken))
	exec(`INSERT INTO medications (id, name_ar, name_en, dosage, frequency_slots, crush, split_ok, created_by)
		VALUES ('med_nif', 'نيفيديبين', 'Nifedipine XL', '30 mg', '["morning"]', 'do_not_crush', 0, 'cg_hussain')`)
	h := &handlers.EmergencyHandler{DB: db, Broker: ins.Broker, Limiter: handlers.NewIPRateLimiter(100)}

	card, _ := emergencyCard(t, h)
	if card.SwallowingDifficulty != nil {
		t.Fatalf("no tag yet: %+v", card.SwallowingDifficulty)
	}
	var nif *handlers.EmergencyMed
	for i := range card.Medications {
		if card.Medications[i].NameEn == "Nifedipine XL" {
			nif = &card.Medications[i]
		}
	}
	if nif == nil || nif.Crush != "do_not_crush" || nif.SplitOK == nil || *nif.SplitOK {
		t.Fatalf("emergency med: %+v", nif)
	}

	exec(`INSERT INTO handover_notes (id, caregiver_id, created_at, shift_type, summary, urgent_flag, tags)
		VALUES ('n1', 'cg_fatima', datetime('now', '-1 day'), 'morning', 'Coughing with water', 1, '["swallowing_difficulty"]')`)
	card, raw := emergencyCard(t, h)
	if card.SwallowingDifficulty == nil || card.SwallowingDifficulty.At == "" {
		t.Fatalf("recent tag: want swallowing_difficulty, got %s", raw)
	}
	if card.SwallowingDifficulty.By != "" {
		t.Fatal("no names on the public card")
	}
}
