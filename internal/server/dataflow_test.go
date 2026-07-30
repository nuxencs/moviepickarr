package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/movie"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
	fiberrecover "github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/rs/zerolog"
)

// Delta 1: every broadcast gets a monotonic seq starting at 1, and Subscribe
// reports the current head so a client can align its gap-detection cursor.
func TestEventBroker_AssignsMonotonicSeq(t *testing.T) {
	broker := newEventBroker()
	client, head := broker.Subscribe()
	defer broker.Unsubscribe(client)

	if head != 0 {
		t.Fatalf("fresh broker head seq = %d, want 0", head)
	}
	if broker.Epoch() == "" {
		t.Fatal("broker epoch is empty")
	}

	broker.Broadcast(event{Type: "movie:added"})
	broker.Broadcast(event{Type: "movie:updated"})

	first := <-client
	second := <-client
	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("seq not monotonic from 1: got %d then %d", first.Seq, second.Seq)
	}
	if broker.HeadSeq() != 2 {
		t.Fatalf("HeadSeq() = %d, want 2", broker.HeadSeq())
	}
}

// Delta 1: a late subscriber's returned head equals the last assigned seq, and
// the next event it receives is head+1 — so it never reads a spurious gap for
// events that happened before it connected.
func TestEventBroker_SubscribeReturnsCurrentHead(t *testing.T) {
	broker := newEventBroker()
	c1, _ := broker.Subscribe()
	defer broker.Unsubscribe(c1)

	broker.Broadcast(event{Type: "a"})
	broker.Broadcast(event{Type: "b"})

	c2, head := broker.Subscribe()
	defer broker.Unsubscribe(c2)
	if head != 2 {
		t.Fatalf("late subscriber head = %d, want 2", head)
	}

	broker.Broadcast(event{Type: "c"})
	got := <-c2
	if got.Seq != head+1 {
		t.Fatalf("first event after subscribe seq = %d, want head+1 = %d", got.Seq, head+1)
	}
}

// An archived member keeps movie attribution but has no active Members board.
// The movie payload must carry that distinction so clients do not link the
// adder's name to a dead id that silently resolves to somebody else's board.
func TestMoviePayload_MarksArchivedAdder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	adder, err := userRepo.Create(ctx, "Gwen")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	added, err := movieRepo.Add(ctx, "Heat", "stash", adder.ID)
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}
	outcome, err := userRepo.Remove(ctx, adder.ID)
	if err != nil {
		t.Fatalf("archive user: %v", err)
	}
	if outcome != domain.OutcomeArchived {
		t.Fatalf("remove outcome = %q, want archived", outcome)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/movies/"+strconv.Itoa(added.ID), nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if archived, ok := body["addedByArchived"].(bool); !ok || !archived {
		t.Fatalf("addedByArchived = %#v, want true", body["addedByArchived"])
	}
}

