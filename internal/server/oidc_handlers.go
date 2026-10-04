package server

import (
	"errors"
	"time"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

// oidcTxCookieName names the AEAD-encrypted transaction cookie that lives from
// initiation to callback.
const oidcTxCookieName = "mpa_oidc_tx"

// SPA routes the callback redirects to. OIDC is redirect-only (302 with ?error=
// or ?linked=), unlike local login's XHR 204/401.
const (
	oidcHomeRedirect  = "/"
	oidcLoginRedirect = "/login"
	oidcLinkRedirect  = "/settings"
)

// Public ?error= buckets the frontend maps to copy. They never carry provider
// detail or tokens.
const (
	errOIDCDenied         = "oidc_denied"
	errOIDCExpired        = "oidc_expired"
	errOIDCUnlinked       = "oidc_unlinked"
	errOIDCFailed         = "oidc_failed"
	errOIDCLinkConflict   = "oidc_link_conflict"
	errOIDCSessionExpired = "oidc_session_expired"
)

// setOIDCTxCookie writes the tx cookie with Max-Age matched to the tx TTL.
func (h *handler) setOIDCTxCookie(c *fiber.Ctx, value string) {
	c.Cookie(&fiber.Cookie{
		Name:     oidcTxCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   int(auth.OIDCTxTTL / time.Second),
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteLaxMode,
		Secure:   isHTTPS(c),
	})
}

// clearOIDCTxCookie expires the tx cookie. Attributes must match
// setOIDCTxCookie, or the browser keeps it.
func (h *handler) clearOIDCTxCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     oidcTxCookieName,
		Value:    "",
		Path:     "/",
		Expires:  cookieEpoch,
		MaxAge:   -1,
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteLaxMode,
		Secure:   isHTTPS(c),
	})
}

// redirectError is the single unhappy-path exit for the OIDC surface.
func redirectError(c *fiber.Ctx, dest, bucket string) error {
	return c.Redirect(dest+"?error="+bucket, fiber.StatusFound)
}

// destForIntent picks the error landing: settings for link, login otherwise.
func destForIntent(intent string) string {
	if intent == auth.IntentLink {
		return oidcLinkRedirect
	}
	return oidcLoginRedirect
}

// beginOIDC seals tx into the cookie and redirects to the provider. Failures
// redirect, not JSON, because initiation is a top-level navigation.
func (h *handler) beginOIDC(c *fiber.Ctx, tx auth.OIDCTx, dest string) error {
	sealed, err := h.oidcTx.Seal(tx)
	if err != nil {
		h.reqLog(c).Error().Err(err).Str("intent", tx.Intent).Msg("sealing oidc tx cookie failed")
		return redirectError(c, dest, errOIDCFailed)
	}
	h.setOIDCTxCookie(c, sealed)
	return c.Redirect(h.oidc.AuthCodeURL(tx), fiber.StatusFound)
}

// handleOIDCLogin starts the unauthenticated login intent.
func (h *handler) handleOIDCLogin(c *fiber.Ctx) error {
	tx, err := auth.NewOIDCTx(auth.IntentLogin)
	if err != nil {
		h.reqLog(c).Error().Err(err).Msg("minting oidc login tx failed")
		return redirectError(c, oidcLoginRedirect, errOIDCFailed)
	}
	return h.beginOIDC(c, tx, oidcLoginRedirect)
}

// handleOIDCLink starts the link intent. The tx carries the session member so
// the callback can check the session still matches.
func (h *handler) handleOIDCLink(c *fiber.Ctx) error {
	tx, err := auth.NewOIDCTx(auth.IntentLink)
	if err != nil {
		h.reqLog(c).Error().Err(err).Msg("minting oidc link tx failed")
		return redirectError(c, oidcLinkRedirect, errOIDCFailed)
	}
	tx.MemberID = actorMemberID(c)
	return h.beginOIDC(c, tx, oidcLinkRedirect)
}

