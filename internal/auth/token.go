// Package auth holds authentication: sessions, local logins, invites, OIDC,
// and the shared token and password primitives.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// tokenBytes is 256 bits: 43 base64url chars, also a valid RFC 7636 PKCE
// verifier, so one width covers every opaque token.
const tokenBytes = 32

// publicIDBytes is the entropy width for non-secret external handles.
const publicIDBytes = 16

// Token pairs an opaque token with its storage hash. Only Hash is persisted, so
// a stolen database row cannot be replayed as a token.
type Token struct {
	Raw  string
	Hash string
}

// GenerateToken mints an opaque token from crypto/rand with its storage hash.
func GenerateToken() (Token, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return Token{}, err
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)
	return Token{Raw: raw, Hash: HashToken(raw)}, nil
}

// GeneratePublicID returns a URL-safe handle for an object whose row id must
// stay private. It is not a secret, so it has no paired hash.
func GeneratePublicID() (string, error) {
	buf := make([]byte, publicIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashToken is SHA-256 hex. The token already has 256 bits of entropy, so a
// slow password hash would add nothing.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
