package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"time"
)

// OIDCTxTTL is how long the encrypted transaction cookie stays valid.
const OIDCTxTTL = 10 * time.Minute

// The intent an OIDC transaction was started for; the callback dispatches on it.
const (
	IntentLogin = "login"
	IntentLink  = "link"
	IntentClaim = "claim"
)

// ErrTxInvalid covers every unusable tx cookie. It deliberately does not say
// which check failed.
var ErrTxInvalid = errors.New("oidc transaction invalid")

// OIDCTx is the state kept across the authorize redirect. It lives only in the
// encrypted mpa_oidc_tx cookie.
type OIDCTx struct {
	State           string `json:"state"`
	Nonce           string `json:"nonce"`
	PKCEVerifier    string `json:"pkce_verifier"`
	Intent          string `json:"intent"`
	MemberID        int    `json:"member_id,omitzero"`
	InviteTokenHash string `json:"invite_token_hash,omitempty"`
	// IssuedAt is unix seconds.
	IssuedAt int64 `json:"iat"`
}

// OIDCTxCodec seals the tx cookie with AES-256-GCM. The key is random per
// process unless MPA_OIDC_TX_SECRET is set, so a restart drops in-flight flows.
type OIDCTxCodec struct {
	aead cipher.AEAD
	now  func() time.Time
}

// NewOIDCTxCodec builds a codec. An empty secret means a random key; any other
// secret is folded to 32 bytes with SHA-256.
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

// WithTxClock overrides the wall clock for tests.
func WithTxClock(clock func() time.Time) OIDCTxOption {
	return func(c *OIDCTxCodec) { c.now = clock }
}

// Seal stamps the issue time and returns the base64url cookie value.
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
	// Output is nonce||ciphertext||tag.
	sealed := c.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open reverses Seal and enforces the TTL. Every failure returns ErrTxInvalid.
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
	// A zero iat is invalid; a future iat only shortens the window.
	issued := time.Unix(tx.IssuedAt, 0)
	if tx.IssuedAt == 0 || c.now().Sub(issued) > OIDCTxTTL {
		return OIDCTx{}, ErrTxInvalid
	}
	return tx, nil
}

// pkceChallengeS256 derives the RFC 7636 S256 code challenge.
func pkceChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
