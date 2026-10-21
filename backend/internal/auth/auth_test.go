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

	"birrcare/backend/internal/db"
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

func TestSessionTokensAreUniqueAndLegacyFormatStillParses(t *testing.T) {
	SetSessionSecret([]byte(strings.Repeat("k", 32)))
	a, b := GenerateSessionToken("cg_ali"), GenerateSessionToken("cg_ali")
	if a == b {
		t.Fatal("two logins in the same second got the same token; logging out one would revoke the other")
	}
	payload := fmt.Sprintf("cg_ali:%d", time.Now().Unix())
	if id, err := ParseSessionToken(payload + ":" + sign(payload)); err != nil || id != "cg_ali" {
		t.Fatalf("pre-nonce cookie rejected: %v", err)
	}
	parts := strings.Split(a, ":")
	parts[2] = "0000000000000000"
	if _, err := ParseSessionToken(strings.Join(parts, ":")); err == nil {
		t.Fatal("token with a swapped nonce accepted")
	}
}

// Issue #2: the middleware refuses a logged-out token and tokens issued before
// sessions_valid_after (PIN reset, role change, "sign out other devices").
func TestMiddlewareRefusesRevokedSessions(t *testing.T) {
	SetSessionSecret([]byte(strings.Repeat("k", 32)))
	database, err := db.InitDB(t.TempDir() + "/birrcare.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO caregivers (id, name, role, pin_hash) VALUES ('cg_ali', 'Ali', 'caregiver', 'x')`); err != nil {
		t.Fatal(err)
	}
	handler := MiddlewareWithConfig(database, Config{})(RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))
	status := func(token string) int {
		req := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	tokenAt := func(issued int64) string {
		payload := fmt.Sprintf("cg_ali:%d:%016x", issued, issued)
		return payload + ":" + sign(payload)
	}

	now := time.Now().Unix()
	stolen, kept := GenerateSessionToken("cg_ali"), GenerateSessionToken("cg_ali")
	if status(stolen) != http.StatusOK {
		t.Fatal("fresh session rejected")
	}
	if err := RevokeSession(database, stolen); err != nil {
		t.Fatal(err)
	}
	if got := status(stolen); got != http.StatusUnauthorized {
		t.Fatalf("logged-out token: want 401, got %d", got)
	}
	if got := status(kept); got != http.StatusOK {
		t.Fatalf("other device's token after logout: want 200, got %d", got)
	}

	before, after := tokenAt(now-60), tokenAt(now)
	if _, err := database.Exec(`UPDATE caregivers SET sessions_valid_after = ? WHERE id = 'cg_ali'`, now-30); err != nil {
		t.Fatal(err)
	}
	if got := status(before); got != http.StatusUnauthorized {
		t.Fatalf("token issued before PIN reset: want 401, got %d", got)
	}
	if got := status(after); got != http.StatusOK {
		t.Fatalf("token issued after PIN reset: want 200, got %d", got)
	}
}

// Issue #17: on a shared tablet a session ends after 20 minutes without use;
// a personal phone keeps its 30-day session. Background GETs (polling, the live
// stream) don't count as use.
func TestSharedSessionIdlesOut(t *testing.T) {
	SetSessionSecret([]byte(strings.Repeat("k", 32)))
	database, err := db.InitDB(t.TempDir() + "/birrcare.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO caregivers (id, name, role, pin_hash) VALUES ('cg_ali', 'Ali', 'caregiver', 'x')`); err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	Now = func() time.Time { return clock }
	t.Cleanup(func() { Now = time.Now })

	// Even with a Cloudflare login available, an idle shared session must not
	// fall back to it (on a tablet that is one sibling's email).
	handler := MiddlewareWithConfig(database, Config{DevFallback: true})(RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))
	status := func(method, token string) int {
		req := httptest.NewRequest(method, "/api/v1/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	shared, personal := GenerateSessionTokenMode("cg_ali", true), GenerateSessionTokenMode("cg_ali", false)
	if s, err := ParseSession(shared); err != nil || !s.Shared || s.CaregiverID != "cg_ali" {
		t.Fatalf("shared token: %+v %v", s, err)
	}
	if s, _ := ParseSession(personal); s.Shared {
		t.Fatal("personal token parsed as shared")
	}
	forged := strings.Split(personal, ":")
	if _, err := ParseSession(strings.Join(append(forged[:3:3], "s", forged[3]), ":")); err == nil {
		t.Fatal("a personal token was turned into a shared one without the key")
	}

	// Use at minute 15 (a write) restarts the 20 minutes.
	clock = clock.Add(15 * time.Minute)
	if got := status("POST", shared); got != http.StatusOK {
		t.Fatalf("shared at 15 min: want 200, got %d", got)
	}
	// Only GETs (background polling) from then on.
	clock = clock.Add(19 * time.Minute)
	if got := status("GET", shared); got != http.StatusOK {
		t.Fatalf("shared 19 min after last use: want 200, got %d", got)
	}
	clock = clock.Add(2 * time.Minute) // 21 min since the last write
	if got := status("GET", shared); got != http.StatusUnauthorized {
		t.Fatalf("shared token idle 21 min: want 401, got %d", got)
	}
	if got := status("POST", shared); got != http.StatusUnauthorized {
		t.Fatal("an idle shared session came back to life")
	}
	if got := status("GET", personal); got != http.StatusOK {
		t.Fatalf("personal token idle 36 min: want 200, got %d", got)
	}

	// A fresh shared login that is never used idles out from its issue time.
	fresh := GenerateSessionTokenMode("cg_ali", true)
	clock = clock.Add(21 * time.Minute)
	if got := status("GET", fresh); got != http.StatusUnauthorized {
		t.Fatalf("unused shared token after 21 min: want 401, got %d", got)
	}
}
