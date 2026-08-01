package server

import (
	"errors"
	"time"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

// oidcTxCookieName is the encrypted transaction cookie set at initiation and
// cleared at the callback. HttpOnly + SameSite=Lax + Path=/ with a
// scheme-derived Secure flag; the value is an AEAD ciphertext, never anything
// readable.
const oidcTxCookieName = "mpa_oidc_tx"

// SPA landing routes the callback redirects to. The OIDC contract is
// redirect-only (302 with ?error= / ?linked=), the deliberate opposite of local
// login's XHR 204/401. These are frontend routes owned by the SPA; the config
// slice can rename them without touching the dispatch logic.
const (
	oidcHomeRedirect  = "/"
	oidcLoginRedirect = "/login"
	oidcLinkRedirect  = "/settings"
)

// Public callback error buckets. Every failed callback outcome collapses to one
// of these in a ?error= query param; the frontend maps them to copy. They never
// carry provider detail or tokens.
const (
	errOIDCDenied         = "oidc_denied"
	errOIDCExpired        = "oidc_expired"
	errOIDCUnlinked       = "oidc_unlinked"
	errOIDCFailed         = "oidc_failed"
	errOIDCLinkConflict   = "oidc_link_conflict"
	errOIDCSessionExpired = "oidc_session_expired"
)

// setOIDCTxCookie writes the encrypted transaction cookie with a Max-Age matched
// to the tx TTL, so a browser drops it around the same time the payload expires.
// Secure tracks the request scheme, mirroring the session cookie.
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

// clearOIDCTxCookie expires the transaction cookie. The callback always clears
// it (the tx is single-use), mirroring the set attributes so the browser drops
// the right cookie.
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

// redirectError sends the browser to an SPA route carrying a ?error= bucket. It
// is the single unhappy-path exit for the whole OIDC surface.
func redirectError(c *fiber.Ctx, dest, bucket string) error {
	return c.Redirect(dest+"?error="+bucket, fiber.StatusFound)
}

// destForIntent picks the SPA landing an error redirect returns to: link errors
// belong on the settings page the member started from; login and claim errors
// land on the login page.
func destForIntent(intent string) string {
	if intent == auth.IntentLink {
		return oidcLinkRedirect
	}
	return oidcLoginRedirect
}

// beginOIDC seals a fresh transaction into the tx cookie and 302s to the
// provider authorize URL. Initiation is a top-level navigation, so a seal
// failure redirects to the login page with a generic error rather than
// returning JSON the browser would render as a raw page.
func (h *handler) beginOIDC(c *fiber.Ctx, tx auth.OIDCTx, dest string) error {
	sealed, err := h.oidcTx.Seal(tx)
	if err != nil {
		h.reqLog(c).Error().Err(err).Str("intent", tx.Intent).Msg("sealing oidc tx cookie failed")
		return redirectError(c, dest, errOIDCFailed)
	}
	h.setOIDCTxCookie(c, sealed)
	return c.Redirect(h.oidc.AuthCodeURL(tx), fiber.StatusFound)
}

// handleOIDCLogin starts the login intent (unauthenticated): mint a tx, stash
// it, and hand the browser to the provider. The callback dispatches on
// intent=login.
func (h *handler) handleOIDCLogin(c *fiber.Ctx) error {
	tx, err := auth.NewOIDCTx(auth.IntentLogin)
	if err != nil {
		h.reqLog(c).Error().Err(err).Msg("minting oidc login tx failed")
		return redirectError(c, oidcLoginRedirect, errOIDCFailed)
	}
	return h.beginOIDC(c, tx, oidcLoginRedirect)
}

// handleOIDCLink starts the link intent (authenticated): the tx carries the
// session member so the callback can re-check the session still matches before
// binding the identity.
func (h *handler) handleOIDCLink(c *fiber.Ctx) error {
	tx, err := auth.NewOIDCTx(auth.IntentLink)
	if err != nil {
		h.reqLog(c).Error().Err(err).Msg("minting oidc link tx failed")
		return redirectError(c, oidcLinkRedirect, errOIDCFailed)
	}
	tx.MemberID = actorMemberID(c)
	return h.beginOIDC(c, tx, oidcLinkRedirect)
}

// handleClaimOIDC starts the claim intent (unauthenticated): it validates the
// invite up front (so a dead link doesn't send the member to the provider), then
// stashes the invite token HASH in the tx and starts the flow. The callback
// re-validates the invite, links, consumes, and mints.
func (h *handler) handleClaimOIDC(c *fiber.Ctx) error {
	token := c.Params("token")
	if _, err := h.invites.Validate(c.UserContext(), token); err != nil {
		// A no-longer-valid or already-used invite: bounce to the SPA claim page,
		// which re-validates and shows the right terminal state.
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

// handleOIDCCallback is the single callback every intent returns to. It runs the
// validation ladder in the spec's order (provider error → tx decrypt → state →
// code exchange + ID-token verify + nonce), then dispatches on the tx intent.
// The tx cookie is single-use, so it is always cleared. Every outcome is a 302.
func (h *handler) handleOIDCCallback(c *fiber.Ctx) error {
	// The tx is spent the moment we read it, success or fail: clear it on every
	// exit so a replayed callback can't reuse it.
	defer h.clearOIDCTxCookie(c)

	// Provider-side refusal (user denied consent, provider error) comes first,
	// before we even open the tx. Intent is unknown here, so land on login.
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
		// A tampered cookie and a member who left the tab open overnight are
		// indistinguishable from here, so this stays warn.
		h.reqLog(c).Warn().Err(err).Msg("oidc tx cookie missing, tampered, or expired")
		return redirectError(c, oidcLoginRedirect, errOIDCExpired)
	}

	dest := destForIntent(tx.Intent)

	if c.Query("state") != tx.State {
		// The tx opened but its state does not match the callback's: a replayed
		// or cross-session callback. Silent until now, which made a CSRF probe
		// look identical to a clean run.
		h.reqLog(c).Warn().Str("intent", tx.Intent).Msg("oidc callback state mismatch")
		return redirectError(c, dest, errOIDCFailed)
	}

	claims, err := h.oidc.Exchange(c.UserContext(), c.Query("code"), tx)
	if err != nil {
		// Code exchange, ID-token verification, or nonce comparison failed. Log the
		// cause internally; the public bucket stays generic.
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
		// We sealed this tx ourselves, so an intent we cannot dispatch means the
		// mint and dispatch sides have drifted apart. That is a bug, not traffic.
		h.reqLog(c).Error().Str("intent", tx.Intent).Msg("oidc tx carries an unknown intent")
		return redirectError(c, dest, errOIDCFailed)
	}
}

// dispatchOIDCLogin matches the verified claims to a linked member. A match
// refreshes snapshots, bumps last_login_at, mints a session, and lands home. An
// unlinked identity is rejected ephemerally: nothing is persisted and the
// attempt is WARN-logged (iss/sub/email, never tokens).
func (h *handler) dispatchOIDCLogin(c *fiber.Ctx, claims auth.OIDCClaims) error {
	memberID, found, err := h.linker.Login(c.UserContext(), claims)
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
	if err := h.issueSession(c, memberID); err != nil {
		h.reqLog(c).Error().Err(err).Int("member_id", memberID).
			Msg("minting session on oidc login failed")
		return redirectError(c, oidcLoginRedirect, errOIDCFailed)
	}
	return c.Redirect(oidcHomeRedirect, fiber.StatusFound)
}

// dispatchOIDCLink binds the identity to the tx member. Because the callback is
// unauthenticated, it re-authenticates the session cookie here and refuses if it
// no longer matches the member who started the link (oidc_session_expired). A
// collision on either UNIQUE is oidc_link_conflict with nothing written; a
// same-member re-link is idempotent success.
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

	if err := h.linker.Link(c.UserContext(), tx.MemberID, claims); err != nil {
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

// dispatchOIDCClaim links the identity to the invite's member, then consumes the
// invite and mints a session. It re-resolves the invite by the hash stashed in
// the tx (it may have expired or been used during the provider round trip). A
// collision writes nothing and does NOT consume the invite; on success the
// invite is consumed last (per ADR 0001), so a failure before that leaves it
// usable.
func (h *handler) dispatchOIDCClaim(c *fiber.Ctx, tx auth.OIDCTx, claims auth.OIDCClaims) error {
	ic, err := h.invites.ResolveClaimByHash(c.UserContext(), tx.InviteTokenHash)
	if err != nil {
		// The invite died between initiation and callback (expired, revoked, used).
		h.reqLog(c).Warn().Err(err).Msg("oidc claim invite no longer valid at callback")
		return redirectError(c, oidcLoginRedirect, errOIDCFailed)
	}

	if err := h.linker.Link(c.UserContext(), ic.UserID, claims); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return redirectError(c, oidcLoginRedirect, errOIDCLinkConflict)
		}
		h.reqLog(c).Error().Err(err).
			Int("member_id", ic.UserID).
			Int64("invite_id", ic.ID).
			Str("issuer", claims.Issuer).
			Str("subject", claims.Subject).
			Msg("oidc claim link failed")
		return redirectError(c, oidcLoginRedirect, errOIDCFailed)
	}

	if err := h.issueSession(c, ic.UserID); err != nil {
		h.reqLog(c).Error().Err(err).Int("member_id", ic.UserID).
			Msg("minting session on oidc claim failed")
		return redirectError(c, oidcLoginRedirect, errOIDCFailed)
	}
	// Consume last: if this fails the identity is linked and the session is up, so
	// the member is in; the still-usable invite ages out or is regenerated.
	if err := h.invites.Consume(c.UserContext(), ic.ID); err != nil {
		// The member is already in; the invite just stays usable until it ages
		// out, so this is an operator cleanup signal, not a failed request.
		h.reqLog(c).Error().Err(err).
			Int("member_id", ic.UserID).
			Int64("invite_id", ic.ID).
			Msg("consuming invite on oidc claim failed")
	}
	return c.Redirect(oidcHomeRedirect, fiber.StatusFound)
}

// handleUnlinkSelf removes the caller's own linked identity
// (DELETE /auth/linked-identity). The self-last-credential guard refuses it with
// 409 when the identity is the member's only remaining credential.
func (h *handler) handleUnlinkSelf(c *fiber.Ctx) error {
	actor := actorMemberID(c)
	if err := h.linker.Unlink(c.UserContext(), actor, actor); err != nil {
		return writeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// handleUnlinkMember removes another member's linked identity (admin,
// DELETE /members/{id}/linked-identity). Removing another member's last
// credential is allowed (they fall back to a placeholder); the last-credential
// guard only fires when an admin unlinks their own account.
func (h *handler) handleUnlinkMember(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	targetID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}
	if err := h.linker.Unlink(c.UserContext(), targetID, actorMemberID(c)); err != nil {
		return writeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// ssoDisabled is the 404 every OIDC path returns when no provider is configured.
// Registered ahead of requireSession so the miss reads as "SSO isn't configured"
// (404), not the blanket "authentication required" (401) an unmatched
// authenticated path would otherwise hit.
func ssoDisabled(c *fiber.Ctx) error {
	return writeProblem(c, fiber.StatusNotFound, "not_found", "sso is not configured")
}

// derefOr returns the pointed-to string or a fallback when nil, so a missing
// snapshot claim logs as empty rather than panicking.
func derefOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}
