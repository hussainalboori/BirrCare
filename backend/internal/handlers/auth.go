package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	DB      *sql.DB
	Broker  *sse.Broker
	Limiter *auth.PINLimiter // nil = auth.DefaultPINLimiter
}

// switcherEntry is all the public PIN switcher needs. It's a separate struct so
// fields added to models.Caregiver (email, role, phone) never leak by default.
type switcherEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AvatarColor string `json:"avatar_color"`
}

// ListCaregivers is public: the switcher renders before anyone signs in.
func (h *AuthHandler) ListCaregivers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`SELECT id, name, avatar_color FROM caregivers WHERE is_active = 1 ORDER BY name ASC`)
	if err != nil {
		http.Error(w, `{"error":"failed to query caregivers"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	caregivers := []switcherEntry{}
	for rows.Next() {
		var c switcherEntry
		if err := rows.Scan(&c.ID, &c.Name, &c.AvatarColor); err != nil {
			continue
		}
		caregivers = append(caregivers, c)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(caregivers)
}

func (h *AuthHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(caregiver)
}

type VerifyPinRequest struct {
	CaregiverID string `json:"caregiver_id"`
	PIN         string `json:"pin"`
}

func (h *AuthHandler) limiter() *auth.PINLimiter {
	if h.Limiter != nil {
		return h.Limiter
	}
	return auth.DefaultPINLimiter
}

func secureCookies() bool {
	return os.Getenv("APP_ENV") == "production"
}

// deviceID returns this browser's signed device id, or "".
func deviceID(r *http.Request) string {
	if c, err := r.Cookie(auth.DeviceCookieName); err == nil {
		if id, ok := auth.ParseDeviceToken(c.Value); ok {
			return id
		}
	}
	return ""
}

// pinLimitKey keys PIN throttling per (caregiver, device). A device that has
// entered this caregiver's PIN correctly before gets its own budget; every other
// client shares one. Someone guessing from elsewhere can then lock out only
// unknown devices, never the family's own phones.
func (h *AuthHandler) pinLimitKey(r *http.Request, caregiverID string) string {
	if id := deviceID(r); id != "" {
		var one int
		if h.DB.QueryRow(`SELECT 1 FROM trusted_devices WHERE caregiver_id = ? AND device_hash = ?`, caregiverID, auth.DeviceHash(id)).Scan(&one) == nil {
			return auth.PINLimitKey(caregiverID, id)
		}
	}
	return auth.PINLimitKey(caregiverID, "")
}

// trustDevice records that this browser knows caregiverID's PIN, issuing a device
// cookie first if it has none.
func (h *AuthHandler) trustDevice(w http.ResponseWriter, r *http.Request, caregiverID string) {
	id := deviceID(r)
	if id == "" {
		var token string
		token, id = auth.NewDeviceToken()
		http.SetCookie(w, &http.Cookie{
			Name:     auth.DeviceCookieName,
			Value:    token,
			Path:     "/",
			Expires:  time.Now().Add(auth.DeviceCookieTTL),
			MaxAge:   int(auth.DeviceCookieTTL.Seconds()),
			HttpOnly: true,
			Secure:   secureCookies(),
			SameSite: http.SameSiteStrictMode,
		})
	}
	if _, err := h.DB.Exec(`INSERT INTO trusted_devices (caregiver_id, device_hash, last_seen) VALUES (?, ?, ?)
		ON CONFLICT (caregiver_id, device_hash) DO UPDATE SET last_seen = excluded.last_seen`,
		caregiverID, auth.DeviceHash(id), time.Now().Unix()); err != nil {
		log.Printf("[AUTH] failed to record trusted device: %v", err)
	}
}

func (h *AuthHandler) VerifyPIN(w http.ResponseWriter, r *http.Request) {
	var req VerifyPinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CaregiverID == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}

	var pinHash string
	var totpEnabled bool
	err := h.DB.QueryRow(`
		SELECT pin_hash, totp_enabled FROM caregivers WHERE id = ? AND is_active = 1
	`, req.CaregiverID).Scan(&pinHash, &totpEnabled)

	if err != nil {
		writeJSONError(w, http.StatusNotFound, "CAREGIVER_NOT_FOUND", "caregiver not found")
		return
	}

	// Brute-force protection: reserve an attempt BEFORE the comparison so parallel
	// bursts cannot exceed the budget. Keyed per (existing caregiver, trusted device),
	// so the limiter map is bounded by the trusted_devices table.
	lim := h.limiter()
	key := h.pinLimitKey(r, req.CaregiverID)
	ok, retryAfter := lim.Begin(key)
	if !ok {
		secs := int(math.Ceil(retryAfter.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":               "TOO_MANY_ATTEMPTS",
			"message":             "Too many incorrect PIN attempts. Try again later.",
			"retry_after_seconds": secs,
		})
		return
	}

	// bcrypt comparison is constant-time with respect to the PIN.
	if err := bcrypt.CompareHashAndPassword([]byte(pinHash), []byte(req.PIN)); err != nil {
		lim.Done(key, false)
		audit.Log(h.DB, req.CaregiverID, "PIN_FAILED", "caregiver", req.CaregiverID, "Incorrect PIN entered on keypad")
		writeJSONError(w, http.StatusUnauthorized, "INCORRECT_PIN", "incorrect PIN")
		return
	}
	lim.Done(key, true)
	h.trustDevice(w, r, req.CaregiverID)

	// Two-factor: a correct PIN alone never signs this account in. Hand back a
	// short-lived challenge that must be paired with an authenticator code.
	if totpEnabled {
		audit.Log(h.DB, req.CaregiverID, "PIN_OK_2FA_PENDING", "caregiver", req.CaregiverID, "PIN accepted, waiting for authenticator code")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "2fa_required",
			"challenge":    auth.NewChallenge(req.CaregiverID),
			"caregiver_id": req.CaregiverID,
		})
		return
	}

	h.completeLogin(w, r, req.CaregiverID, "Switched profile via PIN keypad")
}

func (h *AuthHandler) setSessionCookie(w http.ResponseWriter, caregiverID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    auth.GenerateSessionToken(caregiverID),
		Path:     "/",
		Expires:  time.Now().Add(auth.SessionTTL),
		MaxAge:   int(auth.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   secureCookies(),
		SameSite: http.SameSiteStrictMode,
	})
}

// revokeOtherSessions invalidates every session cookie issued before now (other
// phones, a stolen cookie) and gives the current device a fresh one.
func (h *AuthHandler) revokeOtherSessions(w http.ResponseWriter, r *http.Request, caregiverID string) error {
	if _, err := h.DB.Exec("UPDATE caregivers SET sessions_valid_after = ? WHERE id = ?", time.Now().Unix(), caregiverID); err != nil {
		return err
	}
	// The other devices also lose their own PIN-attempt budget (a lost phone).
	if _, err := h.DB.Exec("DELETE FROM trusted_devices WHERE caregiver_id = ? AND device_hash <> ?", caregiverID, auth.DeviceHash(deviceID(r))); err != nil {
		return err
	}
	h.setSessionCookie(w, caregiverID)
	return nil
}

// completeLogin issues the session, audits and broadcasts the switch, and returns
// the caregiver. Shared by the PIN step and the authenticator step.
func (h *AuthHandler) completeLogin(w http.ResponseWriter, r *http.Request, caregiverID, auditDetail string) {
	var name, avatarColor, role, preferredLang string
	var totpEnabled bool
	if err := h.DB.QueryRow(`
		SELECT name, avatar_color, role, preferred_lang, totp_enabled FROM caregivers WHERE id = ? AND is_active = 1
	`, caregiverID).Scan(&name, &avatarColor, &role, &preferredLang, &totpEnabled); err != nil {
		writeJSONError(w, http.StatusNotFound, "CAREGIVER_NOT_FOUND", "caregiver not found")
		return
	}

	// Switching profile on a shared tablet replaces the previous person's cookie;
	// revoke it too, so a copy of it stops working.
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil && cookie.Value != "" {
		if err := auth.RevokeSession(h.DB, cookie.Value); err != nil {
			internalError(w, "failed to revoke previous session", err)
			return
		}
	}
	h.setSessionCookie(w, caregiverID)
	audit.Log(h.DB, caregiverID, "CAREGIVER_SWITCH", "caregiver", caregiverID, auditDetail)

	h.Broker.Broadcast("CAREGIVER_SWITCHED", models.ActorInfo{
		ID:          caregiverID,
		Name:        name,
		AvatarColor: avatarColor,
	}, map[string]interface{}{
		"caregiver_id":   caregiverID,
		"name":           name,
		"avatar_color":   avatarColor,
		"role":           role,
		"preferred_lang": preferredLang,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"caregiver": map[string]interface{}{
			"id":             caregiverID,
			"name":           name,
			"avatar_color":   avatarColor,
			"role":           role,
			"preferred_lang": preferredLang,
			"totp_enabled":   totpEnabled,
		},
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if c := auth.GetCaregiverFromContext(r.Context()); c != nil {
		audit.Log(h.DB, c.ID, "LOGOUT", "caregiver", c.ID, "Logged out")
	}
	// Clearing the cookie only helps this browser; also refuse the token itself.
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil && cookie.Value != "" {
		if err := auth.RevokeSession(h.DB, cookie.Value); err != nil {
			internalError(w, "failed to sign out", err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secureCookies(),
		SameSite: http.SameSiteStrictMode,
	})

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"logged_out"}`))
}

