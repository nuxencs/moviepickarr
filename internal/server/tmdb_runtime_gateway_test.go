package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"moviepickarr/internal/domain"
	integrationtmdb "moviepickarr/internal/integration/tmdb"
)

type fakeTMDBOperations struct {
	searchFn   func(context.Context, string) ([]tmdbMovie, error)
	findFn     func(context.Context, string) (tmdbMovie, error)
	detailsFn  func(context.Context, int) (tmdbMovieDetails, error)
	discoverFn func(context.Context) ([]string, error)
}

func (f *fakeTMDBOperations) Search(ctx context.Context, query string) ([]tmdbMovie, error) {
	return f.searchFn(ctx, query)
}

func (f *fakeTMDBOperations) FindByIMDb(ctx context.Context, id string) (tmdbMovie, error) {
	return f.findFn(ctx, id)
}

func (f *fakeTMDBOperations) MovieDetails(ctx context.Context, id int) (tmdbMovieDetails, error) {
	return f.detailsFn(ctx, id)
}

func (f *fakeTMDBOperations) DiscoverPopularPosters(ctx context.Context) ([]string, error) {
	return f.discoverFn(ctx)
}

func TestTMDBRuntimeEnricher_KeepsOneSnapshotForInFlightWork(t *testing.T) {
	t.Parallel()
	oldConfig := integrationtmdb.RuntimeConfig{Enabled: true, APIKey: "old", CastLimit: 1}
	newConfig := integrationtmdb.RuntimeConfig{Enabled: true, APIKey: "new", CastLimit: 2}
	runtime := integrationtmdb.NewRuntime(oldConfig, 1)
	factoryCalls := make(map[string]int)
	gateway := newTMDBRuntimeGateway(directTMDBRuntimeSource{runtime: runtime}, func(config integrationtmdb.RuntimeConfig) tmdbOperations {
		factoryCalls[config.APIKey]++
		return &fakeTMDBOperations{
			findFn: func(context.Context, string) (tmdbMovie, error) {
				return tmdbMovie{}, errors.New("unexpected find")
			},
			detailsFn: func(context.Context, int) (tmdbMovieDetails, error) {
				if config.APIKey == "old" {
					runtime.Replace(newConfig, 2, time.Now())
				}
				return tmdbMovieDetails{ID: 603, Credits: tmdbCredits{Cast: []tmdbCastMember{
					{ID: 1, Name: "First", Order: 0},
					{ID: 2, Name: "Second", Order: 1},
				}}}, nil
			},
			searchFn:   func(context.Context, string) ([]tmdbMovie, error) { return nil, nil },
			discoverFn: func(context.Context) ([]string, error) { return nil, nil },
		}
	}, nil)
	tmdbID := 603
	movies := &fakeMovieRepo{movies: map[int]*domain.Movie{1: {ID: 1, TMDBID: &tmdbID}}}
	enricher := newTMDBRuntimeEnricher(gateway, movies, &fakeCandidateRepo{})

	if _, err := enricher.EnrichOne(context.Background(), 1); err != nil {
		t.Fatalf("first enrichment: %v", err)
	}
	if got := len(movies.applyCalls[0].Credits); got != 1 {
		t.Fatalf("in-flight cast count = %d, want old limit 1", got)
	}
	if _, err := enricher.EnrichOne(context.Background(), 1); err != nil {
		t.Fatalf("second enrichment: %v", err)
	}
	if got := len(movies.applyCalls[1].Credits); got != 2 {
		t.Fatalf("new-work cast count = %d, want new limit 2", got)
	}
	if factoryCalls["old"] != 1 || factoryCalls["new"] != 1 {
		t.Fatalf("client factory calls = %#v, want one per revision", factoryCalls)
	}
}

