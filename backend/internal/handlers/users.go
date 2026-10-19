package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/sse"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type UsersHandler struct {
	DB     *sql.DB
	Broker *sse.Broker
}

var pinRegex = regexp.MustCompile(`^\d{4}$`)

// ListUsers returns all users in the system (including inactive ones) - Super Admin only
func (h *UsersHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	if !auth.CanManageUsers(caregiver) {
		http.Error(w, `{"error":"FORBIDDEN","message":"Only Super Admins can manage users"}`, http.StatusForbidden)
		return
	}

	rows, err := h.DB.Query(`
		SELECT id, name, email, role, avatar_color, preferred_lang, phone, is_active, created_at
		FROM caregivers
		ORDER BY created_at ASC
	`)
	if err != nil {
		internalError(w, "failed to query users", err)
		return
	}
	defer rows.Close()

	var users []models.Caregiver
	for rows.Next() {
		var u models.Caregiver
		var email, phone sql.NullString
		var createdAtStr string
		if err := rows.Scan(&u.ID, &u.Name, &email, &u.Role, &u.AvatarColor, &u.PreferredLang, &phone, &u.IsActive, &createdAtStr); err != nil {
			continue
		}
		if email.Valid {
			u.Email = email.String
		}
		if phone.Valid {
			u.Phone = phone.String
		}
		u.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
		users = append(users, u)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

// CreateUser registers a new family member/caregiver with assigned role and 4-digit PIN
func (h *UsersHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	if !auth.CanManageUsers(caregiver) {
		http.Error(w, `{"error":"FORBIDDEN","message":"Only Super Admins can create users"}`, http.StatusForbidden)
		return
	}

	var req models.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, `{"error":"User name is required"}`, http.StatusBadRequest)
		return
	}

	if !pinRegex.MatchString(req.PIN) {
		http.Error(w, `{"error":"PIN must be exactly 4 digits"}`, http.StatusBadRequest)
		return
	}

	validRoles := map[string]bool{
		models.RoleSuperAdmin: true,
		models.RoleAdmin:      true,
		models.RoleCaregiver:  true,
		models.RoleViewer:     true,
	}
	if !validRoles[req.Role] {
		req.Role = models.RoleCaregiver
	}

	if req.AvatarColor == "" {
		req.AvatarColor = "#059669"
	}
	if req.PreferredLang != "en" {
		req.PreferredLang = "ar"
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.PIN), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, `{"error":"failed to hash PIN"}`, http.StatusInternalServerError)
		return
	}

	id := "cg_" + uuid.New().String()[:8]

	var emailVal, phoneVal interface{}
	if req.Email != "" {
		emailVal = strings.TrimSpace(req.Email)
	}
	if req.Phone != "" {
		phoneVal = strings.TrimSpace(req.Phone)
	}

	_, err = h.DB.Exec(`
		INSERT INTO caregivers (id, name, email, role, pin_hash, avatar_color, preferred_lang, phone, is_active, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, datetime('now'))
	`, id, req.Name, emailVal, req.Role, string(hash), req.AvatarColor, req.PreferredLang, phoneVal)

	if err != nil {
		if isUniqueViolation(err) {
			writeJSONError(w, http.StatusConflict, "EMAIL_TAKEN", "a user with this email already exists")
			return
		}
		internalError(w, "failed to insert user", err)
		return
	}

	audit.Log(h.DB, caregiver.ID, "CREATE_USER", "caregiver", id,
		fmt.Sprintf("Created user %s with role %s", req.Name, req.Role))

	// Broadcast update to all connected family devices
	h.Broker.Broadcast("CAREGIVERS_UPDATED", models.ActorInfo{
		ID:          caregiver.ID,
		Name:        caregiver.Name,
		AvatarColor: caregiver.AvatarColor,
	}, map[string]interface{}{
		"action": "created",
		"id":     id,
		"name":   req.Name,
		"role":   req.Role,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":      id,
		"name":    req.Name,
		"role":    req.Role,
		"message": "User created successfully",
	})
}

