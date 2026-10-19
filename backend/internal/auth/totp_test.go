package auth

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 Appendix B (SHA-1 key "12345678901234567890"), last 6 of the 8 digits.
func TestTOTPRFC6238Vectors(t *testing.T) {
	key := []byte("12345678901234567890")
	cases := map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	}
	for ts, want := range cases {
		if got := TOTPCodeAt(key, time.Unix(ts, 0)); got != want {
			t.Errorf("t=%d: got %s want %s", ts, got, want)
		}
	}
}

func TestVerifyTOTPSkewAndReplay(t *testing.T) {
	secret, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	key, _ := b32.DecodeString(secret)
	now := time.Unix(1_800_000_000, 0)

	step, ok := VerifyTOTP(secret, TOTPCodeAt(key, now.Add(-30*time.Second)), now, 0)
	if !ok {
		t.Fatal("previous-step code (clock drift) must be accepted")
	}
	if _, ok := VerifyTOTP(secret, TOTPCodeAt(key, now.Add(-30*time.Second)), now, step); ok {
		t.Fatal("a code already used must not be accepted again")
	}
	if _, ok := VerifyTOTP(secret, TOTPCodeAt(key, now.Add(-90*time.Second)), now, 0); ok {
		t.Fatal("a code from 3 steps ago must be rejected")
	}
	if _, ok := VerifyTOTP(secret, "12345", now, 0); ok {
		t.Fatal("short code must be rejected")
	}
}

func TestRecoveryCodesAndChallenge(t *testing.T) {
	SetSessionSecret([]byte(strings.Repeat("k", 32)))
	plain, hashes, err := NewRecoveryCodes()
	if err != nil || len(plain) != RecoveryCodeCount || len(hashes) != RecoveryCodeCount {
		t.Fatalf("recovery codes: %v %d", err, len(plain))
	}
	if HashRecoveryCode(strings.ToUpper(strings.ReplaceAll(plain[0], "-", " "))) != hashes[0] {
		t.Fatal("recovery code match must ignore case, dashes and spaces")
	}

	ch := NewChallenge("cg_admin")
	if id, err := ParseChallenge(ch); err != nil || id != "cg_admin" {
		t.Fatalf("challenge round trip: %q %v", id, err)
	}
	if _, err := ParseSessionToken(ch); err == nil {
		t.Fatal("a 2FA challenge must never be accepted as a session token")
	}
	if _, err := ParseChallenge(GenerateSessionToken("cg_admin")); err == nil {
		t.Fatal("a session token must never be accepted as a 2FA challenge")
	}
	if _, err := ParseChallenge(strings.Replace(ch, "cg_admin", "cg_other", 1)); err == nil {
		t.Fatal("tampered challenge must be rejected")
	}
}
