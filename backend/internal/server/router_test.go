package server

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/base32"
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

// A leaked PIN alone must never sign in a Super Admin who has 2FA on.
func TestSuperAdminTwoFactor(t *testing.T) {
	t.Setenv("SUPER_ADMIN_PIN", "2468")
	srv, _, _ := setup(t, prodCfg, time.Minute)
	const id = "cg_superadmin"
	sessionOf := func(r *http.Response) *http.Cookie {
		for _, c := range r.Cookies() {
			if c.Name == auth.SessionCookieName && c.Value != "" {
				return c
			}
		}
		return nil
	}
	decode := func(r *http.Response, v interface{}) {
		defer r.Body.Close()
		json.NewDecoder(r.Body).Decode(v)
	}
	codeAt := func(secret string, at time.Time) string {
		key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
		if err != nil {
			t.Fatal(err)
		}
		return auth.TOTPCodeAt(key, at)
	}
	pinLogin := func() *http.Response {
		return do(t, "POST", srv.URL+"/api/v1/auth/verify-pin", map[string]string{"caregiver_id": id, "pin": "2468"}, nil)
	}

	r := pinLogin()
	oldSession := sessionOf(r)
	if r.StatusCode != http.StatusOK || oldSession == nil {
		t.Fatalf("PIN login before 2FA: %d", r.StatusCode)
	}

	// Setup needs the PIN again; caregivers/admins cannot enrol.
	if r := do(t, "POST", srv.URL+"/api/v1/auth/2fa/setup", map[string]string{"pin": "0000"}, nil, oldSession); r.StatusCode != http.StatusForbidden {
		t.Fatalf("setup with wrong PIN: want 403, got %d", r.StatusCode)
	}
	r = do(t, "POST", srv.URL+"/api/v1/auth/2fa/setup", map[string]string{"pin": "2468"}, nil, oldSession)
	var setupRes struct{ Secret, URI string }
	decode(r, &setupRes)
	if r.StatusCode != http.StatusOK || setupRes.Secret == "" || !strings.HasPrefix(setupRes.URI, "otpauth://totp/") {
		t.Fatalf("setup: %d %+v", r.StatusCode, setupRes)
	}

	time.Sleep(1100 * time.Millisecond) // session timestamps have 1s resolution: make oldSession strictly older
	now := time.Now()
	if r := do(t, "POST", srv.URL+"/api/v1/auth/2fa/enable", map[string]string{"code": "000000"}, nil, oldSession); r.StatusCode != http.StatusForbidden {
		t.Fatalf("enable with wrong code: want 403, got %d", r.StatusCode)
	}
	r = do(t, "POST", srv.URL+"/api/v1/auth/2fa/enable", map[string]string{"code": codeAt(setupRes.Secret, now)}, nil, oldSession)
	var enableRes struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	enabledSession := sessionOf(r)
	decode(r, &enableRes)
	if r.StatusCode != http.StatusOK || len(enableRes.RecoveryCodes) != auth.RecoveryCodeCount || enabledSession == nil {
		t.Fatalf("enable: %d, %d codes", r.StatusCode, len(enableRes.RecoveryCodes))
	}

	// Enabling 2FA signs other devices out; the enabling device keeps working.
	if r := do(t, "GET", srv.URL+"/api/v1/auth/me", nil, nil, oldSession); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("pre-2FA session must be revoked, got %d", r.StatusCode)
	}
	if r := do(t, "GET", srv.URL+"/api/v1/auth/me", nil, nil, enabledSession); r.StatusCode != http.StatusOK {
		t.Fatalf("enabling device must stay signed in, got %d", r.StatusCode)
	}

	// The leaked-PIN scenario: correct PIN, no session.
	r = pinLogin()
	var challengeRes struct{ Status, Challenge string }
	if sessionOf(r) != nil {
		t.Fatal("PIN alone must not set a session cookie when 2FA is on")
	}
	decode(r, &challengeRes)
	if challengeRes.Status != "2fa_required" || challengeRes.Challenge == "" {
		t.Fatalf("want 2fa_required challenge, got %+v", challengeRes)
	}
	// The challenge is not a session.
	if r := do(t, "GET", srv.URL+"/api/v1/auth/me", nil, nil, &http.Cookie{Name: auth.SessionCookieName, Value: challengeRes.Challenge}); r.StatusCode != http.StatusUnauthorized {
		t.Fatalf("challenge used as cookie: want 401, got %d", r.StatusCode)
	}
	verify := func(code string) *http.Response {
		return do(t, "POST", srv.URL+"/api/v1/auth/verify-2fa", map[string]string{"challenge": challengeRes.Challenge, "code": code}, nil)
	}
	if r := verify("123456"); r.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong code: want 403, got %d", r.StatusCode)
	}
	// The code used to enable is spent (replay); the next step's code works.
	if r := verify(codeAt(setupRes.Secret, now)); r.StatusCode != http.StatusForbidden {
		t.Fatalf("replayed code: want 403, got %d", r.StatusCode)
	}
	r = verify(codeAt(setupRes.Secret, now.Add(30*time.Second)))
	if r.StatusCode != http.StatusOK || sessionOf(r) == nil {
		t.Fatalf("PIN + code must sign in, got %d", r.StatusCode)
	}

	// Recovery code works exactly once (case/dash-insensitive).
	rc := strings.ToUpper(strings.ReplaceAll(enableRes.RecoveryCodes[0], "-", ""))
	if r := verify(rc); r.StatusCode != http.StatusOK {
		t.Fatalf("recovery code: want 200, got %d", r.StatusCode)
	}
	if r := verify(rc); r.StatusCode != http.StatusForbidden {
		t.Fatalf("reused recovery code: want 403, got %d", r.StatusCode)
	}

	// Disabling needs PIN and a code.
	if r := do(t, "POST", srv.URL+"/api/v1/auth/2fa/disable", map[string]string{"pin": "2468", "code": "000000"}, nil, enabledSession); r.StatusCode != http.StatusForbidden {
		t.Fatalf("disable with wrong code: want 403, got %d", r.StatusCode)
	}
	if r := do(t, "POST", srv.URL+"/api/v1/auth/2fa/disable", map[string]string{"pin": "2468", "code": enableRes.RecoveryCodes[1]}, nil, enabledSession); r.StatusCode != http.StatusOK {
		t.Fatalf("disable: want 200, got %d", r.StatusCode)
	}
	if r := pinLogin(); r.StatusCode != http.StatusOK || sessionOf(r) == nil {
		t.Fatalf("after disabling, PIN login works again: %d", r.StatusCode)
	}
}

