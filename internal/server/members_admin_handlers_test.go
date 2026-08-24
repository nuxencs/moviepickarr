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

// The admin roster is admin-only: a plain member hitting it gets the first-class
// 403 admin_required, which the frontend renders as the "Admins only" screen
// rather than masking it as a 404.
func TestHandleGetRoster_ForbidsNonAdmin(t *testing.T) {
	t.Parallel()
	_, app, _, _ := setupEditMovieTest(t)

	resp := doAs(t, app, jsonReq(http.MethodGet, "/api/v1/members/roster", ``), 1, "member")
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("roster as member: got %d, want 403", resp.StatusCode)
	}
	if code := problemCode(t, resp); code != "admin_required" {
		t.Fatalf("roster as member code = %q, want admin_required", code)
	}
}

// The roster surfaces active and archived members with presence-derived state,
// active-before-archived, so the frontend can split the two sections and read the
// role/archived facts straight off each row.
func TestHandleGetRoster_ReturnsRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	admin, err := userRepo.Create(ctx, "Admin")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := userRepo.SetRole(ctx, admin.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	if _, err := userRepo.Create(ctx, "Priya"); err != nil {
		t.Fatalf("create priya: %v", err)
	}
	dana, err := userRepo.Create(ctx, "Dana")
	if err != nil {
		t.Fatalf("create dana: %v", err)
	}
	if _, err := movieRepo.Add(ctx, "Ronin", "pool", dana.ID); err != nil {
		t.Fatalf("add dana movie: %v", err)
	}
	if _, err := userRepo.Remove(ctx, dana.ID); err != nil {
		t.Fatalf("archive dana: %v", err)
	}

	resp := doAs(t, app, jsonReq(http.MethodGet, "/api/v1/members/roster", ``), admin.ID, "admin")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("roster as admin: got %d, want 200", resp.StatusCode)
	}
	var rows []rosterMemberResponse
	if err := json.UnmarshalRead(resp.Body, &rows); err != nil {
		t.Fatalf("decode roster: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("roster length = %d, want 3", len(rows))
	}
	byID := make(map[int]rosterMemberResponse, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	if byID[admin.ID].Role != domain.RoleAdmin {
		t.Fatalf("admin row role = %q, want admin", byID[admin.ID].Role)
	}
	if !byID[dana.ID].Archived {
		t.Fatal("dana row not marked archived")
	}
	if byID[dana.ID].MoviesAuthored != 1 {
		t.Fatalf("dana moviesAuthored = %d, want 1", byID[dana.ID].MoviesAuthored)
	}
	// Archived rows sort last, so the surface splits the active section cleanly.
	if rows[len(rows)-1].ID != dana.ID {
		t.Fatalf("archived member not last: got id %d", rows[len(rows)-1].ID)
	}
}

// Promote then demote flips the role end to end; a second admin remains so the
// demotion is allowed.
func TestHandleSetRole_PromoteDemote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, app, userRepo, _ := setupEditMovieTest(t)

	admin, err := userRepo.Create(ctx, "Admin")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := userRepo.SetRole(ctx, admin.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("promote admin: %v", err)
	}
	priya, err := userRepo.Create(ctx, "Priya")
	if err != nil {
		t.Fatalf("create priya: %v", err)
	}

	promote := jsonReq(http.MethodPatch, fmt.Sprintf("/api/v1/members/%d/role", priya.ID), `{"role":"admin"}`)
	if resp := doAs(t, app, promote, admin.ID, "admin"); resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("promote priya: got %d, want 204", resp.StatusCode)
	}
	roster, err := userRepo.Roster(ctx)
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	for _, m := range roster {
		if m.ID == priya.ID && m.Role != domain.RoleAdmin {
			t.Fatalf("priya role = %q, want admin", m.Role)
		}
	}

	demote := jsonReq(http.MethodPatch, fmt.Sprintf("/api/v1/members/%d/role", priya.ID), `{"role":"member"}`)
	if resp := doAs(t, app, demote, admin.ID, "admin"); resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("demote priya: got %d, want 204", resp.StatusCode)
	}
}

func TestHandleSetRole_ForbidsNonAdmin(t *testing.T) {
	t.Parallel()
	_, app, _, _ := setupEditMovieTest(t)

	req := jsonReq(http.MethodPatch, "/api/v1/members/1/role", `{"role":"admin"}`)
	resp := doAs(t, app, req, 2, "member")
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("set role as member: got %d, want 403", resp.StatusCode)
	}
	if code := problemCode(t, resp); code != "admin_required" {
		t.Fatalf("set role as member code = %q, want admin_required", code)
	}
}

func TestHandleSetRole_RejectsInvalidRole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, app, userRepo, _ := setupEditMovieTest(t)

	target, err := userRepo.Create(ctx, "Target")
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	req := jsonReq(http.MethodPatch, fmt.Sprintf("/api/v1/members/%d/role", target.ID), `{"role":"superuser"}`)
	resp := doAs(t, app, req, target.ID, "admin")
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("invalid role: got %d, want 400", resp.StatusCode)
	}
}

// Demoting the only admin is refused with 409 so the surface can warn instead of
// stranding the roster with no admin.
func TestHandleSetRole_LastAdminConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, app, userRepo, _ := setupEditMovieTest(t)

	only, err := userRepo.Create(ctx, "Only")
	if err != nil {
		t.Fatalf("create only admin: %v", err)
	}
	if err := userRepo.SetRole(ctx, only.ID, domain.RoleAdmin); err != nil {
		t.Fatalf("promote only admin: %v", err)
	}

	req := jsonReq(http.MethodPatch, fmt.Sprintf("/api/v1/members/%d/role", only.ID), `{"role":"member"}`)
	resp := doAs(t, app, req, only.ID, "admin")
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("demote last admin: got %d, want 409", resp.StatusCode)
	}
}
