package server

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
	"moviepickarr/internal/movie"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
)

const (
	testMemberHeader = "X-Test-Member"
	testRoleHeader   = "X-Test-Role"
)

// mountTestV1 mounts the v1 routes with the session actor read from test headers
// in place of csrfGuard and requireSession. Role defaults to admin; draw, reveal,
// and watch still need testMemberHeader set to the next-up member.
func mountTestV1(app *fiber.App, h *handler) {
	v1 := app.Group("/api/v1")
	v1.Use(func(c *fiber.Ctx) error {
		if raw := c.Get(testMemberHeader); raw != "" {
			if id, err := strconv.Atoi(raw); err == nil {
				c.Locals(localsMemberID, id)
			}
		}
		role := c.Get(testRoleHeader)
		if role == "" {
			role = "admin"
		}
		c.Locals(localsRole, role)
		return c.Next()
	})
	registerV1Routes(v1, h)
}

func setupEditMovieTest(t *testing.T) (*handler, *fiber.App, *repository.SqliteUserRepository, *repository.SqliteMoviesRepository) {
	t.Helper()

	h, app, userRepo, movieRepo, _ := setupEditMovieTestWithDB(t)
	return h, app, userRepo, movieRepo
}

func setupEditMovieTestWithDB(t *testing.T) (*handler, *fiber.App, *repository.SqliteUserRepository, *repository.SqliteMoviesRepository, *db.Pool) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "moviepickarr-test.db")
	dbConn, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.RunMigrations(ctx, dbConn.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	h := newHandler(dbConn, zerolog.Nop())
	t.Cleanup(func() {
		h.Close()
		if err := dbConn.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})

	app := fiber.New()
	mountTestV1(app, h)

	return h, app, repository.NewSqliteUserRepository(dbConn), repository.NewSqliteMoviesRepository(dbConn), dbConn
}

