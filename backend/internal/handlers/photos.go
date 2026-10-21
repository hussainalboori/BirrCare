package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // PNG uploads are accepted and re-saved as JPEG
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Medicine photos: a picture of the box or the pill, taken with the phone
// camera. The phone shrinks the photo before upload; the server checks it is
// really an image and re-saves it as a plain JPEG, which also drops the
// metadata phones embed (GPS location, camera details). Photos live next to
// the database and are linked from medications.image_url, so schedule
// responses carry a short URL rather than the image itself.

const (
	// A decoded image larger than this is refused (a small file can still
	// decode to a huge bitmap).
	maxPhotoPixels = 4096 * 4096
	photoQuality   = 85
	// PhotoURLPrefix is what medications.image_url holds for an uploaded photo.
	PhotoURLPrefix = "/api/v1/medications/photos/"
)

var photoNameRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.jpg$`)

// PhotoDirFromEnv is PHOTO_DIR, else "photos" next to the database.
func PhotoDirFromEnv(dbPath string) string {
	if d := os.Getenv("PHOTO_DIR"); d != "" {
		return d
	}
	return filepath.Join(filepath.Dir(dbPath), "photos")
}

type PhotoHandler struct {
	DB  *sql.DB
	Dir string
}

// Upload: POST /medications/photos with the image as the request body
// (Content-Type image/jpeg or image/png). Admins only, like editing medicines.
func (h *PhotoHandler) Upload(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if me == nil {
		writeJSONError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}
	if !auth.CanManageMedications(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Only Admins and Super Admins can change medicine photos")
		return
	}

	data, err := io.ReadAll(r.Body) // capped by the router's body limit
	if err != nil {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "PHOTO_TOO_LARGE", "photo is too large")
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		writeJSONError(w, http.StatusBadRequest, "NOT_A_PHOTO", "send a JPEG or PNG photo")
		return
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPhotoPixels {
		writeJSONError(w, http.StatusBadRequest, "PHOTO_TOO_BIG", "photo dimensions are too large")
		return
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "NOT_A_PHOTO", "the photo could not be read")
		return
	}

	if err := os.MkdirAll(h.Dir, 0o755); err != nil {
		internalError(w, "failed creating photo folder", err)
		return
	}
	name := uuid.New().String() + ".jpg"
	tmp, err := os.CreateTemp(h.Dir, ".upload-*")
	if err != nil {
		internalError(w, "failed saving photo", err)
		return
	}
	if err := jpeg.Encode(tmp, img, &jpeg.Options{Quality: photoQuality}); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		internalError(w, "failed saving photo", err)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		internalError(w, "failed saving photo", err)
		return
	}
	_ = os.Chmod(tmp.Name(), 0o644)
	if err := os.Rename(tmp.Name(), filepath.Join(h.Dir, name)); err != nil {
		os.Remove(tmp.Name())
		internalError(w, "failed saving photo", err)
		return
	}

	audit.Log(h.DB, me.ID, "UPLOAD_MED_PHOTO", "medication_photo", name,
		fmt.Sprintf("%dx%d %s, %d bytes", cfg.Width, cfg.Height, format, len(data)))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"image_url": PhotoURLPrefix + name})
}

// Serve: GET /medications/photos/{name}. Signed-in caregivers only; a photo
// never changes under its name, so the phone may keep it.
func (h *PhotoHandler) Serve(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !photoNameRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(h.Dir, name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	io.Copy(w, f)
}
