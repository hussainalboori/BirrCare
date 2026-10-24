package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

// GetTrustStatus checks whether the current request is from a trusted environment
// (paired home device, within home geofence, or local LAN/subnet).
func (h *AuthHandler) GetTrustStatus(w http.ResponseWriter, r *http.Request) {
	lat, lon := extractGeoCoords(r)
	status := auth.EvaluateTrust(r, h.DB, lat, lon)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

type SwitchCaregiverRequest struct {
	CaregiverID string   `json:"caregiver_id"`
	Latitude    *float64 `json:"latitude,omitempty"`
	Longitude   *float64 `json:"longitude,omitempty"`
}

// SwitchCaregiver performs an optimistic 1-tap profile switch without a PIN prompt
// if the request comes from a trusted home network, paired bedside tablet, or home geofence.
func (h *AuthHandler) SwitchCaregiver(w http.ResponseWriter, r *http.Request) {
	var req SwitchCaregiverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CaregiverID == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}

	lat, lon := req.Latitude, req.Longitude
	if lat == nil || lon == nil {
		lat, lon = extractGeoCoords(r)
	}

	trust := auth.EvaluateTrust(r, h.DB, lat, lon)
	if !trust.IsTrusted {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":        "PIN_REQUIRED",
			"message":      "Device is outside trusted home network; 4-digit PIN required.",
			"caregiver_id": req.CaregiverID,
		})
		return
	}

	// Verify target caregiver exists and is active
	var totpEnabled bool
	err := h.DB.QueryRow(`
		SELECT totp_enabled FROM caregivers WHERE id = ? AND is_active = 1
	`, req.CaregiverID).Scan(&totpEnabled)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "CAREGIVER_NOT_FOUND", "caregiver not found")
		return
	}

	// 2FA still protects super admins / accounts with an authenticator app
	if totpEnabled {
		audit.Log(h.DB, req.CaregiverID, "1TAP_2FA_PENDING", "caregiver", req.CaregiverID, "1-tap switch verified, waiting for authenticator code")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "2fa_required",
			"challenge":    auth.NewChallenge(req.CaregiverID),
			"caregiver_id": req.CaregiverID,
		})
		return
	}

	auditDetail := fmt.Sprintf("1-tap switch on trusted network (reason: %s", trust.TrustReason)
	if trust.PairedDeviceName != "" {
		auditDetail += fmt.Sprintf(", device: %s)", trust.PairedDeviceName)
	} else {
		auditDetail += ")"
	}

	h.completeLogin(w, r, req.CaregiverID, auditDetail)
}

type PairDeviceRequest struct {
	DeviceName string `json:"device_name"`
	Name       string `json:"name"`
	PIN        string `json:"pin"`
}

// PairDevice registers the current browser / bedside tablet as a trusted home
// device. Admin only, and the admin's own PIN is checked every time: a paired
// device skips the PIN for every sibling for a year.
func (h *AuthHandler) PairDevice(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil || (caregiver.Role != "admin" && caregiver.Role != "super_admin") {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Admin permissions required")
		return
	}

	var req PairDeviceRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if !requireAdminPIN(w, h.DB, h.limiter(), caregiver.ID, req.PIN) {
		return
	}
	name := strings.TrimSpace(req.DeviceName)
	if name == "" {
		name = strings.TrimSpace(req.Name)
	}
	if name == "" {
		name = "Bedside Tablet"
	}

	deviceID := auth.GenerateRandomID(16)
	token := auth.NewHomeDeviceToken(deviceID)
	tokenHash := auth.HomeDeviceHash(deviceID)

	_, err := h.DB.Exec(`
		INSERT INTO paired_devices (id, name, paired_by, device_token_hash, created_at, last_used_at, is_revoked)
		VALUES (?, ?, ?, ?, datetime('now'), datetime('now'), 0)
	`, deviceID, name, caregiver.ID, tokenHash)
	if err != nil {
		internalError(w, "failed to pair device", err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     auth.HomeDeviceCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(auth.HomeDeviceCookieTTL),
		MaxAge:   int(auth.HomeDeviceCookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   secureCookies(),
		SameSite: http.SameSiteStrictMode,
	})

	audit.Log(h.DB, caregiver.ID, "DEVICE_PAIRED", "paired_device", deviceID, fmt.Sprintf("Paired home device: %s", name))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "success",
		"device_id": deviceID,
		"name":      name,
		"token":     token,
	})
}

