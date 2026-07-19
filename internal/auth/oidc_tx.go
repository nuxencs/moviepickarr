package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

// OIDCTxTTL is how long the encrypted transaction cookie stays valid: the whole
// authorize → callback round trip has to finish inside this window. Short by
// design (the user is mid-redirect at the provider), so a stale or replayed tx
// cookie is rejected as expired.
const OIDCTxTTL = 10 * time.Minute

// The intent an OIDC transaction was started for. The single callback dispatches
// on it: login has no session yet, link re-checks the session member, claim
// carries the invite token hash.
const (
	IntentLogin = "login"
	IntentLink  = "link"
	IntentClaim = "claim"
)

// ErrTxInvalid is the single sentinel for every unusable transaction cookie the
// callback should turn into oidc_expired: no cookie, a tampered/forged
// ciphertext (AEAD auth failure), or a payload past its TTL. It deliberately
// does not distinguish the cases; the user just restarts the flow.
var ErrTxInvalid = errors.New("oidc transaction invalid")

// OIDCTx is the state stashed across the authorize redirect: the CSRF/replay
// guards (state, nonce, PKCE verifier) plus the intent and its dispatch inputs.
// It lives only in the encrypted mpa_oidc_tx cookie, never in the database, and
// is cleared at the callback.
type OIDCTx struct {
	State           string `json:"state"`
	Nonce           string `json:"nonce"`
	PKCEVerifier    string `json:"pkce_verifier"`
	Intent          string `json:"intent"`
	MemberID        int    `json:"member_id,omitempty"`
	InviteTokenHash string `json:"invite_token_hash,omitempty"`
	// IssuedAt is the seal timestamp (unix seconds) the TTL is measured from.
	IssuedAt int64 `json:"iat"`
}

// OIDCTxCodec seals and opens the transaction cookie with AES-256-GCM. The key
// is the one symmetric secret in the design: ephemeral random 32 bytes by
// default (so a restart invalidates in-flight flows, which is fine at this TTL),
// or derived from MPA_OIDC_TX_SECRET when an operator wants tx cookies to
// survive restarts and multiple instances to share them.
type OIDCTxCodec struct {
	aead cipher.AEAD
	now  func() time.Time
}

// NewOIDCTxCodec builds a codec over a 32-byte key. An empty secret means
// "generate an ephemeral random key"; a non-empty secret is folded to exactly 32
// bytes with SHA-256 so any-length operator input is accepted. The error is
// non-nil only if the system CSPRNG or the AES/GCM construction is unavailable,
// which is fatal for the OIDC layer.
func NewOIDCTxCodec(secret string, opts ...OIDCTxOption) (*OIDCTxCodec, error) {
	var key [32]byte
	if secret == "" {
		if _, err := rand.Read(key[:]); err != nil {
			return nil, err
		}
	} else {
		key = sha256.Sum256([]byte(secret))
	}

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	c := &OIDCTxCodec{aead: aead, now: time.Now}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// OIDCTxOption configures an OIDCTxCodec at construction.
type OIDCTxOption func(*OIDCTxCodec)

// WithTxClock overrides the wall clock so tests drive tx expiry deterministically.
func WithTxClock(clock func() time.Time) OIDCTxOption {
	return func(c *OIDCTxCodec) { c.now = clock }
}

// Seal stamps the issue time, JSON-encodes the tx, and returns a base64url
// (unpadded) AES-256-GCM ciphertext with the random 12-byte nonce prepended, so
// the whole opaque string is the cookie value.
func (c *OIDCTxCodec) Seal(tx OIDCTx) (string, error) {
	tx.IssuedAt = c.now().Unix()
	plaintext, err := json.Marshal(tx)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	// Seal appends the ciphertext+tag to the nonce prefix, so the output is
	// nonce||ciphertext||tag in one slice.
	sealed := c.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open reverses Seal: decode, split off the nonce, authenticate-and-decrypt,
// then enforce the TTL. Any failure (bad base64, too short, AEAD auth failure,
// malformed JSON, or a payload older than OIDCTxTTL) collapses to ErrTxInvalid,
// so a tampered or stale cookie is one uniform "expired" outcome.
func (c *OIDCTxCodec) Open(cookie string) (OIDCTx, error) {
	if cookie == "" {
		return OIDCTx{}, ErrTxInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(cookie)
	if err != nil {
		return OIDCTx{}, ErrTxInvalid
	}
	nonceSize := c.aead.NonceSize()
	if len(raw) < nonceSize {
		return OIDCTx{}, ErrTxInvalid
	}
	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return OIDCTx{}, ErrTxInvalid
	}

	var tx OIDCTx
	if err := json.Unmarshal(plaintext, &tx); err != nil {
		return OIDCTx{}, ErrTxInvalid
	}
	// TTL: reject anything issued more than OIDCTxTTL ago (or with a missing/zero
	// issue time). A future-dated iat is harmless: it only shortens the window.
	issued := time.Unix(tx.IssuedAt, 0)
	if tx.IssuedAt == 0 || c.now().Sub(issued) > OIDCTxTTL {
		return OIDCTx{}, ErrTxInvalid
	}
	return tx, nil
}

// pkceChallengeS256 derives the RFC 7636 S256 code challenge from a verifier:
// base64url(sha256(verifier)), unpadded. The verifier itself is a 32-byte opaque
// token (GenerateToken), whose 43-char base64url form is a valid verifier.
func pkceChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
