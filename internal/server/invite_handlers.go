package server

import (
	"errors"
	"fmt"
	"regexp"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/domain"

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
	// OIDC is an onboarding choice, not a password-reset bypass. The invite
	// manager doesn't know provider config, so enablement is layered on here.
	resp.Options.OIDC = h.oidcEnabled && !cc.IsReset
	return c.Status(fiber.StatusOK).JSON(resp)
}

// handleClaimPassword redeems an invite via the password path (unauthenticated:
// the member has no session yet). Placeholder takes username + password; reset
// takes password only and revokes every existing session (the invite doubles as
// a locked-out recovery). Credential, invite use, old-session revocation, and
// the replacement session commit together.
func (h *handler) handleClaimPassword(c *fiber.Ctx) error {
	body, ok := parseCredentialBody(c)
	if !ok {
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
	}

	rawSession, session, err := h.sessions.PrepareMint(
		0,
		stringPtrOrNil(c.Get(fiber.HeaderUserAgent)),
	)
	if err != nil {
		return h.writeInternal(c, err, "preparing session on claim failed")
	}
	_, err = h.invites.ClaimPassword(
		c.UserContext(),
		c.Params("token"),
		body.Username,
		body.Password,
		session,
	)
	if err != nil {
		return h.writeClaimError(c, err)
	}
	setSessionCookie(c, rawSession)
	return c.SendStatus(fiber.StatusNoContent)
}

// handleCreateInvite creates a first current generation for a member. A caller
// that already sees a generation must use its exact public handle to replace it.
func (h *handler) handleCreateInvite(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	targetID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}

	var body struct {
		Purpose string `json:"purpose"`
	}
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "invalid request body")
		}
	}

	var rawToken string
	switch body.Purpose {
	case "":
		rawToken, err = h.invites.Issue(c.UserContext(), targetID, actorMemberID(c))
	case "password_reset":
		rawToken, err = h.invites.IssuePasswordReset(c.UserContext(), targetID, actorMemberID(c))
	default:
		return writeProblem(c, fiber.StatusBadRequest, "invalid_request", "unknown invite purpose")
	}
	if err != nil {
		return writeError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(inviteResponse{ClaimURL: claimURL(rawToken)})
}

// handleReplaceInvite retires the exact generation the admin saw and returns a
// new one-time link. A stale handle conflicts without touching its replacement.
func (h *handler) handleReplaceInvite(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	inviteID, err := resolveInviteID(c)
	if err != nil {
		return writeError(c, err)
	}

	rawToken, err := h.invites.Replace(c.UserContext(), inviteID, actorMemberID(c))
	if err != nil {
		return writeError(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(inviteResponse{ClaimURL: claimURL(rawToken)})
}

// handleRevokeInvite revokes only the exact open generation addressed by the
// admin. Expired, spent, revoked, and stale handles conflict.
func (h *handler) handleRevokeInvite(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	inviteID, err := resolveInviteID(c)
	if err != nil {
		return writeError(c, err)
	}

	if err := h.invites.Revoke(c.UserContext(), inviteID); err != nil {
		return writeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// inviteOverviewResponse is one row of the admin invites surface. status is the
// server's snapshot (open / expired), derived per read rather than stored.
// serverNow lets the client advance that state without using its own clock.
// issuedBy is omitted when the invite has no recorded issuer. There is no claim
// URL here: only the token's hash is stored, so an existing link is unrecoverable
// by construction.
type inviteOverviewResponse struct {
	ID         string `json:"id"`
	MemberID   int    `json:"memberId"`
	MemberName string `json:"memberName"`
	Status     string `json:"status"`
	ExpiresAt  string `json:"expiresAt"`
	IssuedAt   string `json:"issuedAt"`
	IssuedBy   string `json:"issuedBy,omitempty"`
}

type invitesOverviewResponse struct {
	ServerNow string                   `json:"serverNow"`
	Items     []inviteOverviewResponse `json:"items"`
}

// handleListInvites returns every current invite an admin can act on (admin
// only), including explicit password-reset links for credentialed members.
// Each row is tagged open or expired and carries an immutable generation handle.
func (h *handler) handleListInvites(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}

	overview, err := h.invites.Overview(c.UserContext())
	if err != nil {
		return h.writeInternal(c, err, "listing invites failed")
	}

	rows := make([]inviteOverviewResponse, 0, len(overview.Items))
	for _, s := range overview.Items {
		row := inviteOverviewResponse{
			ID:         s.PublicID,
			MemberID:   s.UserID,
			MemberName: s.MemberName,
			Status:     s.Status,
			ExpiresAt:  formatTime(&s.ExpiresAt),
			IssuedAt:   formatTime(&s.CreatedAt),
		}
		if s.IssuedBy != nil {
			row.IssuedBy = *s.IssuedBy
		}
		rows = append(rows, row)
	}

	return c.Status(fiber.StatusOK).JSON(invitesOverviewResponse{
		ServerNow: formatTime(&overview.ServerNow),
		Items:     rows,
	})
}

// handleDismissInvite retires only the exact expired generation addressed by
// the admin. Open, spent, revoked, and stale handles conflict.
func (h *handler) handleDismissInvite(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}
	inviteID, err := resolveInviteID(c)
	if err != nil {
		return writeError(c, err)
	}

	if err := h.invites.Dismiss(c.UserContext(), inviteID); err != nil {
		return writeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

var invitePublicIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{20,64}$`)

// resolveInviteID reads the immutable public handle carried in :inviteID.
// Both migrated hex ids and newly minted base64url ids fit this alphabet.
func resolveInviteID(c *fiber.Ctx) (string, error) {
	value := c.Params("inviteID")
	if invitePublicIDPattern.MatchString(value) {
		return value, nil
	}
	return "", fmt.Errorf("%w: inviteID path parameter is invalid", domain.ErrInvalidInput)
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

	memberID := actorMemberID(c)
	if err := h.invites.SetFirstLocalLogin(c.UserContext(), memberID, body.Username, body.Password); err != nil {
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
