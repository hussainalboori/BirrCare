package handlers_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/db"
	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

func setupSmartAuthTestDB(t *testing.T) (*sql.DB, *sse.Broker, func()) {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed opening in-memory db: %v", err)
	}
	if err := db.Migrate(database); err != nil {
		t.Fatalf("failed migrating db: %v", err)
	}
	if err := auth.InitSessionSecret(database); err != nil {
		t.Fatalf("failed init session secret: %v", err)
	}
	broker := sse.InitBroker()
	cleanup := func() {
		database.Close()
	}
	return database, broker, cleanup
}

func createTestCaregiver(t *testing.T, database *sql.DB, id, name, role, pin string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed hashing pin: %v", err)
	}
	_, err = database.Exec(`
		INSERT INTO caregivers (id, name, email, role, pin_hash, is_active)
		VALUES (?, ?, ?, ?, ?, 1)
	`, id, name, id+"@test.local", role, string(hash))
	if err != nil {
		t.Fatalf("failed creating caregiver: %v", err)
	}
}

func TestEvaluateTrustPairedDevice(t *testing.T) {
	database, _, cleanup := setupSmartAuthTestDB(t)
	defer cleanup()

	createTestCaregiver(t, database, "admin1", "Admin Fatima", "admin", "1234")

	// 1. External cellular IP without pairing token
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/trust-status", nil)
	req.RemoteAddr = "203.0.113.195:48212" // Public external cellular IP
	status := auth.EvaluateTrust(req, database)
	if status.IsTrusted {
		t.Errorf("expected external cellular IP to be untrusted, got trusted")
	}
	if status.TrustReason != "untrusted" {
		t.Errorf("expected reason untrusted, got %s", status.TrustReason)
	}

	// 2. Pair a bedside device
	deviceID := auth.GenerateRandomID(16)
	token := auth.NewHomeDeviceToken(deviceID)
	tokenHash := auth.HomeDeviceHash(deviceID)

	_, err := database.Exec(`
		INSERT INTO paired_devices (id, name, paired_by, device_token_hash, created_at, last_used_at, is_revoked)
		VALUES (?, ?, 'admin1', ?, datetime('now'), datetime('now'), 0)
	`, deviceID, "Sickroom iPad", tokenHash)
	if err != nil {
		t.Fatalf("failed inserting paired device: %v", err)
	}

	// Request from rotated public IP, but with paired device cookie
	reqPaired := httptest.NewRequest(http.MethodGet, "/api/v1/auth/trust-status", nil)
	reqPaired.RemoteAddr = "198.51.100.88:12345" // Different rotated ISP IP
	reqPaired.AddCookie(&http.Cookie{
		Name:  auth.HomeDeviceCookieName,
		Value: token,
	})

	statusPaired := auth.EvaluateTrust(reqPaired, database)
	if !statusPaired.IsTrusted {
		t.Errorf("expected paired device to be trusted, got untrusted")
	}
	if statusPaired.TrustReason != "paired_device" {
		t.Errorf("expected trust reason 'paired_device', got %s", statusPaired.TrustReason)
	}
	if statusPaired.PairedDeviceName != "Sickroom iPad" {
		t.Errorf("expected device name 'Sickroom iPad', got %s", statusPaired.PairedDeviceName)
	}

	// 3. Revoke paired device
	_, _ = database.Exec("UPDATE paired_devices SET is_revoked = 1 WHERE id = ?", deviceID)
	statusRevoked := auth.EvaluateTrust(reqPaired, database)
	if statusRevoked.IsTrusted {
		t.Errorf("expected revoked device to be untrusted")
	}
}

// pairTestDevice registers a paired home device and returns its cookie value.
func pairTestDevice(t *testing.T, database *sql.DB, name string) string {
	t.Helper()
	deviceID := auth.GenerateRandomID(16)
	if _, err := database.Exec(`
		INSERT INTO paired_devices (id, name, paired_by, device_token_hash, created_at, last_used_at, is_revoked)
		VALUES (?, ?, NULL, ?, datetime('now'), datetime('now'), 0)
	`, deviceID, name, auth.HomeDeviceHash(deviceID)); err != nil {
		t.Fatalf("failed inserting paired device: %v", err)
	}
	return auth.NewHomeDeviceToken(deviceID)
}

