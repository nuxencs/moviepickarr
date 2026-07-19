package auth

import (
	"context"
	"errors"
	"os"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// oidcScopes is the fixed scope set the app requests: openid for the ID token,
// profile+email for the preferred_username/email snapshots. Hardcoded, not
// operator config (the spec pins them), and deliberately excludes offline_access
// so no refresh token is ever issued.
var oidcScopes = []string{oidc.ScopeOpenID, "profile", "email"}

// ErrOIDCNoIDToken is returned when the token response carries no id_token: the
// provider is not behaving as an OIDC IdP for our request. The callback maps it,
// like every verification failure, to the oidc_failed bucket.
var ErrOIDCNoIDToken = errors.New("token response has no id_token")

// ErrOIDCNonceMismatch is returned when the verified ID token's nonce does not
// match the one stashed in the tx cookie: a replay or mix-up. Maps to oidc_failed.
var ErrOIDCNonceMismatch = errors.New("oidc nonce mismatch")

// OIDCConfig is the relying-party's provider configuration, read from the env
// quartet. All four fields are required; enablement is presence-derived, so a
// partial set means OIDC stays off.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// OIDCConfigFromEnv reads MPA_OIDC_ISSUER / _CLIENT_ID / _CLIENT_SECRET /
// _REDIRECT_URL and reports the config plus whether OIDC is enabled: true iff
// all four are set. A partial quartet is treated as off, not an error, so a
// half-configured deploy simply has no SSO rather than failing boot.
func OIDCConfigFromEnv() (OIDCConfig, bool) {
	cfg := OIDCConfig{
		Issuer:       os.Getenv("MPA_OIDC_ISSUER"),
		ClientID:     os.Getenv("MPA_OIDC_CLIENT_ID"),
		ClientSecret: os.Getenv("MPA_OIDC_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("MPA_OIDC_REDIRECT_URL"),
	}
	enabled := cfg.Issuer != "" && cfg.ClientID != "" && cfg.ClientSecret != "" && cfg.RedirectURL != ""
	return cfg, enabled
}

// OIDCClaims is exactly what the app takes from the verified ID token: the match
// key (Issuer, Subject) and the two informational snapshots. Nothing else from
// the token is trusted, and there is no userinfo call (email_verified included,
// which is ignored by design).
type OIDCClaims struct {
	Issuer            string
	Subject           string
	Email             *string
	PreferredUsername *string
}

// RelyingParty is the deep module over the OIDC relying-party protocol: it owns
// discovery (done once at construction), the authorize-URL builder with
// PKCE/state/nonce, the code exchange, and ID-token verification. Everything
// network-facing and go-oidc-specific lives here; the HTTP layer and the linker
// never import go-oidc or oauth2.
type RelyingParty struct {
	oauth2   oauth2.Config
	verifier *oidc.IDTokenVerifier
	issuer   string
}

// NewRelyingParty runs OIDC discovery against the issuer (one network round trip)
// and builds the oauth2 config and ID-token verifier. A discovery failure means
// the provider is unreachable or misconfigured; the caller logs it and leaves
// OIDC disabled rather than failing boot.
func NewRelyingParty(ctx context.Context, cfg OIDCConfig) (*RelyingParty, error) {
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}
	return &RelyingParty{
		oauth2: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       oidcScopes,
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		issuer:   cfg.Issuer,
	}, nil
}

// AuthCodeURL builds the provider authorize URL for a transaction, carrying the
// state, the nonce (as an OIDC nonce param), and the S256 PKCE challenge derived
// from the tx verifier. The returned URL is where initiation 302s the browser.
func (rp *RelyingParty) AuthCodeURL(tx OIDCTx) string {
	return rp.oauth2.AuthCodeURL(
		tx.State,
		oidc.Nonce(tx.Nonce),
		oauth2.SetAuthURLParam("code_challenge", pkceChallengeS256(tx.PKCEVerifier)),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

// Exchange completes the callback: it trades the authorization code for tokens
// (sending the PKCE verifier and the confidential client secret), pulls the
// id_token out of the response, verifies it (signature, iss, aud, exp/nbf via
// go-oidc), then compares the nonce against the tx. On success it returns the
// claims extracted from the ID token alone. Any failure is a plain error the
// caller folds into the oidc_failed bucket.
func (rp *RelyingParty) Exchange(ctx context.Context, code string, tx OIDCTx) (OIDCClaims, error) {
	token, err := rp.oauth2.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", tx.PKCEVerifier))
	if err != nil {
		return OIDCClaims{}, err
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return OIDCClaims{}, ErrOIDCNoIDToken
	}

	idToken, err := rp.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return OIDCClaims{}, err
	}
	if idToken.Nonce != tx.Nonce {
		return OIDCClaims{}, ErrOIDCNonceMismatch
	}

	var extra struct {
		Email             *string `json:"email"`
		PreferredUsername *string `json:"preferred_username"`
	}
	if err := idToken.Claims(&extra); err != nil {
		return OIDCClaims{}, err
	}

	return OIDCClaims{
		Issuer:            idToken.Issuer,
		Subject:           idToken.Subject,
		Email:             extra.Email,
		PreferredUsername: extra.PreferredUsername,
	}, nil
}

// NewOIDCTx mints a fresh transaction for an intent: state, nonce, and PKCE
// verifier each come from the shared 32-byte opaque-token helper (its 43-char
// base64url output is a valid RFC 7636 verifier). The caller fills MemberID /
// InviteTokenHash for link / claim before sealing.
func NewOIDCTx(intent string) (OIDCTx, error) {
	state, err := GenerateToken()
	if err != nil {
		return OIDCTx{}, err
	}
	nonce, err := GenerateToken()
	if err != nil {
		return OIDCTx{}, err
	}
	verifier, err := GenerateToken()
	if err != nil {
		return OIDCTx{}, err
	}
	return OIDCTx{
		State:        state.Raw,
		Nonce:        nonce.Raw,
		PKCEVerifier: verifier.Raw,
		Intent:       intent,
	}, nil
}
