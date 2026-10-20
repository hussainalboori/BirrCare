package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"birrcare/backend/internal/models"
)

type contextKey string

const (
	CaregiverContextKey contextKey = "authenticated_caregiver"
	SessionCookieName              = "birrcare_session"

	// SessionTTL bounds how long a PIN session token is accepted, independent of
	// the cookie's Expires attribute (which the client controls).
	SessionTTL = 30 * 24 * time.Hour

	minSecretLen = 32
)

// sessionSecret holds the HMAC key used to sign session cookies. It is NEVER a
// hard-coded value: it comes from SESSION_SECRET, a value persisted in the DB, or
// (until InitSessionSecret runs) a random per-process key.
var (
	sessionSecret     atomic.Pointer[[]byte]
	sessionSecretOnce sync.Once
)

func getSecret() []byte {
	sessionSecretOnce.Do(func() {
		if sessionSecret.Load() == nil {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				panic("auth: cannot read crypto/rand: " + err.Error())
			}
			sessionSecret.Store(&b)
		}
	})
	return *sessionSecret.Load()
}

// SetSessionSecret overrides the signing key (used by InitSessionSecret and tests).
func SetSessionSecret(secret []byte) {
	sessionSecretOnce.Do(func() {})
	s := append([]byte(nil), secret...)
	sessionSecret.Store(&s)
}

// InitSessionSecret loads the session signing key. Priority:
//  1. SESSION_SECRET env (must be >= 32 chars)
//  2. a random key persisted in app_settings (generated on first boot), so
//     restarts do not log every sibling out.
func InitSessionSecret(database *sql.DB) error {
	if env := os.Getenv("SESSION_SECRET"); env != "" {
		if len(env) < minSecretLen {
			return fmt.Errorf("SESSION_SECRET must be at least %d characters", minSecretLen)
		}
		SetSessionSecret([]byte(env))
		return nil
	}

	var stored string
	err := database.QueryRow("SELECT value FROM app_settings WHERE key = 'session_secret'").Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return fmt.Errorf("failed generating session secret: %w", err)
		}
		if _, err := database.Exec("INSERT OR IGNORE INTO app_settings (key, value) VALUES ('session_secret', ?)", hex.EncodeToString(b)); err != nil {
			return fmt.Errorf("failed persisting session secret: %w", err)
		}
		err = database.QueryRow("SELECT value FROM app_settings WHERE key = 'session_secret'").Scan(&stored)
	}
	if err != nil {
		return fmt.Errorf("failed loading session secret: %w", err)
	}
	if len(stored) < minSecretLen {
		return errors.New("persisted session secret is too short")
	}
	SetSessionSecret([]byte(stored))
	return nil
}

func sign(payload string) string {
	mac := hmac.New(sha256.New, getSecret())
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// GenerateSessionToken signs caregiverID:timestamp:nonce. The nonce makes every
// login's token unique, so logging out one device never revokes another device's
// cookie issued in the same second.
func GenerateSessionToken(caregiverID string) string {
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		panic("auth: cannot read crypto/rand: " + err.Error())
	}
	payload := fmt.Sprintf("%s:%d:%s", caregiverID, time.Now().Unix(), hex.EncodeToString(nonce))
	return fmt.Sprintf("%s:%s", payload, sign(payload))
}

// ParseSessionToken verifies signature and age, and returns caregiverID
func ParseSessionToken(token string) (string, error) {
	id, _, err := ParseSessionTokenIssued(token)
	return id, err
}

// ParseSessionTokenIssued is ParseSessionToken plus the token's issue time, so the
// middleware can refuse sessions issued before the caregiver revoked them.
func ParseSessionTokenIssued(token string) (string, int64, error) {
	// id:ts:nonce:sig, or id:ts:sig for cookies issued before the nonce was added.
	parts := strings.Split(token, ":")
	if len(parts) != 3 && len(parts) != 4 {
		return "", 0, errors.New("invalid session token format")
	}
	caregiverID, ts, providedSig := parts[0], parts[1], parts[len(parts)-1]

	expectedSig := sign(strings.Join(parts[:len(parts)-1], ":"))
	if !hmac.Equal([]byte(providedSig), []byte(expectedSig)) {
		return "", 0, errors.New("invalid signature")
	}

	issued, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return "", 0, errors.New("invalid session timestamp")
	}
	age := time.Since(time.Unix(issued, 0))
	if age > SessionTTL || age < -5*time.Minute {
		return "", 0, errors.New("session expired")
	}

	return caregiverID, issued, nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// RevokeSession signs out one session token (logout), so a copy of the cookie