func TestWildcardLifecycle_AnyMemberPreservesCurrentAndTurn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)
	first, err := userRepo.Create(ctx, "First")
	if err != nil {
		t.Fatalf("create first member: %v", err)
	}
	second, err := userRepo.Create(ctx, "Second")
	if err != nil {
		t.Fatalf("create second member: %v", err)
	}
	for _, title := range []string{"Current candidate", "Next candidate"} {
		if _, err := movieRepo.Add(ctx, title, "pool", first.ID); err != nil {
			t.Fatalf("seed pool: %v", err)
		}
	}

	drawResp := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/movies/random", `{"clientId":"first"}`), first.ID, "member")
	if drawResp.StatusCode != fiber.StatusOK {
		t.Fatalf("draw status = %d, want 200", drawResp.StatusCode)
	}
	var drawn drawnPayload
	if err := json.UnmarshalRead(drawResp.Body, &drawn); err != nil {
		t.Fatalf("decode draw: %v", err)
	}
	if err := drawResp.Body.Close(); err != nil {
		t.Fatalf("close draw response: %v", err)
	}

	revealResp := doAs(t, app, httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/reveal", nil), first.ID, "member")
	if revealResp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("reveal status = %d, want 204", revealResp.StatusCode)
	}
	if err := revealResp.Body.Close(); err != nil {
		t.Fatalf("close reveal response: %v", err)
	}

	// Selection is a group action: a member who is not next may pick any TMDB title.
	selectResp := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/movies/wildcard", fmt.Sprintf(`{"hostMovieId":%d,"title":"Guest night","tmdbId":998877}`, drawn.ID)), second.ID, "member")
	if selectResp.StatusCode != fiber.StatusCreated {
		t.Fatalf("select wildcard status = %d, want 201", selectResp.StatusCode)
	}
	var selected wildcardResponse
	if err := json.UnmarshalRead(selectResp.Body, &selected); err != nil {
		t.Fatalf("decode selected wildcard: %v", err)
	}
	if err := selectResp.Body.Close(); err != nil {
		t.Fatalf("close select response: %v", err)
	}
	if selected.HostMovieID != drawn.ID || selected.Movie.Status != domain.MovieStatusWildcard || selected.Movie.AddedByID != second.ID {
		t.Fatalf("selected wildcard = %+v", selected)
	}

	watchResp := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/movies/wildcard/watch", fmt.Sprintf(`{"wildcardId":%d}`, selected.ID)), second.ID, "member")
	if watchResp.StatusCode != fiber.StatusOK {
		t.Fatalf("watch wildcard status = %d, want 200", watchResp.StatusCode)
	}
	var watched wildcardResponse
	if err := json.UnmarshalRead(watchResp.Body, &watched); err != nil {
		t.Fatalf("decode watched wildcard: %v", err)
	}
	if err := watchResp.Body.Close(); err != nil {
		t.Fatalf("close watch response: %v", err)
	}
	if watched.Movie.Status != domain.MovieStatusWatched || watched.Movie.WildcardOfMovieID != drawn.ID {
		t.Fatalf("watched wildcard movie = %+v", watched.Movie)
	}

	current, err := movieRepo.GetCurrent(ctx)
	if err != nil || current.ID != drawn.ID {
		t.Fatalf("current after wildcard = %+v, err=%v, want %d", current, err, drawn.ID)
	}
	// The Reveal already passed the turn to Second; the Wildcard watch keeps it.
	next, err := h.nextUpService.Get(ctx)
	if err != nil || next.ID != second.ID {
		t.Fatalf("next up after wildcard = %+v, err=%v, want %d", next, err, second.ID)
	}

	getResp := doAs(t, app, httptest.NewRequest(http.MethodGet, "/api/v1/movies/wildcard", nil), first.ID, "member")
	if getResp.StatusCode != fiber.StatusOK {
		t.Fatalf("get wildcard status = %d, want 200", getResp.StatusCode)
	}
	var active *wildcardResponse
	if err := json.UnmarshalRead(getResp.Body, &active); err != nil {
		t.Fatalf("decode empty wildcard: %v", err)
	}
	if err := getResp.Body.Close(); err != nil {
		t.Fatalf("close get response: %v", err)
	}
	if active != nil {
		t.Fatalf("active wildcard after watch = %+v, want null", active)
	}

	stashed, err := movieRepo.Add(ctx, "Cancel me", "stash", second.ID)
	if err != nil {
		t.Fatalf("seed stash: %v", err)
	}
	selectResp = doAs(t, app, jsonReq(http.MethodPost, "/api/v1/movies/wildcard", fmt.Sprintf(`{"hostMovieId":%d,"movieId":%d}`, drawn.ID, stashed.ID)), second.ID, "member")
	if selectResp.StatusCode != fiber.StatusCreated {
		t.Fatalf("select stashed wildcard status = %d, want 201", selectResp.StatusCode)
	}
	var cancelTarget wildcardResponse
	if err := json.UnmarshalRead(selectResp.Body, &cancelTarget); err != nil {
		t.Fatalf("decode cancel target: %v", err)
	}
	if err := selectResp.Body.Close(); err != nil {
		t.Fatalf("close select response: %v", err)
	}
	cancelResp := doAs(t, app, httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/movies/wildcard?wildcardId=%d", cancelTarget.ID), nil), first.ID, "member")
	if cancelResp.StatusCode != fiber.StatusOK {
		t.Fatalf("cancel wildcard status = %d code=%q, want 200", cancelResp.StatusCode, problemCode(t, cancelResp))
	}
	if err := cancelResp.Body.Close(); err != nil {
		t.Fatalf("close cancel response: %v", err)
	}
	restored, err := movieRepo.FindByID(ctx, stashed.ID)
	if err != nil || restored.Status != string(domain.MovieStatusStash) {
		t.Fatalf("canceled wildcard movie = %+v, err=%v, want stash", restored, err)
	}
}