func switchReq(target string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/switch-caregiver", strings.NewReader(fmt.Sprintf(`{"caregiver_id":%q}`, target)))
	req.RemoteAddr = "203.0.113.10:48212" // external cellular IP
	return req
}

// Issue #53: the phone's reported position, and a device token sent in a
// header, can be forged, so neither may skip the PIN.
func TestSwitchCaregiverIgnoresClientSuppliedTrust(t *testing.T) {
	database, broker, cleanup := setupSmartAuthTestDB(t)
	defer cleanup()
	authH := &handlers.AuthHandler{DB: database, Broker: broker, SwitchLimiter: handlers.NewIPRateLimiter(100)}
	createTestCaregiver(t, database, "caregiver2", "Zainab", "caregiver", "1234")
	_, _ = database.Exec("INSERT INTO app_settings (key, value) VALUES ('home_latitude', '24.713600'), ('home_longitude', '46.675300'), ('home_geofence_meters', '75')")
	token := pairTestDevice(t, database, "Bedside iPad")

	for name, req := range map[string]*http.Request{
		"body": httptest.NewRequest(http.MethodPost, "/api/v1/auth/switch-caregiver",
			strings.NewReader(`{"caregiver_id":"caregiver2","latitude":24.7137,"longitude":46.67535}`)),
		"query": httptest.NewRequest(http.MethodPost, "/api/v1/auth/switch-caregiver?latitude=24.7137&longitude=46.67535",
			strings.NewReader(`{"caregiver_id":"caregiver2"}`)),
		"geo headers":  switchReq("caregiver2"),
		"token header": switchReq("caregiver2"),
	} {
		req.RemoteAddr = "203.0.113.10:48212"
		if name == "geo headers" {
			req.Header.Set("X-Geo-Lat", "24.7137")
			req.Header.Set("X-Geo-Lon", "46.67535")
		}
		if name == "token header" {
			req.Header.Set("X-Home-Device-Token", token)
		}
		rec := httptest.NewRecorder()
		authH.SwitchCaregiver(rec, req)
		var body map[string]interface{}
		json.NewDecoder(rec.Body).Decode(&body)
		if rec.Code != http.StatusForbidden || body["error"] != "PIN_REQUIRED" {
			t.Errorf("%s: expected 403 PIN_REQUIRED, got %d %v", name, rec.Code, body)
		}
		for _, c := range rec.Result().Cookies() {
			if c.Name == auth.SessionCookieName && c.Value != "" {
				t.Errorf("%s: a session cookie was issued", name)
			}
		}
	}

	// The trust status shown to the phone agrees.
	st := httptest.NewRequest(http.MethodGet, "/api/v1/auth/trust-status?latitude=24.7137&longitude=46.67535", nil)
	st.Header.Set("X-Home-Device-Token", token)
	rec := httptest.NewRecorder()
	authH.GetTrustStatus(rec, st)
	var body map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&body)
	if body["trusted"] != false || body["reason"] != "untrusted" {
		t.Errorf("expected {trusted:false, reason:untrusted}, got %v", body)
	}
}

