package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"math"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	HomeDeviceCookieName        = "birrcare_home_device"
	HomeDeviceCookieTTL         = 365 * 24 * time.Hour
	DefaultGeofenceRadiusMeters = 75.0
)

type TrustStatus struct {
	IsTrusted        bool   `json:"trusted"`
	TrustReason      string `json:"reason"` // "paired_device", "trusted_ip", "untrusted"
	PairedDeviceName string `json:"device_name,omitempty"`
	PairedDeviceID   string `json:"-"`
}

// HomeDeviceID returns the paired-device id from this browser's HttpOnly
// cookie (signature checked, not yet looked up), or "".
func HomeDeviceID(r *http.Request) string {
	c, err := r.Cookie(HomeDeviceCookieName)
	if err != nil || c.Value == "" {
		return ""
	}
	id, _ := ParseHomeDeviceToken(c.Value)
	return id
}

type HomeLocationSettings struct {
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
	RadiusMeters float64  `json:"radius_meters"`
}

type PairedDeviceInfo struct {
	ID           string `json:"id"`
	Name         string `json:"device_name"`
	PairedByID   string `json:"paired_by_id"`
	PairedByName string `json:"paired_by_name"`
	CreatedAt    string `json:"created_at"`
	LastUsedAt   string `json:"last_used_at"`
}

// NewHomeDeviceToken creates a cryptographically signed home device pairing token.
func NewHomeDeviceToken(id string) string {
	return id + ":" + sign("home_device|"+id)
}

// ParseHomeDeviceToken validates HMAC signature and extracts the device ID.
func ParseHomeDeviceToken(token string) (string, bool) {
	id, sig, ok := strings.Cut(token, ":")
	if !ok || len(id) == 0 || strings.Contains(sig, ":") {
		return "", false
	}
	expectedSig := sign("home_device|" + id)
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return "", false
	}
	return id, true
}

// HomeDeviceHash hashes the ID for database lookups.
func HomeDeviceHash(id string) string {
	sum := sha256.Sum256([]byte("home_device|" + id))
	return hex.EncodeToString(sum[:])
}

// GenerateRandomID creates a hex random identifier.
func GenerateRandomID(bytesLen int) string {
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		panic("auth: rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// DistanceMeters calculates the Great Circle distance in meters between two coordinates (Haversine formula).
func DistanceMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadius = 6371000.0 // meters
	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)

	lat1Rad := lat1 * (math.Pi / 180.0)
	lat2Rad := lat2 * (math.Pi / 180.0)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadius * c
}

// GetClientIP returns the caller's address. Only Cf-Connecting-Ip is used:
// Cloudflare sets it at the edge and the backend is bound to 127.0.0.1, so it
// cannot be forged from outside. X-Forwarded-For / X-Real-IP are client
// controlled and are ignored.
func GetClientIP(r *http.Request) string {
	if cfIP := strings.TrimSpace(r.Header.Get("Cf-Connecting-Ip")); cfIP != "" {
		return cfIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

// IsTrustedHomeIP reports whether ipStr matches TRUSTED_HOME_IP /
// TRUSTED_HOME_SUBNETS (comma-separated IPs or CIDRs). Private and loopback
// addresses are not trusted by themselves: behind the Cloudflare Tunnel every
// request reaches the backend from 127.0.0.1 or the Docker network.
func IsTrustedHomeIP(ipStr string) bool {
	cleanIP := strings.Trim(strings.TrimSpace(ipStr), "[]")
	ip := net.ParseIP(cleanIP)
	if ip == nil {
		return false
	}
	// Read by name so scripts/tests/env-schema.test.sh can see both settings.
	for _, list := range []string{os.Getenv("TRUSTED_HOME_IP"), os.Getenv("TRUSTED_HOME_SUBNETS")} {
		for _, s := range strings.Split(list, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if t := net.ParseIP(s); t != nil && t.Equal(ip) {
				return true
			}
			if _, cidr, err := net.ParseCIDR(s); err == nil && cidr.Contains(ip) {
				return true
			}
		}
	}
	return false
}

// EvaluateTrust decides whether this browser may switch profile without a PIN.
// Only things the server itself issued or observes count (issue #53):
// Layer 1: the paired-device HttpOnly cookie
// Layer 2: the configured home IP / subnet (TRUSTED_HOME_IP), as Cloudflare reports it
// Anything the phone reports about itself (GPS position, a token in a header)
// can be forged, so it never grants trust.
func EvaluateTrust(r *http.Request, db *sql.DB) TrustStatus {
	// -------------------------------------------------------------
	// Layer 1: Paired Home Device cookie (persistent bedside tablet / phone)
	// -------------------------------------------------------------
	if deviceID := HomeDeviceID(r); deviceID != "" {
		var deviceName string
		var isRevoked int
		err := db.QueryRow(`
			SELECT name, is_revoked FROM paired_devices WHERE device_token_hash = ?
		`, HomeDeviceHash(deviceID)).Scan(&deviceName, &isRevoked)
		if err == nil && isRevoked == 0 {
			// Record last used time
			_, _ = db.Exec("UPDATE paired_devices SET last_used_at = datetime('now') WHERE device_token_hash = ?", HomeDeviceHash(deviceID))
			return TrustStatus{
				IsTrusted:        true,
				TrustReason:      "paired_device",
				PairedDeviceName: deviceName,
				PairedDeviceID:   deviceID,
			}
		}
	}

	// -------------------------------------------------------------
	// Layer 2: Configured home IP / subnet (TRUSTED_HOME_IP)
	// -------------------------------------------------------------
	if IsTrustedHomeIP(GetClientIP(r)) {
		return TrustStatus{IsTrusted: true, TrustReason: "trusted_ip"}
	}

	return TrustStatus{IsTrusted: false, TrustReason: "untrusted"}
}
