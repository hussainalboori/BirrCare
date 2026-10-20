package handlers

import (
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"

	"golang.org/x/crypto/bcrypt"
)

// Two-factor authentication (authenticator app) for Super Admins.
//
//   setup   (PIN)          -> pending secret + otpauth URI for the QR code
//   enable  (code)         -> 2FA on, 8 recovery codes shown once, other devices signed out
//   disable (PIN + code)   -> 2FA off, other devices signed out
//   verify-2fa (challenge + code or recovery code) -> session, after a correct PIN
//
// Setup and disable re-check the PIN so an unlocked phone left signed in cannot be
// used to enrol an attacker's authenticator or switch 2FA off.

// checkOwnPIN verifies the caregiver's PIN under the login brute-force limiter and
// writes the error response itself when it fails.
func (h *AuthHandler) checkOwnPIN(w http.ResponseWriter, caregiverID, pin string) bool {
	var pinHash string
	if err := h.DB.QueryRow("SELECT pin_hash FROM caregivers WHERE id = ? AND is_active = 1", caregiverID).Scan(&pinHash); err != nil {
		writeJSONError(w, http.StatusNotFound, "CAREGIVER_NOT_FOUND", "caregiver not found")
		return false
	}
	lim := h.limiter()
	ok, retryAfter := lim.Begin(caregiverID)
	if !ok {
		writeTooManyAttempts(w, retryAfter)
		return false
	}
	if err := bcrypt.CompareHashAndPassword([]byte(pinHash), []byte(pin)); err != nil {
		lim.Done(caregiverID, false)
		audit.Log(h.DB, caregiverID, "PIN_FAILED", "caregiver", caregiverID, "Incorrect PIN on 2FA settings")
		writeJSONError(w, http.StatusForbidden, "INCORRECT_PIN", "PIN is incorrect")
		return false
	}
	lim.Done(caregiverID, true)
	return true
}

func writeTooManyAttempts(w http.ResponseWriter, retryAfter time.Duration) {
	secs := int(math.Ceil(retryAfter.Seconds()))
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":               "TOO_MANY_ATTEMPTS",
		"message":             "Too many incorrect attempts. Try again later.",
		"retry_after_seconds": secs,
	})
}

// checkSecondFactor accepts a current authenticator code or an unused recovery
// code, consuming it atomically so it can never be used twice.
func (h *AuthHandler) checkSecondFactor(caregiverID, code string) (ok bool, usedRecovery bool) {
	var secret, recoveryJSON sql.NullString
	var lastStep int64
	if err := h.DB.QueryRow(`SELECT totp_secret, totp_last_step, recovery_codes FROM caregivers WHERE id = ? AND totp_enabled = 1`,
		caregiverID).Scan(&secret, &lastStep, &recoveryJSON); err != nil || !secret.Valid {
		return false, false
	}

	code = strings.TrimSpace(code)
	if len(code) == 6 && strings.Trim(code, "0123456789") == "" {
		step, valid := auth.VerifyTOTP(secret.String, code, time.Now(), lastStep)
		if !valid {
			return false, false
		}
		// Conditional update: if a concurrent request already used this step, we lose.
		res, err := h.DB.Exec(`UPDATE caregivers SET totp_last_step = ? WHERE id = ? AND totp_last_step < ?`, step, caregiverID, step)
		if err != nil {
			return false, false
		}
		n, _ := res.RowsAffected()
		return n == 1, false
	}

	var hashes []string
	_ = json.Unmarshal([]byte(recoveryJSON.String), &hashes)
	want := auth.HashRecoveryCode(code)
	i := slices.Index(hashes, want)
	if i < 0 {
		return false, false
	}
	remaining, _ := json.Marshal(slices.Delete(hashes, i, i+1))
	res, err := h.DB.Exec(`UPDATE caregivers SET recovery_codes = ? WHERE id = ? AND recovery_codes = ?`, string(remaining), caregiverID, recoveryJSON.String)
	if err != nil {
		return false, false
	}
	n, _ := res.RowsAffected()
	return n == 1, true
}

type VerifyTwoFactorRequest struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

// VerifyTwoFactor is the second login step: PIN challenge + authenticator code.
func (h *AuthHandler) VerifyTwoFactor(w http.ResponseWriter, r *http.Request) {
	var req VerifyTwoFactorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	caregiverID, err := auth.ParseChallenge(req.Challenge)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "CHALLENGE_EXPIRED", "enter your PIN again")
		return
	}

	lim := h.limiter()
	key := "2fa:" + caregiverID
	ok, retryAfter := lim.Begin(key)
	if !ok {
		writeTooManyAttempts(w, retryAfter)
		return
	}
	valid, usedRecovery := h.checkSecondFactor(caregiverID, req.Code)
	lim.Done(key, valid)
	if !valid {
		audit.Log(h.DB, caregiverID, "2FA_FAILED", "caregiver", caregiverID, "Incorrect authenticator code")
		// 403: a wrong code must not look like "signed out" to the frontend.
		writeJSONError(w, http.StatusForbidden, "INCORRECT_CODE", "incorrect authenticator code")
		return
	}
	if usedRecovery {
		audit.Log(h.DB, caregiverID, "RECOVERY_CODE_USED", "caregiver", caregiverID, "Signed in with a one-time recovery code")
	}
	h.completeLogin(w, r, caregiverID, "Switched profile via PIN + authenticator")
}