// ListPairedDevices returns all active paired bedside / home devices (admin only).
func (h *AuthHandler) ListPairedDevices(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil || (caregiver.Role != "admin" && caregiver.Role != "super_admin") {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Admin permissions required")
		return
	}

	rows, err := h.DB.Query(`
		SELECT p.id, p.name, COALESCE(p.paired_by, ''), COALESCE(c.name, 'Admin'), p.created_at, p.last_used_at
		FROM paired_devices p
		LEFT JOIN caregivers c ON p.paired_by = c.id
		WHERE p.is_revoked = 0
		ORDER BY p.created_at DESC
	`)
	if err != nil {
		internalError(w, "failed to list paired devices", err)
		return
	}
	defer rows.Close()

	list := []auth.PairedDeviceInfo{}
	for rows.Next() {
		var item auth.PairedDeviceInfo
		if err := rows.Scan(&item.ID, &item.Name, &item.PairedByID, &item.PairedByName, &item.CreatedAt, &item.LastUsedAt); err == nil {
			list = append(list, item)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// RevokePairedDevice revokes a paired device so it can no longer bypass PIN.
func (h *AuthHandler) RevokePairedDevice(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil || (caregiver.Role != "admin" && caregiver.Role != "super_admin") {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Admin permissions required")
		return
	}

	deviceID := chi.URLParam(r, "id")
	if deviceID == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_ID", "missing device id")
		return
	}

	res, err := h.DB.Exec("UPDATE paired_devices SET is_revoked = 1 WHERE id = ?", deviceID)
	if err != nil {
		internalError(w, "failed to revoke device", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "paired device not found")
		return
	}

	audit.Log(h.DB, caregiver.ID, "DEVICE_REVOKED", "paired_device", deviceID, "Revoked home device pairing")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

// GetHomeLocation returns the configured home geofence coordinates.
func (h *AuthHandler) GetHomeLocation(w http.ResponseWriter, r *http.Request) {
	var latStr, lonStr, radiusStr string
	_ = h.DB.QueryRow("SELECT value FROM app_settings WHERE key = 'home_latitude'").Scan(&latStr)
	_ = h.DB.QueryRow("SELECT value FROM app_settings WHERE key = 'home_longitude'").Scan(&lonStr)
	_ = h.DB.QueryRow("SELECT value FROM app_settings WHERE key = 'home_geofence_meters'").Scan(&radiusStr)

	res := auth.HomeLocationSettings{
		RadiusMeters: auth.DefaultGeofenceRadiusMeters,
	}

	if latStr != "" && lonStr != "" {
		if lat, err := strconv.ParseFloat(latStr, 64); err == nil {
			res.Latitude = &lat
		}
		if lon, err := strconv.ParseFloat(lonStr, 64); err == nil {
			res.Longitude = &lon
		}
	}
	if radiusStr != "" {
		if rVal, err := strconv.ParseFloat(radiusStr, 64); err == nil && rVal > 0 {
			res.RadiusMeters = rVal
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

type SetHomeLocationRequest struct {
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
	RadiusMeters *float64 `json:"radius_meters"`
}

// SetHomeLocation updates the home geofence coordinates (admin only).
func (h *AuthHandler) SetHomeLocation(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil || (caregiver.Role != "admin" && caregiver.Role != "super_admin") {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Admin permissions required")
		return
	}

	var req SetHomeLocationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}

	if req.Latitude == nil || req.Longitude == nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_COORDS", "latitude and longitude are required")
		return
	}

	lat, lon := *req.Latitude, *req.Longitude
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		writeJSONError(w, http.StatusBadRequest, "INVALID_COORDS", "latitude must be between -90 and 90, longitude between -180 and 180")
		return
	}

	radius := auth.DefaultGeofenceRadiusMeters
	if req.RadiusMeters != nil && *req.RadiusMeters >= 10 && *req.RadiusMeters <= 5000 {
		radius = *req.RadiusMeters
	}

	tx, err := h.DB.Begin()
	if err != nil {
		internalError(w, "failed to begin tx", err)
		return
	}
	defer tx.Rollback()

	upsertSetting := `INSERT INTO app_settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`
	if _, err := tx.Exec(upsertSetting, "home_latitude", strconv.FormatFloat(lat, 'f', 6, 64)); err != nil {
		internalError(w, "failed saving home_latitude", err)
		return
	}
	if _, err := tx.Exec(upsertSetting, "home_longitude", strconv.FormatFloat(lon, 'f', 6, 64)); err != nil {
		internalError(w, "failed saving home_longitude", err)
		return
	}
	if _, err := tx.Exec(upsertSetting, "home_geofence_meters", strconv.FormatFloat(radius, 'f', 1, 64)); err != nil {
		internalError(w, "failed saving home_geofence_meters", err)
		return
	}

	if err := tx.Commit(); err != nil {
		internalError(w, "failed committing home location", err)
		return
	}

	audit.Log(h.DB, caregiver.ID, "HOME_LOCATION_UPDATED", "settings", "home_location", fmt.Sprintf("Updated home geofence: (%.6f, %.6f) radius: %.0fm", lat, lon, radius))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "success",
		"latitude":      lat,
		"longitude":     lon,
		"radius_meters": radius,
	})
}

type VerifyAdminPinRequest struct {
	PIN string `json:"pin"`
}

// VerifyAdminPIN verifies the signed-in admin's PIN for destructive operations
// (like deleting medications), ensuring PIN is required regardless of location.
func (h *AuthHandler) VerifyAdminPIN(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		writeJSONError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	if caregiver.Role != "admin" && caregiver.Role != "super_admin" {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Admin permissions required")
		return
	}

	var req VerifyAdminPinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PIN == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "PIN is required")
		return
	}

	var pinHash string
	if err := h.DB.QueryRow("SELECT pin_hash FROM caregivers WHERE id = ? AND is_active = 1", caregiver.ID).Scan(&pinHash); err != nil {
		writeJSONError(w, http.StatusNotFound, "CAREGIVER_NOT_FOUND", "caregiver not found")
		return
	}

	lim := h.limiter()
	key := h.pinLimitKey(r, caregiver.ID)
	ok, retryAfter := lim.Begin(key)
	if !ok {
		writeTooManyAttempts(w, retryAfter)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(pinHash), []byte(req.PIN)); err != nil {
		lim.Done(key, false)
		audit.Log(h.DB, caregiver.ID, "ADMIN_PIN_FAILED", "caregiver", caregiver.ID, "Incorrect admin PIN verification")
		writeJSONError(w, http.StatusForbidden, "INCORRECT_PIN", "incorrect admin PIN")
		return
	}
	lim.Done(key, true)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "verified"})
}

