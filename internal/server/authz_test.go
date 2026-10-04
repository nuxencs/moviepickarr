package server

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/movie"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
)

// problemCode decodes the machine `code` (the problem+json title) from a 4xx response.
func problemCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	var p problemDetails
	if err := json.UnmarshalRead(resp.Body, &p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return p.Title
}

// doAs issues req as the given member/role via the test actor headers.
func doAs(t *testing.T, app *fiber.App, req *http.Request, memberID int, role domain.Role) *http.Response {
	t.Helper()
	req.Header.Set(testMemberHeader, strconv.Itoa(memberID))
	if role != "" {
		req.Header.Set(testRoleHeader, string(role))
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

func startAs(app *fiber.App, req *http.Request, memberID int, role domain.Role) <-chan asyncHTTPResult {
	req.Header.Set(testMemberHeader, strconv.Itoa(memberID))
	if role != "" {
		req.Header.Set(testRoleHeader, string(role))
	}

	done := make(chan asyncHTTPResult, 1)
	go func() {
		resp, err := app.Test(req, -1)
		done <- asyncHTTPResult{resp: resp, err: err}
	}()
	return done
}

type pausingWatchMovieStore struct {
	*repository.SqliteMoviesRepository
	reached chan<- struct{}
	resume  <-chan struct{}
}

func (s *pausingWatchMovieStore) WatchCurrentDraw(
	ctx context.Context,
	watchedAt time.Time,
	revealsDraw bool,
) (*domain.Movie, *domain.User, error) {
	close(s.reached)
	<-s.resume
	return s.SqliteMoviesRepository.WatchCurrentDraw(ctx, watchedAt, revealsDraw)
}

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
	imdbID := "tt0113277"
	if err := movieRepo.SetExternalIDs(ctx, movie.ID, nil, &imdbID); err != nil {
		t.Fatalf("set movie identity: %v", err)
	}

	cases := []struct {
		name string
		req  *http.Request
	}{
		{"edit", jsonReq(http.MethodPut, fmt.Sprintf("/api/v1/movies/%d", movie.ID), `{"title":"X","link":"https://www.imdb.com/title/tt0113277/"}`)},
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

func TestAuthz_GuestCannotRunPoolOrHeroCommands(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)
	guest, err := userRepo.Create(ctx, "Guest")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		req  *http.Request
	}{
		{"promote", jsonReq(http.MethodPost, "/api/v1/movies/999/move", `{"target":"pool"}`)},
		{"draw", jsonReq(http.MethodPost, "/api/v1/movies/random", `{"clientId":"guest"}`)},
		{"reveal", jsonReq(http.MethodPost, "/api/v1/movies/current/reveal", ``)},
		{"watch_current", jsonReq(http.MethodPost, "/api/v1/movies/current/watch", ``)},
		{"select_wildcard", jsonReq(http.MethodPost, "/api/v1/movies/wildcard", `{"hostMovieId":1,"movieId":2}`)},
		{"cancel_wildcard", jsonReq(http.MethodDelete, "/api/v1/movies/wildcard?wildcardId=1", ``)},
		{"watch_wildcard", jsonReq(http.MethodPost, "/api/v1/movies/wildcard/watch", `{"wildcardId":1}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doAs(t, app, tc.req, guest.ID, domain.RoleGuest)
			if resp.StatusCode != fiber.StatusForbidden {
				t.Fatalf("status = %d, want 403", resp.StatusCode)
			}
			if code := problemCode(t, resp); code != "guest_restricted" {
				t.Fatalf("problem code = %q, want guest_restricted", code)
			}
		})
	}

	pooled, err := movieRepo.Add(ctx, "Heat", "pool", guest.ID)
	if err != nil {
		t.Fatal(err)
	}
	resp := doAs(t, app,
		jsonReq(http.MethodPost, fmt.Sprintf("/api/v1/movies/%d/move", pooled.ID), `{"target":"stash"}`),
		guest.ID,
		domain.RoleGuest,
	)
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("guest demote status = %d, want 204", resp.StatusCode)
	}

	resp = doAs(t, app,
		jsonReq(http.MethodPost, "/api/v1/movies", `{"title":"Arrival","tmdbId":329865}`),
		guest.ID,
		domain.RoleGuest,
	)
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("guest stash add status = %d, want 201", resp.StatusCode)
	}
}

func TestAuthz_DrawIsNextUpOnly(t *testing.T) {
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

	// A member or admin who is not up is refused. Admins get no exception.
	for _, role := range []domain.Role{domain.RoleMember, domain.RoleAdmin} {
		resp := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/movies/random", `{"clientId":"c"}`), second.ID, role)
		if resp.StatusCode != fiber.StatusForbidden {
			t.Fatalf("not-up %s draw: expected 403, got %d", role, resp.StatusCode)
		}
		if code := problemCode(t, resp); code != "not_next_up" {
			t.Fatalf("not-up %s draw: expected code not_next_up, got %q", role, code)
		}
	}

	resp := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/movies/random", `{"clientId":"c"}`), first.ID, "member")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("next-up draw: expected 200, got %d", resp.StatusCode)
	}
}

// Turn skip is admin-only, names the holder the admin saw, and refuses while
// the drawer still owns an unrevealed draw.
func TestAuthz_SkipNextUpIsAdminOnly(t *testing.T) {
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
	if up, err := h.nextUpService.Get(ctx); err != nil || up.ID != first.ID {
		t.Fatalf("expected next up = first (%d), got %+v err=%v", first.ID, up, err)
	}
	skipReq := func(memberID int) *http.Request {
		return jsonReq(http.MethodPost, "/api/v1/settings/next-up/skip", fmt.Sprintf(`{"memberId":%d}`, memberID))
	}
	expectProblem := func(step string, resp *http.Response, status int, code string) {
		t.Helper()
		if resp.StatusCode != status {
			t.Fatalf("%s: expected %d, got %d", step, status, resp.StatusCode)
		}
		if got := problemCode(t, resp); got != code {
			t.Fatalf("%s: expected code %s, got %q", step, code, got)
		}
	}

	expectProblem("member skip", doAs(t, app, skipReq(first.ID), second.ID, "member"), fiber.StatusForbidden, "admin_required")
	expectProblem("stale skip", doAs(t, app, skipReq(second.ID), second.ID, "admin"), fiber.StatusConflict, "next_up_changed")

	resp := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/movies/random", `{"clientId":"c"}`), first.ID, "member")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("next-up draw: expected 200, got %d", resp.StatusCode)
	}
	expectProblem("skip during reel", doAs(t, app, skipReq(first.ID), second.ID, "admin"), fiber.StatusConflict, "draw_not_revealed")

	resp = doAs(t, app, httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/reveal", nil), first.ID, "member")
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("reveal: expected 204, got %d", resp.StatusCode)
	}
	// Reveal handed the turn to second. The admin skips it back to first.
	resp = doAs(t, app, skipReq(second.ID), second.ID, "admin")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("admin skip: expected 200, got %d", resp.StatusCode)
	}
	if up, err := h.nextUpService.Get(ctx); err != nil || up.ID != first.ID {
		t.Fatalf("after skip: next up = %+v err=%v, want %d", up, err, first.ID)
	}
}

// Rotation-on-reveal: the drawer holds the turn through their own Reveal, then
// the next member marks that draw watched and draws the following one.
func TestRotation_PassesOnRevealNextMemberWatchesThenDraws(t *testing.T) {
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
	expectStatus := func(step string, resp *http.Response, want int) {
		t.Helper()
		if resp.StatusCode != want {
			t.Fatalf("%s: expected %d, got %d", step, want, resp.StatusCode)
		}
	}
	drawReq := func() *http.Request {
		return jsonReq(http.MethodPost, "/api/v1/movies/random", `{"clientId":"c"}`)
	}
	revealReq := func() *http.Request {
		return httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/reveal", nil)
	}
	watchReq := func() *http.Request {
		return httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/watch", nil)
	}

	if got := nextUpID(); got != first.ID {
		t.Fatalf("before draw: next up = %d, want %d", got, first.ID)
	}

	// The turn must not move on draw: the drawer still confirms the Reveal.
	expectStatus("first draw", doAs(t, app, drawReq(), first.ID, "member"), fiber.StatusOK)
	if got := nextUpID(); got != first.ID {
		t.Fatalf("after draw: next up = %d, want unchanged %d", got, first.ID)
	}

	expectStatus("first reveal", doAs(t, app, revealReq(), first.ID, "member"), fiber.StatusNoContent)
	if got := nextUpID(); got != second.ID {
		t.Fatalf("after reveal: next up = %d, want %d", got, second.ID)
	}

	// The drawer's turn is over; the next member marks it watched.
	resp := doAs(t, app, watchReq(), first.ID, "member")
	expectStatus("drawer watch", resp, fiber.StatusForbidden)
	if code := problemCode(t, resp); code != "not_next_up" {
		t.Fatalf("drawer watch: got problem %q, want not_next_up", code)
	}
	expectStatus("second watch", doAs(t, app, watchReq(), second.ID, "member"), fiber.StatusOK)
	if got := nextUpID(); got != second.ID {
		t.Fatalf("after watch: next up = %d, want unchanged %d", got, second.ID)
	}

	expectStatus("second draw", doAs(t, app, drawReq(), second.ID, "member"), fiber.StatusOK)
	if got := nextUpID(); got != second.ID {
		t.Fatalf("after second draw: next up = %d, want unchanged %d", got, second.ID)
	}
}

// Watching an unrevealed draw is its Reveal, so the watch commits the handoff.
// The command lock keeps the outgoing holder out until that commit publishes.
func TestRotation_WatchOwnsTurnThroughCommit(t *testing.T) {
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

			// Pause immediately before the atomic store operation delegates and
			// commits. The command lock must still exclude the outgoing holder.
			watchStoreReached := make(chan struct{})
			resumeWatchStore := make(chan struct{})
			resumed := false
			resume := func() {
				if !resumed {
					close(resumeWatchStore)
					resumed = true
				}
			}
			t.Cleanup(resume)

			h.movieService.Close()
			h.movieService = movie.NewService(&pausingWatchMovieStore{
				SqliteMoviesRepository: movieRepo,
				reached:                watchStoreReached,
				resume:                 resumeWatchStore,
			}, movie.DrawConfig{
				OnRevealed: revealBroadcaster(h.broker),
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
			<-watchStoreReached

			if h.drawCommandMu.TryLock() {
				h.drawCommandMu.Unlock()
				t.Fatal("watch released the command lock before its store commit")
			}

			commandDone := startAs(app, tt.request(), first.ID, "member")
			resume()
			command := <-commandDone
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
