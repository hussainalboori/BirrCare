package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"github.com/go-chi/chi/v5"
)

// Several photos per medicine (box front, back, the pill); one is the main one.

func medPhoto(n int) string {
	return fmt.Sprintf("%s%08d-0000-0000-0000-000000000000.jpg", handlers.PhotoURLPrefix, n)
}

func medJSON(t *testing.T, h *handlers.MedicationHandler, method, id string, body map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	ali := &models.Caregiver{ID: "cg_ali", Name: "Ali", Role: models.RoleAdmin}
	b, _ := json.Marshal(body)
	url := "/api/v1/medications"
	if id != "" {
		url += "/" + id
	}
	r := httptest.NewRequest(method, url, bytes.NewReader(b))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", id)
	r = r.WithContext(context.WithValue(authContext(r.Context(), ali), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	if method == http.MethodPost {
		h.CreateMedication(rec, r)
	} else {
		h.UpdateMedication(rec, r)
	}
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func medPhotos(t *testing.T, h *handlers.MedicationHandler, id string) (string, []string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ListMedications(rec, httptest.NewRequest(http.MethodGet, "/api/v1/medications", nil))
	var list []models.Medication
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	for _, m := range list {
		if m.ID == id {
			return m.ImageURL, m.PhotoURLs
		}
	}
	t.Fatalf("medicine %s not listed", id)
	return "", nil
}

func TestMedicineHasSeveralPhotos(t *testing.T) {
	h, _, cleanup := setupTestDB(t)
	defer cleanup()

	code, out := medJSON(t, h, http.MethodPost, "", map[string]interface{}{
		"name_ar": "إليكويس", "name_en": "Eliquis", "dosage": "5 mg", "frequency_slots": []string{"morning"},
		"photo_urls": []string{medPhoto(1), medPhoto(2), medPhoto(2), medPhoto(3)},
	})
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, out)
	}
	id, _ := out["id"].(string)
	main, photos := medPhotos(t, h, id)
	if main != medPhoto(1) || len(photos) != 3 {
		t.Fatalf("create: main %q photos %v; want the first as main, repeats dropped", main, photos)
	}

	// Choose another main photo and drop one.
	if code, out := medJSON(t, h, http.MethodPut, id, map[string]interface{}{
		"photo_urls": []string{medPhoto(3), medPhoto(1)}, "image_url": medPhoto(3),
	}); code != http.StatusOK {
		t.Fatalf("update: %d %v", code, out)
	}
	if main, photos := medPhotos(t, h, id); main != medPhoto(3) || len(photos) != 2 {
		t.Fatalf("update: main %q photos %v", main, photos)
	}

	// An edit that doesn't send photos leaves them alone.
	if code, _ := medJSON(t, h, http.MethodPut, id, map[string]interface{}{"dosage": "5 mg twice"}); code != http.StatusOK {
		t.Fatalf("plain edit: %d", code)
	}
	if _, photos := medPhotos(t, h, id); len(photos) != 2 {
		t.Fatalf("plain edit lost photos: %v", photos)
	}

	// Removing every photo leaves no main photo pointing at a removed one.
	if code, _ := medJSON(t, h, http.MethodPut, id, map[string]interface{}{"photo_urls": []string{}}); code != http.StatusOK {
		t.Fatalf("remove all: %d", code)
	}
	if main, photos := medPhotos(t, h, id); main != "" || len(photos) != 0 {
		t.Fatalf("remove all: main %q photos %v", main, photos)
	}

	// Refused: a link from elsewhere, and more than 6.
	if code, out := medJSON(t, h, http.MethodPut, id, map[string]interface{}{"photo_urls": []string{"https://example.com/a.jpg"}}); code != http.StatusBadRequest || out["error"] != "INVALID_PHOTOS" {
		t.Fatalf("foreign link: %d %v", code, out)
	}
	seven := []string{}
	for i := 1; i <= 7; i++ {
		seven = append(seven, medPhoto(i))
	}
	if code, _ := medJSON(t, h, http.MethodPut, id, map[string]interface{}{"photo_urls": seven}); code != http.StatusBadRequest {
		t.Fatalf("7 photos: want 400, got %d", code)
	}
}
