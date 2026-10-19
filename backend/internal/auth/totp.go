package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP per RFC 6238: HMAC-SHA1, 30-second steps, 6 digits. These are the defaults
// every authenticator app (Google/Microsoft Authenticator, iOS Passwords, 1Password)
// assumes, so the otpauth URI only needs the secret.

const (
	totpPeriod = 30
	totpDigits = 6
	// Accept the previous and next step to tolerate phone clock drift.
	totpSkew = 1
	// RecoveryCodeCount one-time codes are issued when 2FA is enabled.
	RecoveryCodeCount = 8
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random 160-bit secret, base32-encoded for authenticator apps.
func NewTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b32.EncodeToString(b), nil
}

// TOTPURI builds the otpauth:// URI shown as a QR code (or tapped on a phone).
func TOTPURI(secret, account string) string {
	label := url.PathEscape("Birrcare:" + account)
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", "Birrcare")
	return "otpauth://totp/" + label + "?" + v.Encode()
}

func hotp(key []byte, counter int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, bin%1_000_000)
}

// TOTPCodeAt returns the code for a raw key at time t (exported for tests).
func TOTPCodeAt(key []byte, t time.Time) string {
	return hotp(key, t.Unix()/totpPeriod)
}

// VerifyTOTP checks a 6-digit code against a base32 secret. It returns the matched
// time step; steps <= lastStep are refused so an observed code cannot be replayed.
func VerifyTOTP(secret, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, false
	}
	key, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return 0, false
	}
	cur := now.Unix() / totpPeriod
	for d := int64(-totpSkew); d <= totpSkew; d++ {
		step := cur + d
		if step <= lastStep {
			continue
		}
		if hmac.Equal([]byte(hotp(key, step)), []byte(code)) {
			return step, true
		}
	}
	return 0, false
}

// NewRecoveryCodes returns plaintext codes (shown once) and their SHA-256 hashes
// (stored). The codes carry 50 bits of entropy, so a fast hash is sufficient.
func NewRecoveryCodes() (plain, hashes []string, err error) {
	for i := 0; i < RecoveryCodeCount; i++ {
		b := make([]byte, 7)
		if _, err := rand.Read(b); err != nil {
			return nil, nil, err
		}
		c := strings.ToLower(b32.EncodeToString(b))[:10]
		c = c[:5] + "-" + c[5:]
		plain = append(plain, c)
		hashes = append(hashes, HashRecoveryCode(c))
	}
	return plain, hashes, nil
}

// HashRecoveryCode normalises (case, dashes, spaces) and hashes a recovery code.
func HashRecoveryCode(code string) string {
	n := strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
	sum := sha256.Sum256([]byte("birrcare-recovery:" + n))
	return hex.EncodeToString(sum[:])
}

// Two-factor login challenge: proof that the PIN was correct, valid for a few
// minutes, exchanged for a session only together with a valid code. The payload
// is domain-separated from session tokens so one can never be used as the other.

const ChallengeTTL = 5 * time.Minute

func NewChallenge(caregiverID string) string {
	payload := fmt.Sprintf("2fa|%s|%d", caregiverID, time.Now().Unix())
	return payload + "|" + sign(payload)
}

func ParseChallenge(token string) (string, error) {
	parts := strings.Split(token, "|")
	if len(parts) != 4 || parts[0] != "2fa" {
		return "", fmt.Errorf("invalid challenge")
	}
	payload := strings.Join(parts[:3], "|")
	if !hmac.Equal([]byte(parts[3]), []byte(sign(payload))) {
		return "", fmt.Errorf("invalid challenge signature")
	}
	var ts int64
	if _, err := fmt.Sscanf(parts[2], "%d", &ts); err != nil {
		return "", fmt.Errorf("invalid challenge time")
	}
	age := time.Since(time.Unix(ts, 0))
	if age > ChallengeTTL || age < -time.Minute {
		return "", fmt.Errorf("challenge expired")
	}
	return parts[1], nil
}