func TestHandleGetPooledMovies_ShipsLeanTiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)
	metaRepo := repository.NewSqliteMovieMetadataRepository(dbConn)
	creditsRepo := repository.NewSqliteMovieCreditsRepository(dbConn)

	user, err := userRepo.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	pooled, err := movieRepo.Add(ctx, "The Matrix", "pool", user.ID)
	if err != nil {
		t.Fatalf("create pooled movie: %v", err)
	}

	poster, backdrop := "/poster.jpg", "/backdrop.jpg"
	if err := metaRepo.UpsertMetadata(ctx, domain.MovieMetadata{
		MovieID:      pooled.ID,
		PosterPath:   &poster,
		BackdropPath: &backdrop,
		Runtime:      136,
		VoteAverage:  8.2,
		Tagline:      "Free your mind.",
		Overview:     "A hacker learns the truth.",
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	if err := creditsRepo.ReplaceCredits(ctx, pooled.ID, []domain.MovieCredit{{
		MovieID: pooled.ID,
		Person:  domain.Person{ID: 9340, Name: "Lana Wachowski"},
		Kind:    domain.CreditKindCrew,
		Job:     "Director",
	}}); err != nil {
		t.Fatalf("seed credits: %v", err)
	}

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/movies/pool", nil), -1)
	if err != nil {
		t.Fatalf("get pooled movies: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("get pooled movies status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	var payload []map[string]jsontext.Value
	if err := json.UnmarshalRead(resp.Body, &payload); err != nil {
		t.Fatalf("decode pooled movies: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("pooled movie count = %d, want 1", len(payload))
	}
	for _, key := range []string{"movieID", "title", "posterPath", "runtime", "voteAverage"} {
		if _, ok := payload[0][key]; !ok {
			t.Errorf("pooled tile missing %q: %v", key, payload[0])
		}
	}
	for _, key := range []string{"status", "backdropPath", "tagline", "overview", "cast", "crew"} {
		if _, ok := payload[0][key]; ok {
			t.Errorf("pooled tile includes detail field %q: %v", key, payload[0])
		}
	}
}

// postMove moves a movie with userID as the session member.
func postMove(t *testing.T, app *fiber.App, userID, movieID int, target string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/movies/%d/move", movieID),
		strings.NewReader(fmt.Sprintf(`{"target":%q}`, target)),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(userID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

type failMoveProjectionStore struct {
	*repository.SqliteMoviesRepository
	failReads bool
}

func (s *failMoveProjectionStore) PromoteToPoolIfRoom(
	ctx context.Context,
	id, maxPool int,
) (int64, error) {
	changed, err := s.SqliteMoviesRepository.PromoteToPoolIfRoom(ctx, id, maxPool)
	if err == nil && changed == 1 {
		s.failReads = true
	}
	return changed, err
}

func (s *failMoveProjectionStore) FindByUserIDAndStatus(
	ctx context.Context,
	userID int,
	status string,
) ([]*domain.Movie, error) {
	if s.failReads {
		return nil, errors.New("forced post-commit projection failure")
	}
	return s.SqliteMoviesRepository.FindByUserIDAndStatus(ctx, userID, status)
}

func movieStatus(t *testing.T, ctx context.Context, movieRepo *repository.SqliteMoviesRepository, id int) string {
	t.Helper()

	m, err := movieRepo.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("fetch movie: %v", err)
	}
	return m.Status
}

// A blind toggle let a duplicate click move the movie back; a move to the
// current location must be a no-op.
func TestHandleMove_DirectionalIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Cara")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Heat", "stash", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}

	if resp := postMove(t, app, user.ID, movie.ID, "pool"); resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("first promote: expected 204, got %d", resp.StatusCode)
	}
	if got := movieStatus(t, ctx, movieRepo, movie.ID); got != "pool" {
		t.Fatalf("after first promote: expected pool, got %q", got)
	}

	if resp := postMove(t, app, user.ID, movie.ID, "pool"); resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("duplicate promote: expected 204, got %d", resp.StatusCode)
	}
	if got := movieStatus(t, ctx, movieRepo, movie.ID); got != "pool" {
		t.Fatalf("after duplicate promote: expected pool (no revert), got %q", got)
	}

	if resp := postMove(t, app, user.ID, movie.ID, "stash"); resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("demote: expected 204, got %d", resp.StatusCode)
	}
	if got := movieStatus(t, ctx, movieRepo, movie.ID); got != "stash" {
		t.Fatalf("after demote: expected stash, got %q", got)
	}
}