func TestGetHomeLocationAdminOnly(t *testing.T) {
	database, broker, cleanup := setupSmartAuthTestDB(t)
	defer cleanup()
	authH := &handlers.AuthHandler{DB: database, Broker: broker}
	_, _ = database.Exec("INSERT INTO app_settings (key, value) VALUES ('home_latitude', '24.713600'), ('home_longitude', '46.675300')")

	for _, tc := range []struct {
		who  *models.Caregiver
		code int
	}{
		{nil, http.StatusForbidden},
		{&models.Caregiver{ID: "v1", Role: "viewer"}, http.StatusForbidden},
		{&models.Caregiver{ID: "c1", Role: "caregiver"}, http.StatusForbidden},
		{&models.Caregiver{ID: "a1", Role: "admin"}, http.StatusOK},
		{&models.Caregiver{ID: "s1", Role: "super_admin"}, http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/home-location", nil)
		if tc.who != nil {
			req = req.WithContext(context.WithValue(req.Context(), auth.CaregiverContextKey, tc.who))
		}
		rec := httptest.NewRecorder()
		authH.GetHomeLocation(rec, req)
		if rec.Code != tc.code {
			t.Errorf("%+v: expected %d, got %d", tc.who, tc.code, rec.Code)
		}
		if tc.code == http.StatusForbidden && strings.Contains(rec.Body.String(), "24.71") {
			t.Errorf("%+v: home coordinates leaked: %s", tc.who, rec.Body.String())
		}
	}
}

// Issue #53: even on a paired tablet, one tap reaches only caregiver accounts.
// Admins, super admins and viewers always enter their own PIN, so a one-tap
// session can never edit medications.
func TestSwitchCaregiverOneTapOnlyToCaregivers(t *testing.T) {
	database, broker, cleanup := setupSmartAuthTestDB(t)
	defer cleanup()
	authH := &handlers.AuthHandler{DB: database, Broker: broker, SwitchLimiter: handlers.NewIPRateLimiter(100)}
	createTestCaregiver(t, database, "admin1", "Admin Sarah", "admin", "1234")
	createTestCaregiver(t, database, "super1", "Super Admin", "super_admin", "8888")
	createTestCaregiver(t, database, "viewer1", "Uncle", "viewer", "5678")
	createTestCaregiver(t, database, "caregiver2", "Zainab", "caregiver", "4321")
	token := pairTestDevice(t, database, "Bedside iPad")
	deviceID, _ := auth.ParseHomeDeviceToken(token)

	tap := func(target string) *httptest.ResponseRecorder {
		req := switchReq(target)
		req.AddCookie(&http.Cookie{Name: auth.HomeDeviceCookieName, Value: token})
		rec := httptest.NewRecorder()
		authH.SwitchCaregiver(rec, req)
		return rec
	}

	for _, target := range []string{"admin1", "super1", "viewer1"} {
		rec := tap(target)
		var body map[string]interface{}
		json.NewDecoder(rec.Body).Decode(&body)
		if rec.Code != http.StatusForbidden || body["error"] != "PIN_REQUIRED" {
			t.Errorf("one tap to %s: expected 403 PIN_REQUIRED, got %d %v", target, rec.Code, body)
		}
	}

	rec := tap("caregiver2")
	if rec.Code != http.StatusOK {
		t.Fatalf("one tap to a caregiver: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// The switch is audited with the trust reason, device and client IP.
	var details string
	if err := database.QueryRow(`SELECT details FROM audit_logs WHERE action = '1TAP_SWITCH' AND caregiver_id = 'caregiver2'`).Scan(&details); err != nil {
		t.Fatalf("expected a 1TAP_SWITCH audit row: %v", err)
	}
	for _, want := range []string{"paired_device", deviceID, "203.0.113.10"} {
		if !strings.Contains(details, want) {
			t.Errorf("audit detail %q is missing %q", details, want)
		}
	}

	// The session that one tap produced cannot change a prescription.
	var session string
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			session = c.Value
		}
	}
	mw := auth.MiddlewareWithConfig(database, auth.Config{})
	medH := &handlers.MedicationHandler{DB: database, Broker: broker}
	put := httptest.NewRequest(http.MethodPut, "/api/v1/medications/m1", strings.NewReader(`{"name_en":"Changed"}`))
	put.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: session})
	putRec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		me := auth.GetCaregiverFromContext(r.Context())
		if me == nil || me.ID != "caregiver2" {
			t.Fatalf("expected the one-tap session to be caregiver2, got %+v", me)
		}
		rc := chi.NewRouteContext()
		rc.URLParams.Add("id", "m1")
		medH.UpdateMedication(w, r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc)))
	})).ServeHTTP(putRec, put)
	if putRec.Code != http.StatusForbidden {
		t.Errorf("one-tap session editing a medication: expected 403, got %d", putRec.Code)
	}
}

