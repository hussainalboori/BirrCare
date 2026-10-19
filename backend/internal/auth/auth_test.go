package auth

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionTokenRejectsLegacyDefaultSecret(t *testing.T) {
	SetSessionSecret([]byte(strings.Repeat("k", 32)))

	// Forge a token with the old hard-coded secret that shipped in the repo.
	payload := fmt.Sprintf("cg_hussain:%d", time.Now().Unix())
	mac := hmac.New(sha256.New, []byte("birrcare-super-secret-family-recovery-key-2026"))
	mac.Write([]byte(payload))
	forged := payload + ":" + hex.EncodeToString(mac.Sum(nil))
	if _, err := ParseSessionToken(forged); err == nil {
		t.Fatal("token signed with the public legacy secret was accepted")
	}

	good := GenerateSessionToken("cg_ali")
	if id, err := ParseSessionToken(good); err != nil || id != "cg_ali" {
		t.Fatalf("valid token rejected: %v", err)
	}
	tampered := strings.Replace(good, "cg_ali", "cg_hussain", 1)
	if _, err := ParseSessionToken(tampered); err == nil {
		t.Fatal("tampered token accepted")
	}
}

func TestSessionTokenExpires(t *testing.T) {
	SetSessionSecret([]byte(strings.Repeat("k", 32)))
	old := time.Now().Add(-SessionTTL - time.Hour).Unix()
	payload := fmt.Sprintf("cg_ali:%d", old)
	if _, err := ParseSessionToken(payload + ":" + sign(payload)); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestPINLimiterLockout(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := NewPINLimiter()
	l.Now = func() time.Time { return now }

	for i := 0; i < l.MaxFailures; i++ {
		ok, _ := l.Begin("cg_ali")
		if !ok {
			t.Fatalf("attempt %d blocked too early", i+1)
		}
		l.Done("cg_ali", false)
	}
	if ok, retry := l.Begin("cg_ali"); ok || retry <= 0 {
		t.Fatalf("expected lockout after %d failures", l.MaxFailures)
	}
	// Other caregivers unaffected
	if ok, _ := l.Begin("cg_fatima"); !ok {
		t.Fatal("lockout leaked to another caregiver")
	}
	l.Done("cg_fatima", true)

	// After lockout: exactly one probe, failure doubles the lockout.
	now = now.Add(l.BaseLockout + time.Second)
	if ok, _ := l.Begin("cg_ali"); !ok {
		t.Fatal("probe after lockout should be allowed")
	}
	if ok, _ := l.Begin("cg_ali"); ok {
		t.Fatal("second concurrent probe after lockout should be blocked")
	}
	l.Done("cg_ali", false)
	if _, retry := l.Begin("cg_ali"); retry < 2*l.BaseLockout-time.Second {
		t.Fatalf("expected exponential back-off, got %v", retry)
	}

	// Success resets.
	now = now.Add(l.MaxLockout)
	if ok, _ := l.Begin("cg_ali"); !ok {
		t.Fatal("expected allowed")
	}
	l.Done("cg_ali", true)
	if ok, _ := l.Begin("cg_ali"); !ok {
		t.Fatal("success should reset the counter")
	}
}

func TestPINLimiterParallelBurst(t *testing.T) {
	l := NewPINLimiter()
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := l.Begin("cg_ali"); ok {
				allowed.Add(1)
				time.Sleep(time.Millisecond)
				l.Done("cg_ali", false)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got > int32(l.MaxFailures) {
		t.Fatalf("parallel burst got %d guesses, budget is %d", got, l.MaxFailures)
	}
}

// --- Cloudflare Access JWT ---

type testIssuer struct {
	key    *rsa.PrivateKey
	kid    string
	server *httptest.Server
	team   string
}

func newTestIssuer(t *testing.T) *testIssuer {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ti := &testIssuer{key: k, kid: "kid-1"}
	ti.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"keys": []map[string]string{{
			"kid": ti.kid, "kty": "RSA", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(k.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes()),
		}}})
	}))
	t.Cleanup(ti.server.Close)
	ti.team = "family.cloudflareaccess.com"
	return ti
}

func (ti *testIssuer) verifier() *CFAccessVerifier {
	v := NewCFAccessVerifier(ti.team, "aud-123")
	v.CertsURL = ti.server.URL
	return v
}

func (ti *testIssuer) sign(t *testing.T, alg string, claims map[string]interface{}) string {
	t.Helper()
	h, _ := json.Marshal(map[string]string{"alg": alg, "kid": ti.kid, "typ": "JWT"})
	p, _ := json.Marshal(claims)
	unsigned := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p)
	d := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, ti.key, crypto.SHA256, d[:])
	if err != nil {
		t.Fatal(err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestCFAccessVerify(t *testing.T) {
	ti := newTestIssuer(t)
	v := ti.verifier()
	base := func() map[string]interface{} {
		return map[string]interface{}{
			"aud":   []string{"aud-123"},
			"email": "ali@family.local",
			"iss":   "https://" + ti.team,
			"exp":   time.Now().Add(time.Hour).Unix(),
			"nbf":   time.Now().Add(-time.Minute).Unix(),
		}
	}

	email, err := v.Verify(context.Background(), ti.sign(t, "RS256", base()))
	if err != nil || email != "ali@family.local" {
		t.Fatalf("valid jwt rejected: %v", err)
	}

	bad := map[string]func(map[string]interface{}){
		"wrong audience": func(c map[string]interface{}) { c["aud"] = "other-app" },
		"wrong issuer":   func(c map[string]interface{}) { c["iss"] = "https://evil.cloudflareaccess.com" },
		"expired":        func(c map[string]interface{}) { c["exp"] = time.Now().Add(-time.Hour).Unix() },
		"no email":       func(c map[string]interface{}) { delete(c, "email") },
	}
	for name, mutate := range bad {
		c := base()
		mutate(c)
		if _, err := v.Verify(context.Background(), ti.sign(t, "RS256", c)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// alg=none / tampered payload
	tok := ti.sign(t, "RS256", base())
	parts := strings.Split(tok, ".")
	evil, _ := json.Marshal(map[string]interface{}{"aud": "aud-123", "email": "hussain@family.local", "iss": "https://" + ti.team, "exp": time.Now().Add(time.Hour).Unix()})
	if _, err := v.Verify(context.Background(), parts[0]+"."+base64.RawURLEncoding.EncodeToString(evil)+"."+parts[2]); err == nil {
		t.Error("tampered payload accepted")
	}
	none := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","kid":"kid-1"}`)) + "." + parts[1] + "."
	if _, err := v.Verify(context.Background(), none); err == nil {
		t.Error("alg=none accepted")
	}

	// Different key, same kid
	other := newTestIssuer(t)
	other.team = ti.team
	if _, err := v.Verify(context.Background(), other.sign(t, "RS256", base())); err == nil {
		t.Error("jwt signed by a foreign key accepted")
	}
}
