package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
)

type AuditHandler struct {
	DB *sql.DB
}

// Verify walks the audit hash chain and verifies that no entries were modified or deleted.
// Only Super Admins can invoke this endpoint.
func (h *AuditHandler) Verify(w http.ResponseWriter, r *http.Request) {
	caregiver := auth.GetCaregiverFromContext(r.Context())
	if caregiver == nil {
		http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
		return
	}
	if !auth.CanManageUsers(caregiver) {
		http.Error(w, `{"error":"FORBIDDEN","message":"Only Super Admins can verify audit log"}`, http.StatusForbidden)
		return
	}

	res, err := audit.Verify(h.DB)
	if err != nil {
		internalError(w, "failed to verify audit log", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}
