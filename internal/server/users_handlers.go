package server

import (
	"fmt"
	"strconv"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

func (h *handler) handleGetUsers(c *fiber.Ctx) error {
	ctx := c.UserContext()

	users, err := h.userService.List(ctx)
	if err != nil {
		return writeError(c, err)
	}

	// Only pool/stash movies are rendered on the users board, so fetch exactly
	// those (both status-indexed) instead of loading the entire movies table —
	// including the ever-growing watched history — only to filter it back down
	// in Go. The bucketing loop below relies on each row's Status, which the two
	// status-scoped queries set correctly.
	pooled, err := h.movieService.Pooled(ctx)
	if err != nil {
		return writeError(c, err)
	}
	stashed, err := h.movieService.Stashed(ctx)
	if err != nil {
		return writeError(c, err)
	}
	visible := make([]*domain.Movie, 0, len(pooled)+len(stashed))
	visible = append(visible, pooled...)
	visible = append(visible, stashed...)

	// Boards render tile-level data only, so build lean tiles and skip the
	// credits batch-load (GetCreditsByMovieIDs over every board movie) — a
	// read-path saving on its own, on top of the smaller wire payload.
	meta := h.metaFor(c, visible)
	poolByUser := make(map[int]map[string]leanMovieTile)
	stashByUser := make(map[int]map[string]leanMovieTile)

	for i := range visible {
		tile := toLeanTile(visible[i], meta[visible[i].ID])
		key := strconv.Itoa(visible[i].ID)

		if visible[i].Status == "pool" {
			if poolByUser[visible[i].AddedByID] == nil {
				poolByUser[visible[i].AddedByID] = map[string]leanMovieTile{}
			}
			poolByUser[visible[i].AddedByID][key] = tile
			continue
		}

		if stashByUser[visible[i].AddedByID] == nil {
			stashByUser[visible[i].AddedByID] = map[string]leanMovieTile{}
		}
		stashByUser[visible[i].AddedByID][key] = tile
	}

	response := make([]userResponse, 0, len(users))
	for i := range users {
		currentPool := poolByUser[users[i].ID]
		if currentPool == nil {
			currentPool = map[string]leanMovieTile{}
		}
		stash := stashByUser[users[i].ID]
		if stash == nil {
			stash = map[string]leanMovieTile{}
		}

		response = append(response, userResponse{
			ID:          users[i].ID,
			Name:        users[i].Name,
			CurrentPool: currentPool,
			Stash:       stash,
			CreatedAt:   formatTime(users[i].CreatedAt),
		})
	}

	return c.Status(fiber.StatusOK).JSON(response)
}

func (h *handler) handleCreateUser(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeError(c, fmt.Errorf("%w: invalid request body", domain.ErrInvalidInput))
	}

	name := sanitizeInput(body.Name)
	if name == "" {
		return writeError(c, fmt.Errorf("%w: name is required", domain.ErrInvalidInput))
	}

	ctx := c.UserContext()
	// Onboarding is one lifecycle write: the placeholder, initial next-up
	// assignment, and first invite commit together. The raw claim token is kept
	// outside persistence and returned only by this direct response.
	createdUser, rawToken, err := h.invites.CreateMemberWithInvite(ctx, name, actorMemberID(c))
	if err != nil {
		return writeError(c, err)
	}

	// Stats list every roster member (zero rows included), so a new member
	// must show up there immediately, not after the cache TTL.
	h.invalidateStatsCache()

	payload := userResponse{
		ID:          createdUser.ID,
		Name:        createdUser.Name,
		CurrentPool: map[string]leanMovieTile{},
		Stash:       map[string]leanMovieTile{},
		CreatedAt:   formatTime(createdUser.CreatedAt),
	}

	// The broadcast carries the roster row only: the claim URL is a one-time
	// secret and goes solely in the direct response to the issuing admin.
	h.broker.Broadcast(event{Type: "user:created", Data: payload})

	return c.Status(fiber.StatusCreated).JSON(createMemberResponse{
		userResponse: payload,
		ClaimURL:     claimURL(rawToken),
	})
}

// createMemberResponse is the POST /members payload: the new roster row plus the
// one-time claim URL. The claim URL is response-only (never broadcast).
type createMemberResponse struct {
	userResponse
	ClaimURL string `json:"claimUrl"`
}

// removeMemberResponse reports which of the two removal paths ran, so the roster
// UI can show "deleted" (gone for good) vs "archived" (restorable, attribution
// kept) after the same admin action.
type removeMemberResponse struct {
	Outcome domain.RemoveOutcome `json:"outcome"`
}

// handleDeleteUser removes a member as one admin action with two outcomes: a
// hard delete when they authored nothing, or an archive that preserves their
// watch-history attribution. Both leave the active roster, so both broadcast
// user:deleted; the response body carries the outcome for the follow-up toast.
func (h *handler) handleDeleteUser(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}

	memberID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()
	outcome, err := h.userService.Remove(ctx, memberID)
	if err != nil {
		return writeError(c, err)
	}

	h.invalidateStatsCache()

	h.broker.Broadcast(event{Type: "user:deleted", Data: fiber.Map{"userID": memberID}})

	return c.Status(fiber.StatusOK).JSON(removeMemberResponse{Outcome: outcome})
}

// handleRestoreUser reactivates an archived member and re-issues their claim
// link as one admin action: archiving stripped the credentials, so a fresh
// invite is what lets them log back in. It returns the roster row plus the
// one-time claim URL (response-only, like member-create) and broadcasts the
// member back onto the active board.
func (h *handler) handleRestoreUser(c *fiber.Ctx) error {
	if ok, err := h.requireAdmin(c); !ok {
		return err
	}

	memberID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()
	restoredUser, rawToken, err := h.invites.RestoreMemberWithInvite(ctx, memberID, actorMemberID(c))
	if err != nil {
		return writeError(c, err)
	}

	// Build the response from the member projection read before the lifecycle
	// commit. SSE consumers use user:created as an invalidation and refetch the
	// member's movie rows, so no fallible read can lose this one-time claim URL.
	payload := userResponse{
		ID:          restoredUser.ID,
		Name:        restoredUser.Name,
		CurrentPool: map[string]leanMovieTile{},
		Stash:       map[string]leanMovieTile{},
		CreatedAt:   formatTime(restoredUser.CreatedAt),
	}

	h.invalidateStatsCache()

	// The claim URL is a one-time secret, so it goes only in the direct response;
	// the broadcast carries the roster row alone.
	h.broker.Broadcast(event{Type: "user:created", Data: payload})

	return c.Status(fiber.StatusOK).JSON(createMemberResponse{
		userResponse: payload,
		ClaimURL:     claimURL(rawToken),
	})
}
