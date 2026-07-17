package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/movie"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
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
