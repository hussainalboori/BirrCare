package handlers

import (
	"database/sql"
	"encoding/json"
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

func (h *AuthHandler) ListCaregivers(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`
		SELECT id, name, email, role, avatar_color, preferred_lang, is_active, created_at
		FROM caregivers WHERE is_active = 1 ORDER BY name ASC
	`)
	if err != nil {
		http.Error(w, `{"error":"failed to query caregivers"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var caregivers []models.Caregiver
	for rows.Next() {
		var c models.Caregiver
		var email sql.NullString
		var createdAtStr string
		if err := rows.Scan(&c.ID, &c.Name, &email, &c.Role, &c.AvatarColor, &c.PreferredLang, &c.IsActive, &createdAtStr); err != nil {
			continue
		}
		if email.Valid {
			c.Email = email.String
		}
		c.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
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
	// bursts cannot exceed the budget. Keyed by (existing) caregiver id only, so the
	// limiter map is bounded by the number of caregivers.
	lim := h.limiter()
	ok, retryAfter := lim.Begin(req.CaregiverID)
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
		lim.Done(req.CaregiverID, false)
		audit.Log(h.DB, req.CaregiverID, "PIN_FAILED", "caregiver", req.CaregiverID, "Incorrect PIN entered on keypad")
		writeJSONError(w, http.StatusUnauthorized, "INCORRECT_PIN", "incorrect PIN")
		return
	}
	lim.Done(req.CaregiverID, true)

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

	h.completeLogin(w, req.CaregiverID, "Switched profile via PIN keypad")
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
func (h *AuthHandler) revokeOtherSessions(w http.ResponseWriter, caregiverID string) error {
	if _, err := h.DB.Exec("UPDATE caregivers SET sessions_valid_after = ? WHERE id = ?", time.Now().Unix(), caregiverID); err != nil {
		return err
	}
	h.setSessionCookie(w, caregiverID)
	return nil
}

// completeLogin issues the session, audits and broadcasts the switch, and returns
// the caregiver. Shared by the PIN step and the authenticator step.
func (h *AuthHandler) completeLogin(w http.ResponseWriter, caregiverID, auditDetail string) {
	var name, avatarColor, role, preferredLang string
	var totpEnabled bool
	if err := h.DB.QueryRow(`
		SELECT name, avatar_color, role, preferred_lang, totp_enabled FROM caregivers WHERE id = ? AND is_active = 1
	`, caregiverID).Scan(&name, &avatarColor, &role, &preferredLang, &totpEnabled); err != nil {
		writeJSONError(w, http.StatusNotFound, "CAREGIVER_NOT_FOUND", "caregiver not found")
		return
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
	ok, retryAfter := lim.Begin(caregiver.ID)
	if !ok {
		writeTooManyAttempts(w, retryAfter)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(pinHash), []byte(req.CurrentPIN)); err != nil {
		lim.Done(caregiver.ID, false)
		audit.Log(h.DB, caregiver.ID, "PIN_FAILED", "caregiver", caregiver.ID, "Incorrect current PIN on change-PIN")
		// 403, not 401: the session is valid, so the frontend must not treat this as "signed out".
		writeJSONError(w, http.StatusForbidden, "INCORRECT_PIN", "current PIN is incorrect")
		return
	}
	lim.Done(caregiver.ID, true)

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
	if err := h.revokeOtherSessions(w, caregiver.ID); err != nil {
		internalError(w, "failed to revoke sessions", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success"})
}
