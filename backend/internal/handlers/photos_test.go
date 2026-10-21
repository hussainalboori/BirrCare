package handlers_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/models"
	"github.com/go-chi/chi/v5"
)

// A medicine box photo from the phone: uploaded by an admin, re-saved as a
// clean JPEG (no GPS / EXIF), and served only under its random name.

func photoFixture(t *testing.T) (*handlers.PhotoHandler, string) {
	t.Helper()
	database, _, cleanup := setupSmartAuthTestDB(t)
	t.Cleanup(cleanup)
	createTestCaregiver(t, database, "ad1", "Fatima", "admin", "1234")
	dir := filepath.Join(t.TempDir(), "photos")
	return &handlers.PhotoHandler{DB: database, Dir: dir}, dir
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for x := 0; x < 40; x++ {
		img.Set(x, 10, color.RGBA{200, 30, 30, 255})
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// withExif inserts an APP1 "Exif" segment carrying a fake GPS marker.
func withExif(j []byte) []byte {
	payload := append([]byte("Exif\x00\x00"), []byte("GPSLatitude 26.2235 GPSLongitude 50.5876")...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	out := append([]byte{}, j[:2]...)
	out = append(out, seg...)
	out = append(out, payload...)
	return append(out, j[2:]...)
}

func upload(h *handlers.PhotoHandler, role string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/medications/photos", bytes.NewReader(body))
	req.Header.Set("Content-Type", "image/jpeg")
	req = req.WithContext(context.WithValue(req.Context(), auth.CaregiverContextKey, &models.Caregiver{ID: "ad1", Name: "Fatima", Role: role}))
	rec := httptest.NewRecorder()
	h.Upload(rec, req)
	return rec
}

func serve(h *handlers.PhotoHandler, name string) *httptest.ResponseRecorder {
	rc := chi.NewRouteContext()
	rc.URLParams.Add("name", name)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/medications/photos/"+name, nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	h.Serve(rec, req)
	return rec
}

func TestPhotoUploadStripsMetadataAndServes(t *testing.T) {
	h, dir := photoFixture(t)
	original := withExif(testJPEG(t))
	if !bytes.Contains(original, []byte("GPSLatitude")) {
		t.Fatal("fixture should carry GPS metadata")
	}

	rec := upload(h, "admin", original)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var res map[string]string
	json.NewDecoder(rec.Body).Decode(&res)
	if !strings.HasPrefix(res["image_url"], handlers.PhotoURLPrefix) || !strings.HasSuffix(res["image_url"], ".jpg") {
		t.Fatalf("unexpected image_url %q", res["image_url"])
	}
	name := strings.TrimPrefix(res["image_url"], handlers.PhotoURLPrefix)

	saved, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("photo not saved: %v", err)
	}
	if bytes.Contains(saved, []byte("Exif")) || bytes.Contains(saved, []byte("GPSLatitude")) {
		t.Error("the saved photo must not keep EXIF / GPS data")
	}

	got := serve(h, name)
	if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("serve: %d %s", got.Code, got.Header().Get("Content-Type"))
	}
	if _, err := jpeg.Decode(got.Body); err != nil {
		t.Errorf("served file is not a JPEG: %v", err)
	}
	if cc := got.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("photos should be cacheable, got %q", cc)
	}
}

func TestPhotoUploadPNGBecomesJPEG(t *testing.T) {
	h, _ := photoFixture(t)
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 20, 20)))
	rec := upload(h, "admin", b.Bytes())
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for PNG, got %d", rec.Code)
	}
}

func TestPhotoUploadRefusals(t *testing.T) {
	h, dir := photoFixture(t)
	if rec := upload(h, "caregiver", testJPEG(t)); rec.Code != http.StatusForbidden {
		t.Errorf("caregiver: expected 403, got %d", rec.Code)
	}
	if rec := upload(h, "admin", []byte("<svg onload=alert(1)></svg>")); rec.Code != http.StatusBadRequest {
		t.Errorf("not an image: expected 400, got %d", rec.Code)
	}
	if rec := upload(h, "admin", hugePNGHeader(20000, 20000)); rec.Code != http.StatusBadRequest {
		t.Errorf("20000x20000: expected 400, got %d", rec.Code)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("nothing may be saved for refused uploads, found %d files", len(entries))
	}
}

func TestPhotoServeOnlyValidNames(t *testing.T) {
	h, _ := photoFixture(t)
	for _, name := range []string{"../birrcare.db", "x.jpg", "..%2Fbirrcare.db", "00000000-0000-0000-0000-000000000000.jpg"} {
		if rec := serve(h, name); rec.Code != http.StatusNotFound {
			t.Errorf("%q: expected 404, got %d", name, rec.Code)
		}
	}
}

// hugePNGHeader is a PNG signature + IHDR claiming w x h, without pixel data.
func hugePNGHeader(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	chunk := append([]byte("IHDR"), ihdr...)
	out := []byte("\x89PNG\r\n\x1a\n")
	l := make([]byte, 4)
	binary.BigEndian.PutUint32(l, 13)
	out = append(out, l...)
	out = append(out, chunk...)
	c := make([]byte, 4)
	binary.BigEndian.PutUint32(c, crc32.ChecksumIEEE(chunk))
	return append(out, c...)
}