// Delta 2: a draw carries self-contained reel candidates (the pre-draw pool,
// winner included) on BOTH the HTTP response and the movie:drawn broadcast, so
// every client renders the full reel without consulting its local pool cache.
func TestHandleGetRandomMovie_CarriesSelfContainedCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Gwen")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	for _, title := range []string{"Heat", "Casino", "Goodfellas"} {
		if _, err := movieRepo.Add(ctx, title, "pool", user.ID); err != nil {
			t.Fatalf("seed pool: %v", err)
		}
	}

	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/movies/random", strings.NewReader(`{"clientId":"c-test"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		MovieID    int    `json:"movieID"`
		DrawnAt    string `json:"drawnAt"`
		RevealAt   string `json:"revealAt"`
		Candidates []struct {
			MovieID    int    `json:"movieID"`
			PosterPath string `json:"posterPath"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// The server owns the reveal timing: the payload carries the auto-reveal
	// deadline so clients derive the confirm countdown from it.
	drawnAt, err := time.Parse(time.RFC3339, body.DrawnAt)
	if err != nil {
		t.Fatalf("drawnAt not RFC3339: %q (%v)", body.DrawnAt, err)
	}
	revealAt, err := time.Parse(time.RFC3339, body.RevealAt)
	if err != nil {
		t.Fatalf("revealAt not RFC3339: %q (%v)", body.RevealAt, err)
	}
	if !revealAt.After(drawnAt) {
		t.Fatalf("revealAt %v not after drawnAt %v", revealAt, drawnAt)
	}
	if len(body.Candidates) != 3 {
		t.Fatalf("expected 3 reel candidates (the pre-draw pool), got %d", len(body.Candidates))
	}
	winnerInCandidates := false
	for _, c := range body.Candidates {
		if c.MovieID == body.MovieID {
			winnerInCandidates = true
		}
	}
	if !winnerInCandidates {
		t.Fatalf("winner %d not present in reel candidates", body.MovieID)
	}

	// The broadcast carries the same self-contained payload, with a seq assigned.
	select {
	case e := <-client:
		if e.Type != "movie:drawn" {
			t.Fatalf("expected movie:drawn broadcast, got %q", e.Type)
		}
		if e.Seq == 0 {
			t.Fatal("broadcast seq was not assigned")
		}
		pp, ok := e.Data.(drawnPayload)
		if !ok {
			t.Fatalf("event data is %T, want drawnPayload", e.Data)
		}
		if len(pp.Candidates) != 3 {
			t.Fatalf("broadcast candidates = %d, want 3", len(pp.Candidates))
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("no movie:drawn broadcast received")
	}
}

type countingDrawRepository struct {
	*repository.SqliteMoviesRepository
	poolReads       int
	repositoryCalls int
}

func (r *countingDrawRepository) FindByStatus(
	ctx context.Context,
	status string,
) ([]*domain.Movie, error) {
	r.repositoryCalls++
	if status == string(domain.MovieStatusPool) {
		r.poolReads++
	}
	return r.SqliteMoviesRepository.FindByStatus(ctx, status)
}

func (r *countingDrawRepository) FindByID(ctx context.Context, id int) (*domain.Movie, error) {
	r.repositoryCalls++
	return r.SqliteMoviesRepository.FindByID(ctx, id)
}

func (r *countingDrawRepository) GetCurrent(ctx context.Context) (*domain.Movie, error) {
	r.repositoryCalls++
	return r.SqliteMoviesRepository.GetCurrent(ctx)
}

func (r *countingDrawRepository) UpdateStatus(ctx context.Context, id int, status string) error {
	r.repositoryCalls++
	return r.SqliteMoviesRepository.UpdateStatus(ctx, id, status)
}

type countingDrawMetadataRepository struct {
	domain.MovieMetadataRepo
	repositoryCalls *int
}

func (r *countingDrawMetadataRepository) GetMetadataByMovieIDs(
	ctx context.Context,
	ids []int,
) (map[int]*domain.MovieMetadata, error) {
	(*r.repositoryCalls)++
	return r.MovieMetadataRepo.GetMetadataByMovieIDs(ctx, ids)
}

// The draw service already reads the complete eligible pool before choosing a
// winner. Candidate construction must reuse that snapshot instead of issuing a
// second pool query after the draw lock has been released.
func TestHandleGetRandomMovie_ReadsCandidatePoolOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)
	countingRepo := &countingDrawRepository{SqliteMoviesRepository: movieRepo}
	h.movieService.Close()
	h.movieService = movie.NewService(countingRepo, movie.DrawConfig{
		OnRevealed: revealBroadcaster(h.broker),
	})
	h.movieMetadata = &countingDrawMetadataRepository{
		MovieMetadataRepo: h.movieMetadata,
		repositoryCalls:   &countingRepo.repositoryCalls,
	}

	user, err := userRepo.Create(ctx, "Gwen")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	for _, title := range []string{"Heat", "Casino", "Goodfellas"} {
		if _, err := movieRepo.Add(ctx, title, "pool", user.ID); err != nil {
			t.Fatalf("seed pool: %v", err)
		}
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/movies/random",
		strings.NewReader(`{"clientId":"c-test"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if countingRepo.poolReads != 1 {
		t.Fatalf("pool reads = %d, want 1", countingRepo.poolReads)
	}
	if countingRepo.repositoryCalls != 4 {
		t.Fatalf("draw repository calls = %d, want 4", countingRepo.repositoryCalls)
	}
}

// The confirm bar counts down to revealAt, and it can start at any point in the
// draw (Skip lands the reel early, a tab switch can mount it late). So the draw
// payload has to say when the server stamped it: the client anchors the
// deadline as serverNow -> revealAt measured against its own arrival time.
// drawnAt can't serve, being second-truncated and already a round-trip stale.
func TestHandleGetRandomMovie_StampsServerNowForTheConfirmDeadline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Gwen")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	for _, title := range []string{"Heat", "Casino"} {
		if _, err := movieRepo.Add(ctx, title, "pool", user.ID); err != nil {
			t.Fatalf("seed pool: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/movies/random", strings.NewReader(`{"clientId":"c-test"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	var body struct {
		RevealAt  string `json:"revealAt"`
		ServerNow string `json:"serverNow"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	serverNow, err := time.Parse(time.RFC3339Nano, body.ServerNow)
	if err != nil {
		t.Fatalf("serverNow not RFC3339: %q (%v)", body.ServerNow, err)
	}
	// Sub-second precision, or the countdown jitters by up to a second.
	if serverNow.Nanosecond() == 0 {
		t.Fatalf("serverNow %q looks truncated to the second", body.ServerNow)
	}
	revealAt, err := time.Parse(time.RFC3339Nano, body.RevealAt)
	if err != nil {
		t.Fatalf("revealAt not RFC3339: %q (%v)", body.RevealAt, err)
	}
	left := revealAt.Sub(serverNow)
	if left <= 0 || left > movie.DefaultAutoRevealDelay {
		t.Fatalf("revealAt - serverNow = %v, want within (0, %v]", left, movie.DefaultAutoRevealDelay)
	}
}

// countEvents drains a broker client for frames of the given type until the
// channel goes quiet for `within`.
func countEvents(client chan event, typ string, within time.Duration) int {
	n := 0
	for {
		select {
		case e, ok := <-client:
			if !ok {
				return n
			}
			if e.Type == typ {
				n++
			}
		case <-time.After(within):
			return n
		}
	}
}

func seedPoolAndDraw(t *testing.T, app *fiber.App, movieRepo *repository.SqliteMoviesRepository, userID int, titles ...string) {
	t.Helper()
	ctx := context.Background()
	for _, title := range titles {
		if _, err := movieRepo.Add(ctx, title, "pool", userID); err != nil {
			t.Fatalf("seed pool: %v", err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/movies/random", strings.NewReader(`{"clientId":"c-test"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("draw: err=%v status=%v", err, resp.StatusCode)
	}
}

type injectedAutoRevealTimer struct {
	started chan func()
	starts  int
	onStart func()
}

func (t *injectedAutoRevealTimer) start(_ time.Duration, fn func()) func() {
	t.starts++
	if t.onStart != nil {
		t.onStart()
	}
	t.started <- fn
	return func() {}
}

type panickingBatchMetadataRepo struct {
	domain.MovieMetadataRepo
}

func (r *panickingBatchMetadataRepo) GetMetadataByMovieIDs(
	context.Context,
	[]int,
) (map[int]*domain.MovieMetadata, error) {
	panic("injected metadata panic")
}

func TestHandleGetRandomMovie_PublishesBeforeAutoRevealCanFire(t *testing.T) {
	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Iris")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	for _, title := range []string{"Drive", "Collateral"} {
		if _, err := movieRepo.Add(ctx, title, "pool", user.ID); err != nil {
			t.Fatalf("seed pool: %v", err)
		}
	}

	timer := &injectedAutoRevealTimer{started: make(chan func(), 1)}
	h.movieService.Close()
	h.movieService = movie.NewService(movieRepo, movie.DrawConfig{
		AutoRevealDelay: time.Second,
		StartTimer:      timer.start,
		OnRevealed:      revealBroadcaster(h.broker),
	})

	metadataReached := make(chan struct{})
	resumeMetadata := make(chan struct{})
	resumed := false
	resume := func() {
		if !resumed {
			close(resumeMetadata)
			resumed = true
		}
	}
	t.Cleanup(resume)
	h.movieMetadata = &pausingBatchMetadataRepo{
		MovieMetadataRepo: h.movieMetadata,
		reached:           metadataReached,
		resume:            resumeMetadata,
	}

	client, _ := h.broker.Subscribe()
	t.Cleanup(func() { h.broker.Unsubscribe(client) })

	drawDone := startAs(
		app,
		jsonReq(http.MethodPost, "/api/v1/movies/random", `{"clientId":"first"}`),
		user.ID,
		"member",
	)
	<-metadataReached

	// The injected timer makes either implementation deterministic. Before the
	// fix it has already been armed, so fire it while publication is paused. The
	// fixed path arms only after movie:drawn, so release publication first.
	var fire func()
	select {
	case fire = <-timer.started:
		fire()
	default:
	}

	publicationResumedAt := time.Now().UTC()
	resume()
	draw := <-drawDone
	if draw.err != nil || draw.resp.StatusCode != fiber.StatusOK {
		t.Fatalf("draw: err=%v status=%v", draw.err, draw.resp.StatusCode)
	}
	if fire == nil {
		fire = <-timer.started
		fire()
	}

	var body struct {
		MovieID    int    `json:"movieID"`
		ServerNow  string `json:"serverNow"`
		Candidates []struct {
			MovieID int `json:"movieID"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(draw.resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode draw response: %v", err)
	}
	serverNow, err := time.Parse(time.RFC3339Nano, body.ServerNow)
	if err != nil {
		t.Fatalf("parse serverNow %q: %v", body.ServerNow, err)
	}
	if serverNow.Before(publicationResumedAt) {
		t.Fatalf("serverNow = %v, want at or after candidate I/O resumed at %v", serverNow, publicationResumedAt)
	}
	winnerIncluded := false
	for _, candidate := range body.Candidates {
		if candidate.MovieID == body.MovieID {
			winnerIncluded = true
			break
		}
	}
	if !winnerIncluded {
		t.Fatalf("draw candidates = %+v, want winner %d", body.Candidates, body.MovieID)
	}

	var lifecycle []string
	for len(client) > 0 {
		switch e := <-client; e.Type {
		case "movie:drawn", "movie:revealed":
			lifecycle = append(lifecycle, e.Type)
		}
	}
	want := []string{"movie:drawn", "movie:revealed"}
	if !slices.Equal(lifecycle, want) {
		t.Fatalf("lifecycle events = %v, want %v", lifecycle, want)
	}
	if ap, ok := h.movieService.ActiveDraw(); !ok || !ap.Revealed {
		t.Fatalf("active draw after timer = %+v, ok=%v, want revealed", ap, ok)
	}
}

func TestHandleGetRandomMovie_PanicPublishesFallbackBeforeAutoReveal(t *testing.T) {
	ctx := context.Background()
	h, _, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Jules")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	for _, title := range []string{"Drive", "Collateral"} {
		if _, err := movieRepo.Add(ctx, title, "pool", user.ID); err != nil {
			t.Fatalf("seed pool: %v", err)
		}
	}

	timerStartSeq := make(chan uint64, 1)
	timer := &injectedAutoRevealTimer{
		started: make(chan func(), 1),
		onStart: func() {
			timerStartSeq <- h.broker.HeadSeq()
		},
	}
	h.movieService.Close()
	h.movieService = movie.NewService(movieRepo, movie.DrawConfig{
		AutoRevealDelay: time.Second,
		StartTimer:      timer.start,
		OnRevealed:      revealBroadcaster(h.broker),
	})
	h.movieMetadata = &panickingBatchMetadataRepo{MovieMetadataRepo: h.movieMetadata}

	app := fiber.New()
	app.Use(fiberrecover.New())
	mountTestV1(app, h)

	client, _ := h.broker.Subscribe()
	t.Cleanup(func() { h.broker.Unsubscribe(client) })

	resp := doAs(
		t,
		app,
		jsonReq(http.MethodPost, "/api/v1/movies/random", `{"clientId":"first"}`),
		user.ID,
		"member",
	)
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("recovered draw panic: got %d, want 500", resp.StatusCode)
	}

	drawnEvent := <-client
	if drawnEvent.Type != "movie:drawn" {
		t.Fatalf("first lifecycle event = %q, want movie:drawn", drawnEvent.Type)
	}
	if head := <-timerStartSeq; head != drawnEvent.Seq {
		t.Fatalf("broker head when timer started = %d, want published draw seq %d", head, drawnEvent.Seq)
	}
	drawn, ok := drawnEvent.Data.(drawnPayload)
	if !ok {
		t.Fatalf("fallback draw payload type = %T, want drawnPayload", drawnEvent.Data)
	}
	if len(drawn.Candidates) != 0 || drawn.ID == 0 || drawn.ServerNow == "" {
		t.Fatalf("fallback draw payload = %+v, want identified draw without candidates", drawn)
	}

	fire := <-timer.started
	if timer.starts != 1 {
		t.Fatalf("fallback publication armed %d timers, want 1", timer.starts)
	}
	fire()

	revealedEvent := <-client
	if revealedEvent.Type != "movie:revealed" {
		t.Fatalf("second lifecycle event = %q, want movie:revealed", revealedEvent.Type)
	}
}

// Delta fix: the server OWNS the auto-reveal. With no client confirmation, it
// reveals the draw itself and broadcasts movie:revealed after autoRevealDelay — so
// every client (even a backgrounded, timer-throttled tab) closes off one broadcast
// rather than its own countdown.
func TestHandleGetRandomMovie_ServerAutoReveals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)
	// Short auto-reveal window for the test; the delay now lives in the movie
	// service's DrawConfig, so swap in a service configured with it.
	h.movieService = movie.NewService(movieRepo, movie.DrawConfig{
		AutoRevealDelay: 60 * time.Millisecond,
		OnRevealed:      revealBroadcaster(h.broker),
	})

	user, err := userRepo.Create(ctx, "Iris")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	seedPoolAndDraw(t, app, movieRepo, user.ID, "Drive", "Collateral")

	if got := countEvents(client, "movie:revealed", 500*time.Millisecond); got < 1 {
		t.Fatalf("server did not auto-reveal: expected a movie:revealed broadcast, got %d", got)
	}
	if ap, ok := h.movieService.ActiveDraw(); !ok || !ap.Revealed {
		t.Fatalf("active draw not marked revealed after auto-reveal (ok=%v)", ok)
	}
}

// A manual confirm before the deadline cancels the server timer: exactly one
// movie:revealed total, even after waiting past autoRevealDelay.
func TestHandleRevealCurrentMovie_CancelsServerAutoReveal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)
	h.movieService = movie.NewService(movieRepo, movie.DrawConfig{
		AutoRevealDelay: 100 * time.Millisecond,
		OnRevealed:      revealBroadcaster(h.broker),
	})

	user, err := userRepo.Create(ctx, "Jude")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	seedPoolAndDraw(t, app, movieRepo, user.ID, "Sicario", "Arrival")

	// Confirm well within the 100ms window → cancels the pending auto-reveal.
	revReq := httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/reveal", nil)
	if resp, err := app.Test(revReq, -1); err != nil || resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("reveal: err=%v status=%v", err, resp.StatusCode)
	}

	// Over a window longer than autoRevealDelay, exactly one reveal (the manual one).
	if got := countEvents(client, "movie:revealed", 300*time.Millisecond); got != 1 {
		t.Fatalf("expected exactly 1 movie:revealed (manual confirm; auto-reveal canceled), got %d", got)
	}
}

// Watching before the reel lands is a terminal reveal too. Other clients may
// still be animating, so they need the reveal frame before the watched frame.
func TestHandleWatchCurrentMovie_RevealsUnrevealedDraw(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)
	h.movieService = movie.NewService(movieRepo, movie.DrawConfig{
		AutoRevealDelay: time.Hour,
		OnRevealed:      revealBroadcaster(h.broker),
	})

	user, err := userRepo.Create(ctx, "Mara")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	seedPoolAndDraw(t, app, movieRepo, user.ID, "Moon", "Sunshine")
	watchReq := httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/watch", nil)
	if resp, err := app.Test(watchReq, -1); err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("watch: err=%v status=%v", err, resp.StatusCode)
	}

	if got := countEvents(client, "movie:revealed", 100*time.Millisecond); got != 1 {
		t.Fatalf("expected exactly 1 movie:revealed before watched, got %d", got)
	}
	if _, ok := h.movieService.ActiveDraw(); ok {
		t.Fatal("watch left an active draw behind")
	}
}

// Watching and handing off the turn are one durable transition. If next-up
// persistence fails, the movie must stay current, its in-memory draw must stay
// active, and clients must hear none of the terminal lifecycle events.
func TestHandleWatchCurrentMovie_RollsBackWhenNextUpRotationFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo, pool := setupEditMovieTestWithDB(t)
	var logOutput bytes.Buffer
	h.log = zerolog.New(&logOutput)
	h.movieService.Close()
	h.movieService = movie.NewService(movieRepo, movie.DrawConfig{
		AutoRevealDelay: time.Hour,
		OnRevealed:      revealBroadcaster(h.broker),
	})

	first, err := userRepo.Create(ctx, "Nora")
	if err != nil {
		t.Fatalf("create first member: %v", err)
	}
	second, err := userRepo.Create(ctx, "Omar")
	if err != nil {
		t.Fatalf("create second member: %v", err)
	}
	if next, err := h.nextUpService.Get(ctx); err != nil || next.ID != first.ID {
		t.Fatalf("seed next up: got %+v, err=%v, want member %d", next, err, first.ID)
	}

	seedPoolAndDraw(t, app, movieRepo, first.ID, "Thief", "Manhunter")
	current, err := movieRepo.GetCurrent(ctx)
	if err != nil {
		t.Fatalf("get current before watch: %v", err)
	}
	activeBefore, ok := h.movieService.ActiveDraw()
	if !ok || activeBefore.MovieID != current.ID || activeBefore.Revealed {
		t.Fatalf("active draw before watch = %+v, ok=%v", activeBefore, ok)
	}

	_, err = pool.Write.ExecContext(ctx, `
		CREATE TRIGGER fail_next_up_rotation
		BEFORE UPDATE OF user_id ON next_up
		BEGIN
			SELECT RAISE(ABORT, 'forced next-up rotation failure');
		END
	`)
	if err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}

	client, _ := h.broker.Subscribe()
	t.Cleanup(func() { h.broker.Unsubscribe(client) })

	watchReq := httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/watch", nil)
	resp := doAs(t, app, watchReq, first.ID, "member")
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("watch status = %d, want 500", resp.StatusCode)
	}
	logged := logOutput.String()
	if !strings.Contains(logged, "watch current movie and advance next up failed") {
		t.Fatalf("watch failure log missing operation: %q", logged)
	}
	if !strings.Contains(logged, "forced next-up rotation failure") {
		t.Fatalf("watch failure log missing database cause: %q", logged)
	}

	stillCurrent, err := movieRepo.GetCurrent(ctx)
	if err != nil {
		t.Fatalf("get current after failed watch: %v", err)
	}
	if stillCurrent.ID != current.ID || stillCurrent.Status != "current" || stillCurrent.WatchedAt != nil {
		t.Fatalf("current after failed watch = %+v, want unchanged movie %d", stillCurrent, current.ID)
	}

	activeAfter, ok := h.movieService.ActiveDraw()
	if !ok || activeAfter.MovieID != activeBefore.MovieID || activeAfter.Generation != activeBefore.Generation || activeAfter.Revealed {
		t.Fatalf("active draw after failed watch = %+v, ok=%v, want unchanged %+v", activeAfter, ok, activeBefore)
	}

	next, err := h.nextUpService.Get(ctx)
	if err != nil {
		t.Fatalf("get next up after failed watch: %v", err)
	}
	if next.ID != first.ID {
		t.Fatalf("next up after failed watch = %d, want %d", next.ID, first.ID)
	}

	var events []string
	for len(client) > 0 {
		events = append(events, (<-client).Type)
	}
	if len(events) != 0 {
		t.Fatalf("events after failed watch = %v, want none", events)
	}

	if _, err := pool.Write.ExecContext(ctx, "DROP TRIGGER fail_next_up_rotation"); err != nil {
		t.Fatalf("remove failure trigger: %v", err)
	}

	retryReq := httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/watch", nil)
	retry := doAs(t, app, retryReq, first.ID, "member")
	if retry.StatusCode != fiber.StatusOK {
		t.Fatalf("retry watch status = %d, want 200", retry.StatusCode)
	}

	watched, err := movieRepo.FindByID(ctx, current.ID)
	if err != nil {
		t.Fatalf("get movie after retry: %v", err)
	}
	if watched.Status != "watched" || watched.WatchedAt == nil {
		t.Fatalf("movie after retry = %+v, want watched with watched_at", watched)
	}
	if _, ok := h.movieService.ActiveDraw(); ok {
		t.Fatal("retry left an active draw behind")
	}

	next, err = h.nextUpService.Get(ctx)
	if err != nil {
		t.Fatalf("get next up after retry: %v", err)
	}
	if next.ID != second.ID {
		t.Fatalf("next up after retry = %d, want %d", next.ID, second.ID)
	}

	events = events[:0]
	for len(client) > 0 {
		events = append(events, (<-client).Type)
	}
	wantEvents := []string{"movie:revealed", "settings:next-up-changed", "movie:watched"}
	if !slices.Equal(events, wantEvents) {
		t.Fatalf("events after retry = %v, want %v", events, wantEvents)
	}
}

type pausingBatchMetadataRepo struct {
	domain.MovieMetadataRepo
	reached chan struct{}
	resume  chan struct{}
}

func (r *pausingBatchMetadataRepo) GetMetadataByMovieIDs(
	ctx context.Context,
	ids []int,
) (map[int]*domain.MovieMetadata, error) {
	close(r.reached)
	<-r.resume
	return r.MovieMetadataRepo.GetMetadataByMovieIDs(ctx, ids)
}

func TestMovieNightCommandsPublishInMutationOrder(t *testing.T) {
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

	metadataReached := make(chan struct{})
	resumeMetadata := make(chan struct{})
	resumed := false
	resume := func() {
		if !resumed {
			close(resumeMetadata)
			resumed = true
		}
	}
	t.Cleanup(resume)
	h.movieMetadata = &pausingBatchMetadataRepo{
		MovieMetadataRepo: h.movieMetadata,
		reached:           metadataReached,
		resume:            resumeMetadata,
	}

	client, _ := h.broker.Subscribe()
	t.Cleanup(func() { h.broker.Unsubscribe(client) })

	drawDone := startAs(
		app,
		jsonReq(http.MethodPost, "/api/v1/movies/random", `{"clientId":"first"}`),
		first.ID,
		"member",
	)
	<-metadataReached

	// A draw owns the command until movie:drawn is published. TryLock chooses the
	// deterministic order for both the red and green implementations without a
	// timing assertion: the old implementation lets watch finish while metadata
	// is paused; the fixed implementation releases draw first.
	drawOwnsPublication := !h.movieNightMu.TryLock()
	if !drawOwnsPublication {
		h.movieNightMu.Unlock()
	}
	watchDone := startAs(
		app,
		httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/watch", nil),
		first.ID,
		"member",
	)

	var draw, watch asyncHTTPResult
	if drawOwnsPublication {
		resume()
		draw = <-drawDone
		watch = <-watchDone
	} else {
		watch = <-watchDone
		resume()
		draw = <-drawDone
	}
	if draw.err != nil || draw.resp.StatusCode != fiber.StatusOK {
		t.Fatalf("draw: err=%v status=%v", draw.err, draw.resp.StatusCode)
	}
	if watch.err != nil || watch.resp.StatusCode != fiber.StatusOK {
		t.Fatalf("watch: err=%v status=%v", watch.err, watch.resp.StatusCode)
	}

	var lifecycle []string
	for len(client) > 0 {
		switch e := <-client; e.Type {
		case "movie:drawn", "movie:revealed", "movie:watched":
			lifecycle = append(lifecycle, e.Type)
		}
	}
	want := []string{"movie:drawn", "movie:revealed", "movie:watched"}
	if !slices.Equal(lifecycle, want) {
		t.Fatalf("lifecycle events = %v, want %v", lifecycle, want)
	}

	up, err := h.nextUpService.Get(ctx)
	if err != nil {
		t.Fatalf("next up after watch: %v", err)
	}
	if up.ID != second.ID {
		t.Fatalf("next up after watch = %d, want %d", up.ID, second.ID)
	}
}

// The draw must not leak through the pool reads. DrawRandom flips the winner to
// "current" straight away, so before this hold every pool read dropped the tile
// mid-spin: reload the page during the reel (or open the board on a second
// client) and the missing poster gave the winner away ahead of the reveal. Both
// the pool list and the per-member board pools have to keep it until the reveal.
func TestPoolReadsHoldTheDrawnMovieUntilRevealed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)
	h.movieService = movie.NewService(movieRepo, movie.DrawConfig{
		// Long enough that only the explicit reveal below flips the draw.
		AutoRevealDelay: time.Hour,
		OnRevealed:      revealBroadcaster(h.broker),
	})

	user, err := userRepo.Create(ctx, "Nell")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	seedPoolAndDraw(t, app, movieRepo, user.ID, "Alien", "Aliens", "Prometheus")

	current, err := movieRepo.GetCurrent(ctx)
	if err != nil {
		t.Fatalf("get current: %v", err)
	}

	poolIDs := func() []int {
		t.Helper()
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/movies/pool", nil), -1)
		if err != nil || resp.StatusCode != fiber.StatusOK {
			t.Fatalf("get pool: err=%v status=%v", err, resp.StatusCode)
		}
		var movies []struct {
			MovieID int `json:"movieID"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&movies); err != nil {
			t.Fatalf("decode pool: %v", err)
		}
		ids := make([]int, 0, len(movies))
		for _, m := range movies {
			ids = append(ids, m.MovieID)
		}
		return ids
	}

	boardPoolIDs := func() []int {
		t.Helper()
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/members", nil), -1)
		if err != nil || resp.StatusCode != fiber.StatusOK {
			t.Fatalf("get members: err=%v status=%v", err, resp.StatusCode)
		}
		var members []struct {
			CurrentPool map[string]struct {
				MovieID int `json:"movieID"`
			} `json:"currentPool"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&members); err != nil {
			t.Fatalf("decode members: %v", err)
		}
		var ids []int
		for _, m := range members {
			for _, tile := range m.CurrentPool {
				ids = append(ids, tile.MovieID)
			}
		}
		return ids
	}

	detailStatus := func(movieID int) domain.MovieStatus {
		t.Helper()
		resp, err := app.Test(
			httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/movies/%d", movieID), nil),
			-1,
		)
		if err != nil || resp.StatusCode != fiber.StatusOK {
			t.Fatalf("get movie detail: err=%v status=%v", err, resp.StatusCode)
		}
		var movie struct {
			Status domain.MovieStatus `json:"status"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&movie); err != nil {
			t.Fatalf("decode movie detail: %v", err)
		}
		return movie.Status
	}

	poolState := func() struct {
		PoolLocked     bool `json:"poolLocked"`
		DrawInProgress bool `json:"drawInProgress"`
	} {
		t.Helper()
		resp, err := app.Test(
			httptest.NewRequest(http.MethodGet, "/api/v1/settings/pool-lock", nil),
			-1,
		)
		if err != nil || resp.StatusCode != fiber.StatusOK {
			t.Fatalf("get pool state: err=%v status=%v", err, resp.StatusCode)
		}
		var state struct {
			PoolLocked     bool `json:"poolLocked"`
			DrawInProgress bool `json:"drawInProgress"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
			t.Fatalf("decode pool state: %v", err)
		}
		return state
	}

	if got := poolIDs(); !slices.Contains(got, current.ID) || len(got) != 3 {
		t.Fatalf("pool during the draw = %v, want all 3 seeded movies incl. the winner %d", got, current.ID)
	}
	if got := boardPoolIDs(); !slices.Contains(got, current.ID) || len(got) != 3 {
		t.Fatalf("board pools during the draw = %v, want all 3 incl. the winner %d", got, current.ID)
	}
	if got := detailStatus(current.ID); got != domain.MovieStatusPool {
		t.Fatalf("winner detail status during the draw = %q, want pool", got)
	}
	if got := poolState(); !got.DrawInProgress {
		t.Fatal("pool state did not report the held draw")
	}

	revReq := httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/reveal", nil)
	if resp, err := app.Test(revReq, -1); err != nil || resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("reveal: err=%v status=%v", err, resp.StatusCode)
	}

	if got := poolIDs(); slices.Contains(got, current.ID) || len(got) != 2 {
		t.Fatalf("pool after the reveal = %v, want the 2 remaining movies without %d", got, current.ID)
	}
	if got := boardPoolIDs(); slices.Contains(got, current.ID) || len(got) != 2 {
		t.Fatalf("board pools after the reveal = %v, want the 2 remaining without %d", got, current.ID)
	}
	if got := detailStatus(current.ID); got != domain.MovieStatusCurrent {
		t.Fatalf("winner detail status after the reveal = %q, want current", got)
	}
	if got := poolState(); got.DrawInProgress {
		t.Fatal("pool state still reports a draw after reveal")
	}
}

// Closing the two ways the hold could still give the winner away.
//
// The held tile looks pooled but is really "current", so without care it answers
// mutations differently from the tiles beside it (a prober learns the winner),
// and it stops costing its adder a pool slot (a free fourth movie for the length
// of every draw). Both are settled server-side: pool mutations are frozen while
// a draw is unrevealed, and the held tile still counts against the cap.
func TestPoolIsFrozenAndStillCountsWhileADrawIsUnrevealed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)
	h.movieService = movie.NewService(movieRepo, movie.DrawConfig{
		AutoRevealDelay: time.Hour,
		OnRevealed:      revealBroadcaster(h.broker),
	})

	user, err := userRepo.Create(ctx, "Ripley")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	stashed, err := movieRepo.Add(ctx, "Barbarella", "stash", user.ID)
	if err != nil {
		t.Fatalf("seed stash: %v", err)
	}
	spare, err := movieRepo.Add(ctx, "Solaris", "stash", user.ID)
	if err != nil {
		t.Fatalf("seed stash: %v", err)
	}
	seedPoolAndDraw(t, app, movieRepo, user.ID, "Alien", "Aliens", "Prometheus")

	winner, err := movieRepo.GetCurrent(ctx)
	if err != nil {
		t.Fatalf("get current: %v", err)
	}
	var bystander *domain.Movie
	pooled, err := movieRepo.FindByStatus(ctx, "pool")
	if err != nil || len(pooled) == 0 {
		t.Fatalf("read pool rows: err=%v len=%d", err, len(pooled))
	}
	bystander = pooled[0]

	as := func(method, path, body string) (int, string) {
		t.Helper()
		var req *http.Request
		if body == "" {
			req = httptest.NewRequest(method, path, nil)
		} else {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		var problem struct {
			Title string `json:"title"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&problem)
		return resp.StatusCode, problem.Title
	}

	// The held winner and the tile beside it must answer identically: same
	// status, same problem type. Anything else identifies the draw.
	demoteWinner, titleWinner := as(http.MethodPost, fmt.Sprintf("/api/v1/movies/%d/move", winner.ID), `{"target":"stash"}`)
	demoteOther, titleOther := as(http.MethodPost, fmt.Sprintf("/api/v1/movies/%d/move", bystander.ID), `{"target":"stash"}`)
	if demoteWinner != fiber.StatusConflict || titleWinner != "draw_in_progress" {
		t.Fatalf("demote winner = %d/%q, want 409/draw_in_progress", demoteWinner, titleWinner)
	}
	if demoteOther != demoteWinner || titleOther != titleWinner {
		t.Fatalf("demote bystander = %d/%q, want the same answer as the winner (%d/%q)", demoteOther, titleOther, demoteWinner, titleWinner)
	}

	deleteWinner, titleWinner := as(http.MethodDelete, fmt.Sprintf("/api/v1/movies/%d", winner.ID), "")
	deleteOther, titleOther := as(http.MethodDelete, fmt.Sprintf("/api/v1/movies/%d", bystander.ID), "")
	if deleteWinner != fiber.StatusConflict || titleWinner != "draw_in_progress" {
		t.Fatalf("delete winner = %d/%q, want 409/draw_in_progress", deleteWinner, titleWinner)
	}
	if deleteOther != deleteWinner || titleOther != titleWinner {
		t.Fatalf("delete bystander = %d/%q, want the same answer as the winner (%d/%q)", deleteOther, titleOther, deleteWinner, titleWinner)
	}

	// The stash is not part of the ceremony and stays editable.
	if status, title := as(http.MethodDelete, fmt.Sprintf("/api/v1/movies/%d", spare.ID), ""); status != fiber.StatusNoContent {
		t.Fatalf("delete stashed movie = %d/%q, want 204 (the freeze is the pool's)", status, title)
	}

	// 2 rows in the pool + the held winner = a full pool, so the promotion is
	// refused. Without counting the held tile this would succeed and leave the
	// member with four.
	if status, title := as(http.MethodPost, fmt.Sprintf("/api/v1/movies/%d/move", stashed.ID), `{"target":"pool"}`); status != fiber.StatusConflict || title != "pool_limit_reached" {
		t.Fatalf("promote during the draw = %d/%q, want 409/pool_limit_reached", status, title)
	}

	revReq := httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/reveal", nil)
	if resp, err := app.Test(revReq, -1); err != nil || resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("reveal: err=%v status=%v", err, resp.StatusCode)
	}

	// The reveal thaws the pool: the winner has left it, so there's room again.
	if status, title := as(http.MethodPost, fmt.Sprintf("/api/v1/movies/%d/move", stashed.ID), `{"target":"pool"}`); status != fiber.StatusOK {
		t.Fatalf("promote after the reveal = %d/%q, want 200", status, title)
	}
	if status, title := as(http.MethodPost, fmt.Sprintf("/api/v1/movies/%d/move", bystander.ID), `{"target":"stash"}`); status != fiber.StatusOK {
		t.Fatalf("demote after the reveal = %d/%q, want 200", status, title)
	}
}