func TestHandleMove_RejectsMissingTarget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Dev")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Drive", "stash", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/movies/%d/move", movie.ID),
		strings.NewReader(`{}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for missing target, got %d", resp.StatusCode)
	}
	if got := movieStatus(t, ctx, movieRepo, movie.ID); got != "stash" {
		t.Fatalf("movie should be unchanged, got %q", got)
	}
}

// The client pending-guard cannot block a double-click before React re-renders.
// No duplicate may get a 409 (the movie counted against its own promotion) or a
// 400 (ErrInvalidState from a lost read-modify-write race).
func TestHandleMove_ConcurrentDuplicatePromote_NoSpuriousError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Eve")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	// Fill two of three pool slots so the duplicates also hit the pool-limit check.
	if _, err := movieRepo.Add(ctx, "Alien", "pool", user.ID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	if _, err := movieRepo.Add(ctx, "Aliens", "pool", user.ID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Predator", "stash", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}

	const clicks = 12
	results := make([]struct {
		status int
		err    error
	}, clicks)
	var wg sync.WaitGroup
	for i := range clicks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/api/v1/movies/%d/move", movie.ID),
				strings.NewReader(`{"target":"pool"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))
			resp, err := app.Test(req, -1)
			if err != nil {
				results[i].err = err
				return
			}
			results[i].status = resp.StatusCode
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			t.Fatalf("click %d: app.Test: %v", i, r.err)
		}
		if r.status < 200 || r.status >= 300 {
			t.Fatalf("click %d: expected 2xx (idempotent), got %d", i, r.status)
		}
	}
	if got := movieStatus(t, ctx, movieRepo, movie.ID); got != "pool" {
		t.Fatalf("after concurrent promotes: expected pool, got %q", got)
	}
	pooled, err := movieRepo.FindByUserIDAndStatus(ctx, user.ID, "pool")
	if err != nil {
		t.Fatalf("fetch pool: %v", err)
	}
	if len(pooled) != 3 {
		t.Fatalf("expected pool of 3 after concurrent promotes, got %d", len(pooled))
	}
}

// countMovedEvents counts "movie:moved" frames until the channel is quiet for `within`.
func countMovedEvents(client chan event, within time.Duration) int {
	count := 0
	for {
		select {
		case e, ok := <-client:
			if !ok {
				return count
			}
			if e.Type == "movie:moved" {
				count++
			}
		case <-time.After(within):
			return count
		}
	}
}

// A no-op broadcast would turn every redundant click into an invalidation storm.
func TestHandleMove_NoOpDuplicateSuppressesBroadcast(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Faye")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Fargo", "stash", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}

	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	if resp := postMove(t, app, user.ID, movie.ID, "pool"); resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("promote: expected 204, got %d", resp.StatusCode)
	}
	if got := countMovedEvents(client, 100*time.Millisecond); got != 1 {
		t.Fatalf("real move: expected 1 movie:moved broadcast, got %d", got)
	}

	if resp := postMove(t, app, user.ID, movie.ID, "pool"); resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("duplicate promote: expected 204, got %d", resp.StatusCode)
	}
	if got := countMovedEvents(client, 100*time.Millisecond); got != 0 {
		t.Fatalf("no-op duplicate: expected 0 broadcasts (suppression), got %d", got)
	}
}

func TestHandleMove_PublishesCommittedMoveWithoutResponseProjection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)
	h.movieService.Close()
	h.movieService = movie.NewService(&failMoveProjectionStore{
		SqliteMoviesRepository: movieRepo,
	}, movie.DrawConfig{})

	user, err := userRepo.Create(ctx, "Projection failure owner")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movieRecord, err := movieRepo.Add(ctx, "Committed move", "stash", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}

	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	resp := postMove(t, app, user.ID, movieRecord.ID, "pool")
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("move: expected 204, got %d", resp.StatusCode)
	}
	if got := movieStatus(t, ctx, movieRepo, movieRecord.ID); got != "pool" {
		t.Fatalf("committed move status = %q, want pool", got)
	}
	if got := countMovedEvents(client, 100*time.Millisecond); got != 1 {
		t.Fatalf("committed move: expected 1 movie:moved broadcast, got %d", got)
	}
}

