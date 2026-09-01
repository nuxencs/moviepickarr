package server

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"testing"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

// rosterHas reports whether GET /api/v1/members lists a member id (the active
// roster), so the delete/archive tests can assert a member left the board.
func rosterHas(t *testing.T, app *fiber.App, memberID int) bool {
	t.Helper()
	resp := doAs(t, app, jsonReq(http.MethodGet, "/api/v1/members", ``), 1, "admin")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("list members: status %d", resp.StatusCode)
	}
	var roster []struct {
		UserID int `json:"userID"`
	}
	if err := json.UnmarshalRead(resp.Body, &roster); err != nil {
		t.Fatalf("decode roster: %v", err)
	}
	for _, r := range roster {
		if r.UserID == memberID {
			return true
		}
	}
	return false
}

// A member who authored no movies is hard-deleted: 200 with outcome "deleted",
// and they leave the active roster.
func TestHandleDeleteUser_HardDeleteReportsOutcome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, _ := setupEditMovieTest(t)
	member, err := userRepo.Create(ctx, "NoMovies")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	resp := doAs(t, app,
		jsonReq(http.MethodDelete, fmt.Sprintf("/api/v1/members/%d", member.ID), ``), 1, "admin")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("delete: expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Outcome string `json:"outcome"`
	}
	if err := json.UnmarshalRead(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Outcome != "deleted" {
		t.Fatalf("outcome = %q, want deleted", body.Outcome)
	}
	if rosterHas(t, app, member.ID) {
		t.Fatal("hard-deleted member still on roster")
	}
}

// A member who authored movies is archived: 200 with outcome "archived", they
// leave the active roster, but the movie's attribution row survives.
func TestHandleDeleteUser_ArchiveReportsOutcome(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)
	member, err := userRepo.Create(ctx, "HasMovies")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Sicario", "pool", member.ID)
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}

	resp := doAs(t, app,
		jsonReq(http.MethodDelete, fmt.Sprintf("/api/v1/members/%d", member.ID), ``), 1, "admin")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("delete: expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Outcome string `json:"outcome"`
	}
	if err := json.UnmarshalRead(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Outcome != "archived" {
		t.Fatalf("outcome = %q, want archived", body.Outcome)
	}
	if rosterHas(t, app, member.ID) {
		t.Fatal("archived member still on roster")
	}

	// Attribution survives: the movie still resolves to the archived member.
	got, err := movieRepo.FindByID(ctx, movie.ID)
	if err != nil {
		t.Fatalf("find movie: %v", err)
	}
	if got.AddedByID != member.ID {
		t.Fatalf("attribution lost: addedBy=%d, want %d", got.AddedByID, member.ID)
	}
}

func TestHandleDeleteUser_RefusesLastActiveAdmin(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		authored bool
	}{
		{name: "hard delete", authored: false},
		{name: "archive", authored: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			_, app, userRepo, movieRepo := setupEditMovieTest(t)
			admin, err := userRepo.Create(ctx, "OnlyAdmin")
			if err != nil {
				t.Fatalf("create admin: %v", err)
			}
			if _, err := userRepo.SetRole(ctx, domain.RoleChange{MemberID: admin.ID, Role: domain.RoleAdmin}); err != nil {
				t.Fatalf("promote admin: %v", err)
			}
			if tc.authored {
				if _, err := movieRepo.Add(ctx, "Admin's movie", "pool", admin.ID); err != nil {
					t.Fatalf("add admin movie: %v", err)
				}
			}

			resp := doAs(t, app,
				jsonReq(http.MethodDelete, fmt.Sprintf("/api/v1/members/%d", admin.ID), ``), admin.ID, "admin")
			if resp.StatusCode != fiber.StatusConflict {
				t.Fatalf("remove last admin: got %d, want 409", resp.StatusCode)
			}
			if !rosterHas(t, app, admin.ID) {
				t.Fatal("last admin left active roster after refused removal")
			}
		})
	}
}

// Restore reactivates an archived member and hands back a fresh claim URL in one
// action; the member returns to the active roster.
func TestHandleRestoreUser_ReactivatesAndReinvites(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)
	member, err := userRepo.Create(ctx, "Returning")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	if _, err := movieRepo.Add(ctx, "Prisoners", "pool", member.ID); err != nil {
		t.Fatalf("add movie: %v", err)
	}

	// Archive first (authored a movie), then restore.
	if resp := doAs(t, app,
		jsonReq(http.MethodDelete, fmt.Sprintf("/api/v1/members/%d", member.ID), ``), 1, "admin"); resp.StatusCode != fiber.StatusOK {
		t.Fatalf("archive: expected 200, got %d", resp.StatusCode)
	}

	resp := doAs(t, app,
		jsonReq(http.MethodPost, fmt.Sprintf("/api/v1/members/%d/restore", member.ID), ``), 1, "admin")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("restore: expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		UserID   int    `json:"userID"`
		ClaimURL string `json:"claimUrl"`
	}
	if err := json.UnmarshalRead(resp.Body, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.UserID != member.ID {
		t.Fatalf("restore userID = %d, want %d", body.UserID, member.ID)
	}
	if body.ClaimURL == "" {
		t.Fatal("restore returned no claim URL")
	}
	if !rosterHas(t, app, member.ID) {
		t.Fatal("restored member not back on roster")
	}
}

// Restoring a member who is not archived is a 404: there is nothing to restore.
func TestHandleRestoreUser_NotArchived404(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, _ := setupEditMovieTest(t)
	member, err := userRepo.Create(ctx, "Active")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	resp := doAs(t, app,
		jsonReq(http.MethodPost, fmt.Sprintf("/api/v1/members/%d/restore", member.ID), ``), 1, "admin")
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("restore active member: expected 404, got %d", resp.StatusCode)
	}
}

// Restore is admin-only: a plain member is refused with 403 admin_required
// before any state changes.
func TestHandleRestoreUser_AdminOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, _ := setupEditMovieTest(t)
	member, err := userRepo.Create(ctx, "Nonadmin")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}

	resp := doAs(t, app,
		jsonReq(http.MethodPost, fmt.Sprintf("/api/v1/members/%d/restore", member.ID), ``), member.ID, "member")
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	if code := problemCode(t, resp); code != "admin_required" {
		t.Fatalf("expected admin_required, got %q", code)
	}
}