// SignOutOtherDevices keeps this device signed in and signs out every other one
// (a lost phone, a cookie left on a shared tablet).
func (h *AuthHandler) SignOutOtherDevices(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if err := h.revokeOtherSessions(w, r, caregiver.ID); err != nil {
		internalError(w, "failed to revoke sessions", err)
		return
	}
	audit.Log(h.DB, caregiver.ID, "SIGN_OUT_OTHER_DEVICES", "caregiver", caregiver.ID, "Signed out all other devices")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}

type ChangePinRequest struct {
	CurrentPIN string `json:"current_pin"`
	NewPIN     string `json:"new_pin"`
}

// weakPIN rejects the seeded default and repeated digits ("1111"), the first
// guesses anyone holding a sibling's unlocked phone would try.
func weakPIN(pin string) bool {
	return pin == "1234" || strings.Count(pin, pin[:1]) == len(pin)
}

// ChangePIN lets the signed-in caregiver change their own PIN. The current PIN is
// verified under the same brute-force limiter as the login keypad, so a session
// left open on a shared phone can't be used to guess it.
func (h *AuthHandler) ChangePIN(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		writeJSONError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Select your profile and enter your PIN")
		return
	}

	var req ChangePinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	if !pinRegex.MatchString(req.NewPIN) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PIN", "new PIN must be exactly 4 digits")
		return
	}
	if weakPIN(req.NewPIN) {
		writeJSONError(w, http.StatusBadRequest, "WEAK_PIN", "choose a PIN that is not 1234 or one repeated digit")
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
	if err := bcrypt.CompareHashAndPassword([]byte(pinHash), []byte(req.CurrentPIN)); err != nil {
		lim.Done(key, false)
		audit.Log(h.DB, caregiver.ID, "PIN_FAILED", "caregiver", caregiver.ID, "Incorrect current PIN on change-PIN")
		// 403, not 401: the session is valid, so the frontend must not treat this as "signed out".
		writeJSONError(w, http.StatusForbidden, "INCORRECT_PIN", "current PIN is incorrect")
		return
	}
	lim.Done(key, true)

	if req.NewPIN == req.CurrentPIN {
		writeJSONError(w, http.StatusBadRequest, "SAME_PIN", "the new PIN is the same as the current one")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPIN), bcrypt.DefaultCost)
	if err != nil {
		internalError(w, "failed to hash PIN", err)
		return
	}
	if _, err := h.DB.Exec("UPDATE caregivers SET pin_hash = ? WHERE id = ?", string(hash), caregiver.ID); err != nil {
		internalError(w, "failed to change PIN", err)
		return
	}
	audit.Log(h.DB, caregiver.ID, "PIN_CHANGED", "caregiver", caregiver.ID, "Changed own 4-digit PIN")
	// A PIN change usually means the old one leaked: sign every other device out.
	if err := h.revokeOtherSessions(w, r, caregiver.ID); err != nil {
		internalError(w, "failed to revoke sessions", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
