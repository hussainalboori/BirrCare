package auth

import (
	"strings"
	"testing"
	"time"
)

// Issue #4: failures from one client must not block another client's correct PIN.
func TestPINLimiterPerDeviceKeys(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := NewPINLimiter()
	l.Now = func() time.Time { return now }
	fail := func(key string) {
		t.Helper()
		ok, _ := l.Begin(key)
		if !ok {
			t.Fatalf("%s: throttled too early", key)
		}
		l.Done(key, false)
	}

	attacker := PINLimitKey("cg_ali", "")
	for i := 0; i < l.MaxFailures; i++ {
		fail(attacker)
	}
	if ok, _ := l.Begin(attacker); ok {
		t.Fatal("untrusted clients must be locked after MaxFailures")
	}

	// Ali's own phone and tablet are unaffected, and independent of each other.
	phone, tablet := PINLimitKey("cg_ali", "phone"), PINLimitKey("cg_ali", "tablet")
	for i := 0; i < l.MaxFailures; i++ {
		fail(phone)
	}
	if ok, _ := l.Begin(phone); ok {
		t.Fatal("a trusted device still has its own limit")
	}
	if ok, _ := l.Begin(tablet); !ok {
		t.Fatal("another trusted device was locked by someone else's failures")
	}
	l.Done(tablet, true)

	// Keys never collide across caregivers or between trusted and untrusted.
	if PINLimitKey("cg_ali", "") == PINLimitKey("cg_fatima", "") || PINLimitKey("cg_ali", "x") == PINLimitKey("cg_fatima", "x") {
		t.Fatal("keys collide across caregivers")
	}
	if ok, _ := l.Begin(PINLimitKey("cg_fatima", "")); !ok {
		t.Fatal("another caregiver was locked")
	}
}

func TestDeviceToken(t *testing.T) {
	SetSessionSecret([]byte(strings.Repeat("k", 32)))
	tok, id := NewDeviceToken()
	if got, ok := ParseDeviceToken(tok); !ok || got != id {
		t.Fatalf("valid device token rejected: %q %v", got, ok)
	}
	other, _ := NewDeviceToken()
	if _, ok := ParseDeviceToken(id + ":" + strings.SplitN(other, ":", 2)[1]); ok {
		t.Fatal("device token with another token's signature accepted")
	}
	if _, ok := ParseDeviceToken(GenerateSessionToken("cg_ali")); ok {
		t.Fatal("a session token must not parse as a device token")
	}
	if _, err := ParseSessionToken(tok); err == nil {
		t.Fatal("a device token must not parse as a session token")
	}
}
