package server

import (
	"errors"

	"moviepickarr/internal/auth"

	"github.com/gofiber/fiber/v2"
)

// claimURL builds the one-time claim link shown to the admin at issuance. It is
// a relative SPA path (`/claim/<token>`) with the raw token and no member id, so
// the frontend router resolves it and the token is the only secret carried. The
// admin delivers it out-of-band; there is no resend.
func claimURL(rawToken string) string {
	return "/claim/" + rawToken
}

// inviteResponse carries the one-time claim URL back to the issuing admin. It is
// returned only in the direct HTTP response, never broadcast over SSE: the URL
// is a single-use secret.
type inviteResponse struct {
	ClaimURL string `json:"claimUrl"`
}

// claimResponse drives the SPA /claim/<token> page. Mode is "placeholder" (set a
// fresh username + password) or "reset" (password only, username already set).
// The options block reports which credential paths to offer.
type claimResponse struct {
	DisplayName string `json:"displayName"`
	Mode        string `json:"mode"`
	Options     struct {
		Password bool `json:"password"`
		OIDC     bool `json:"oidc"`
	} `json:"options"`
}

// writeClaimError maps the invite sentinels to the two distinct claim-page
// states and defers everything else (validation, conflict, infra) to writeError.
// Invalid/expired/revoked collapse to one 404 "no longer valid"; an already-used
// invite is a distinct 410 "already set up".
func (h *handler) writeClaimError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, auth.ErrInviteUsed):
		return writeProblem(c, fiber.StatusGone, "invite_used", "this invite has already been set up")
	case errors.Is(err, auth.ErrInviteInvalid):
		return writeProblem(c, fiber.StatusNotFound, "invite_invalid", "this invite is no longer valid")
	default:
		return writeError(c, err)
	}
}

// handleValidateClaim is the read-only claim-page data endpoint (unauthenticated,
// GET, CSRF-exempt). It returns the greet-by name, placeholder-vs-reset mode, and
// the credential options, or one of the two distinct no-longer-valid / already-set-up
// states.
func (h *handler) handleValidateClaim(c *fiber.Ctx) error {
	cc, err := h.invites.Validate(c.UserContext(), c.Params("token"))
	if err != nil {
		return h.writeClaimError(c, err)
	}

	resp := claimResponse{DisplayName: cc.DisplayName, Mode: "placeholder"}
	if cc.IsReset {
		resp.Mode = "reset"
	}
	resp.Options.Password = cc.Options.Password
	// OIDC is offered on the claim page exactly when a provider is configured. The
	// invite manager doesn't know about OIDC config, so enablement is layered on
	// here from the handler's presence-derived flag.
	resp.Options.OIDC = h.oidcEnabled
	return c.Status(fiber.StatusOK).JSON(resp)
}

// handleClaimPassword redeems an invite via the password path (unauthenticated:
// the member has no session yet). Placeholder takes username + password; reset
// takes password only and revokes every existing session (the invite doubles as
// a locked-out recovery). Both consume the invite once and mint a fresh session
// → 204 + cookie; the SPA hydrates via /me.
func (h *handler) handleClaimPassword(c *fiber.Ctx) error {
	body, ok := parseCredentialBody(c)
	if !ok {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}

	res, err := h.invites.ClaimPassword(c.UserContext(), c.Params("token"), body.Username, body.Password)
	if err != nil {
		return h.writeClaimError(c, err)
	}

	// Reset closes every existing session first (the recovery), then mints fresh
	// for this device so the redeemer lands logged in with exactly one live
	// session. The invite is consumed last, so a failure here leaves it usable.
	if res.WasReset {
		if err := h.sessions.RevokeAll(c.UserContext(), res.MemberID); err != nil {
			return h.writeInternal(c, err, "revoking sessions on invite reset failed")
		}
	}
	if err := h.issueSession(c, res.MemberID); err != nil {
		return h.writeInternal(c, err, "minting session on claim failed")
	}
	if err := h.invites.Consume(c.UserContext(), res.InviteID); err != nil {
		return h.writeInternal(c, err, "consuming invite on claim failed")
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// handleReissueInvite (re)issues a member's claim link (admin). It revokes any
// current valid invite and returns a fresh one-time URL, serving both re-invite
// and regenerate. A missing member surfaces as 404.
func (h *handler) handleReissueInvite(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	targetID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}

	rawToken, err := h.invites.Issue(c.UserContext(), targetID, actorMemberID(c))
	if err != nil {
		return writeError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(inviteResponse{ClaimURL: claimURL(rawToken)})
}

// handleRevokeInvite revokes a member's current valid invite (admin). Revoking
// when there is nothing valid to cancel is a 404, not a silent no-op.
func (h *handler) handleRevokeInvite(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	targetID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}

	if err := h.invites.Revoke(c.UserContext(), targetID); err != nil {
		return writeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// handleSelfServeLocalLogin is the authed credential-completeness path
// (POST /auth/local-login): a logged-in member with no local login sets their
// first username + password. The session is the proof, so there is no
// current-password check; a member who already has a local login gets 409.
func (h *handler) handleSelfServeLocalLogin(c *fiber.Ctx) error {
	body, ok := parseCredentialBody(c)
	if !ok {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}

	if err := h.localAuth.SetFirstLocalLogin(c.UserContext(), actorMemberID(c), body.Username, body.Password); err != nil {
		return writeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// credentialBody is the shared {username, password} request shape for the claim
// and self-serve credential paths. parseCredentialBody parses it, returning ok
// false when the body is unreadable so each caller writes the same 400.
type credentialBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func parseCredentialBody(c *fiber.Ctx) (credentialBody, bool) {
	var body credentialBody
	if err := c.BodyParser(&body); err != nil {
		return credentialBody{}, false
	}
	return body, true
}