// requireAdminPIN checks the signed-in admin's own PIN before a destructive or
// trust-granting action (issue #29: 1-tap switching never skips this). It
// writes the error response and returns false when the PIN is missing or wrong.
func requireAdminPIN(w http.ResponseWriter, db *sql.DB, lim *auth.PINLimiter, caregiverID, pin string) bool {
	pin = strings.TrimSpace(pin)
	if pin == "" {
		writeJSONError(w, http.StatusForbidden, "ADMIN_PIN_REQUIRED", "admin PIN required")
		return false
	}
	var pinHash string
	if err := db.QueryRow("SELECT pin_hash FROM caregivers WHERE id = ? AND is_active = 1", caregiverID).Scan(&pinHash); err != nil {
		writeJSONError(w, http.StatusForbidden, "INCORRECT_ADMIN_PIN", "incorrect admin PIN")
		return false
	}
	key := auth.PINLimitKey(caregiverID, "")
	ok, retryAfter := lim.Begin(key)
	if !ok {
		writeTooManyAttempts(w, retryAfter)
		return false
	}
	match := false
	if bcrypt.CompareHashAndPassword([]byte(pinHash), []byte(pin)) == nil {
		match = true
	} else {
		// Also allow any active super_admin's PIN (household administrator master PIN)
		rows, err := db.Query("SELECT pin_hash FROM caregivers WHERE role = 'super_admin' AND is_active = 1")
		if err == nil {
			for rows.Next() {
				var superHash string
				if rows.Scan(&superHash) == nil {
					if bcrypt.CompareHashAndPassword([]byte(superHash), []byte(pin)) == nil {
						match = true
						break
					}
				}
			}
			rows.Close()
		}
	}

	if !match {
		lim.Done(key, false)
		audit.Log(db, caregiverID, "ADMIN_PIN_FAILED", "caregiver", caregiverID, "Incorrect admin PIN for a protected action")
		writeJSONError(w, http.StatusForbidden, "INCORRECT_ADMIN_PIN", "incorrect admin PIN")
		return false
	}
	lim.Done(key, true)
	return true
}

func extractGeoCoords(r *http.Request) (*float64, *float64) {
	latStr := r.URL.Query().Get("latitude")
	lonStr := r.URL.Query().Get("longitude")
	if latStr == "" {
		latStr = r.Header.Get("X-Geo-Lat")
	}
	if lonStr == "" {
		lonStr = r.Header.Get("X-Geo-Lon")
	}

	if latStr == "" || lonStr == "" {
		return nil, nil
	}

	lat, err1 := strconv.ParseFloat(latStr, 64)
	lon, err2 := strconv.ParseFloat(lonStr, 64)
	if err1 != nil || err2 != nil {
		return nil, nil
	}

	return &lat, &lon
}
