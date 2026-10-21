package handlers_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/db"
	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"
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
	status := auth.EvaluateTrust(req, database, nil, nil)
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

	statusPaired := auth.EvaluateTrust(reqPaired, database, nil, nil)
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
	statusRevoked := auth.EvaluateTrust(reqPaired, database, nil, nil)
	if statusRevoked.IsTrusted {
		t.Errorf("expected revoked device to be untrusted")
	}
}

func TestEvaluateTrustGeofence(t *testing.T) {
	database, _, cleanup := setupSmartAuthTestDB(t)
	defer cleanup()

	// Configure home coordinates (e.g. Riyadh home: 24.713600, 46.675300, radius: 75m)
	_, _ = database.Exec("INSERT INTO app_settings (key, value) VALUES ('home_latitude', '24.713600')")
	_, _ = database.Exec("INSERT INTO app_settings (key, value) VALUES ('home_longitude', '46.675300')")
	_, _ = database.Exec("INSERT INTO app_settings (key, value) VALUES ('home_geofence_meters', '75')")

	// 1. Within 20 meters of home coordinates
	nearLat := 24.713700
	nearLon := 46.675350
	reqNear := httptest.NewRequest(http.MethodGet, "/api/v1/auth/trust-status", nil)
	reqNear.RemoteAddr = "203.0.113.10:48212" // External cellular IP

	statusNear := auth.EvaluateTrust(reqNear, database, &nearLat, &nearLon)
	if !statusNear.IsTrusted {
		t.Errorf("expected coordinates within geofence to be trusted")
	}
	if statusNear.TrustReason != "geofence" {
		t.Errorf("expected trust reason 'geofence', got %s", statusNear.TrustReason)
	}

	// 2. Outside geofence (e.g. 5 kilometers away)
	farLat := 24.750000
	farLon := 46.720000
	reqFar := httptest.NewRequest(http.MethodGet, "/api/v1/auth/trust-status", nil)
	reqFar.RemoteAddr = "203.0.113.10:48212"

	statusFar := auth.EvaluateTrust(reqFar, database, &farLat, &farLon)
	if statusFar.IsTrusted {
		t.Errorf("expected coordinates outside geofence to be untrusted")
	}
	if statusFar.TrustReason != "untrusted" {
		t.Errorf("expected trust reason 'untrusted', got %s", statusFar.TrustReason)
	}
}

func TestEvaluateTrustLANSubnet(t *testing.T) {
	database, _, cleanup := setupSmartAuthTestDB(t)
	defer cleanup()

	// 1. Local LAN IP 192.168.1.15
	reqLAN := httptest.NewRequest(http.MethodGet, "/api/v1/auth/trust-status", nil)
	reqLAN.RemoteAddr = "192.168.1.15:52341"

	statusLAN := auth.EvaluateTrust(reqLAN, database, nil, nil)
	if !statusLAN.IsTrusted {
		t.Errorf("expected 192.168.x.x private LAN to be trusted")
	}
	if statusLAN.TrustReason != "lan_subnet" {
		t.Errorf("expected trust reason 'lan_subnet', got %s", statusLAN.TrustReason)
	}

	// 2. Localhost 127.0.0.1
	reqLocal := httptest.NewRequest(http.MethodGet, "/api/v1/auth/trust-status", nil)
	reqLocal.RemoteAddr = "127.0.0.1:8080"
	statusLocal := auth.EvaluateTrust(reqLocal, database, nil, nil)
	if !statusLocal.IsTrusted || statusLocal.TrustReason != "lan_subnet" {
		t.Errorf("expected 127.0.0.1 to be trusted lan_subnet")
	}
}

func TestSwitchCaregiver1TapOnTrusted(t *testing.T) {
	database, broker, cleanup := setupSmartAuthTestDB(t)
	defer cleanup()

	authH := &handlers.AuthHandler{DB: database, Broker: broker}

	createTestCaregiver(t, database, "caregiver1", "Hussain", "caregiver", "1234")
	createTestCaregiver(t, database, "caregiver2", "Zainab", "caregiver", "1234")

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
	reqTrusted.RemoteAddr = "192.168.1.55:54321" // Home Wi-Fi
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

	// 1. Pair device
	pairPayload := `{"name":"Living Room Bedside Tablet"}`
	reqPair := httptest.NewRequest(http.MethodPost, "/api/v1/auth/pair-device", bytes.NewBufferString(pairPayload)).WithContext(adminContext)
	recPair := httptest.NewRecorder()

	authH.PairDevice(recPair, reqPair)
	if recPair.Code != http.StatusOK {
		t.Fatalf("expected 200 on pair device, got %d", recPair.Code)
	}

	var pairResp map[string]interface{}
	json.NewDecoder(recPair.Body).Decode(&pairResp)
	deviceID, _ := pairResp["device_id"].(string)
	token, _ := pairResp["token"].(string)
	if deviceID == "" || token == "" {
		t.Fatalf("expected device_id and token in pair response")
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

