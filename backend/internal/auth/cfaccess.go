package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CFAccessVerifier validates Cloudflare Access JWTs (Cf-Access-Jwt-Assertion)
// against the team's public JWKS at https://<team>/cdn-cgi/access/certs.
// See https://developers.cloudflare.com/cloudflare-one/identity/authorization-cookie/validating-json/
type CFAccessVerifier struct {
	Issuer   string // "https://<team>.cloudflareaccess.com"
	Audience string // Access application AUD tag
	CertsURL string
	Client   *http.Client
	Now      func() time.Time

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchMu   sync.Mutex
	lastFetch time.Time
}

const (
	jwksMinRefresh = 30 * time.Second
	jwtLeeway      = 60 * time.Second
)

func NewCFAccessVerifier(teamDomain, audience string) *CFAccessVerifier {
	team := strings.TrimRight(strings.TrimSpace(teamDomain), "/")
	team = strings.TrimPrefix(strings.TrimPrefix(team, "https://"), "http://")
	issuer := "https://" + team
	return &CFAccessVerifier{
		Issuer:   issuer,
		Audience: strings.TrimSpace(audience),
		CertsURL: issuer + "/cdn-cgi/access/certs",
		Client:   &http.Client{Timeout: 5 * time.Second},
		Now:      time.Now,
		keys:     map[string]*rsa.PublicKey{},
	}
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

type cfClaims struct {
	Aud   json.RawMessage `json:"aud"`
	Email string          `json:"email"`
	Exp   int64           `json:"exp"`
	Nbf   int64           `json:"nbf"`
	Iss   string          `json:"iss"`
}

func (c cfClaims) hasAudience(want string) bool {
	var one string
	if json.Unmarshal(c.Aud, &one) == nil {
		return one == want
	}
	var many []string
	if json.Unmarshal(c.Aud, &many) == nil {
		for _, a := range many {
			if a == want {
				return true
			}
		}
	}
	return false
}

// Verify checks signature (RS256 only), issuer, audience and expiry and returns
// the authenticated email.
func (v *CFAccessVerifier) Verify(ctx context.Context, token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errors.New("malformed jwt")
	}
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", errors.New("malformed jwt header")
	}
	var hdr jwtHeader
	if err := json.Unmarshal(hb, &hdr); err != nil {
		return "", errors.New("malformed jwt header")
	}
	if hdr.Alg != "RS256" {
		return "", fmt.Errorf("unsupported jwt alg %q", hdr.Alg)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", errors.New("malformed jwt signature")
	}

	key, err := v.key(ctx, hdr.Kid)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return "", errors.New("invalid jwt signature")
	}

	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("malformed jwt payload")
	}
	var claims cfClaims
	if err := json.Unmarshal(pb, &claims); err != nil {
		return "", errors.New("malformed jwt payload")
	}
	now := v.Now()
	if claims.Iss != v.Issuer {
		return "", fmt.Errorf("unexpected issuer %q", claims.Iss)
	}
	if !claims.hasAudience(v.Audience) {
		return "", errors.New("audience mismatch")
	}
	if claims.Exp == 0 || now.After(time.Unix(claims.Exp, 0).Add(jwtLeeway)) {
		return "", errors.New("jwt expired")
	}
	if claims.Nbf != 0 && now.Add(jwtLeeway).Before(time.Unix(claims.Nbf, 0)) {
		return "", errors.New("jwt not yet valid")
	}
	if claims.Email == "" {
		return "", errors.New("jwt has no email claim (service token?)")
	}
	return claims.Email, nil
}

func (v *CFAccessVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	k := v.keys[kid]
	v.mu.RUnlock()
	if k != nil {
		return k, nil
	}

	// Unknown kid: refresh JWKS (keys rotate), rate limited to avoid hammering CF.
	v.fetchMu.Lock()
	defer v.fetchMu.Unlock()
	v.mu.RLock()
	k = v.keys[kid]
	v.mu.RUnlock()
	if k != nil {
		return k, nil
	}
	if !v.lastFetch.IsZero() && v.Now().Sub(v.lastFetch) < jwksMinRefresh {
		return nil, errors.New("unknown jwt kid")
	}
	if err := v.refresh(ctx); err != nil {
		return nil, err
	}
	v.mu.RLock()
	k = v.keys[kid]
	v.mu.RUnlock()
	if k == nil {
		return nil, errors.New("unknown jwt kid")
	}
	return k, nil
}

type jwks struct {
	Keys []struct {
		Kid string `json:"kid"`
		Kty string `json:"kty"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

func (v *CFAccessVerifier) refresh(ctx context.Context) error {
	v.lastFetch = v.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.CertsURL, nil)
	if err != nil {
		return err
	}
	resp, err := v.Client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching cloudflare access certs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching cloudflare access certs: status %d", resp.StatusCode)
	}
	var set jwks
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&set); err != nil {
		return fmt.Errorf("decoding cloudflare access certs: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		nb, err1 := base64.RawURLEncoding.DecodeString(k.N)
		eb, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil || len(eb) == 0 || len(eb) > 4 {
			continue
		}
		e := 0
		for _, b := range eb {
			e = e<<8 | int(b)
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}
	}
	if len(keys) == 0 {
		return errors.New("cloudflare access certs contained no usable RSA keys")
	}
	v.mu.Lock()
	v.keys = keys
	v.mu.Unlock()
	return nil
}