// UpdateUser updates user profile and role assignments - Super Admin only
func (h *UsersHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	if !auth.CanManageUsers(caregiver) {
		http.Error(w, `{"error":"FORBIDDEN","message":"Only Super Admins can update users"}`, http.StatusForbidden)
		return
	}

	userID := chi.URLParam(r, "id")
	if userID == "" {
		http.Error(w, `{"error":"missing user id"}`, http.StatusBadRequest)
		return
	}

	var req models.UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request payload"}`, http.StatusBadRequest)
		return
	}

	validRoles := map[string]bool{
		models.RoleSuperAdmin: true,
		models.RoleAdmin:      true,
		models.RoleCaregiver:  true,
		models.RoleViewer:     true,
	}
	if req.Role != "" && !validRoles[req.Role] {
		http.Error(w, `{"error":"invalid role"}`, http.StatusBadRequest)
		return
	}

	query := `
		UPDATE caregivers
		SET name = COALESCE(NULLIF(?, ''), name),
		    role = COALESCE(NULLIF(?, ''), role),
		    avatar_color = COALESCE(NULLIF(?, ''), avatar_color),
		    preferred_lang = COALESCE(NULLIF(?, ''), preferred_lang),
		    phone = COALESCE(NULLIF(?, ''), phone)
	`
	args := []interface{}{req.Name, req.Role, req.AvatarColor, req.PreferredLang, req.Phone}
	if strings.TrimSpace(req.Email) != "" {
		query += ", email = ?"
		args = append(args, strings.TrimSpace(req.Email))
	}
	query += " WHERE id = ?"
	args = append(args, userID)

	// Never allow demoting the last active super admin (would lock everyone out of user management).
	if req.Role != "" && req.Role != models.RoleSuperAdmin {
		var currentRole string
		var activeSupers int
		_ = h.DB.QueryRow("SELECT role FROM caregivers WHERE id = ?", userID).Scan(&currentRole)
		_ = h.DB.QueryRow("SELECT COUNT(*) FROM caregivers WHERE role = 'super_admin' AND is_active = 1").Scan(&activeSupers)
		if currentRole == models.RoleSuperAdmin && activeSupers <= 1 {
			writeJSONError(w, http.StatusBadRequest, "LAST_SUPER_ADMIN", "cannot demote the last super admin")
			return
		}
	}

	_, err := h.DB.Exec(query, args...)

	if err != nil {
		if isUniqueViolation(err) {
			writeJSONError(w, http.StatusConflict, "EMAIL_TAKEN", "a user with this email already exists")
			return
		}
		internalError(w, "failed to update user", err)
		return
	}

	audit.Log(h.DB, caregiver.ID, "UPDATE_USER", "caregiver", userID,
		fmt.Sprintf("Updated user %s (role: %s)", userID, req.Role))

	h.Broker.Broadcast("CAREGIVERS_UPDATED", models.ActorInfo{
		ID:          caregiver.ID,
		Name:        caregiver.Name,
		AvatarColor: caregiver.AvatarColor,
	}, map[string]interface{}{
		"action": "updated",
		"id":     userID,
		"role":   req.Role,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "User updated successfully",
	})
}

// ResetUserPIN allows super admin to reset any user's 4-digit PIN
func (h *UsersHandler) ResetUserPIN(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	if !auth.CanManageUsers(caregiver) {
		http.Error(w, `{"error":"FORBIDDEN","message":"Only Super Admins can reset PINs"}`, http.StatusForbidden)
		return
	}

	userID := chi.URLParam(r, "id")
	if userID == "" {
		http.Error(w, `{"error":"missing user id"}`, http.StatusBadRequest)
		return
	}

	var req models.ResetPinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !pinRegex.MatchString(req.NewPIN) {
		http.Error(w, `{"error":"new PIN must be exactly 4 digits"}`, http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPIN), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, `{"error":"failed to hash PIN"}`, http.StatusInternalServerError)
		return
	}

	_, err = h.DB.Exec("UPDATE caregivers SET pin_hash = ? WHERE id = ?", string(hash), userID)
	if err != nil {
		internalError(w, "failed to reset PIN", err)
		return
	}

	audit.Log(h.DB, caregiver.ID, "RESET_USER_PIN", "caregiver", userID, "Reset 4-digit PIN for user")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "PIN reset successfully",
	})
}

// ToggleUserActive enables or disables user login / presence
func (h *UsersHandler) ToggleUserActive(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	if !auth.CanManageUsers(caregiver) {
		http.Error(w, `{"error":"FORBIDDEN","message":"Only Super Admins can change user active status"}`, http.StatusForbidden)
		return
	}

	userID := chi.URLParam(r, "id")
	if userID == "" {
		http.Error(w, `{"error":"missing user id"}`, http.StatusBadRequest)
		return
	}

	if userID == caregiver.ID {
		http.Error(w, `{"error":"Cannot deactivate your own active super admin account"}`, http.StatusBadRequest)
		return
	}

	_, err := h.DB.Exec("UPDATE caregivers SET is_active = CASE WHEN is_active = 1 THEN 0 ELSE 1 END WHERE id = ?", userID)
	if err != nil {
		internalError(w, "failed to toggle user status", err)
		return
	}

	audit.Log(h.DB, caregiver.ID, "TOGGLE_USER_STATUS", "caregiver", userID, "Toggled user active status")

	h.Broker.Broadcast("CAREGIVERS_UPDATED", models.ActorInfo{
		ID:          caregiver.ID,
		Name:        caregiver.Name,
		AvatarColor: caregiver.AvatarColor,
	}, map[string]interface{}{
		"action": "toggled_status",
		"id":     userID,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "User active status toggled",
	})
}