// Issue #2: a copied session cookie must stop working after logout, "sign out
// other devices", a PIN reset, a role change and a deactivation.
func TestSessionRevocation(t *testing.T) {
	t.Setenv("SUPER_ADMIN_PIN", "2468")
	srv, _, _ := setup(t, prodCfg, time.Minute)
	sessionOf := func(r *http.Response) *http.Cookie {
		for _, c := range r.Cookies() {
			if c.Name == auth.SessionCookieName && c.Value != "" {
				return c
			}
		}
		t.Fatalf("no session cookie (status %d)", r.StatusCode)
		return nil
	}
	login := func(id, pin string) *http.Cookie {
		return sessionOf(do(t, "POST", srv.URL+"/api/v1/auth/verify-pin", map[string]string{"caregiver_id": id, "pin": pin}, nil))
	}
	me := func(c *http.Cookie) int { return do(t, "GET", srv.URL+"/api/v1/auth/me", nil, nil, c).StatusCode }
	expect := func(what string, c *http.Cookie, want int) {
		t.Helper()
		if got := me(c); got != want {
			t.Fatalf("%s: want %d, got %d", what, want, got)
		}
	}
	tick := func() { time.Sleep(1100 * time.Millisecond) } // session timestamps have 1s resolution
	admin := login("cg_superadmin", "2468")

	// Logout refuses that exact token, not just the browser's copy.
	phone, tablet := login("cg_fatima", "1234"), login("cg_fatima", "1234")
	tick()
	tablet2 := login("cg_fatima", "1234")
	do(t, "POST", srv.URL+"/api/v1/auth/logout", nil, nil, tablet2)
	expect("logged-out cookie", tablet2, http.StatusUnauthorized)
	expect("other device after logout", phone, http.StatusOK)

	// Switching profile on a shared tablet revokes the previous person's cookie.
	shared := login("cg_ali", "1234")
	r0 := do(t, "POST", srv.URL+"/api/v1/auth/verify-pin", map[string]string{"caregiver_id": "cg_fatima", "pin": "1234"}, nil, shared)
	sessionOf(r0)
	expect("previous person's cookie after a profile switch", shared, http.StatusUnauthorized)

	// Sign out other devices: this device gets a fresh cookie, the rest stop working.
	r := do(t, "POST", srv.URL+"/api/v1/auth/sign-out-others", nil, nil, phone)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("sign-out-others: %d", r.StatusCode)
	}
	phone = sessionOf(r)
	expect("other device after sign-out-others", tablet, http.StatusUnauthorized)
	expect("device that signed the others out", phone, http.StatusOK)

	// A super admin resetting the PIN locks out the old cookie.
	tick()
	if r := do(t, "POST", srv.URL+"/api/v1/users/cg_fatima/reset-pin", map[string]string{"new_pin": "4826"}, nil, admin); r.StatusCode != http.StatusOK {
		t.Fatalf("reset-pin: %d", r.StatusCode)
	}
	expect("cookie after PIN reset", phone, http.StatusUnauthorized)

	// Editing the name keeps sessions; changing the role revokes them.
	phone = login("cg_fatima", "4826")
	tick()
	if r := do(t, "PUT", srv.URL+"/api/v1/users/cg_fatima", map[string]string{"name": "فاطمة م", "role": "caregiver"}, nil, admin); r.StatusCode != http.StatusOK {
		t.Fatalf("update name: %d", r.StatusCode)
	}
	expect("cookie after same-role edit", phone, http.StatusOK)
	if r := do(t, "PUT", srv.URL+"/api/v1/users/cg_fatima", map[string]string{"role": "viewer"}, nil, admin); r.StatusCode != http.StatusOK {
		t.Fatalf("update role: %d", r.StatusCode)
	}
	expect("cookie after role change", phone, http.StatusUnauthorized)

	// Deactivate then re-enable: the old cookie must not come back.
	phone = login("cg_fatima", "4826")
	tick()
	for i := 0; i < 2; i++ {
		if r := do(t, "DELETE", srv.URL+"/api/v1/users/cg_fatima", nil, nil, admin); r.StatusCode != http.StatusOK {
			t.Fatalf("toggle-active: %d", r.StatusCode)
		}
	}
	expect("cookie after deactivate + reactivate", phone, http.StatusUnauthorized)
	login("cg_fatima", "4826")
}