// The pool-count check shares the status flip's atomic UPDATE, so with one free
// slot exactly one promote wins and the rest get 409.
func TestHandleMove_ConcurrentDistinctPromotes_RespectPoolCap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Gus")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := movieRepo.Add(ctx, "Fargo", "pool", user.ID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	if _, err := movieRepo.Add(ctx, "Brick", "pool", user.ID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}

	const contenders = 4
	ids := make([]int, contenders)
	for i := range contenders {
		m, err := movieRepo.Add(ctx, fmt.Sprintf("Contender %d", i), "stash", user.ID)
		if err != nil {
			t.Fatalf("seed contender %d: %v", i, err)
		}
		ids[i] = m.ID
	}

	results := make([]struct {
		status int
		err    error
	}, contenders)
	var wg sync.WaitGroup
	for i := range contenders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/api/v1/movies/%d/move", ids[i]),
				strings.NewReader(`{"target":"pool"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))
			resp, err := app.Test(req, -1)
			if err != nil {
				results[i].err = err
				return
			}
			results[i].status = resp.StatusCode
		}(i)
	}
	wg.Wait()

	var promoted, rejected int
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("contender %d: app.Test: %v", i, r.err)
		}
		switch r.status {
		case fiber.StatusNoContent:
			promoted++
		case fiber.StatusConflict:
			rejected++
		default:
			t.Fatalf("contender %d: expected 204 or 409, got %d", i, r.status)
		}
	}
	if promoted != 1 {
		t.Fatalf("expected exactly 1 promotion to win the free slot, got %d", promoted)
	}
	if rejected != contenders-1 {
		t.Fatalf("expected %d pool-limit rejections, got %d", contenders-1, rejected)
	}

	pooled, err := movieRepo.FindByUserIDAndStatus(ctx, user.ID, "pool")
	if err != nil {
		t.Fatalf("fetch pool: %v", err)
	}
	if len(pooled) != 3 {
		t.Fatalf("pool cap breached: expected 3, got %d", len(pooled))
	}
}

// No demote may fail with the lost-race ErrInvalidState.
func TestHandleMove_ConcurrentDemoteIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Hank")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Holes", "pool", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}

	const clicks = 8
	results := make([]struct {
		status int
		err    error
	}, clicks)
	var wg sync.WaitGroup
	for i := range clicks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/api/v1/movies/%d/move", movie.ID),
				strings.NewReader(`{"target":"stash"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))
			resp, err := app.Test(req, -1)
			if err != nil {
				results[i].err = err
				return
			}
			results[i].status = resp.StatusCode
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			t.Fatalf("click %d: app.Test: %v", i, r.err)
		}
		if r.status < 200 || r.status >= 300 {
			t.Fatalf("click %d: expected 2xx (idempotent demote), got %d", i, r.status)
		}
	}
	if got := movieStatus(t, ctx, movieRepo, movie.ID); got != "stash" {
		t.Fatalf("after concurrent demotes: expected stash, got %q", got)
	}
}

// The button says "Add to <user>'s stash", so an add must never auto-promote.
func TestHandleAddMovie_LandsInStashEvenWithPoolRoom(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Casey")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/movies",
		strings.NewReader(`{"title":"Dune","tmdbId":438631}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("expected status 201, got %d", resp.StatusCode)
	}

	pooled, err := movieRepo.FindByUserIDAndStatus(ctx, user.ID, "pool")
	if err != nil {
		t.Fatalf("fetch pool: %v", err)
	}
	if len(pooled) != 0 {
		t.Fatalf("expected empty pool after add, got %d pooled", len(pooled))
	}

	stashed, err := movieRepo.FindByUserIDAndStatus(ctx, user.ID, "stash")
	if err != nil {
		t.Fatalf("fetch stash: %v", err)
	}
	if len(stashed) != 1 || stashed[0].Title != "Dune" {
		t.Fatalf("expected Dune stashed, got %#v", stashed)
	}
}

// A failed cleanup DELETE must not leave an identityless stash row behind a 409.
func TestHandleAddMovie_DuplicateIdentityDoesNotLeaveOrphanWhenCleanupFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)

	user, err := userRepo.Create(ctx, "Casey")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	existing, err := movieRepo.Add(ctx, "Dune", "stash", user.ID)
	if err != nil {
		t.Fatalf("create existing movie: %v", err)
	}
	tmdbID := 438631
	if err := movieRepo.SetExternalIDs(ctx, existing.ID, &tmdbID, nil); err != nil {
		t.Fatalf("set existing identity: %v", err)
	}

	if _, err := dbConn.Write.ExecContext(ctx, `
		CREATE TRIGGER reject_movie_cleanup
		BEFORE DELETE ON movies
		BEGIN
			SELECT RAISE(ABORT, 'injected cleanup failure');
		END
	`); err != nil {
		t.Fatalf("install cleanup failure: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/movies",
		strings.NewReader(`{"title":"Dune duplicate","tmdbId":438631}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("expected status 409, got %d", resp.StatusCode)
	}

	var total, identityless int
	if err := dbConn.Read.QueryRowContext(ctx, "SELECT COUNT(*) FROM movies").Scan(&total); err != nil {
		t.Fatalf("count movies: %v", err)
	}
	if err := dbConn.Read.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM movies WHERE tmdb_id IS NULL AND imdb_id IS NULL",
	).Scan(&identityless); err != nil {
		t.Fatalf("count identityless movies: %v", err)
	}
	if total != 1 || identityless != 0 {
		t.Fatalf("movies after duplicate = %d total, %d identityless; want 1 total, 0 identityless", total, identityless)
	}
}