func TestSwitchCaregiverRateLimitedPerDevice(t *testing.T) {
	database, broker, cleanup := setupSmartAuthTestDB(t)
	defer cleanup()
	authH := &handlers.AuthHandler{DB: database, Broker: broker, SwitchLimiter: handlers.NewIPRateLimiter(10)}
	createTestCaregiver(t, database, "caregiver2", "Zainab", "caregiver", "1234")
	tablet := pairTestDevice(t, database, "Bedside iPad")
	other := pairTestDevice(t, database, "Kitchen tablet")

	tap := func(token string) int {
		req := switchReq("caregiver2")
		req.AddCookie(&http.Cookie{Name: auth.HomeDeviceCookieName, Value: token})
		rec := httptest.NewRecorder()
		authH.SwitchCaregiver(rec, req)
		return rec.Code
	}

	limited := 0
	for i := 0; i < 20; i++ {
		code := tap(tablet)
		if i < 10 && code != http.StatusOK {
			t.Fatalf("switch %d: expected 200 within the budget, got %d", i+1, code)
		}
		if code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited != 10 {
		t.Errorf("expected the last 10 of 20 rapid switches to get 429, got %d", limited)
	}
	if code := tap(other); code != http.StatusOK {
		t.Errorf("another paired device has its own budget: expected 200, got %d", code)
	}

	// Untrusted callers are throttled too, so the endpoint can't be probed freely.
	for i := 0; i < 11; i++ {
		rec := httptest.NewRecorder()
		authH.SwitchCaregiver(rec, switchReq("caregiver2"))
		if i == 10 && rec.Code != http.StatusTooManyRequests {
			t.Errorf("11th untrusted switch from one IP: expected 429, got %d", rec.Code)
		}
	}
}

func TestEvaluateTrustHomeIP(t *testing.T) {
	database, _, cleanup := setupSmartAuthTestDB(t)
	defer cleanup()
	t.Setenv("TRUSTED_HOME_IP", "198.51.100.7")

	// Behind the Cloudflare Tunnel every request arrives from loopback or the
	// Docker network, so those addresses alone must never skip the PIN.
	for _, addr := range []string{"127.0.0.1:8080", "192.168.1.15:52341", "172.18.0.3:40000", "10.0.0.5:1234"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/trust-status", nil)
		req.RemoteAddr = addr
		if st := auth.EvaluateTrust(req, database); st.IsTrusted {
			t.Errorf("%s must not be trusted by itself (reason %s)", addr, st.TrustReason)
		}
	}

	// Client-controlled forwarding headers are ignored.
	spoof := httptest.NewRequest(http.MethodGet, "/api/v1/auth/trust-status", nil)
	spoof.RemoteAddr = "127.0.0.1:8080"
	spoof.Header.Set("X-Forwarded-For", "198.51.100.7")
	spoof.Header.Set("X-Real-IP", "198.51.100.7")
	if st := auth.EvaluateTrust(spoof, database); st.IsTrusted {
		t.Errorf("X-Forwarded-For / X-Real-IP must not grant trust")
	}

	// The configured home IP, as reported by Cloudflare, is trusted.
	home := httptest.NewRequest(http.MethodGet, "/api/v1/auth/trust-status", nil)
	home.RemoteAddr = "127.0.0.1:8080"
	home.Header.Set("Cf-Connecting-Ip", "198.51.100.7")
	if st := auth.EvaluateTrust(home, database); !st.IsTrusted || st.TrustReason != "trusted_ip" {
		t.Errorf("expected Cf-Connecting-Ip = TRUSTED_HOME_IP to be trusted_ip, got %+v", st)
	}

	// CIDR form.
	t.Setenv("TRUSTED_HOME_IP", "")
	t.Setenv("TRUSTED_HOME_SUBNETS", "198.51.100.0/24")
	if st := auth.EvaluateTrust(home, database); !st.IsTrusted {
		t.Errorf("expected TRUSTED_HOME_SUBNETS CIDR match to be trusted")
	}
}

func TestTrustStatusJSONMatchesFrontend(t *testing.T) {
	database, broker, cleanup := setupSmartAuthTestDB(t)
	defer cleanup()
	authH := &handlers.AuthHandler{DB: database, Broker: broker}
	token := pairTestDevice(t, database, "Bedside iPad")

	// The frontend reads trusted / reason / device_name.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/trust-status", nil)
	req.AddCookie(&http.Cookie{Name: auth.HomeDeviceCookieName, Value: token})
	rec := httptest.NewRecorder()
	authH.GetTrustStatus(rec, req)
	var body map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&body)
	if body["trusted"] != true || body["reason"] != "paired_device" || body["device_name"] != "Bedside iPad" {
		t.Errorf("expected {trusted:true, reason:paired_device, device_name:Bedside iPad}, got %v", body)
	}
	if strings.Contains(rec.Body.String(), "home_device") || strings.Contains(rec.Body.String(), "geofence") {
		t.Errorf("trust status must not expose device ids or geofence state: %s", rec.Body.String())
	}
}

