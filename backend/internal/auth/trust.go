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
	"strconv"
	"strings"
	"time"
)

const (
	HomeDeviceCookieName        = "birrcare_home_device"
	HomeDeviceHeaderName        = "X-Home-Device-Token"
	HomeDeviceCookieTTL         = 365 * 24 * time.Hour
	DefaultGeofenceRadiusMeters = 75.0
)

type TrustStatus struct {
	IsTrusted              bool   `json:"is_trusted"`
	TrustReason            string `json:"trust_reason"` // "paired_device", "geofence", "lan_subnet", "untrusted"
	HomeGeofenceConfigured bool   `json:"home_geofence_configured"`
	PairedDeviceName       string `json:"paired_device_name,omitempty"`
}

type HomeLocationSettings struct {
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
	RadiusMeters float64  `json:"radius_meters"`
}

type PairedDeviceInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
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

// GetClientIP extracts client IP taking headers into account.
func GetClientIP(r *http.Request) string {
	// 1. Cloudflare connecting IP
	if cfIP := strings.TrimSpace(r.Header.Get("Cf-Connecting-Ip")); cfIP != "" {
		return cfIP
	}
	// 2. X-Forwarded-For
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			ip := strings.TrimSpace(parts[0])
			if ip != "" {
				return ip
			}
		}
	}
	// 3. X-Real-IP
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	// 4. RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

// IsLocalOrPrivateIP checks if the IP is in private/loopback address space or configured TRUSTED_HOME_IP.
func IsLocalOrPrivateIP(ipStr string) bool {
	cleanIP := strings.Trim(strings.TrimSpace(ipStr), "[]")
	ip := net.ParseIP(cleanIP)
	if ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return true
		}
	}

	// Check environment variable TRUSTED_HOME_IP or TRUSTED_HOME_SUBNETS
	envTrusted := os.Getenv("TRUSTED_HOME_IP")
	if envTrusted == "" {
		envTrusted = os.Getenv("TRUSTED_HOME_SUBNETS")
	}
	if envTrusted != "" {
		subnets := strings.Split(envTrusted, ",")
		for _, s := range subnets {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if s == cleanIP {
				return true
			}
			if _, cidr, err := net.ParseCIDR(s); err == nil && ip != nil {
				if cidr.Contains(ip) {
					return true
				}
			}
		}
	}

	return false
}

// EvaluateTrust checks all 3 layers:
// Layer 1: Paired Home Device Token
// Layer 2: Geofence Coordinates (Haversine <= 75m)
// Layer 3: Local Private LAN / Subnet / Configured IP
func EvaluateTrust(r *http.Request, db *sql.DB, lat, lon *float64) TrustStatus {
	// Check if home geofence is configured in settings
	var homeLatStr, homeLonStr, homeRadiusStr string
	_ = db.QueryRow("SELECT value FROM app_settings WHERE key = 'home_latitude'").Scan(&homeLatStr)
	_ = db.QueryRow("SELECT value FROM app_settings WHERE key = 'home_longitude'").Scan(&homeLonStr)
	_ = db.QueryRow("SELECT value FROM app_settings WHERE key = 'home_geofence_meters'").Scan(&homeRadiusStr)

	homeConfigured := false
	var homeLat, homeLon float64
	var homeRadius = DefaultGeofenceRadiusMeters

	if homeLatStr != "" && homeLonStr != "" {
		var err1, err2 error
		homeLat, err1 = strconv.ParseFloat(homeLatStr, 64)
		homeLon, err2 = strconv.ParseFloat(homeLonStr, 64)
		if err1 == nil && err2 == nil {
			homeConfigured = true
		}
	}
	if homeRadiusStr != "" {
		if rVal, err := strconv.ParseFloat(homeRadiusStr, 64); err == nil && rVal > 0 {
			homeRadius = rVal
		}
	}

	// -------------------------------------------------------------
	// Layer 1: Paired Home Device Token (persistent bedside tablet / phone)
	// -------------------------------------------------------------
	var token string
	if c, err := r.Cookie(HomeDeviceCookieName); err == nil && c.Value != "" {
		token = c.Value
	} else if hTok := r.Header.Get(HomeDeviceHeaderName); hTok != "" {
		token = hTok
	}

	if token != "" {
		if deviceID, ok := ParseHomeDeviceToken(token); ok {
			var deviceName string
			var isRevoked int
			err := db.QueryRow(`
				SELECT name, is_revoked FROM paired_devices WHERE device_token_hash = ?
			`, HomeDeviceHash(deviceID)).Scan(&deviceName, &isRevoked)
			if err == nil && isRevoked == 0 {
				// Record last used time
				_, _ = db.Exec("UPDATE paired_devices SET last_used_at = datetime('now') WHERE device_token_hash = ?", HomeDeviceHash(deviceID))
				return TrustStatus{
					IsTrusted:              true,
					TrustReason:            "paired_device",
					HomeGeofenceConfigured: homeConfigured,
					PairedDeviceName:       deviceName,
				}
			}
		}
	}

	// -------------------------------------------------------------
	// Layer 2: Geofence Fallback (Haversine formula <= 75m)
	// -------------------------------------------------------------
	if homeConfigured && lat != nil && lon != nil {
		dist := DistanceMeters(*lat, *lon, homeLat, homeLon)
		if dist <= homeRadius {
			return TrustStatus{
				IsTrusted:              true,
				TrustReason:            "geofence",
				HomeGeofenceConfigured: true,
			}
		}
	}

	// -------------------------------------------------------------
	// Layer 3: Local Private LAN / Subnet / Configured IP
	// -------------------------------------------------------------
	clientIP := GetClientIP(r)
	if IsLocalOrPrivateIP(clientIP) {
		return TrustStatus{
			IsTrusted:              true,
			TrustReason:            "lan_subnet",
			HomeGeofenceConfigured: homeConfigured,
		}
	}

	// Untrusted (e.g., cellular network outside geofence, not paired)
	return TrustStatus{
		IsTrusted:              false,
		TrustReason:            "untrusted",
		HomeGeofenceConfigured: homeConfigured,
	}
}