// No event may claim an add whose response read failed.
func TestHandleAddMovie_ResponseReadFailureRollsBackInsert(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, _, dbConn := setupEditMovieTestWithDB(t)

	user, err := userRepo.Create(ctx, "Reader")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := dbConn.Write.ExecContext(ctx, `
		CREATE TABLE movie_add_read_failures (movie_id INTEGER NOT NULL);
		CREATE TRIGGER fail_movie_add_response_read
		AFTER INSERT ON movies
		WHEN NEW.title = 'Unreadable add'
		BEGIN
			INSERT INTO movie_add_read_failures (movie_id) VALUES (NEW.id);
			DELETE FROM movies WHERE id = NEW.id;
		END
	`); err != nil {
		t.Fatalf("install response-read failure: %v", err)
	}

	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/movies",
		strings.NewReader(`{"title":"Unreadable add","tmdbId":438632}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}

	var movies, markers int
	if err := dbConn.Read.QueryRowContext(ctx, "SELECT COUNT(*) FROM movies").Scan(&movies); err != nil {
		t.Fatalf("count movies: %v", err)
	}
	if err := dbConn.Read.QueryRowContext(ctx, "SELECT COUNT(*) FROM movie_add_read_failures").Scan(&markers); err != nil {
		t.Fatalf("count failure markers: %v", err)
	}
	if movies != 0 || markers != 0 {
		t.Fatalf("failed add left %d movies and %d failure markers; want 0 and 0", movies, markers)
	}

	select {
	case got := <-client:
		t.Fatalf("failed add broadcast event %q", got.Type)
	default:
	}
}

func TestHandleDeleteMovie_PoolLock(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Dana")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	pooled, err := movieRepo.Add(ctx, "Locked In", "pool", user.ID)
	if err != nil {
		t.Fatalf("create pooled movie: %v", err)
	}
	stashed, err := movieRepo.Add(ctx, "Just Parked", "stash", user.ID)
	if err != nil {
		t.Fatalf("create stashed movie: %v", err)
	}

	if err := h.settingsService.SetPoolLock(ctx, true); err != nil {
		t.Fatalf("lock the pool: %v", err)
	}

	del := func(id int) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/movies/%d", id), nil)
		req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		return resp.StatusCode
	}

	if status := del(pooled.ID); status != fiber.StatusForbidden {
		t.Fatalf("delete pooled movie while locked = %d, want 403", status)
	}
	if _, err := movieRepo.FindByID(ctx, pooled.ID); err != nil {
		t.Fatalf("expected the pooled movie to survive the refusal: %v", err)
	}

	// The lock is the pool's, not a member's list's.
	if status := del(stashed.ID); status != fiber.StatusNoContent {
		t.Fatalf("delete stashed movie while locked = %d, want 204", status)
	}

	if err := h.settingsService.SetPoolLock(ctx, false); err != nil {
		t.Fatalf("unlock the pool: %v", err)
	}
	if status := del(pooled.ID); status != fiber.StatusNoContent {
		t.Fatalf("delete pooled movie while unlocked = %d, want 204", status)
	}
}