// left on a shared tablet stops working. Unparseable tokens are already useless.
func RevokeSession(database *sql.DB, token string) error {
	_, issued, err := ParseSessionTokenIssued(token)
	if err != nil {
		return nil
	}
	now := time.Now().Unix()
	if _, err := database.Exec("DELETE FROM revoked_sessions WHERE expires_at < ?", now); err != nil {
		return err
	}
	_, err = database.Exec("INSERT OR IGNORE INTO revoked_sessions (token_hash, expires_at) VALUES (?, ?)",
		tokenHash(token), issued+int64(SessionTTL.Seconds()))
	return err
}

// sessionRevoked fails closed: if the lookup errors, the cookie is not trusted.
func sessionRevoked(database *sql.DB, token string) bool {
	var one int
	err := database.QueryRow("SELECT 1 FROM revoked_sessions WHERE token_hash = ?", tokenHash(token)).Scan(&one)
	return !errors.Is(err, sql.ErrNoRows)
}

// Config controls which identity sources the middleware trusts.
type Config struct {
	// CFAccess validates Cf-Access-Jwt-Assertion (nil = disabled).
	CFAccess *CFAccessVerifier
	// TrustEmailHeader trusts the raw Cf-Access-Authenticated-User-Email header.
	// INSECURE: any client that can reach the frontend/backend without passing
	// through Cloudflare (LAN, host, misrouted tunnel) can spoof it.
	TrustEmailHeader bool
	// DevFallback authenticates anonymous requests as the first active caregiver.
	// Must be false in production.
	DevFallback bool
}

