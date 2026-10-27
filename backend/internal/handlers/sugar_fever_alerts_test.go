package handlers_test

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/push"
)

// Issue #54: low/very high blood sugar and fever raise alerts, and one list says
// which alerts are emergencies.

type alertRow struct {
	kind, target string
}

func alertsFor(t *testing.T, med *handlers.MedicationHandler) []alertRow {
	t.Helper()
	rows, err := med.DB.Query(`SELECT kind, COALESCE(target_caregiver_id, '') FROM alerts ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []alertRow
	for rows.Next() {
		var a alertRow
		rows.Scan(&a.kind, &a.target)
		out = append(out, a)
	}
	return out
}

// pushRecorder is a fake push service; each caregiver's phone has its own path.
func pushRecorder(t *testing.T, med *handlers.MedicationHandler, caregivers ...string) (*push.Notifier, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var got []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, strings.TrimPrefix(r.URL.Path, "/"))
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)
	n, err := push.Init(med.DB)
	if err != nil {
		t.Fatal(err)
	}
	n.SetHTTPClient(srv.Client())
	for _, cg := range caregivers {
		k, _ := ecdh.P256().GenerateKey(rand.Reader)
		auth := make([]byte, 16)
		rand.Read(auth)
		if _, err := med.DB.Exec(`INSERT INTO push_subscriptions (id, caregiver_id, endpoint, p256dh, auth) VALUES (?, ?, ?, ?, ?)`,
			"sub_"+cg, cg, srv.URL+"/"+cg, base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(auth)); err != nil {
			t.Fatal(err)
		}
	}
	// Pushes are sent in the background: wait until n arrive (or time out).
	return n, func() []string {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			l := len(got)
			mu.Unlock()
			if l >= len(caregivers)-1 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		time.Sleep(100 * time.Millisecond) // and nothing more
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

func putOnDuty(t *testing.T, med *handlers.MedicationHandler, caregiverID string) {
	t.Helper()
	if _, err := med.DB.Exec(`INSERT INTO shifts (id, name_ar, name_en, start_time, end_time, duration_hours, is_active, display_order)
		VALUES ('shift_all', 'كامل', 'All day', '00:00', '23:59', 24, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := med.DB.Exec(`INSERT INTO shift_assignments (id, shift_id, caregiver_id, date) VALUES ('as_1', 'shift_all', ?, ?)`,
		caregiverID, time.Now().In(handlers.ClinicalLocation()).Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
}

func TestLowSugarAlertsEveryoneAndPushes(t *testing.T) {
	med, vitals, cleanup := setupTestDB(t)
	defer cleanup()
	n, pushed := pushRecorder(t, med, "cg_fatima", "cg_ali", "cg_hussain")
	vitals.Push = n

	code, out := postVitalsCode(t, vitals, map[string]interface{}{"blood_sugar": 2.9}) // logged by Fatima
	if code != http.StatusCreated {
		t.Fatalf("want 201, got %d %v", code, out)
	}
	if got := alertsFor(t, med); !reflect.DeepEqual(got, []alertRow{{"sugar_low", ""}}) {
		t.Fatalf("want one sugar_low for everyone, got %v", got)
	}
	got := pushed()
	if len(got) != 2 || strings.Contains(strings.Join(got, ","), "cg_fatima") {
		t.Fatalf("want a push to every other caregiver (ali, hussain), got %v", got)
	}

	// Very high sugar: orange, to whoever is on duty.
	putOnDuty(t, med, "cg_ali")
	med.DB.Exec(`DELETE FROM alerts`)
	postVitalsCode(t, vitals, map[string]interface{}{"blood_sugar": 15.2})
	if got := alertsFor(t, med); !reflect.DeepEqual(got, []alertRow{{"sugar_very_high", "cg_ali"}}) {
		t.Fatalf("want sugar_very_high for the on-duty caregiver, got %v", got)
	}

	// Normal sugar: nothing.
	med.DB.Exec(`DELETE FROM alerts`)
	postVitalsCode(t, vitals, map[string]interface{}{"blood_sugar": 6.1})
	if got := alertsFor(t, med); len(got) != 0 {
		t.Fatalf("normal sugar must not alert, got %v", got)
	}
}

func TestFeverAlerts(t *testing.T) {
	med, vitals, cleanup := setupTestDB(t)
	defer cleanup()
	handlers.SetOxygenFeverThresholds(handlers.OxygenFeverThresholds{SpO2Warn: 92, SpO2Critical: 88, FeverC: 38.0})

	// Nobody on duty: a fever reaches everyone rather than nobody.
	postVitalsCode(t, vitals, map[string]interface{}{"temperature_c": 38.4})
	if got := alertsFor(t, med); !reflect.DeepEqual(got, []alertRow{{"fever", ""}}) {
		t.Fatalf("no one on duty: want fever for everyone, got %v", got)
	}

	putOnDuty(t, med, "cg_ali")
	cases := []struct {
		name string
		body map[string]interface{}
		want []alertRow
	}{
		{"fever 38.4 → on-duty caregiver", map[string]interface{}{"temperature_c": 38.4}, []alertRow{{"fever", "cg_ali"}}},
		{"38.2 with SpO2 91 → fever_high to everyone", map[string]interface{}{"temperature_c": 38.2, "spo2": 91}, []alertRow{{"fever_high", ""}}},
		{"39.0 (default high) → fever_high", map[string]interface{}{"temperature_c": 39.0}, []alertRow{{"fever_high", ""}}},
		{"37.9 → nothing", map[string]interface{}{"temperature_c": 37.9}, nil},
		{"38.5 with SpO2 86 → oxygen first, then high fever", map[string]interface{}{"temperature_c": 38.5, "spo2": 86}, []alertRow{{"spo2_low", ""}, {"fever_high", ""}}},
	}
	for _, c := range cases {
		med.DB.Exec(`DELETE FROM alerts`)
		if code, out := postVitalsCode(t, vitals, c.body); code != http.StatusCreated {
			t.Fatalf("%s: %d %v", c.name, code, out)
		}
		if got := alertsFor(t, med); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, got)
		}
	}
}

func TestHighFeverThresholdSetting(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	defer handlers.SetOxygenFeverThresholds(handlers.OxygenFeverThresholds{SpO2Warn: 92, SpO2Critical: 88, FeverC: 38.0})
	h := &handlers.ClinicalSettingsHandler{DB: med.DB}
	admin := &models.Caregiver{ID: "cg_ali", Role: models.RoleAdmin}

	for _, bad := range []string{`{"fever_high_c": 38.0}`, `{"fever_high_c": 40.5}`, `{"fever_c": 38.8, "fever_high_c": 38.6}`} {
		if rec := settingsPut(h, admin, bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", bad, rec.Code)
		}
	}
	if rec := settingsPut(h, admin, `{"fever_high_c": 39.5}`); rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", rec.Code, rec.Body.String())
	}
	handlers.SetOxygenFeverThresholds(handlers.OxygenFeverThresholds{})
	handlers.LoadClinicalSettings(med.DB)
	var got handlers.ClinicalSettings
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.FeverHighC != 39.5 {
		t.Fatalf("fever_high_c should persist across a restart, got %+v", got)
	}
}