// handleClaimOIDC starts the claim intent. It validates the invite first so a
// dead or password-reset link never reaches the provider.
func (h *handler) handleClaimOIDC(c *fiber.Ctx) error {
	token := c.Params("token")
	claim, err := h.invites.Validate(c.UserContext(), token)
	if err != nil || claim.IsReset {
		// The SPA claim page shows the right terminal state, or the password form
		// for a reset link.
		return c.Redirect("/claim/"+token, fiber.StatusFound)
	}

	tx, err := auth.NewOIDCTx(auth.IntentClaim)
	if err != nil {
		h.reqLog(c).Error().Err(err).Msg("minting oidc claim tx failed")
		return redirectError(c, oidcLoginRedirect, errOIDCFailed)
	}
	tx.InviteTokenHash = auth.HashToken(token)
	return h.beginOIDC(c, tx, oidcLoginRedirect)
}

// handleOIDCCallback validates in order (provider error, tx, state, code
// exchange with ID-token and nonce), then dispatches on the tx intent.
func (h *handler) handleOIDCCallback(c *fiber.Ctx) error {
	// Single-use: clear on every exit so a replayed callback cannot reuse it.
	defer h.clearOIDCTxCookie(c)

	// Intent is unknown before the tx opens, so land on login.
	if provErr := c.Query("error"); provErr != "" {
		h.reqLog(c).Warn().
			Str("provider_error", provErr).
			Str("provider_error_description", c.Query("error_description")).
			Msg("oidc provider returned an error")
		return redirectError(c, oidcLoginRedirect, errOIDCDenied)
	}

	tx, err := h.oidcTx.Open(c.Cookies(oidcTxCookieName))
	if err != nil {
		// Missing, tampered, or expired tx cookie: one uniform expired outcome.
		// Tampering and a stale tab look the same here, so warn only.
		h.reqLog(c).Warn().Err(err).Msg("oidc tx cookie missing, tampered, or expired")
		return redirectError(c, oidcLoginRedirect, errOIDCExpired)
	}

	dest := destForIntent(tx.Intent)

	if c.Query("state") != tx.State {
		// Replayed or cross-session callback. Logged so a CSRF probe is visible.
		h.reqLog(c).Warn().Str("intent", tx.Intent).Msg("oidc callback state mismatch")
		return redirectError(c, dest, errOIDCFailed)
	}

	claims, err := h.oidc.Exchange(c.UserContext(), c.Query("code"), tx)
	if err != nil {
		// Log the cause; the public bucket stays generic.
		h.reqLog(c).Warn().Err(err).Str("intent", tx.Intent).
			Msg("oidc code exchange or id-token verification failed")
		return redirectError(c, dest, errOIDCFailed)
	}

	switch tx.Intent {
	case auth.IntentLogin:
		return h.dispatchOIDCLogin(c, claims)
	case auth.IntentLink:
		return h.dispatchOIDCLink(c, tx, claims)
	case auth.IntentClaim:
		return h.dispatchOIDCClaim(c, tx, claims)
	default:
		// We sealed this tx, so an unknown intent is a bug, not traffic.
		h.reqLog(c).Error().Str("intent", tx.Intent).Msg("oidc tx carries an unknown intent")
		return redirectError(c, dest, errOIDCFailed)
	}
}

// dispatchOIDCLogin signs in the linked member. An unlinked identity persists
// nothing and is only warn-logged (never tokens).
func (h *handler) dispatchOIDCLogin(c *fiber.Ctx, claims auth.OIDCClaims) error {
	rawSession, session, err := h.sessions.PrepareMint(
		0,
		stringPtrOrNil(c.Get(fiber.HeaderUserAgent)),
	)
	if err != nil {
		h.reqLog(c).Error().Err(err).Msg("preparing session on oidc login failed")
		return redirectError(c, oidcLoginRedirect, errOIDCFailed)
	}
	_, found, err := h.invites.CompleteOIDCLogin(c.UserContext(), claims, session)
	if err != nil {
		h.reqLog(c).Error().Err(err).
			Str("issuer", claims.Issuer).
			Str("subject", claims.Subject).
			Msg("oidc login dispatch failed")
		return redirectError(c, oidcLoginRedirect, errOIDCFailed)
	}
	if !found {
		h.reqLog(c).Warn().
			Str("issuer", claims.Issuer).
			Str("subject", claims.Subject).
			Str("email", derefOr(claims.Email, "")).
			Msg("oidc login for an unlinked identity rejected")
		return redirectError(c, oidcLoginRedirect, errOIDCUnlinked)
	}
	setSessionCookie(c, rawSession)
	return c.Redirect(oidcHomeRedirect, fiber.StatusFound)
}

