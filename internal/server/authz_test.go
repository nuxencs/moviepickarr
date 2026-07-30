package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"moviepickarr/internal/movie"

	"github.com/gofiber/fiber/v2"
)

// problemCode decodes the machine `code` (the problem+json title) from a 4xx
// response so the authz tests can assert on it, not just the status.
func problemCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	var p problemDetails
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return p.Title
}

// doAs issues req as the given member/role via the test actor headers.
func doAs(t *testing.T, app *fiber.App, req *http.Request, memberID int, role string) *http.Response {
	t.Helper()
	req.Header.Set(testMemberHeader, strconv.Itoa(memberID))
	if role != "" {
		req.Header.Set(testRoleHeader, role)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func jsonReq(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

type asyncHTTPResult struct {
	resp *http.Response
	err  error
}

func startAs(app *fiber.App, req *http.Request, memberID int, role string) <-chan asyncHTTPResult {
	req.Header.Set(testMemberHeader, strconv.Itoa(memberID))
	if role != "" {
		req.Header.Set(testRoleHeader, role)
	}

	done := make(chan asyncHTTPResult, 1)
	go func() {
		resp, err := app.Test(req, -1)
		done <- asyncHTTPResult{resp: resp, err: err}
	}()
	return done
}

// A member who did not add a movie cannot edit, delete or move it: 403 not_adder,
// with no admin override. Missing resources still 404 (never masked as 403).
func TestAuthz_AdderOnlyMutations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	owner, err := userRepo.Create(ctx, "Owner")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := userRepo.Create(ctx, "Other")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Heat", "stash", owner.ID)
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}

	cases := []struct {
		name string
		req  *http.Request
	}{
		{"edit", jsonReq(http.MethodPut, fmt.Sprintf("/api/v1/movies/%d", movie.ID), `{"title":"X","link":"https://example.com/x"}`)},
		{"delete", jsonReq(http.MethodDelete, fmt.Sprintf("/api/v1/movies/%d", movie.ID), ``)},
		{"move", jsonReq(http.MethodPost, fmt.Sprintf("/api/v1/movies/%d/move", movie.ID), `{"target":"pool"}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name+"_non_adder_403", func(t *testing.T) {
			// An admin non-adder is still refused: no admin override on adder actions.
			resp := doAs(t, app, tc.req, other.ID, "admin")
			if resp.StatusCode != fiber.StatusForbidden {
				t.Fatalf("expected 403, got %d", resp.StatusCode)
			}
			if code := problemCode(t, resp); code != "not_adder" {
				t.Fatalf("expected code not_adder, got %q", code)
			}
		})
	}

	// A missing movie is a genuine 404 for the adder path, not a masked 403.
	resp := doAs(t, app, jsonReq(http.MethodDelete, "/api/v1/movies/99999", ``), owner.ID, "member")
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("missing movie: expected 404, got %d", resp.StatusCode)
	}
}

// Admin-only actions refuse a plain member with 403 admin_required.
func TestAuthz_AdminOnlyActions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, _ := setupEditMovieTest(t)
	victim, err := userRepo.Create(ctx, "Victim")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	cases := []struct {
		name string
		req  *http.Request
	}{
		{"create_member", jsonReq(http.MethodPost, "/api/v1/members", `{"name":"New"}`)},
		{"delete_member", jsonReq(http.MethodDelete, fmt.Sprintf("/api/v1/members/%d", victim.ID), ``)},
		{"set_pool_lock", jsonReq(http.MethodPut, "/api/v1/settings/pool-lock", `{"poolLocked":true}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doAs(t, app, tc.req, victim.ID, "member")
			if resp.StatusCode != fiber.StatusForbidden {
				t.Fatalf("expected 403, got %d", resp.StatusCode)
			}
			if code := problemCode(t, resp); code != "admin_required" {
				t.Fatalf("expected code admin_required, got %q", code)
			}
		})
	}
}

// The draw/reveal/watch cycle is next-up-or-admin: a member who is not up gets
// 403 not_next_up; the member whose turn it is may draw.
func TestAuthz_DrawIsNextUpOrAdmin(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)

	first, err := userRepo.Create(ctx, "First")
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := userRepo.Create(ctx, "Second")
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if _, err := movieRepo.Add(ctx, "Drive", "pool", first.ID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}

	// Get self-seeds next up to the first roster member.
	if up, err := h.nextUpService.Get(ctx); err != nil || up.ID != first.ID {
		t.Fatalf("expected next up = first (%d), got %+v err=%v", first.ID, up, err)
	}

	// The member who is not up is refused.
	resp := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/movies/random", `{"clientId":"c"}`), second.ID, "member")
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("not-up draw: expected 403, got %d", resp.StatusCode)
	}
	if code := problemCode(t, resp); code != "not_next_up" {
		t.Fatalf("expected code not_next_up, got %q", code)
	}

	// The member whose turn it is may draw.
	resp = doAs(t, app, jsonReq(http.MethodPost, "/api/v1/movies/random", `{"clientId":"c"}`), first.ID, "member")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("next-up draw: expected 200, got %d", resp.StatusCode)
	}
}

