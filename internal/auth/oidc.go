package auth

import (
	"context"
	"errors"
	"os"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// oidcScopes is fixed. It omits offline_access so no refresh token is issued.
var oidcScopes = []string{oidc.ScopeOpenID, "profile", "email"}

// ErrOIDCNoIDToken means the token response carried no id_token.
var ErrOIDCNoIDToken = errors.New("token response has no id_token")

// ErrOIDCNonceMismatch means the ID token nonce does not match the tx cookie.
var ErrOIDCNonceMismatch = errors.New("oidc nonce mismatch")

// OIDCConfig is the relying party's provider configuration.
type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// OIDCConfigFromEnv reads the MPA_OIDC_* quartet. OIDC is enabled only when all
// four are set; a partial set means no SSO, not a boot failure.
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

// OIDCClaims is all the app trusts from the verified ID token. There is no
// userinfo call, and email_verified is ignored by design.
type OIDCClaims struct {
	Issuer            string
	Subject           string
	Email             *string
	PreferredUsername *string
}

// RelyingParty owns the OIDC protocol, so callers never import go-oidc or oauth2.
type RelyingParty struct {
	oauth2   oauth2.Config
	verifier *oidc.IDTokenVerifier
	issuer   string
}

// NewRelyingParty runs OIDC discovery (one network round trip). The caller
// leaves OIDC disabled on failure rather than failing boot.
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

// AuthCodeURL builds the provider authorize URL with state, nonce, and the S256
// PKCE challenge.
func (rp *RelyingParty) AuthCodeURL(tx OIDCTx) string {
	return rp.oauth2.AuthCodeURL(
		tx.State,
		oidc.Nonce(tx.Nonce),
		oauth2.SetAuthURLParam("code_challenge", pkceChallengeS256(tx.PKCEVerifier)),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

// Exchange trades the code for tokens, verifies the ID token and its nonce, and
// returns claims from the ID token alone.
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

// NewOIDCTx mints a transaction for an intent. A GenerateToken value is a valid
// RFC 7636 PKCE verifier.
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