// dispatchOIDCLink binds the identity to the tx member. The callback is
// unauthenticated, so it re-checks that the session still belongs to that member.
// A same-member re-link is an idempotent success.
func (h *handler) dispatchOIDCLink(c *fiber.Ctx, tx auth.OIDCTx, claims auth.OIDCClaims) error {
	as, err := h.sessions.Authenticate(c.UserContext(), c.Cookies(sessionCookieName))
	if err != nil {
		if !errors.Is(err, auth.ErrSessionInvalid) {
			h.reqLog(c).Error().Err(err).Int("member_id", tx.MemberID).
				Msg("session lookup on oidc link failed")
		}
		return redirectError(c, oidcLinkRedirect, errOIDCSessionExpired)
	}
	if as.UserID != tx.MemberID {
		return redirectError(c, oidcLinkRedirect, errOIDCSessionExpired)
	}

	if err := h.invites.LinkOIDC(c.UserContext(), tx.MemberID, claims, as.TokenHash); err != nil {
		if errors.Is(err, auth.ErrSessionInvalid) {
			return redirectError(c, oidcLinkRedirect, errOIDCSessionExpired)
		}
		if errors.Is(err, domain.ErrConflict) {
			return redirectError(c, oidcLinkRedirect, errOIDCLinkConflict)
		}
		h.reqLog(c).Error().Err(err).
			Int("member_id", tx.MemberID).
			Str("issuer", claims.Issuer).
			Str("subject", claims.Subject).
			Msg("oidc link dispatch failed")
		return redirectError(c, oidcLinkRedirect, errOIDCFailed)
	}
	return c.Redirect(oidcLinkRedirect+"?linked=1", fiber.StatusFound)
}

// dispatchOIDCClaim links, consumes the invite and creates the session in one
// transaction. It re-resolves the invite, which may have died during the
// provider round trip.
func (h *handler) dispatchOIDCClaim(c *fiber.Ctx, tx auth.OIDCTx, claims auth.OIDCClaims) error {
	rawSession, session, err := h.sessions.PrepareMint(
		0,
		stringPtrOrNil(c.Get(fiber.HeaderUserAgent)),
	)
	if err != nil {
		h.reqLog(c).Error().Err(err).Msg("preparing session on oidc claim failed")
		return redirectError(c, oidcLoginRedirect, errOIDCFailed)
	}
	_, err = h.invites.ClaimOIDCByHash(
		c.UserContext(),
		tx.InviteTokenHash,
		claims,
		session,
	)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return redirectError(c, oidcLoginRedirect, errOIDCLinkConflict)
		}
		h.reqLog(c).Warn().Err(err).Msg("oidc claim transition failed")
		return redirectError(c, oidcLoginRedirect, errOIDCFailed)
	}

	setSessionCookie(c, rawSession)
	return c.Redirect(oidcHomeRedirect, fiber.StatusFound)
}

// handleUnlinkSelf removes the caller's linked identity; 409 when it is their
// last credential.
func (h *handler) handleUnlinkSelf(c *fiber.Ctx) error {
	actor := actorMemberID(c)
	if err := h.invites.UnlinkOIDC(c.UserContext(), actor, actor); err != nil {
		return writeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// handleUnlinkMember removes a member's linked identity (admin). Removing
// another member's last credential is allowed; they become a placeholder.
func (h *handler) handleUnlinkMember(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	targetID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}
	if err := h.invites.UnlinkOIDC(c.UserContext(), targetID, actorMemberID(c)); err != nil {
		return writeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ssoDisabled answers every OIDC path with 404 when no provider is configured.
func ssoDisabled(c *fiber.Ctx) error {
	return writeProblem(c, fiber.StatusNotFound, "not_found", "sso is not configured")
}

// derefOr returns *s, or fallback when s is nil.
func derefOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}