func settingsPut(h *handlers.ClinicalSettingsHandler, who *models.Caregiver, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
	r = r.WithContext(authContext(r.Context(), who))
	rec := httptest.NewRecorder()
	h.Put(rec, r)
	return rec
}

func TestHeldInsulinWithLowSugarAlerts(t *testing.T) {
	h, plan := setupInsulin(t)
	rec := call(h.LogInsulin, insCaregiver, handlers.LogInsulinRequest{PlanID: plan, Status: "held", BloodSugar: f(3.2)})
	if rec.Code != http.StatusCreated {
		t.Fatalf("held: %d %s", rec.Code, rec.Body.String())
	}
	var kind, ref string
	if err := h.DB.QueryRow(`SELECT kind, COALESCE(ref_id, '') FROM alerts`).Scan(&kind, &ref); err != nil || kind != "sugar_low" {
		t.Fatalf("want a sugar_low alert, got %q %v", kind, err)
	}
	var sugar float64
	if err := h.DB.QueryRow(`SELECT blood_sugar FROM vitals_logs WHERE id = ?`, ref).Scan(&sugar); err != nil || sugar != 3.2 {
		t.Fatalf("the alert should point at the recorded reading, got %v %v", sugar, err)
	}
	// A normal reading at the next check raises nothing new.
	call(h.LogInsulin, insCaregiver, handlers.LogInsulinRequest{PlanID: plan, Status: "held", BloodSugar: f(6.0)})
	var n int
	h.DB.QueryRow(`SELECT COUNT(*) FROM alerts`).Scan(&n)
	if n != 1 {
		t.Fatalf("want 1 alert, got %d", n)
	}
}

