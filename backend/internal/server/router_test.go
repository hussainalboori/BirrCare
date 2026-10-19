package server

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/db"
	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"

	"golang.org/x/crypto/bcrypt"
)

func setup(t *testing.T, cfg auth.Config, timeout time.Duration) (*httptest.Server, *sql.DB, *sse.Broker) {
	t.Helper()
	database, err := db.InitDB(filepath.Join(t.TempDir(), "birrcare.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	pinHash, _ := bcrypt.GenerateFromPassword([]byte("1234"), bcrypt.DefaultCost)
	_, _ = database.Exec(`
		INSERT OR IGNORE INTO caregivers (id, name, email, role, pin_hash, avatar_color, preferred_lang)
		VALUES 
			('cg_ali', 'علي', 'ali@test.local', 'admin', ?, '#2563eb', 'ar'),
			('cg_fatima', 'فاطمة', 'fatima@test.local', 'caregiver', ?, '#059669', 'ar')
	`, string(pinHash), string(pinHash))
	if err := auth.InitSessionSecret(database); err != nil {
		t.Fatal(err)
	}
	broker := sse.InitBroker()
	h := NewRouter(Options{
		DB: database, Broker: broker, Auth: cfg, Limiter: auth.NewPINLimiter(),
		RequestTimeout: timeout, MaxBodyBytes: 1 << 20,
		AllowedOrigins: []string{"http://localhost:3000"},
	})
	srv := httptest.NewServer(h)
	t.Cleanup(func() { broker.Close(); srv.Close(); database.Close() })
	return srv, database, broker
}

var prodCfg = auth.Config{} // no dev fallback, no CF, no raw-header trust

func do(t *testing.T, method, url string, body interface{}, hdr map[string]string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, url, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestProductionRequiresAuthAndIgnoresSpoofedHeader(t *testing.T) {
	srv, _, _ := setup(t, prodCfg, time.Minute)

	if r := do(t, "GET", srv.URL+"/api/v1/vitals", nil, nil); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous read: want 401, got %d", r.StatusCode)
	}
	spoof := map[string]string{"Cf-Access-Authenticated-User-Email": "hussain@family.local"}
	if r := do(t, "GET", srv.URL+"/api/v1/users", nil, spoof); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("spoofed email header: want 401, got %d", r.StatusCode)
	}
	dose := map[string]string{"medication_id": "med_aspirin", "slot": "morning", "scheduled_date": time.Now().Format("2006-01-02")}
	if r := do(t, "POST", srv.URL+"/api/v1/medications/log", dose, spoof); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("spoofed dose log: want 401, got %d", r.StatusCode)
	}
	if r := do(t, "GET", srv.URL+"/api/v1/events/stream", nil, nil); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous SSE: want 401, got %d", r.StatusCode)
	}
	if r := do(t, "GET", srv.URL+"/api/v1/caregivers", nil, nil); r.StatusCode != http.StatusOK {
		t.Fatalf("caregiver list for PIN picker: want 200, got %d", r.StatusCode)
	}
	if r := do(t, "GET", srv.URL+"/api/v1/health", nil, map[string]string{"Origin": "https://evil.example"}); r.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("CORS reflected arbitrary origin: %q", r.Header.Get("Access-Control-Allow-Origin"))
	}
}

