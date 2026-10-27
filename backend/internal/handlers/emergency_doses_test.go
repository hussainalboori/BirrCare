package handlers_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"github.com/go-chi/chi/v5"
)

// Issue #56: the emergency card says when she last took her blood thinner, her
// last insulin and the as-needed doses of the last 24 hours.

const emergencyTestToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func emergencyCard(t *testing.T, h *handlers.EmergencyHandler) (handlers.EmergencyCard, string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "198.51.100.20:1"
	rc := chi.NewRouteContext()
	rc.URLParams.Add("token", emergencyTestToken)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	h.Card(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("card: %d %s", rec.Code, rec.Body.String())
	}
	var card handlers.EmergencyCard
	if err := json.Unmarshal(rec.Body.Bytes(), &card); err != nil {
		t.Fatal(err)
	}
	return card, rec.Body.String()
}

func TestEmergencyCardRecentDoses(t *testing.T) {
	ins, planID := setupInsulin(t)
	db := ins.DB
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	ts := func(d time.Duration) string { return time.Now().UTC().Add(-d).Format("2006-01-02 15:04:05") }

	exec(`INSERT OR IGNORE INTO patient_profile (id) VALUES (1)`)
	exec(`UPDATE patient_profile SET emergency_token_hash = ? WHERE id = 1`, handlers.EmergencyTokenHashForTest(emergencyTestToken))
	exec(`INSERT INTO medications (id, name_ar, name_en, dosage, frequency_slots, is_critical, created_by)
		VALUES ('med_apix', 'أبيكسابان', 'Apixaban', '5 mg', '["morning","bedtime"]', 1, 'cg_hussain')`)
	exec(`INSERT INTO medications (id, name_ar, name_en, dosage, frequency_slots, is_prn, created_by)
		VALUES ('med_para', 'باراسيتامول', 'Paracetamol', '1 g', '[]', 1, 'cg_hussain')`)

	// Apixaban: a later entry that was cancelled, and the real one 8 h 15 min ago.
	exec(`INSERT INTO medication_logs (id, medication_id, caregiver_id, slot, scheduled_date, taken_at, recorded_at, status)
		VALUES ('l_real', 'med_apix', 'cg_ali', 'bedtime', '2026-10-26', ?, ?, 'taken')`, ts(8*time.Hour+15*time.Minute), ts(8*time.Hour))
	exec(`INSERT INTO medication_logs (id, medication_id, caregiver_id, slot, scheduled_date, taken_at, recorded_at, status, voided_at, voided_by, void_reason)
		VALUES ('l_void', 'med_apix', 'cg_ali', 'morning', '2026-10-27', ?, ?, 'taken', ?, 'cg_ali', 'wrong medicine')`, ts(time.Hour), ts(time.Hour), ts(50*time.Minute))
	// Insulin 2 h ago; a later held check doesn't count.
	exec(`INSERT INTO insulin_logs (id, plan_id, caregiver_id, given_at, status, units, blood_sugar, reason, scale_snapshot)
		VALUES ('i1', ?, 'cg_fatima', ?, 'given', 4, 12.5, '', '{}')`, planID, ts(2*time.Hour))
	exec(`INSERT INTO insulin_logs (id, plan_id, caregiver_id, given_at, status, units, blood_sugar, reason, scale_snapshot)
		VALUES ('i2', ?, 'cg_fatima', ?, 'held', 0, 5.0, '', '{}')`, planID, ts(30*time.Minute))
	// Paracetamol ×3 in 24 h, plus one cancelled and one from yesterday.
	for i, d := range []time.Duration{20 * time.Hour, 12 * time.Hour, 3 * time.Hour} {
		exec(`INSERT INTO prn_logs (id, medication_id, caregiver_id, given_at, reason_tag) VALUES (?, 'med_para', 'cg_ali', ?, 'pain')`, "p"+string(rune('a'+i)), ts(d))
	}
	exec(`INSERT INTO prn_logs (id, medication_id, caregiver_id, given_at, reason_tag, voided_at) VALUES ('pv', 'med_para', 'cg_ali', ?, 'pain', ?)`, ts(time.Hour), ts(time.Hour))
	exec(`INSERT INTO prn_logs (id, medication_id, caregiver_id, given_at, reason_tag) VALUES ('pold', 'med_para', 'cg_ali', ?, 'pain')`, ts(30*time.Hour))

	h := &handlers.EmergencyHandler{DB: db, Broker: ins.Broker, Limiter: handlers.NewIPRateLimiter(100)}
	card, raw := emergencyCard(t, h)

	var apix *handlers.EmergencyMed
	for i := range card.Medications {
		if card.Medications[i].NameEn == "Apixaban" {
			apix = &card.Medications[i]
		}
	}
	if apix == nil || apix.LastDose == nil {
		t.Fatalf("Apixaban should carry its last dose: %+v", card.Medications)
	}
	if apix.LastDose.Status != "taken" || math.Abs(apix.LastDose.HoursAgo-8.25) > 0.1 || apix.LastDose.Vomited {
		t.Fatalf("last dose should be the real one 8.25 h ago, not the cancelled one: %+v", apix.LastDose)
	}
	if at, _ := time.Parse(time.RFC3339, apix.LastDose.At); time.Since(at) < 8*time.Hour {
		t.Fatalf("last_dose.at is the cancelled entry: %s", apix.LastDose.At)
	}

	if card.LastInsulin == nil || card.LastInsulin.Units != 4 || card.LastInsulin.PlanNameEn != "NovoRapid" || card.LastInsulin.BloodSugar == nil || *card.LastInsulin.BloodSugar != 12.5 {
		t.Fatalf("last insulin should be the 4 units given 2 h ago: %+v", card.LastInsulin)
	}
	if len(card.PRN24h) != 1 || card.PRN24h[0].Count != 3 || card.PRN24h[0].TotalDose != "3 g" {
		t.Fatalf("paracetamol ×3 in 24 h (3 g): %+v", card.PRN24h)
	}

	// Partial, then vomited.
	exec(`INSERT INTO medication_logs (id, medication_id, caregiver_id, slot, scheduled_date, taken_at, recorded_at, status)
		VALUES ('l_part', 'med_apix', 'cg_ali', 'morning', '2026-10-27', ?, ?, 'partial')`, ts(10*time.Minute), ts(10*time.Minute))
	exec(`INSERT INTO medication_log_events (id, log_id, type, minutes_after, caregiver_id) VALUES ('e1', 'l_part', 'vomited', 5, 'cg_ali')`)
	card, raw = emergencyCard(t, h)
	for _, m := range card.Medications {
		if m.NameEn == "Apixaban" && (m.LastDose == nil || m.LastDose.Status != "partial" || !m.LastDose.Vomited) {
			t.Fatalf("partial dose then vomited: %+v", m.LastDose)
		}
	}

	// Nothing private on a public page.
	low := strings.ToLower(raw)
	for _, bad := range []string{"pin", "email", "audit", "hash", "test.local", "cg_"} {
		if strings.Contains(low, bad) {
			t.Errorf("the public card must not contain %q", bad)
		}
	}
}

func TestTotalDose(t *testing.T) {
	for _, c := range []struct {
		dosage string
		count  int
		want   string
	}{{"1 g", 3, "3 g"}, {"500mg", 3, "1500 mg"}, {"2.5 mg", 2, "5 mg"}, {"1 tablet twice", 2, ""}, {"", 1, ""}} {
		if got := handlers.TotalDoseForTest(c.dosage, c.count); got != c.want {
			t.Errorf("%q ×%d: want %q, got %q", c.dosage, c.count, c.want, got)
		}
	}
}
