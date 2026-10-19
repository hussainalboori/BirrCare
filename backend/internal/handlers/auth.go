package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"
	"golang.org/x/crypto/bcrypt"
)

type AuthHandler struct {
	DB     *sql.DB
	Broker *sse.Broker
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

func (h *AuthHandler) VerifyPIN(w http.ResponseWriter, r *http.Request) {
	var req VerifyPinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	var pinHash string
	var name, avatarColor, role, preferredLang string
	err := h.DB.QueryRow(`
		SELECT pin_hash, name, avatar_color, role, preferred_lang
		FROM caregivers WHERE id = ? AND is_active = 1
	`, req.CaregiverID).Scan(&pinHash, &name, &avatarColor, &role, &preferredLang)

	if err != nil {
		http.Error(w, `{"error":"caregiver not found"}`, http.StatusNotFound)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(pinHash), []byte(req.PIN)); err != nil {
		http.Error(w, `{"error":"incorrect PIN"}`, http.StatusUnauthorized)
		return
	}

	// Issue session token
	token := auth.GenerateSessionToken(req.CaregiverID)

	isSecure := os.Getenv("APP_ENV") == "production"
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(30 * 24 * time.Hour),
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
	})

	audit.Log(h.DB, req.CaregiverID, "CAREGIVER_SWITCH", "caregiver", req.CaregiverID, "Switched profile via PIN keypad")

	// Broadcast SSE
	h.Broker.Broadcast("CAREGIVER_SWITCHED", models.ActorInfo{
		ID:          req.CaregiverID,
		Name:        name,
		AvatarColor: avatarColor,
	}, map[string]interface{}{
		"caregiver_id":   req.CaregiverID,
		"name":           name,
		"avatar_color":   avatarColor,
		"role":           role,
		"preferred_lang": preferredLang,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"caregiver": map[string]interface{}{
			"id":             req.CaregiverID,
			"name":           name,
			"avatar_color":   avatarColor,
			"role":           role,
			"preferred_lang": preferredLang,
		},
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"logged_out"}`))
}
