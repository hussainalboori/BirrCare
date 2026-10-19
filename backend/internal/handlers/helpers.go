package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// writeJSONError writes a JSON error body with the correct Content-Type.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": code, "message": message})
}

// internalError logs the real error server-side and returns a generic message,
// so SQL / schema details never leak to clients.
func internalError(w http.ResponseWriter, what string, err error) {
	log.Printf("[ERROR] %s: %v", what, err)
	writeJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", what)
}

// isUniqueViolation reports whether err is specifically a UNIQUE constraint
// failure (not a FOREIGN KEY / CHECK / NOT NULL failure).
func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
	}
	return false
}

const sqliteTimeLayout = "2006-01-02 15:04:05"

// sqliteToRFC3339 converts a stored UTC "YYYY-MM-DD HH:MM:SS" into RFC3339 with
// an explicit Z, so browsers (notably iOS Safari) don't misparse or treat it as local.
func sqliteToRFC3339(s string) string {
	t, err := time.Parse(sqliteTimeLayout, s)
	if err != nil {
		return s
	}
	return t.UTC().Format(time.RFC3339)
}

var (
	clinicalLocOnce sync.Once
	clinicalLoc     *time.Location
)

// ClinicalLocation is the household's time zone used to decide which calendar day
// a dose belongs to. Set APP_TIMEZONE (e.g. "Asia/Riyadh"); defaults to the
// process local zone (TZ env), which is UTC in a bare container.
func ClinicalLocation() *time.Location {
	clinicalLocOnce.Do(func() {
		clinicalLoc = time.Local
		if tz := os.Getenv("APP_TIMEZONE"); tz != "" {
			if loc, err := time.LoadLocation(tz); err == nil {
				clinicalLoc = loc
			} else {
				log.Printf("[WARN] invalid APP_TIMEZONE %q, falling back to %s: %v", tz, time.Local, err)
			}
		}
	})
	return clinicalLoc
}

// clinicalToday returns today's date (YYYY-MM-DD) in the household time zone.
func clinicalToday() string {
	return time.Now().In(ClinicalLocation()).Format("2006-01-02")
}

// parseClinicalDate validates a strict YYYY-MM-DD date and returns it canonicalised.
func parseClinicalDate(s string) (string, time.Time, bool) {
	d, err := time.ParseInLocation("2006-01-02", s, ClinicalLocation())
	if err != nil {
		return "", time.Time{}, false
	}
	return d.Format("2006-01-02"), d, true
}