// Issue #3: the public switcher list must not expose emails, roles or phones.
func TestPublicCaregiverListIsMinimal(t *testing.T) {
	srv, _, _ := setup(t, prodCfg, time.Minute)
	r := do(t, "GET", srv.URL+"/api/v1/caregivers", nil, nil)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", r.StatusCode)
	}
	var list []map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&list); err != nil || len(list) < 2 {
		t.Fatalf("decode: %v (%d entries)", err, len(list))
	}
	for _, c := range list {
		for k := range c {
			if k != "id" && k != "name" && k != "avatar_color" {
				t.Fatalf("public /caregivers leaks %q: %v", k, c)
			}
		}
		if c["id"] == "" || c["name"] == "" {
			t.Fatalf("switcher entry missing id/name: %v", c)
		}
	}
}

// Issue #4: wrong PINs from an unknown client must not lock the family's own
// phones out; they get their own budget once they have signed in before.
func TestPINLockoutSparesTrustedDevices(t *testing.T) {
	t.Setenv("SUPER_ADMIN_PIN", "2468")
	srv, _, _ := setup(t, prodCfg, time.Minute)
	pin := func(id, p string, cookies ...*http.Cookie) *http.Response {
		return do(t, "POST", srv.URL+"/api/v1/auth/verify-pin", map[string]string{"caregiver_id": id, "pin": p}, nil, cookies...)
	}
	cookie := func(r *http.Response, name string) *http.Cookie {
		for _, c := range r.Cookies() {
			if c.Name == name && c.Value != "" {
				return c
			}
		}
		t.Fatalf("no %s cookie (status %d)", name, r.StatusCode)
		return nil
	}

	r := pin("cg_ali", "1234")
	phone := cookie(r, auth.DeviceCookieName)
	if !phone.HttpOnly || phone.SameSite != http.SameSiteStrictMode {
		t.Fatalf("device cookie must be HttpOnly; SameSite=Strict: %+v", phone)
	}

	for i := 0; i < 5; i++ {
		if r := pin("cg_ali", "0000"); r.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attacker guess %d: want 401, got %d", i, r.StatusCode)
		}
	}
	if r := pin("cg_ali", "1234"); r.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("unknown clients must be locked after 5 failures, got %d", r.StatusCode)
	}
	if r := pin("cg_ali", "1234", phone); r.StatusCode != http.StatusOK {
		t.Fatalf("Ali's own phone must still sign in, got %d", r.StatusCode)
	}

	// A device cookie for someone else, or a forged one, gets no separate budget.
	other := cookie(pin("cg_fatima", "1234"), auth.DeviceCookieName)
	if r := pin("cg_ali", "1234", other); r.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("a device trusted only for Fatima must not bypass Ali's lockout, got %d", r.StatusCode)
	}
	forged := &http.Cookie{Name: auth.DeviceCookieName, Value: strings.Repeat("a", 32) + ":00"}
	if r := pin("cg_ali", "1234", forged); r.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("forged device cookie bypassed the lockout, got %d", r.StatusCode)
	}

	// A PIN reset forgets trusted devices: the old PIN's devices start over as unknown.
	admin := cookie(pin("cg_superadmin", "2468"), auth.SessionCookieName)
	if r := do(t, "POST", srv.URL+"/api/v1/users/cg_ali/reset-pin", map[string]string{"new_pin": "4826"}, nil, admin); r.StatusCode != http.StatusOK {
		t.Fatalf("reset-pin: %d", r.StatusCode)
	}
	if r := pin("cg_ali", "4826", phone); r.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after a PIN reset the phone is no longer trusted, got %d", r.StatusCode)
	}
}