func TestPINBruteForceLockoutAndCookieFlags(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	srv, database, _ := setup(t, prodCfg, time.Minute)

	for i := 0; i < 5; i++ {
		r := do(t, "POST", srv.URL+"/api/v1/auth/verify-pin", map[string]string{"caregiver_id": "cg_ali", "pin": "0000"}, nil)
		if r.StatusCode != http.StatusUnauthorized {
			t.Fatalf("wrong PIN %d: want 401, got %d", i, r.StatusCode)
		}
	}
	r := do(t, "POST", srv.URL+"/api/v1/auth/verify-pin", map[string]string{"caregiver_id": "cg_ali", "pin": "1234"}, nil)
	if r.StatusCode != http.StatusTooManyRequests || r.Header.Get("Retry-After") == "" {
		t.Fatalf("after 5 failures even the right PIN must be throttled: got %d", r.StatusCode)
	}
	var failures int
	database.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='PIN_FAILED' AND caregiver_id='cg_ali'`).Scan(&failures)
	if failures != 5 {
		t.Fatalf("expected 5 PIN_FAILED audit rows, got %d", failures)
	}

	// Different caregiver is unaffected; check cookie hardening.
	r = do(t, "POST", srv.URL+"/api/v1/auth/verify-pin", map[string]string{"caregiver_id": "cg_fatima", "pin": "1234"}, nil)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("correct PIN: want 200, got %d", r.StatusCode)
	}
	var session *http.Cookie
	for _, c := range r.Cookies() {
		if c.Name == auth.SessionCookieName {
			session = c
		}
	}
	if session == nil || !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie must be HttpOnly; Secure; SameSite=Strict: %+v", session)
	}

	me := do(t, "GET", srv.URL+"/api/v1/auth/me", nil, nil, session)
	var c models.Caregiver
	json.NewDecoder(me.Body).Decode(&c)
	if me.StatusCode != http.StatusOK || c.ID != "cg_fatima" {
		t.Fatalf("session not honoured: %d %+v", me.StatusCode, c)
	}
}

func TestSSEStreamOutlivesRequestTimeout(t *testing.T) {
	srv, _, broker := setup(t, auth.Config{DevFallback: true}, 100*time.Millisecond)

	resp, err := http.Get(srv.URL + "/api/v1/events/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status %d", resp.StatusCode)
	}

	go func() {
		time.Sleep(400 * time.Millisecond) // 4x the request timeout
		broker.Broadcast("MED_LOGGED", models.ActorInfo{ID: "cg_ali"}, map[string]string{"slot": "morning"})
	}()

	got := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.Contains(sc.Text(), `"MED_LOGGED"`) {
				got <- true
				return
			}
		}
		got <- false
	}()
	select {
	case ok := <-got:
		if !ok {
			t.Fatal("stream was closed before the event arrived (killed by request timeout?)")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

// The crisis screen must reach the doctor's number without a PIN session.
func TestContactsArePublic(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "birrcare.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	broker := sse.InitBroker()
	srv := httptest.NewServer(NewRouter(Options{
		DB: database, Broker: broker, Auth: prodCfg, Limiter: auth.NewPINLimiter(),
		RequestTimeout: time.Second, MaxBodyBytes: 1 << 20,
		Contacts: handlers.Contacts{EmergencyNumber: "997", DoctorName: "Dr. Noura", DoctorPhone: "+966500000000"},
	}))
	t.Cleanup(func() { broker.Close(); srv.Close(); database.Close() })

	res := do(t, "GET", srv.URL+"/api/v1/contacts", nil, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var got handlers.Contacts
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.EmergencyNumber != "997" || got.DoctorPhone != "+966500000000" || got.DoctorName != "Dr. Noura" {
		t.Fatalf("unexpected contacts: %+v", got)
	}
}

func TestChangeOwnPIN(t *testing.T) {
	srv, database, _ := setup(t, prodCfg, time.Minute)
	login := func(pin string) (*http.Response, *http.Cookie) {
		r := do(t, "POST", srv.URL+"/api/v1/auth/verify-pin", map[string]string{"caregiver_id": "cg_fatima", "pin": pin}, nil)
		for _, c := range r.Cookies() {
			if c.Name == auth.SessionCookieName {
				return r, c
			}
		}
		return r, nil
	}
	change := func(cur, next string, cookies ...*http.Cookie) int {
		return do(t, "POST", srv.URL+"/api/v1/auth/change-pin", map[string]string{"current_pin": cur, "new_pin": next}, nil, cookies...).StatusCode
	}

	if code := change("1234", "4826"); code != http.StatusUnauthorized {
		t.Fatalf("signed out: want 401, got %d", code)
	}
	_, session := login("1234")
	if session == nil {
		t.Fatal("login failed")
	}
	for _, c := range []struct {
		cur, next string
		want      int
	}{
		{"1234", "12a4", http.StatusBadRequest}, // not 4 digits
		{"1234", "7777", http.StatusBadRequest}, // repeated digit
		{"1234", "1234", http.StatusBadRequest}, // default PIN
		{"0000", "4826", http.StatusForbidden},  // wrong current PIN (403 so the UI stays signed in)
	} {
		if code := change(c.cur, c.next, session); code != c.want {
			t.Fatalf("%s -> %s: want %d, got %d", c.cur, c.next, c.want, code)
		}
	}
	if code := change("1234", "4826", session); code != http.StatusOK {
		t.Fatalf("valid change: want 200, got %d", code)
	}
	if r, _ := login("1234"); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old PIN must stop working, got %d", r.StatusCode)
	}
	if r, _ := login("4826"); r.StatusCode != http.StatusOK {
		t.Fatalf("new PIN must work, got %d", r.StatusCode)
	}
	var n int
	database.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action='PIN_CHANGED' AND caregiver_id='cg_fatima' AND details NOT LIKE '%4826%'`).Scan(&n)
	if n != 1 {
		t.Fatalf("expected one PIN_CHANGED audit row without the PIN in it, got %d", n)
	}
}
