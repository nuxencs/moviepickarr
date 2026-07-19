package server

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeIdP is an in-process OpenID provider standing up exactly the three
// endpoints go-oidc touches: discovery, JWKS, and the token endpoint. It signs
// real RS256 ID tokens with a per-test RSA key, so the relying-party path
// (discovery, JWKS fetch, signature / iss / aud / exp / nonce verification) runs
// end to end against a controllable issuer. The authorization endpoint is never
// hit; tests replay the code + state the initiation redirect would have carried.
type fakeIdP struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	kid    string

	mu          sync.Mutex
	nextIDToken string
}

// newFakeIdP generates a signing key and starts the provider. It is torn down
// with the test.
func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	idp := &fakeIdP{key: key, kid: "test-key"}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                 idp.server.URL,
			"authorization_endpoint": idp.server.URL + "/auth",
			"token_endpoint":         idp.server.URL + "/token",
			"jwks_uri":               idp.server.URL + "/keys",
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		pub := idp.key.Public().(*rsa.PublicKey)
		writeJSON(w, map[string]any{"keys": []map[string]any{{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": idp.kid,
			"n":   b64url(pub.N.Bytes()),
			"e":   b64url(big.NewInt(int64(pub.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		idp.mu.Lock()
		idToken := idp.nextIDToken
		idp.mu.Unlock()
		writeJSON(w, map[string]any{
			"access_token": "fake-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     idToken,
		})
	})

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

// issuer is the provider's issuer identifier (its base URL): the value that
// lands in a verified ID token's iss claim and thus in an oidc_identities row.
func (idp *fakeIdP) issuer() string { return idp.server.URL }

// idTokenClaims is the controllable subset of an ID token a test sets before
// replaying a callback. Aud must match the relying party's client id; Nonce must
// match the one the initiation redirect carried.
type idTokenClaims struct {
	Sub               string
	Aud               string
	Nonce             string
	Email             string
	PreferredUsername string
	// ExpOffset lets a test mint an already-expired token; zero means +1h.
	ExpOffset time.Duration
}

// setIDToken signs an ID token with the given claims and arms the token endpoint
// to return it on the next exchange.
func (idp *fakeIdP) setIDToken(t *testing.T, c idTokenClaims) {
	t.Helper()
	exp := time.Hour
	if c.ExpOffset != 0 {
		exp = c.ExpOffset
	}
	claims := map[string]any{
		"iss":   idp.issuer(),
		"sub":   c.Sub,
		"aud":   c.Aud,
		"exp":   time.Now().Add(exp).Unix(),
		"iat":   time.Now().Unix(),
		"nonce": c.Nonce,
	}
	if c.Email != "" {
		claims["email"] = c.Email
	}
	if c.PreferredUsername != "" {
		claims["preferred_username"] = c.PreferredUsername
	}

	idp.mu.Lock()
	idp.nextIDToken = idp.signJWT(t, claims)
	idp.mu.Unlock()
}

// signJWT builds and RS256-signs a compact JWT for the given claims.
func (idp *fakeIdP) signJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": idp.kid}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshal jwt header: %v", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal jwt claims: %v", err)
	}
	signingInput := b64url(headerJSON) + "." + b64url(claimsJSON)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, idp.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return signingInput + "." + b64url(sig)
}

func b64url(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
