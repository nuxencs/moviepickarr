package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/movie"
	"moviepickarr/internal/repository"
	"moviepickarr/internal/settings"

	"github.com/gofiber/fiber/v2"
)

type pausingPoolPromotionRepo struct {
	*repository.SqliteMoviesRepository
	reached chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (r *pausingPoolPromotionRepo) PromoteToPoolIfRoom(
	ctx context.Context,
	id, maxPool int,
) (int64, error) {
	r.once.Do(func() { close(r.reached) })
	<-r.resume
	return r.SqliteMoviesRepository.PromoteToPoolIfRoom(ctx, id, maxPool)
}

type pausingPoolDeleteRepo struct {
	*repository.SqliteMoviesRepository
	reached chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (r *pausingPoolDeleteRepo) Delete(ctx context.Context, id int) error {
	r.once.Do(func() { close(r.reached) })
	<-r.resume
	return r.SqliteMoviesRepository.Delete(ctx, id)
}

func TestPoolLockWaitsForAdmittedMove(t *testing.T) {
	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)

	owner, err := userRepo.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	stashed, err := movieRepo.Add(ctx, "Arrival", "stash", owner.ID)
	if err != nil {
		t.Fatalf("create stashed movie: %v", err)
	}

	pausingRepo := &pausingPoolPromotionRepo{
		SqliteMoviesRepository: movieRepo,
		reached:                make(chan struct{}),
		resume:                 make(chan struct{}),
	}
	h.movieService.Close()
	h.movieService = movie.NewService(pausingRepo, movie.DrawConfig{
		OnRevealed: revealBroadcaster(h.broker),
	})
	defer func() {
		select {
		case <-pausingRepo.resume:
		default:
			close(pausingRepo.resume)
		}
	}()

	moveDone := startAs(
		app,
		jsonReq(
			http.MethodPost,
			fmt.Sprintf("/api/v1/movies/%d/move", stashed.ID),
			`{"target":"pool"}`,
		),
		owner.ID,
		"member",
	)
	waitForSignal(t, pausingRepo.reached, "paused pool promotion")
	if h.poolStateMu.TryLock() {
		h.poolStateMu.Unlock()
		t.Fatal("admitted move released the pool-state boundary before committing")
	}

	lockDone := startAs(
		app,
		jsonReq(http.MethodPut, "/api/v1/settings/pool-lock", `{"poolLocked":true}`),
		owner.ID,
		"admin",
	)

	close(pausingRepo.resume)

	for name, request := range map[string]struct {
		done       <-chan asyncHTTPResult
		wantStatus int
	}{
		"move": {done: moveDone, wantStatus: fiber.StatusNoContent},
		"lock": {done: lockDone, wantStatus: fiber.StatusOK},
	} {
		select {
		case result := <-request.done:
			if result.err != nil {
				t.Fatalf("%s request: %v", name, result.err)
			}
			if result.resp.StatusCode != request.wantStatus {
				t.Fatalf("%s status = %d, want %d", name, result.resp.StatusCode, request.wantStatus)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("%s request did not finish", name)
		}
	}

	poolLocked, err := h.settingsService.GetPoolLock(ctx)
	if err != nil {
		t.Fatalf("read pool lock: %v", err)
	}
	if !poolLocked {
		t.Fatal("pool lock = false, want true")
	}
	moved, err := movieRepo.FindByID(ctx, stashed.ID)
	if err != nil {
		t.Fatalf("read moved movie: %v", err)
	}
	if moved.Status != string(domain.MovieStatusPool) {
		t.Fatalf("movie status = %q, want pool", moved.Status)
	}
}

func TestPoolLockWaitsForAdmittedDelete(t *testing.T) {
	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)

	owner, err := userRepo.Create(ctx, "Daria")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	pooled, err := movieRepo.Add(ctx, "Primer", "pool", owner.ID)
	if err != nil {
		t.Fatalf("create pooled movie: %v", err)
	}

	pausingRepo := &pausingPoolDeleteRepo{
		SqliteMoviesRepository: movieRepo,
		reached:                make(chan struct{}),
		resume:                 make(chan struct{}),
	}
	h.movieService.Close()
	h.movieService = movie.NewService(pausingRepo, movie.DrawConfig{
		OnRevealed: revealBroadcaster(h.broker),
	})
	defer func() {
		select {
		case <-pausingRepo.resume:
		default:
			close(pausingRepo.resume)
		}
	}()

	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	deleteDone := startAs(
		app,
		jsonReq(http.MethodDelete, fmt.Sprintf("/api/v1/movies/%d", pooled.ID), ""),
		owner.ID,
		"member",
	)
	waitForSignal(t, pausingRepo.reached, "paused pool delete")
	if h.poolStateMu.TryLock() {
		h.poolStateMu.Unlock()
		t.Fatal("admitted delete released the pool-state boundary before committing")
	}

	lockDone := startAs(
		app,
		jsonReq(http.MethodPut, "/api/v1/settings/pool-lock", `{"poolLocked":true}`),
		owner.ID,
		"admin",
	)
	close(pausingRepo.resume)

	for name, expected := range map[string]struct {
		done   <-chan asyncHTTPResult
		status int
	}{
		"delete": {done: deleteDone, status: fiber.StatusNoContent},
		"lock":   {done: lockDone, status: fiber.StatusOK},
	} {
		select {
		case result := <-expected.done:
			if result.err != nil {
				t.Fatalf("%s request: %v", name, result.err)
			}
			if result.resp.StatusCode != expected.status {
				t.Fatalf("%s status = %d, want %d", name, result.resp.StatusCode, expected.status)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("%s request did not finish", name)
		}
	}

	for index, want := range []string{"movie:deleted", "settings:pool-lock-changed"} {
		select {
		case got := <-client:
			if got.Type != want {
				t.Fatalf("event %d type = %q, want %q", index, got.Type, want)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("event %d was not published", index)
		}
	}

	if _, err := movieRepo.FindByID(ctx, pooled.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleted movie lookup = %v, want ErrNotFound", err)
	}
	poolLocked, err := h.settingsService.GetPoolLock(ctx)
	if err != nil {
		t.Fatalf("read pool lock: %v", err)
	}
	if !poolLocked {
		t.Fatal("pool lock = false, want true")
	}
}

type pausingSettingsRepo struct {
	mu            sync.Mutex
	value         string
	trueCommitted chan struct{}
	releaseTrue   chan struct{}
	trueOnce      sync.Once
}

func (r *pausingSettingsRepo) List(context.Context) ([]*domain.Settings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return []*domain.Settings{{Key: "pool_locked", Value: r.value}}, nil
}

func (r *pausingSettingsRepo) FindByKey(context.Context, string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.value, nil
}

func (r *pausingSettingsRepo) Set(_ context.Context, _ string, value string) error {
	r.mu.Lock()
	r.value = value
	r.mu.Unlock()

	switch value {
	case "true":
		r.trueOnce.Do(func() { close(r.trueCommitted) })
		<-r.releaseTrue
	}
	return nil
}

func TestPoolLockWritesPublishInCommitOrder(t *testing.T) {
	h, app, _, _ := setupEditMovieTest(t)
	repo := &pausingSettingsRepo{
		value:         "false",
		trueCommitted: make(chan struct{}),
		releaseTrue:   make(chan struct{}),
	}
	h.settingsService = settings.NewService(repo)
	defer func() {
		select {
		case <-repo.releaseTrue:
		default:
			close(repo.releaseTrue)
		}
	}()

	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	firstDone := startAs(
		app,
		jsonReq(http.MethodPut, "/api/v1/settings/pool-lock", `{"poolLocked":true}`),
		1,
		"admin",
	)
	waitForSignal(t, repo.trueCommitted, "first pool-lock commit")
	if h.poolStateMu.TryLock() {
		h.poolStateMu.Unlock()
		t.Fatal("first lock write released the publication boundary before returning")
	}

	secondDone := startAs(
		app,
		jsonReq(http.MethodPut, "/api/v1/settings/pool-lock", `{"poolLocked":false}`),
		1,
		"admin",
	)

	close(repo.releaseTrue)
	for name, done := range map[string]<-chan asyncHTTPResult{
		"first":  firstDone,
		"second": secondDone,
	} {
		select {
		case result := <-done:
			if result.err != nil {
				t.Fatalf("%s request: %v", name, result.err)
			}
			if result.resp.StatusCode != fiber.StatusOK {
				t.Fatalf("%s status = %d, want 200", name, result.resp.StatusCode)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("%s request did not finish", name)
		}
	}

	for index, want := range []bool{true, false} {
		select {
		case got := <-client:
			if got.Type != "settings:pool-lock-changed" {
				t.Fatalf("event %d type = %q", index, got.Type)
			}
			payload, ok := got.Data.(settingsResponse)
			if !ok {
				t.Fatalf("event %d payload type = %T", index, got.Data)
			}
			if payload.PoolLocked != want {
				t.Fatalf("event %d poolLocked = %t, want %t", index, payload.PoolLocked, want)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("event %d was not published", index)
		}
	}

	if got, err := repo.FindByKey(context.Background(), "pool_locked"); err != nil {
		t.Fatalf("read final setting: %v", err)
	} else if got != "false" {
		t.Fatalf("final pool lock = %q, want false", got)
	}
}