func TestEmergencyAlertsBlockBackupAndUpdate(t *testing.T) {
	for _, kind := range []string{"spo2_low", "sugar_low"} {
		t.Run(kind, func(t *testing.T) {
			bh, med, _ := backupFixture(t)
			now := time.Now().In(handlers.ClinicalLocation())
			raw, _ := json.Marshal(handlers.BackupSchedule{Times: []string{now.Add(-time.Minute).Format("15:04")}, AvoidBusy: true})
			med.DB.Exec(`INSERT INTO app_settings (key, value) VALUES ('backup_schedule', ?)`, string(raw))
			if _, err := med.DB.Exec(`INSERT INTO alerts (id, kind, created_by, created_at) VALUES ('al1', ?, 'cg_fatima', datetime('now', '-10 minutes'))`, kind); err != nil {
				t.Fatal(err)
			}
			var d handlers.BackupDueForTest
			json.NewDecoder(internalCall(bh.Due, "127.0.0.1:1", testBackupSecret, nil).Body).Decode(&d)
			if d.Due || d.Reason != "crisis" {
				t.Fatalf("backup: want postponed for crisis, got %+v", d)
			}

			uh, db, _ := updatesFixture(t)
			if _, err := db.Exec(`INSERT INTO alerts (id, kind, created_by, created_at) VALUES ('al1', ?, 'cg1', datetime('now', '-10 minutes'))`, kind); err != nil {
				t.Fatal(err)
			}
			rec := callUpdate(uh.Install, asRole("sa1", "super_admin"), http.MethodPost, `{"target_sha":"`+target+`"}`)
			var body map[string]interface{}
			json.NewDecoder(rec.Body).Decode(&body)
			if rec.Code != http.StatusConflict || body["error"] != "ACTIVE_CRISIS" {
				t.Fatalf("update: want 409 ACTIVE_CRISIS, got %d %v", rec.Code, body)
			}
		})
	}
}

func TestUnackedMarksEmergencies(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	med.DB.Exec(`INSERT INTO alerts (id, kind, created_by, created_at) VALUES ('a1', 'sugar_low', 'cg_ali', datetime('now')), ('a2', 'fever', 'cg_ali', datetime('now'))`)
	h := &handlers.AlertsHandler{DB: med.DB}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(authContext(r.Context(), &models.Caregiver{ID: "cg_fatima", Role: models.RoleCaregiver}))
	rec := httptest.NewRecorder()
	h.Unacked(rec, r)
	var got []handlers.Alert
	json.Unmarshal(rec.Body.Bytes(), &got)
	flags := map[string]bool{}
	for _, a := range got {
		flags[a.Kind] = a.Emergency
	}
	if !reflect.DeepEqual(flags, map[string]bool{"sugar_low": true, "fever": false}) {
		t.Fatalf("emergency flags: %v", flags)
	}
}

// One source of truth: the shared fixture, the Go list and the alerts table agree,
// and no SQL string carries its own copy of the emergency list.
func TestAlertKindsSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../clinical/alert-kinds.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Kinds     []string `json:"kinds"`
		Emergency []string `json:"emergency"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.Emergency, models.EmergencyAlertKinds) {
		t.Fatalf("models.EmergencyAlertKinds %v must match clinical/alert-kinds.json %v", models.EmergencyAlertKinds, f.Emergency)
	}
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	for i, k := range f.Kinds {
		if _, err := med.DB.Exec(`INSERT INTO alerts (id, kind) VALUES (?, ?)`, "k"+string(rune('a'+i)), k); err != nil {
			t.Errorf("alerts table rejects kind %q: %v", k, err)
		}
	}

	files, _ := filepath.Glob("*.go")
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, _ := os.ReadFile(name)
		if strings.Contains(string(src), "'bp_crisis', 'bp_low', 'fast_positive'") {
			t.Errorf("%s has its own copy of the emergency list; use models.EmergencyAlertKindsSQL()", name)
		}
	}
}