func TestTMDBRuntimeGateway_AuthenticationFailureSuspendsMatchingRuntime(t *testing.T) {
	t.Parallel()
	runtime := integrationtmdb.NewRuntime(integrationtmdb.RuntimeConfig{Enabled: true, APIKey: "bad"}, 4)
	var rejected int
	gateway := newTMDBRuntimeGateway(directTMDBRuntimeSource{runtime: runtime}, func(integrationtmdb.RuntimeConfig) tmdbOperations {
		return &fakeTMDBOperations{
			searchFn:   func(context.Context, string) ([]tmdbMovie, error) { return nil, errTMDBAuthentication },
			findFn:     func(context.Context, string) (tmdbMovie, error) { return tmdbMovie{}, nil },
			detailsFn:  func(context.Context, int) (tmdbMovieDetails, error) { return tmdbMovieDetails{}, nil },
			discoverFn: func(context.Context) ([]string, error) { return nil, nil },
		}
	}, func(context.Context, integrationtmdb.RuntimeSnapshot, error) { rejected++ })

	_, err := gateway.Search(context.Background(), "Alien")
	if !errors.Is(err, integrationtmdb.ErrAPIKeyRejected) {
		t.Fatalf("first search error = %v, want rejected key", err)
	}
	_, err = gateway.Search(context.Background(), "Alien")
	if !errors.Is(err, integrationtmdb.ErrAPIKeyRejected) {
		t.Fatalf("second search error = %v, want suspended runtime", err)
	}
	if rejected != 1 {
		t.Fatalf("rejection callbacks = %d, want 1", rejected)
	}
}

func TestTMDBRuntimeGateway_StaleAuthenticationFailureDoesNotNotifyScheduler(t *testing.T) {
	t.Parallel()
	runtime := integrationtmdb.NewRuntime(integrationtmdb.RuntimeConfig{Enabled: true, APIKey: "working"}, 4)
	stale, err := runtime.Acquire()
	if err != nil {
		t.Fatalf("acquire stale snapshot: %v", err)
	}
	if !runtime.AuthenticationRejected(stale) {
		t.Fatal("suspend runtime")
	}
	if !runtime.ConnectionSucceeded(4, "working") {
		t.Fatal("resume runtime")
	}

	var rejected int
	gateway := newTMDBRuntimeGateway(
		directTMDBRuntimeSource{runtime: runtime},
		func(integrationtmdb.RuntimeConfig) tmdbOperations { return &fakeTMDBOperations{} },
		func(context.Context, integrationtmdb.RuntimeSnapshot, error) { rejected++ },
	)

	if err := gateway.classify(context.Background(), stale, errTMDBAuthentication); !errors.Is(err, integrationtmdb.ErrAPIKeyRejected) {
		t.Fatalf("stale request error = %v, want rejected key", err)
	}
	if rejected != 0 {
		t.Fatalf("rejection callbacks = %d, want none for stale snapshot", rejected)
	}
	if _, err := runtime.Acquire(); err != nil {
		t.Fatalf("runtime after stale rejection: %v", err)
	}
}

func TestTMDBRuntimeGateway_RetainsPreviousRevisionClientForInFlightRun(t *testing.T) {
	t.Parallel()
	runtime := integrationtmdb.NewRuntime(integrationtmdb.RuntimeConfig{Enabled: true, APIKey: "old"}, 1)
	calls := map[string]int{}
	gateway := newTMDBRuntimeGateway(
		directTMDBRuntimeSource{runtime: runtime},
		func(config integrationtmdb.RuntimeConfig) tmdbOperations {
			calls[config.APIKey]++
			return &fakeTMDBOperations{}
		},
		nil,
	)
	oldSnapshot, _, err := gateway.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire old revision: %v", err)
	}
	runtime.Replace(integrationtmdb.RuntimeConfig{Enabled: true, APIKey: "new"}, 2, time.Now())
	if _, _, err := gateway.acquire(context.Background()); err != nil {
		t.Fatalf("acquire new revision: %v", err)
	}
	_ = gateway.client(oldSnapshot)

	if calls["old"] != 1 || calls["new"] != 1 {
		t.Fatalf("factory calls = %#v, want previous and current clients reused", calls)
	}
}