func TestSwitchCaregiver1TapOnTrusted(t *testing.T) {
	database, broker, cleanup := setupSmartAuthTestDB(t)
	defer cleanup()

	authH := &handlers.AuthHandler{DB: database, Broker: broker, SwitchLimiter: handlers.NewIPRateLimiter(100)}

	createTestCaregiver(t, database, "caregiver1", "Hussain", "caregiver", "1234")
	createTestCaregiver(t, database, "caregiver2", "Zainab", "caregiver", "1234")

	t.Setenv("TRUSTED_HOME_IP", "198.51.100.7")

	// 0. A forged X-Forwarded-For from the LAN range does not skip the PIN.
	reqSpoof := httptest.NewRequest(http.MethodPost, "/api/v1/auth/switch-caregiver", bytes.NewBufferString(`{"caregiver_id":"caregiver2"}`))
	reqSpoof.RemoteAddr = "127.0.0.1:8080"
	reqSpoof.Header.Set("X-Forwarded-For", "192.168.1.10")
	recSpoof := httptest.NewRecorder()
	authH.SwitchCaregiver(recSpoof, reqSpoof)
	if recSpoof.Code != http.StatusForbidden {
		t.Errorf("expected 403 for forged X-Forwarded-For, got %d", recSpoof.Code)
	}

	// 1. Attempt switch from untrusted external IP -> 403 PIN_REQUIRED
	payload := `{"caregiver_id":"caregiver2"}`
	reqUntrusted := httptest.NewRequest(http.MethodPost, "/api/v1/auth/switch-caregiver", bytes.NewBufferString(payload))
	reqUntrusted.RemoteAddr = "203.0.113.50:4123" // External cellular IP
	recUntrusted := httptest.NewRecorder()

	authH.SwitchCaregiver(recUntrusted, reqUntrusted)
	if recUntrusted.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for untrusted switch, got %d", recUntrusted.Code)
	}

	var errResp map[string]interface{}
	json.NewDecoder(recUntrusted.Body).Decode(&errResp)
	if errResp["error"] != "PIN_REQUIRED" {
		t.Errorf("expected error PIN_REQUIRED, got %v", errResp["error"])
	}

	// 2. Attempt switch from home private LAN -> 200 OK with session cookie!
	reqTrusted := httptest.NewRequest(http.MethodPost, "/api/v1/auth/switch-caregiver", bytes.NewBufferString(payload))
	reqTrusted.RemoteAddr = "127.0.0.1:8080"
	reqTrusted.Header.Set("Cf-Connecting-Ip", "198.51.100.7") // home public IP
	recTrusted := httptest.NewRecorder()

	authH.SwitchCaregiver(recTrusted, reqTrusted)
	if recTrusted.Code != http.StatusOK {
		t.Errorf("expected 200 OK for trusted 1-tap switch, got %d (body: %s)", recTrusted.Code, recTrusted.Body.String())
	}

	var okResp map[string]interface{}
	json.NewDecoder(recTrusted.Body).Decode(&okResp)
	if okResp["status"] != "success" {
		t.Errorf("expected status success, got %v", okResp["status"])
	}

	// Check session cookie was set
	cookies := recTrusted.Result().Cookies()
	var foundSession bool
	for _, c := range cookies {
		if c.Name == auth.SessionCookieName && c.Value != "" {
			foundSession = true
			break
		}
	}
	if !foundSession {
		t.Errorf("expected birrcare_session cookie to be issued on 1-tap switch")
	}
}