func envBool(k string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(k)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

// ConfigFromEnv builds Config from:
//
//	CF_ACCESS_TEAM_DOMAIN        e.g. "yourteam.cloudflareaccess.com"
//	CF_ACCESS_AUD                Access application AUD tag
//	CF_ACCESS_TRUST_EMAIL_HEADER "true" to trust the raw email header (not recommended)
//	AUTH_DEV_FALLBACK            default true unless APP_ENV=production
func ConfigFromEnv() Config {
	cfg := Config{
		TrustEmailHeader: envBool("CF_ACCESS_TRUST_EMAIL_HEADER", false),
		DevFallback:      envBool("AUTH_DEV_FALLBACK", os.Getenv("APP_ENV") != "production"),
	}
	team, aud := os.Getenv("CF_ACCESS_TEAM_DOMAIN"), os.Getenv("CF_ACCESS_AUD")
	if team != "" && aud != "" {
		cfg.CFAccess = NewCFAccessVerifier(team, aud)
	} else if team != "" || aud != "" {
		log.Println("[AUTH] WARNING: both CF_ACCESS_TEAM_DOMAIN and CF_ACCESS_AUD are required; Cloudflare JWT validation disabled")
	}
	if cfg.TrustEmailHeader {
		log.Println("[AUTH] WARNING: trusting raw Cf-Access-Authenticated-User-Email header without JWT validation")
	}
	if cfg.DevFallback {
		log.Println("[AUTH] WARNING: AUTH_DEV_FALLBACK enabled - anonymous requests act as the first active caregiver")
	}
	return cfg
}

// Middleware injects the authenticated Caregiver into context using ConfigFromEnv.
func Middleware(database *sql.DB) func(http.Handler) http.Handler {
	return MiddlewareWithConfig(database, ConfigFromEnv())
}

// MiddlewareWithConfig injects authenticated Caregiver into context.
// Priority:
// 1. Session Cookie (active PIN persona on device)
// 2. Cloudflare Access JWT (Cf-Access-Jwt-Assertion), validated against team JWKS
// 3. Raw Cf-Access-Authenticated-User-Email header (only if explicitly trusted)
// 4. Dev fallback to first active caregiver (only if explicitly enabled)
func MiddlewareWithConfig(database *sql.DB, cfg Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var caregiver *models.Caregiver

			// 1. Check Session Cookie
			if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
				if caregiverID, issued, err := ParseSessionTokenIssued(cookie.Value); err == nil && !sessionRevoked(database, cookie.Value) {
					caregiver = getCaregiverByID(database, caregiverID)
					// Revoked: PIN, role or 2FA changed, or "sign out other devices", after this cookie was issued.
					if caregiver != nil && issued < caregiver.SessionsValidAfter {
						caregiver = nil
					}
				}
			}

			// 2. Cloudflare Access signed JWT
			if caregiver == nil && cfg.CFAccess != nil {
				token := r.Header.Get("Cf-Access-Jwt-Assertion")
				if token == "" {
					if c, err := r.Cookie("CF_Authorization"); err == nil {
						token = c.Value
					}
				}
				if token != "" {
					if email, err := cfg.CFAccess.Verify(r.Context(), token); err == nil {
						caregiver = getCaregiverByEmail(database, email)
					} else {
						log.Printf("[AUTH] Rejected Cloudflare Access JWT: %v", err)
					}
				}
			}

			// 3. Raw header (legacy / explicitly trusted only)
			if caregiver == nil && cfg.TrustEmailHeader {
				if cfEmail := r.Header.Get("Cf-Access-Authenticated-User-Email"); cfEmail != "" {
					caregiver = getCaregiverByEmail(database, cfEmail)
				}
			}

			// 4. Dev / local fallback
			if caregiver == nil && cfg.DevFallback {
				caregiver = getDefaultCaregiver(database)
			}

			if caregiver != nil {
				ctx := context.WithValue(r.Context(), CaregiverContextKey, caregiver)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuth rejects requests that have no authenticated caregiver in context.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if GetCaregiverFromContext(r.Context()) == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"UNAUTHENTICATED","message":"Select your profile and enter your PIN"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GetCaregiverFromContext returns the authenticated caregiver or nil.
func GetCaregiverFromContext(ctx context.Context) *models.Caregiver {
	c, ok := ctx.Value(CaregiverContextKey).(*models.Caregiver)
	if !ok {
		return nil
	}
	return c
}

const caregiverCols = `id, name, email, role, avatar_color, preferred_lang, is_active, created_at, totp_enabled, sessions_valid_after`

func scanCaregiver(row *sql.Row) *models.Caregiver {
	var c models.Caregiver
	var email sql.NullString
	var createdAtStr string
	if err := row.Scan(&c.ID, &c.Name, &email, &c.Role, &c.AvatarColor, &c.PreferredLang, &c.IsActive, &createdAtStr, &c.TOTPEnabled, &c.SessionsValidAfter); err != nil {
		return nil
	}
	if email.Valid {
		c.Email = email.String
	}
	c.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
	return &c
}

func getCaregiverByID(database *sql.DB, id string) *models.Caregiver {
	return scanCaregiver(database.QueryRow(`SELECT `+caregiverCols+` FROM caregivers WHERE id = ? AND is_active = 1`, id))
}

func getCaregiverByEmail(database *sql.DB, email string) *models.Caregiver {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil
	}
	return scanCaregiver(database.QueryRow(`SELECT `+caregiverCols+` FROM caregivers WHERE lower(email) = lower(?) AND is_active = 1`, email))
}

func getDefaultCaregiver(database *sql.DB) *models.Caregiver {
	return scanCaregiver(database.QueryRow(`SELECT ` + caregiverCols + ` FROM caregivers WHERE is_active = 1 ORDER BY id ASC LIMIT 1`))
}

// CanManageUsers checks if caregiver has super_admin privileges
func CanManageUsers(c *models.Caregiver) bool {
	return c != nil && c.Role == models.RoleSuperAdmin
}

// CanManageMedications checks if caregiver can add/edit medication prescriptions (super_admin or admin)
func CanManageMedications(c *models.Caregiver) bool {
	return c != nil && (c.Role == models.RoleSuperAdmin || c.Role == models.RoleAdmin)
}

// CanTakeActions checks if caregiver can perform clinical logging (super_admin, admin, caregiver).
// Viewers are strictly prohibited from writing or logging data.
func CanTakeActions(c *models.Caregiver) bool {
	return c != nil && (c.Role == models.RoleSuperAdmin || c.Role == models.RoleAdmin || c.Role == models.RoleCaregiver)
}

// CanManageShifts checks if caregiver has super_admin privileges to configure work shift slots and assignments.
func CanManageShifts(c *models.Caregiver) bool {
	return c != nil && c.Role == models.RoleSuperAdmin
}

