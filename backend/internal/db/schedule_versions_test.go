package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// Issue #57: an existing database gets exactly one schedule version per medicine,
// and running the migration again changes nothing.
func TestBackfillScheduleVersions(t *testing.T) {
	t.Setenv("APP_TIMEZONE", "Asia/Riyadh")
	d, err := InitDB(filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := Migrate(d); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := d.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Medicines from before the history existed: one still given, one stopped
	// (with its deactivation in the audit log), one stopped with no record of when.
	exec(`INSERT INTO caregivers (id, name, email, role, pin_hash) VALUES ('cg_a', 'Ali', 'a@x', 'admin', 'h')`)
	exec(`INSERT INTO medications (id, name_ar, name_en, dosage, frequency_slots, slot_times, created_at) VALUES
		('m_on', 'أ', 'On', '5 mg', '["morning","evening"]', '{"morning":"08:00","evening":"20:00"}', '2026-09-30 22:30:00'),
		('m_off', 'ب', 'Off', '81 mg', '["morning"]', '{"morning":"08:00"}', '2026-09-01 08:00:00'),
		('m_unknown', 'ج', 'Unknown', '1 g', '["morning"]', '{"morning":"08:00"}', '2026-09-01 08:00:00')`)
	exec(`UPDATE medications SET is_active = 0 WHERE id IN ('m_off', 'm_unknown')`)
	exec(`INSERT INTO audit_logs (id, caregiver_id, action, entity_type, entity_id, details, created_at)
		VALUES ('au1', 'cg_a', 'DEACTIVATE_MEDICATION', 'medication', 'm_off', 'Deactivated prescription', '2026-10-10 06:00:00')`)

	for run := 1; run <= 2; run++ {
		if err := Migrate(d); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}
	type v struct{ from, to, slots, dosage string }
	got := map[string][]v{}
	rows, err := d.Query(`SELECT medication_id, effective_from, COALESCE(effective_to, ''), frequency_slots, dosage FROM medication_schedule_versions`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		var x v
		rows.Scan(&id, &x.from, &x.to, &x.slots, &x.dosage)
		got[id] = append(got[id], x)
	}
	rows.Close()
	// 22:30 UTC on 30 Sep is 1 Oct in Riyadh.
	if w := []v{{"2026-10-01", "", `["morning","evening"]`, "5 mg"}}; len(got["m_on"]) != 1 || got["m_on"][0] != w[0] {
		t.Errorf("m_on: want %v, got %v", w, got["m_on"])
	}
	if w := []v{{"2026-09-01", "2026-10-09", `["morning"]`, "81 mg"}}; len(got["m_off"]) != 1 || got["m_off"][0] != w[0] {
		t.Errorf("m_off: want it to end the day before it was stopped, got %v", got["m_off"])
	}
	if len(got["m_unknown"]) != 1 || got["m_unknown"][0].to != "2026-08-31" {
		t.Errorf("m_unknown: a stop of unknown date expects nothing, got %v", got["m_unknown"])
	}
	var stoppedOn, stoppedBy sql.NullString
	d.QueryRow(`SELECT stopped_on, stopped_by FROM medications WHERE id = 'm_off'`).Scan(&stoppedOn, &stoppedBy)
	if stoppedOn.String != "2026-10-10" || stoppedBy.String != "cg_a" {
		t.Errorf("m_off stop: %v %v", stoppedOn, stoppedBy)
	}
}
