package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// DeviceCookieName marks a browser that has entered a caregiver's PIN correctly
// before. PIN throttling gives each such device its own budget, so someone guessing
// from elsewhere can lock out only unknown devices, never the family's own phones.
// It is a random id, not a credential: it grants no access by itself.
const (
	DeviceCookieName = "birrcare_device"
	DeviceCookieTTL  = 365 * 24 * time.Hour
)

// NewDeviceToken returns a signed device token and its id.
func NewDeviceToken() (token, id string) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("auth: cannot read crypto/rand: " + err.Error())
	}
	id = hex.EncodeToString(b)
	return id + ":" + sign("device|"+id), id
}

// ParseDeviceToken checks the signature and returns the device id.
func ParseDeviceToken(token string) (string, bool) {
	id, sig, ok := strings.Cut(token, ":")
	if !ok || len(id) != 32 || strings.Contains(sig, ":") {
		return "", false
	}
	if !hmac.Equal([]byte(sig), []byte(sign("device|"+id))) {
		return "", false
	}
	return id, true
}

// DeviceHash is how a device id is stored server side.
func DeviceHash(id string) string {
	sum := sha256.Sum256([]byte("device|" + id))
	return hex.EncodeToString(sum[:])
}

// PINLimitKey is the limiter key for a PIN attempt. trustedDeviceID is a device
// this caregiver has signed in from before, or "" for any other client: those all
// share one budget, which still caps a guesser at a handful of tries.
func PINLimitKey(caregiverID, trustedDeviceID string) string {
	if trustedDeviceID == "" {
		return caregiverID + "|untrusted"
	}
	return caregiverID + "|device:" + trustedDeviceID
}
