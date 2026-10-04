package server

import (
	"errors"
	"fmt"
	"regexp"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

// claimURL builds the relative SPA claim link. It carries no member id, so the
// token is the only secret in it.
func claimURL(rawToken string) string {
	return "/claim/" + rawToken
}

// inviteResponse is never broadcast over SSE: the claim URL is a secret.
type inviteResponse struct {
	ClaimURL string `json:"claimUrl"`
}

// claimResponse drives the /claim/<token> page. Mode is "placeholder" (new
// username and password) or "reset" (password only).
type claimResponse struct {
	DisplayName string `json:"displayName"`
	Mode        string `json:"mode"`
	Options     struct {
		Password bool `json:"password"`
		OIDC     bool `json:"oidc"`
	} `json:"options"`
}

// writeClaimError maps invalid, expired, and revoked invites to one 404 and a
// used invite to 410. The rest goes to writeError.
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

// handleValidateClaim is the unauthenticated, read-only claim-page data.
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
	// OIDC is an onboarding choice, not a password-reset bypass.
	resp.Options.OIDC = h.oidcEnabled && !cc.IsReset
	return c.Status(fiber.StatusOK).JSON(resp)
}

// handleClaimPassword redeems an invite with a password. A reset also revokes
// every old session. All writes commit together.
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

// inviteOverviewResponse is one admin invites row. It has no claim URL: only
// the token hash is stored. serverNow lets the client expire rows without its
// own clock.
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

// handleListInvites returns every current invite an admin can act on,
// including password-reset links.
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

// resolveInviteID reads :inviteID. Migrated hex ids and new base64url ids both
// fit the alphabet.
func resolveInviteID(c *fiber.Ctx) (string, error) {
	value := c.Params("inviteID")
	if invitePublicIDPattern.MatchString(value) {
		return value, nil
	}
	return "", fmt.Errorf("%w: inviteID path parameter is invalid", domain.ErrInvalidInput)
}

// handleSelfServeLocalLogin sets a first local login for a logged-in member.
// The session is the proof, so there is no current-password check.
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

// credentialBody is the {username, password} body of the claim and self-serve
// paths.
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