func TestDevicePairingAndManagement(t *testing.T) {
	database, broker, cleanup := setupSmartAuthTestDB(t)
	defer cleanup()

	authH := &handlers.AuthHandler{DB: database, Broker: broker}

	createTestCaregiver(t, database, "admin1", "Admin Sarah", "admin", "1234")

	adminContext := context.WithValue(context.Background(), auth.CaregiverContextKey, &models.Caregiver{
		ID:   "admin1",
		Name: "Admin Sarah",
		Role: "admin",
	})

	// 0. Pairing needs the admin's PIN, and only admins can pair.
	for _, tc := range []struct {
		ctx     context.Context
		payload string
		code    int
	}{
		{adminContext, `{"device_name":"Tablet"}`, http.StatusForbidden},
		{adminContext, `{"device_name":"Tablet","pin":"9999"}`, http.StatusForbidden},
		{context.WithValue(context.Background(), auth.CaregiverContextKey, &models.Caregiver{ID: "admin1", Role: "caregiver"}), `{"device_name":"Tablet","pin":"1234"}`, http.StatusForbidden},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/pair-device", bytes.NewBufferString(tc.payload)).WithContext(tc.ctx)
		rec := httptest.NewRecorder()
		authH.PairDevice(rec, req)
		if rec.Code != tc.code {
			t.Errorf("pair %s: expected %d, got %d", tc.payload, tc.code, rec.Code)
		}
	}

	// 1. Pair device (field names as the frontend sends them)
	pairPayload := `{"device_name":"Living Room Bedside Tablet","pin":"1234"}`
	reqPair := httptest.NewRequest(http.MethodPost, "/api/v1/auth/pair-device", bytes.NewBufferString(pairPayload)).WithContext(adminContext)
	recPair := httptest.NewRecorder()

	authH.PairDevice(recPair, reqPair)
	if recPair.Code != http.StatusOK {
		t.Fatalf("expected 200 on pair device, got %d", recPair.Code)
	}

	var pairResp map[string]interface{}
	json.NewDecoder(recPair.Body).Decode(&pairResp)
	deviceID, _ := pairResp["device_id"].(string)
	if deviceID == "" {
		t.Fatalf("expected device_id in pair response")
	}
	// Issue #53: the token lives only in the HttpOnly cookie.
	if _, ok := pairResp["token"]; ok {
		t.Errorf("pair response must not contain the device token: %v", pairResp)
	}
	var cookie *http.Cookie
	for _, c := range recPair.Result().Cookies() {
		if c.Name == auth.HomeDeviceCookieName {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.Value == "" {
		t.Fatalf("expected an HttpOnly paired-device cookie, got %+v", cookie)
	}
	paired := httptest.NewRequest(http.MethodGet, "/api/v1/auth/trust-status", nil)
	paired.AddCookie(cookie)
	if st := auth.EvaluateTrust(paired, database); !st.IsTrusted || st.TrustReason != "paired_device" {
		t.Errorf("the issued cookie should make this device trusted, got %+v", st)
	}
	header := httptest.NewRequest(http.MethodGet, "/api/v1/auth/trust-status", nil)
	header.Header.Set("X-Home-Device-Token", cookie.Value)
	if st := auth.EvaluateTrust(header, database); st.IsTrusted {
		t.Errorf("a device token in a header must not be trusted, got %+v", st)
	}

	// 2. List paired devices
	reqList := httptest.NewRequest(http.MethodGet, "/api/v1/auth/paired-devices", nil).WithContext(adminContext)
	recList := httptest.NewRecorder()
	authH.ListPairedDevices(recList, reqList)

	var list []auth.PairedDeviceInfo
	json.NewDecoder(recList.Body).Decode(&list)
	if len(list) != 1 || list[0].Name != "Living Room Bedside Tablet" {
		t.Errorf("expected 1 paired device named 'Living Room Bedside Tablet', got %+v", list)
	}

	// 3. Set Home Location
	locPayload := `{"latitude":24.713600,"longitude":46.675300,"radius_meters":80}`
	reqLoc := httptest.NewRequest(http.MethodPut, "/api/v1/settings/home-location", bytes.NewBufferString(locPayload)).WithContext(adminContext)
	recLoc := httptest.NewRecorder()
	authH.SetHomeLocation(recLoc, reqLoc)
	if recLoc.Code != http.StatusOK {
		t.Fatalf("expected 200 on set home location, got %d", recLoc.Code)
	}

	// 4. Get Home Location
	reqGetLoc := httptest.NewRequest(http.MethodGet, "/api/v1/settings/home-location", nil).WithContext(adminContext)
	recGetLoc := httptest.NewRecorder()
	authH.GetHomeLocation(recGetLoc, reqGetLoc)

	var locResp auth.HomeLocationSettings
	json.NewDecoder(recGetLoc.Body).Decode(&locResp)
	if locResp.Latitude == nil || *locResp.Latitude != 24.7136 || locResp.RadiusMeters != 80 {
		t.Errorf("unexpected home location response: %+v", locResp)
	}
}

func TestVerifyAdminPIN(t *testing.T) {
	database, broker, cleanup := setupSmartAuthTestDB(t)
	defer cleanup()

	authH := &handlers.AuthHandler{DB: database, Broker: broker}
	createTestCaregiver(t, database, "admin1", "Admin Sarah", "admin", "1234")

	adminContext := context.WithValue(context.Background(), auth.CaregiverContextKey, &models.Caregiver{
		ID:   "admin1",
		Name: "Admin Sarah",
		Role: "admin",
	})

	// 1. Wrong PIN
	wrongPayload := `{"pin":"9999"}`
	reqWrong := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-admin-pin", bytes.NewBufferString(wrongPayload)).WithContext(adminContext)
	recWrong := httptest.NewRecorder()
	authH.VerifyAdminPIN(recWrong, reqWrong)
	if recWrong.Code != http.StatusForbidden {
		t.Errorf("expected 403 on wrong admin PIN, got %d", recWrong.Code)
	}

	// 2. Correct PIN (1234)
	correctPayload := `{"pin":"1234"}`
	reqCorrect := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify-admin-pin", bytes.NewBufferString(correctPayload)).WithContext(adminContext)
	recCorrect := httptest.NewRecorder()
	authH.VerifyAdminPIN(recCorrect, reqCorrect)
	if recCorrect.Code != http.StatusOK {
		t.Errorf("expected 200 on correct admin PIN, got %d", recCorrect.Code)
	}
}

func TestDeleteMedicationRequiresAdminPIN(t *testing.T) {
	database, broker, cleanup := setupSmartAuthTestDB(t)
	defer cleanup()
	createTestCaregiver(t, database, "super1", "Super Admin", "super_admin", "8888")
	createTestCaregiver(t, database, "admin1", "Admin Sarah", "admin", "1234")
	medH := &handlers.MedicationHandler{DB: database, Broker: broker}
	admin := &models.Caregiver{ID: "admin1", Name: "Admin Sarah", Role: "admin"}

	del := func(headerPin, bodyPin string) int {
		rc := chi.NewRouteContext()
		rc.URLParams.Add("id", "no-such-med")
		var body io.Reader
		if bodyPin != "" {
			body = strings.NewReader(fmt.Sprintf(`{"admin_pin":%q}`, bodyPin))
		}
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/medications/no-such-med", body)
		if headerPin != "" {
			req.Header.Set("X-Admin-PIN", headerPin)
		}
		ctx := context.WithValue(context.WithValue(req.Context(), auth.CaregiverContextKey, admin), chi.RouteCtxKey, rc)
		rec := httptest.NewRecorder()
		medH.DeleteMedication(rec, req.WithContext(ctx))
		return rec.Code
	}

	if code := del("", ""); code != http.StatusForbidden {
		t.Errorf("no PIN: expected 403, got %d", code)
	}
	if code := del("9999", ""); code != http.StatusForbidden {
		t.Errorf("wrong PIN: expected 403, got %d", code)
	}
	if code := del("1234", ""); code == http.StatusForbidden {
		t.Errorf("correct admin PIN in header must pass PIN check, got %d", code)
	}
	if code := del("", "1234"); code == http.StatusForbidden {
		t.Errorf("correct admin PIN in body must pass PIN check, got %d", code)
	}
	if code := del("8888", ""); code == http.StatusForbidden {
		t.Errorf("super admin PIN in header must pass PIN check, got %d", code)
	}
}