// Issue #5: API responses carry hardening headers; the SSE stream keeps its own caching.
func TestSecurityHeaders(t *testing.T) {
	srv, _, _ := setup(t, prodCfg, time.Minute)
	for _, path := range []string{"/api/v1/caregivers", "/api/v1/auth/me", "/api/v1/does-not-exist"} {
		r := do(t, "GET", srv.URL+path, nil, nil)
		for k, want := range map[string]string{
			"X-Content-Type-Options":  "nosniff",
			"X-Frame-Options":         "DENY",
			"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
			"Cache-Control":           "no-store",
		} {
			if got := r.Header.Get(k); got != want {
				t.Fatalf("%s %s: want %q, got %q", path, k, want, got)
			}
		}
	}
}

// Issue #17: a browser marked as shared gets shared-device sessions from the PIN
// keypad; /auth/me says so; switching back to a personal phone reissues it.
func TestSharedDeviceMode(t *testing.T) {
	srv, _, _ := setup(t, prodCfg, time.Minute)
	cookie := func(r *http.Response, name string) *http.Cookie {
		for _, c := range r.Cookies() {
			if c.Name == name {
				return c
			}
		}
		return nil
	}
	sharedOf := func(c *http.Cookie) bool {
		var me struct {
			ID           string `json:"id"`
			SharedDevice bool   `json:"shared_device"`
			IdleMinutes  int    `json:"idle_minutes"`
		}
		r := do(t, "GET", srv.URL+"/api/v1/auth/me", nil, nil, c)
		if r.StatusCode != http.StatusOK {
			t.Fatalf("me: %d", r.StatusCode)
		}
		json.NewDecoder(r.Body).Decode(&me)
		if me.SharedDevice != (me.IdleMinutes == 20) {
			t.Fatalf("idle_minutes %d with shared_device %v", me.IdleMinutes, me.SharedDevice)
		}
		return me.SharedDevice
	}

	login := do(t, "POST", srv.URL+"/api/v1/auth/verify-pin", map[string]string{"caregiver_id": "cg_fatima", "pin": "1234"}, nil)
	personal := cookie(login, auth.SessionCookieName)
	if sharedOf(personal) {
		t.Fatal("a phone is shared before anyone marked it")
	}

	r := do(t, "POST", srv.URL+"/api/v1/auth/device-mode", map[string]bool{"shared": true}, nil, personal)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("device-mode: %d", r.StatusCode)
	}
	mark, reissued := cookie(r, auth.SharedDeviceCookieName), cookie(r, auth.SessionCookieName)
	if mark == nil || mark.Value != "1" || !mark.HttpOnly || reissued == nil || !sharedOf(reissued) {
		t.Fatalf("device-mode did not mark the device and reissue a shared session: %+v %+v", mark, reissued)
	}
	if do(t, "GET", srv.URL+"/api/v1/auth/me", nil, nil, personal).StatusCode != http.StatusUnauthorized {
		t.Fatal("the session from before the switch still works")
	}

	// The next person to sign in on this tablet gets a shared session too.
	r = do(t, "POST", srv.URL+"/api/v1/auth/verify-pin", map[string]string{"caregiver_id": "cg_ali", "pin": "1234"}, nil, mark)
	ali := cookie(r, auth.SessionCookieName)
	if !sharedOf(ali) {
		t.Fatal("PIN sign-in on a shared device issued a 30-day session")
	}
	if do(t, "POST", srv.URL+"/api/v1/auth/touch", nil, nil, ali).StatusCode != http.StatusOK {
		t.Fatal("touch failed")
	}

	r = do(t, "POST", srv.URL+"/api/v1/auth/device-mode", map[string]bool{"shared": false}, nil, ali, mark)
	if c := cookie(r, auth.SharedDeviceCookieName); c == nil || c.MaxAge >= 0 {
		t.Fatal("the shared mark was not cleared")
	}
	if sharedOf(cookie(r, auth.SessionCookieName)) {
		t.Fatal("still shared after switching to a personal phone")
	}
}
