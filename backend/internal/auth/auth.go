package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"birrcare/backend/internal/models"
)

type contextKey string

const (
	CaregiverContextKey contextKey = "authenticated_caregiver"
	SessionCookieName              = "birrcare_session"
)

var (
	// Secret key for signing session cookies
	sessionSecret = []byte(getEnv("SESSION_SECRET", "birrcare-super-secret-family-recovery-key-2026"))
)

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// GenerateSessionToken signs caregiverID:timestamp
func GenerateSessionToken(caregiverID string) string {
	ts := time.Now().Unix()
	payload := fmt.Sprintf("%s:%d", caregiverID, ts)
	mac := hmac.New(sha256.New, sessionSecret)
	mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s:%s", payload, signature)
}

// ParseSessionToken verifies signature and returns caregiverID
func ParseSessionToken(token string) (string, error) {
	parts := strings.Split(token, ":")
	if len(parts) != 3 {
		return "", errors.New("invalid session token format")
	}
	caregiverID := parts[0]
	ts := parts[1]
	providedSig := parts[2]

	payload := fmt.Sprintf("%s:%s", caregiverID, ts)
	mac := hmac.New(sha256.New, sessionSecret)
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(providedSig), []byte(expectedSig)) {
		return "", errors.New("invalid signature")
	}

	return caregiverID, nil
}

// Middleware injects authenticated Caregiver into context.
// Priority:
// 1. Session Cookie (active PIN persona on device)
// 2. Cloudflare Access Header (Cf-Access-Authenticated-User-Email)
// 3. Fallback to default active caregiver if local dev or single family setup
func Middleware(database *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var caregiver *models.Caregiver

			// 1. Check Session Cookie
			if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
				if caregiverID, err := ParseSessionToken(cookie.Value); err == nil {
					caregiver = getCaregiverByID(database, caregiverID)
				}
			}

			// 2. Check Cloudflare Access Header if no session cookie or if cookie invalid
			if caregiver == nil {
				cfEmail := r.Header.Get("Cf-Access-Authenticated-User-Email")
				if cfEmail != "" {
					caregiver = getCaregiverByEmail(database, cfEmail)
				}
			}

			// 3. Dev / local fallback: If still nil, select the first active caregiver
			if caregiver == nil {
				caregiver = getDefaultCaregiver(database)
			}

			if caregiver != nil {
				ctx := context.WithValue(r.Context(), CaregiverContextKey, caregiver)
				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				next.ServeHTTP(w, r)
			}
		})
	}
}

// GetCaregiverFromContext returns the authenticated caregiver or nil.
func GetCaregiverFromContext(ctx context.Context) *models.Caregiver {
	c, ok := ctx.Value(CaregiverContextKey).(*models.Caregiver)
	if !ok {
		return nil
	}
	return c
}

func getCaregiverByID(database *sql.DB, id string) *models.Caregiver {
	row := database.QueryRow(`
		SELECT id, name, email, role, avatar_color, preferred_lang, is_active, created_at
		FROM caregivers WHERE id = ? AND is_active = 1
	`, id)

	var c models.Caregiver
	var email sql.NullString
	var createdAtStr string
	err := row.Scan(&c.ID, &c.Name, &email, &c.Role, &c.AvatarColor, &c.PreferredLang, &c.IsActive, &createdAtStr)
	if err != nil {
		return nil
	}
	if email.Valid {
		c.Email = email.String
	}
	c.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
	return &c
}

func getCaregiverByEmail(database *sql.DB, email string) *models.Caregiver {
	row := database.QueryRow(`
		SELECT id, name, email, role, avatar_color, preferred_lang, is_active, created_at
		FROM caregivers WHERE email = ? AND is_active = 1
	`, email)

	var c models.Caregiver
	var mail sql.NullString
	var createdAtStr string
	err := row.Scan(&c.ID, &c.Name, &mail, &c.Role, &c.AvatarColor, &c.PreferredLang, &c.IsActive, &createdAtStr)
	if err != nil {
		return nil
	}
	if mail.Valid {
		c.Email = mail.String
	}
	c.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
	return &c
}

func getDefaultCaregiver(database *sql.DB) *models.Caregiver {
	row := database.QueryRow(`
		SELECT id, name, email, role, avatar_color, preferred_lang, is_active, created_at
		FROM caregivers WHERE is_active = 1 ORDER BY id ASC LIMIT 1
	`)

	var c models.Caregiver
	var email sql.NullString
	var createdAtStr string
	err := row.Scan(&c.ID, &c.Name, &email, &c.Role, &c.AvatarColor, &c.PreferredLang, &c.IsActive, &createdAtStr)
	if err != nil {
		return nil
	}
	if email.Valid {
		c.Email = email.String
	}
	c.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
	return &c
}
