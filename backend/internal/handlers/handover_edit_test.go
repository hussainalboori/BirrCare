package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"github.com/go-chi/chi/v5"
)

func TestHandoverUpdateAndDelete(t *testing.T) {
	med, _, cleanup := setupTestDB(t)
	defer cleanup()
	h := &handlers.HandoverHandler{DB: med.DB, Broker: med.Broker}

	fatima := &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}
	ali := &models.Caregiver{ID: "cg_ali", Name: "Ali", Role: models.RoleAdmin}
	zainab := &models.Caregiver{ID: "cg_zainab", Name: "Zainab", Role: models.RoleViewer}

	// Insert another regular caregiver to test cross-caregiver permission check
	if _, err := med.DB.Exec(`INSERT INTO caregivers (id, name, email, role, pin_hash, avatar_color, preferred_lang) VALUES ('cg_mohammed', 'محمد', 'm@test.local', 'caregiver', 'hash', '#059669', 'ar')`); err != nil {
		t.Fatalf("failed inserting test caregiver: %v", err)
	}
	mohammed := &models.Caregiver{ID: "cg_mohammed", Name: "Mohammed", Role: models.RoleCaregiver}

	// 1. Fatima creates a handover note
	createCode, createResp := postNote(t, h, fatima, map[string]interface{}{
		"shift_type": "report",
		"summary":    "Initial report text",
		"tags":       []string{"ate_half"},
	})
	if createCode != http.StatusCreated {
		t.Fatalf("failed to create note: code %d, resp: %v", createCode, createResp)
	}
	noteID, ok := createResp["id"].(string)
	if !ok || noteID == "" {
		t.Fatalf("missing id in create response: %v", createResp)
	}

	putNote := func(who *models.Caregiver, id string, body map[string]interface{}) (int, map[string]interface{}) {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(http.MethodPut, "/api/v1/handovers/"+id, bytes.NewReader(b))
		rc := chi.NewRouteContext()
		rc.URLParams.Add("id", id)
		r = r.WithContext(context.WithValue(authContext(r.Context(), who), chi.RouteCtxKey, rc))
		rec := httptest.NewRecorder()
		h.UpdateHandover(rec, r)
		var out map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	deleteNote := func(who *models.Caregiver, id string) (int, map[string]interface{}) {
		r := httptest.NewRequest(http.MethodDelete, "/api/v1/handovers/"+id, nil)
		rc := chi.NewRouteContext()
		rc.URLParams.Add("id", id)
		r = r.WithContext(context.WithValue(authContext(r.Context(), who), chi.RouteCtxKey, rc))
		rec := httptest.NewRecorder()
		h.DeleteHandover(rec, r)
		var out map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	// 2. Viewer cannot edit
	code, _ := putNote(zainab, noteID, map[string]interface{}{"summary": "Viewer trying to edit"})
	if code != http.StatusForbidden {
		t.Fatalf("viewer edit: expected 403, got %d", code)
	}

	// 3. Another caregiver (Mohammed) cannot edit Fatima's note
	code, _ = putNote(mohammed, noteID, map[string]interface{}{"summary": "Mohammed trying to edit"})
	if code != http.StatusForbidden {
		t.Fatalf("other caregiver edit: expected 403, got %d", code)
	}

	// 4. Fatima (author) can edit her own note
	code, out := putNote(fatima, noteID, map[string]interface{}{
		"summary":     "Updated report by Fatima",
		"urgent_flag": false,
		"tags":        []string{"slept_well", "needs_rest"},
	})
	if code != http.StatusOK {
		t.Fatalf("author edit: expected 200, got %d (%v)", code, out)
	}

	// Verify update in DB
	var summary, tags string
	var urgent int
	err := med.DB.QueryRow(`SELECT summary, tags, urgent_flag FROM handover_notes WHERE id = ?`, noteID).Scan(&summary, &tags, &urgent)
	if err != nil {
		t.Fatalf("failed to query updated note: %v", err)
	}
	if summary != "Updated report by Fatima" || tags != `["slept_well","needs_rest"]` || urgent != 0 {
		t.Fatalf("unexpected updated values: summary=%q, tags=%q, urgent=%d", summary, tags, urgent)
	}

	// 5. Admin (Ali) can also edit Fatima's note
	code, _ = putNote(ali, noteID, map[string]interface{}{
		"summary": "Updated by Admin",
		"tags":    []string{"pain"}, // pain triggers urgent
	})
	if code != http.StatusOK {
		t.Fatalf("admin edit: expected 200, got %d", code)
	}
	err = med.DB.QueryRow(`SELECT summary, urgent_flag FROM handover_notes WHERE id = ?`, noteID).Scan(&summary, &urgent)
	if err != nil || summary != "Updated by Admin" || urgent != 1 {
		t.Fatalf("admin update failed: %v, summary=%q, urgent=%d", err, summary, urgent)
	}

	// Verify NOTE_UPDATED audit entry
	var auditCount int
	med.DB.QueryRow(`SELECT count(*) FROM audit_logs WHERE action = 'NOTE_UPDATED' AND entity_id = ?`, noteID).Scan(&auditCount)
	if auditCount != 2 {
		t.Fatalf("expected 2 NOTE_UPDATED audit rows, got %d", auditCount)
	}

	// 6. Viewer cannot delete
	code, _ = deleteNote(zainab, noteID)
	if code != http.StatusForbidden {
		t.Fatalf("viewer delete: expected 403, got %d", code)
	}

	// 7. Another caregiver (Mohammed) cannot delete Fatima's note
	code, _ = deleteNote(mohammed, noteID)
	if code != http.StatusForbidden {
		t.Fatalf("other caregiver delete: expected 403, got %d", code)
	}

	// 8. Attach a duty_handovers entry referencing this note to test FK cascade/cleanup
	_, err = med.DB.Exec(`INSERT INTO duty_handovers (id, note_id, assignment_id, shift_id, date, from_caregiver_id, to_caregiver_id, created_at) VALUES ('dh1', ?, 'a1', 's24', '2026-10-24', 'cg_fatima', 'cg_ali', '2026-10-24 10:00:00')`, noteID)
	if err != nil {
		t.Fatalf("failed to insert test duty handover: %v", err)
	}

	// 9. Admin (Ali) deletes the note
	code, out = deleteNote(ali, noteID)
	if code != http.StatusOK {
		t.Fatalf("admin delete: expected 200, got %d (%v)", code, out)
	}

	// Verify it's gone from handover_notes and duty_handovers
	var noteCount, dhCount int
	med.DB.QueryRow(`SELECT count(*) FROM handover_notes WHERE id = ?`, noteID).Scan(&noteCount)
	med.DB.QueryRow(`SELECT count(*) FROM duty_handovers WHERE note_id = ?`, noteID).Scan(&dhCount)
	if noteCount != 0 || dhCount != 0 {
		t.Fatalf("expected note and duty_handover to be deleted, got noteCount=%d, dhCount=%d", noteCount, dhCount)
	}

	// Verify NOTE_DELETED audit entry
	var deleteAuditCount int
	med.DB.QueryRow(`SELECT count(*) FROM audit_logs WHERE action = 'NOTE_DELETED' AND entity_id = ?`, noteID).Scan(&deleteAuditCount)
	if deleteAuditCount != 1 {
		t.Fatalf("expected 1 NOTE_DELETED audit row, got %d", deleteAuditCount)
	}
}