type pinOnlyRequest struct {
	PIN string `json:"pin"`
}

// SetupTwoFactor starts enrolment: a pending secret the app scans as a QR code.
func (h *AuthHandler) SetupTwoFactor(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageUsers(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Two-factor authentication is for Super Admin accounts")
		return
	}
	if caregiver.TOTPEnabled {
		writeJSONError(w, http.StatusConflict, "ALREADY_ENABLED", "two-factor authentication is already on")
		return
	}
	var req pinOnlyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	if !h.checkOwnPIN(w, caregiver.ID, req.PIN) {
		return
	}
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		internalError(w, "failed to generate secret", err)
		return
	}
	if _, err := h.DB.Exec("UPDATE caregivers SET totp_pending_secret = ? WHERE id = ?", secret, caregiver.ID); err != nil {
		internalError(w, "failed to start two-factor setup", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"secret": secret,
		"uri":    auth.TOTPURI(secret, caregiver.Name),
	})
}

type codeRequest struct {
	Code string `json:"code"`
}

// EnableTwoFactor confirms the first code from the app, turns 2FA on, and returns
// the recovery codes (the only time they are ever shown).
func (h *AuthHandler) EnableTwoFactor(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanManageUsers(caregiver) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Two-factor authentication is for Super Admin accounts")
		return
	}
	var req codeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	var pending sql.NullString
	_ = h.DB.QueryRow("SELECT totp_pending_secret FROM caregivers WHERE id = ?", caregiver.ID).Scan(&pending)
	if !pending.Valid || pending.String == "" {
		writeJSONError(w, http.StatusBadRequest, "NO_SETUP", "start setup first")
		return
	}

	lim := h.limiter()
	key := "2fa:" + caregiver.ID
	ok, retryAfter := lim.Begin(key)
	if !ok {
		writeTooManyAttempts(w, retryAfter)
		return
	}
	step, valid := auth.VerifyTOTP(pending.String, req.Code, time.Now(), 0)
	lim.Done(key, valid)
	if !valid {
		writeJSONError(w, http.StatusForbidden, "INCORRECT_CODE", "that code doesn't match; check the phone's time and try the newest code")
		return
	}

	plain, hashes, err := auth.NewRecoveryCodes()
	if err != nil {
		internalError(w, "failed to generate recovery codes", err)
		return
	}
	hashesJSON, _ := json.Marshal(hashes)
	if _, err := h.DB.Exec(`UPDATE caregivers SET totp_secret = totp_pending_secret, totp_pending_secret = NULL,
		totp_enabled = 1, totp_last_step = ?, recovery_codes = ? WHERE id = ?`, step, string(hashesJSON), caregiver.ID); err != nil {
		internalError(w, "failed to enable two-factor", err)
		return
	}
	audit.Log(h.DB, caregiver.ID, "2FA_ENABLED", "caregiver", caregiver.ID, "Authenticator app enabled; other devices signed out")
	if err := h.revokeOtherSessions(w, caregiver.ID); err != nil {
		internalError(w, "failed to revoke sessions", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "enabled", "recovery_codes": plain})
}

type disableRequest struct {
	PIN  string `json:"pin"`
	Code string `json:"code"`
}

// DisableTwoFactor needs both the PIN and a code (or recovery code).
func (h *AuthHandler) DisableTwoFactor(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil || !caregiver.TOTPEnabled {
		writeJSONError(w, http.StatusBadRequest, "NOT_ENABLED", "two-factor authentication is not on")
		return
	}
	var req disableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	if !h.checkOwnPIN(w, caregiver.ID, req.PIN) {
		return
	}
	lim := h.limiter()
	key := "2fa:" + caregiver.ID
	ok, retryAfter := lim.Begin(key)
	if !ok {
		writeTooManyAttempts(w, retryAfter)
		return
	}
	valid, _ := h.checkSecondFactor(caregiver.ID, req.Code)
	lim.Done(key, valid)
	if !valid {
		audit.Log(h.DB, caregiver.ID, "2FA_FAILED", "caregiver", caregiver.ID, "Incorrect code while disabling 2FA")
		writeJSONError(w, http.StatusForbidden, "INCORRECT_CODE", "incorrect authenticator code")
		return
	}
	if _, err := h.DB.Exec(`UPDATE caregivers SET totp_enabled = 0, totp_secret = NULL, totp_pending_secret = NULL,
		recovery_codes = NULL, totp_last_step = 0 WHERE id = ?`, caregiver.ID); err != nil {
		internalError(w, "failed to disable two-factor", err)
		return
	}
	audit.Log(h.DB, caregiver.ID, "2FA_DISABLED", "caregiver", caregiver.ID, "Authenticator app removed; other devices signed out")
	if err := h.revokeOtherSessions(w, caregiver.ID); err != nil {
		internalError(w, "failed to revoke sessions", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "disabled"})
}
