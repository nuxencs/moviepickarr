// Package auth holds the shared crypto primitives for the authentication and
// per-member identity layer: an opaque-token generator and an argon2id
// password wrapper. Both are consumed by the session store, invite/claim flow,
// OIDC relying-party flow, and local login; keeping them in one audited place
// means every credential path reuses the same generation and hashing rules.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// tokenBytes is the raw entropy width of every opaque token. 32 bytes is 256
// bits, which base64url-encodes to 43 unpadded characters and also satisfies
// RFC 7636's 43-128 char range for a PKCE code verifier, so one width covers
// session cookies, invite claim URLs, and OIDC state/nonce/PKCE alike.
const tokenBytes = 32

// Token pairs a freshly minted opaque token with the hash a table stores. Raw
// goes to the caller (session cookie, claim URL, OIDC parameter) and is never
// persisted; Hash is the only representation written to the database, so a
// stolen database row can't be replayed as a token.
type Token struct {
	// Raw is the base64url-unpadded token handed to the client. Never store it.
	Raw string
	// Hash is HashToken(Raw): the lookup key persisted in a token_hash column.
	Hash string
}

// GenerateToken mints a new opaque token from crypto/rand and returns it with
// its storage hash. The error is non-nil only if the system CSPRNG is
// unavailable, which is fatal for the whole auth layer.
func GenerateToken() (Token, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return Token{}, err
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)
	return Token{Raw: raw, Hash: HashToken(raw)}, nil
}

// HashToken returns the storage hash of a raw token so an inbound cookie or
// claim token can be matched against a stored token_hash. It is SHA-256,
// hex-encoded: the token already carries 256 bits of uniform entropy, so there
// is nothing to brute-force and no need for a slow password hash. Same input
// always yields the same hash, so lookups are a plain equality check.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