// Rotation-on-watch (Model B): the turn holds across draw → reveal and only
// passes on watch, so next up == the runner for the whole cycle.
func TestRotation_HoldsAcrossCycleAdvancesOnWatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)

	first, err := userRepo.Create(ctx, "First")
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := userRepo.Create(ctx, "Second")
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	// Two pooled movies so the pool is still non-empty after the watch, which is
	// the condition under which the rotation advances.
	for _, title := range []string{"Drive", "Collateral"} {
		if _, err := movieRepo.Add(ctx, title, "pool", first.ID); err != nil {
			t.Fatalf("seed pool: %v", err)
		}
	}

	nextUpID := func() int {
		up, err := h.nextUpService.Get(ctx)
		if err != nil {
			t.Fatalf("next up: %v", err)
		}
		return up.ID
	}

	if got := nextUpID(); got != first.ID {
		t.Fatalf("before draw: next up = %d, want %d", got, first.ID)
	}

	// Draw as the runner: the turn must NOT move on draw.
	resp := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/movies/random", `{"clientId":"c"}`), first.ID, "member")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("draw: expected 200, got %d", resp.StatusCode)
	}
	if got := nextUpID(); got != first.ID {
		t.Fatalf("after draw: next up = %d, want unchanged %d", got, first.ID)
	}

	// Reveal: still the runner's turn.
	resp = doAs(t, app, httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/reveal", nil), first.ID, "member")
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("reveal: expected 204, got %d", resp.StatusCode)
	}
	if got := nextUpID(); got != first.ID {
		t.Fatalf("after reveal: next up = %d, want unchanged %d", got, first.ID)
	}

	// Watch: the turn passes to the next member.
	resp = doAs(t, app, httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/watch", nil), first.ID, "member")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("watch: expected 200, got %d", resp.StatusCode)
	}
	if got := nextUpID(); got != second.ID {
		t.Fatalf("after watch: next up = %d, want %d", got, second.ID)
	}
}

func TestRotation_WatchOwnsTurnThroughAdvance(t *testing.T) {
	tests := []struct {
		name       string
		request    func() *http.Request
		wantStatus int
	}{
		{
			name: "draw",
			request: func() *http.Request {
				return jsonReq(http.MethodPost, "/api/v1/movies/random", `{"clientId":"stale-runner"}`)
			},
			wantStatus: fiber.StatusForbidden,
		},
		{
			name: "reveal",
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/reveal", nil)
			},
			wantStatus: fiber.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			h, app, userRepo, movieRepo := setupEditMovieTest(t)

			first, err := userRepo.Create(ctx, "First")
			if err != nil {
				t.Fatalf("create first: %v", err)
			}
			second, err := userRepo.Create(ctx, "Second")
			if err != nil {
				t.Fatalf("create second: %v", err)
			}
			for _, title := range []string{"Drive", "Collateral"} {
				if _, err := movieRepo.Add(ctx, title, "pool", first.ID); err != nil {
					t.Fatalf("seed pool: %v", err)
				}
			}
			if up, err := h.nextUpService.Get(ctx); err != nil || up.ID != first.ID {
				t.Fatalf("seed next up: got %+v, err=%v, want member %d", up, err, first.ID)
			}

			// Pause watch after its movie update has committed and its service lock
			// has been released, but before the handler advances next up.
			watchPersisted := make(chan struct{})
			resumeWatch := make(chan struct{})
			resumed := false
			resume := func() {
				if !resumed {
					close(resumeWatch)
					resumed = true
				}
			}
			t.Cleanup(resume)

			h.movieService.Close()
			h.movieService = movie.NewService(movieRepo, movie.DrawConfig{
				OnRevealed: func(movie.ActiveDraw) {
					close(watchPersisted)
					<-resumeWatch
				},
			})

			resp := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/movies/random", `{"clientId":"first"}`), first.ID, "member")
			if resp.StatusCode != fiber.StatusOK {
				t.Fatalf("initial draw: got %d, want 200", resp.StatusCode)
			}

			watchDone := startAs(
				app,
				httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/watch", nil),
				first.ID,
				"member",
			)
			<-watchPersisted

			// TryLock makes the scheduling deterministic. Before the fix, watch
			// does not own the command lock and the stale request can finish while
			// rotation is paused. With the fix, release watch first; the stale
			// request then checks authorization against the rotated turn.
			watchOwnsTurn := !h.movieNightMu.TryLock()
			if !watchOwnsTurn {
				h.movieNightMu.Unlock()
			}

			commandDone := startAs(app, tt.request(), first.ID, "member")
			if watchOwnsTurn {
				resume()
			}
			command := <-commandDone
			if !watchOwnsTurn {
				resume()
			}
			if command.err != nil {
				t.Fatalf("%s request: %v", tt.name, command.err)
			}

			watch := <-watchDone
			if watch.err != nil {
				t.Fatalf("watch request: %v", watch.err)
			}
			if watch.resp.StatusCode != fiber.StatusOK {
				t.Fatalf("watch: got %d, want 200", watch.resp.StatusCode)
			}
			if command.resp.StatusCode != tt.wantStatus {
				t.Fatalf("concurrent old-holder %s: got %d, want %d", tt.name, command.resp.StatusCode, tt.wantStatus)
			}
			if code := problemCode(t, command.resp); code != "not_next_up" {
				t.Fatalf("concurrent old-holder %s: got problem %q, want not_next_up", tt.name, code)
			}

			up, err := h.nextUpService.Get(ctx)
			if err != nil {
				t.Fatalf("next up after watch: %v", err)
			}
			if up.ID != second.ID {
				t.Fatalf("next up after watch = %d, want %d", up.ID, second.ID)
			}
			if current, err := movieRepo.GetCurrent(ctx); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("current after watch = %+v, err=%v, want no current movie", current, err)
			}
			if pooled, err := movieRepo.CountByStatus(ctx, "pool"); err != nil || pooled != 1 {
				t.Fatalf("pool after watch = %d, err=%v, want one remaining movie", pooled, err)
			}
		})
	}
}
