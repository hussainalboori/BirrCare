package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"github.com/go-chi/chi/v5"
)

// Bruises on a blood thinner: where, how big, a photo to compare, and bleeding
// signs that alert everyone.

func bruiseFixture(t *testing.T) (*handlers.BruiseHandler, *handlers.MedicationHandler) {
	t.Helper()
	med, _, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)
	return &handlers.BruiseHandler{DB: med.DB, Broker: med.Broker, PhotoDir: filepath.Join(t.TempDir(), "photos")}, med
}

var (
	bruiseFatima = &models.Caregiver{ID: "cg_fatima", Name: "Fatima", Role: models.RoleCaregiver}
	bruiseViewer = &models.Caregiver{ID: "cg_zainab", Name: "Zainab", Role: models.RoleViewer}
)

func postBruise(t *testing.T, h *handlers.BruiseHandler, who *models.Caregiver, body map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/bruises", bytes.NewReader(b))
	r = r.WithContext(authContext(r.Context(), who))
	rec := httptest.NewRecorder()
	h.Create(rec, r)
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func countAlerts(t *testing.T, h *handlers.BruiseHandler, kind string) int {
	t.Helper()
	var n int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM alerts WHERE kind = ?`, kind).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBruiseWithPhotoNoAlert(t *testing.T) {
	h, _ := bruiseFixture(t)

	// Upload the photo first, as the phone does.
	r := httptest.NewRequest(http.MethodPost, "/api/v1/bruises/photos", bytes.NewReader(withExif(testJPEG(t))))
	r = r.WithContext(authContext(r.Context(), bruiseFatima))
	rec := httptest.NewRecorder()
	h.UploadPhoto(rec, r)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var up map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &up)
	url := up["photo_url"]
	if !strings.HasPrefix(url, handlers.BruisePhotoURLPrefix) {
		t.Fatalf("photo url %q", url)
	}
	name := strings.TrimPrefix(url, handlers.BruisePhotoURLPrefix)
	saved, err := os.ReadFile(filepath.Join(h.PhotoDir, "bruises", name))
	if err != nil || bytes.Contains(saved, []byte("GPSLatitude")) {
		t.Fatalf("photo must be saved in photos/bruises without phone metadata (err %v)", err)
	}

	code, out := postBruise(t, h, bruiseFatima, map[string]interface{}{
		"body_part": "arm_left", "size": "medium", "change": "new", "photo_url": url, "notes": "after bumping the door",
	})
	if code != http.StatusCreated || out["body_part"] != "arm_left" || out["photo_url"] != url {
		t.Fatalf("bruise: %d %v", code, out)
	}
	if n := countAlerts(t, h, "bleeding"); n != 0 {
		t.Fatalf("a bruise alone must not alert everyone, got %d alerts", n)
	}

	// Served back to a signed-in phone.
	sr := httptest.NewRequest(http.MethodGet, url, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	sr = sr.WithContext(context.WithValue(sr.Context(), chi.RouteCtxKey, rctx))
	srec := httptest.NewRecorder()
	h.ServePhoto(srec, sr)
	if srec.Code != http.StatusOK || srec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("serve: %d", srec.Code)
	}

	// Listed, newest first.
	lrec := httptest.NewRecorder()
	h.List(lrec, httptest.NewRequest(http.MethodGet, "/api/v1/bruises?days=7", nil))
	var list []handlers.BruiseLog
	_ = json.Unmarshal(lrec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].Size != "medium" || !strings.Contains(list[0].CaregiverName, "Fatima") {
		t.Fatalf("list: %+v", list)
	}
}

func TestBleedingSignAlertsEveryone(t *testing.T) {
	h, _ := bruiseFixture(t)
	code, out := postBruise(t, h, bruiseFatima, map[string]interface{}{"bleeding_signs": []string{"gums", "gums", "stool"}})
	if code != http.StatusCreated {
		t.Fatalf("bleeding only: %d %v", code, out)
	}
	if signs, _ := out["bleeding_signs"].([]interface{}); len(signs) != 2 {
		t.Fatalf("signs must be de-duplicated: %v", out["bleeding_signs"])
	}
	if n := countAlerts(t, h, "bleeding"); n != 1 {
		t.Fatalf("want 1 bleeding alert, got %d", n)
	}
	var audits int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'LOG_BRUISE'`).Scan(&audits)
	if audits != 1 {
		t.Fatalf("want the report audited, got %d", audits)
	}
}

func TestBruiseValidation(t *testing.T) {
	h, _ := bruiseFixture(t)
	cases := []struct {
		body map[string]interface{}
		code string
	}{
		{map[string]interface{}{}, "EMPTY_REPORT"},
		{map[string]interface{}{"body_part": "tail"}, "INVALID_BODY_PART"},
		{map[string]interface{}{"body_part": "belly", "size": "huge"}, "INVALID_SIZE"},
		{map[string]interface{}{"body_part": "belly", "change": "purple"}, "INVALID_CHANGE"},
		{map[string]interface{}{"bleeding_signs": []string{"ears"}}, "INVALID_BLEEDING_SIGN"},
		{map[string]interface{}{"body_part": "belly", "photo_url": "https://example.com/x.jpg"}, "INVALID_PHOTO"},
		{map[string]interface{}{"body_part": "belly", "photo_url": handlers.PhotoURLPrefix + "00000000-0000-0000-0000-000000000000.jpg"}, "INVALID_PHOTO"},
	}
	for _, c := range cases {
		if code, out := postBruise(t, h, bruiseFatima, c.body); code != http.StatusBadRequest || out["error"] != c.code {
			t.Fatalf("%v: want 400 %s, got %d %v", c.body, c.code, code, out)
		}
	}
	if code, _ := postBruise(t, h, bruiseViewer, map[string]interface{}{"body_part": "belly"}); code != http.StatusForbidden {
		t.Fatalf("viewer: want 403, got %d", code)
	}
	// A resend from the offline outbox returns the first report.
	body := map[string]interface{}{"body_part": "leg_right", "client_request_id": "req-1"}
	c1, first := postBruise(t, h, bruiseFatima, body)
	c2, again := postBruise(t, h, bruiseFatima, body)
	if c1 != http.StatusCreated || c2 != http.StatusOK || first["id"] != again["id"] {
		t.Fatalf("retry: %d %v / %d %v", c1, first, c2, again)
	}
}
