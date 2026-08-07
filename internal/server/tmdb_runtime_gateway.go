package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"moviepickarr/internal/domain"
	integrationtmdb "moviepickarr/internal/integration/tmdb"
)

type tmdbOperations interface {
	Search(context.Context, string) ([]tmdbMovie, error)
	FindByIMDb(context.Context, string) (tmdbMovie, error)
	MovieDetails(context.Context, int) (tmdbMovieDetails, error)
	DiscoverPopularPosters(context.Context) ([]string, error)
}

type tmdbOperationsFactory func(integrationtmdb.RuntimeConfig) tmdbOperations

type tmdbRuntimeSource interface {
	Acquire(context.Context) (integrationtmdb.RuntimeSnapshot, error)
	AuthenticationRejected(context.Context, integrationtmdb.RuntimeSnapshot) (bool, error)
}

type directTMDBRuntimeSource struct {
	runtime *integrationtmdb.Runtime
}

func (s directTMDBRuntimeSource) Acquire(context.Context) (integrationtmdb.RuntimeSnapshot, error) {
	return s.runtime.Acquire()
}

func (s directTMDBRuntimeSource) AuthenticationRejected(
	_ context.Context,
	snapshot integrationtmdb.RuntimeSnapshot,
) (bool, error) {
	return s.runtime.AuthenticationRejected(snapshot), nil
}

type cachedTMDBOperations struct {
	revision int64
	config   integrationtmdb.RuntimeConfig
	client   tmdbOperations
}

// tmdbRuntimeGateway is the one runtime acquisition seam for search,
// enrichment, and poster discovery. The cache preserves a shared rate limiter
// and HTTP connection pool for a configuration revision. The mutex is held only
// while resolving a client, never during remote work.
type tmdbRuntimeGateway struct {
	runtime  tmdbRuntimeSource
	factory  tmdbOperationsFactory
	onReject func(context.Context, integrationtmdb.RuntimeSnapshot, error)

	mu     sync.Mutex
	cached []*cachedTMDBOperations
}

func newTMDBRuntimeGateway(
	runtime tmdbRuntimeSource,
	factory tmdbOperationsFactory,
	onReject func(context.Context, integrationtmdb.RuntimeSnapshot, error),
) *tmdbRuntimeGateway {
	return &tmdbRuntimeGateway{runtime: runtime, factory: factory, onReject: onReject}
}

func defaultTMDBOperationsFactory(config integrationtmdb.RuntimeConfig) tmdbOperations {
	return &tmdbClient{
		apiKey:      config.APIKey,
		baseURL:     "https://api.themoviedb.org/3",
		limiter:     newRateLimiter(config.MinInterval),
		maxRetries:  config.MaxRetries,
		backoffBase: config.Backoff,
		http:        &http.Client{Timeout: 8 * time.Second},
	}
}

func (g *tmdbRuntimeGateway) acquire(ctx context.Context) (integrationtmdb.RuntimeSnapshot, tmdbOperations, error) {
	snapshot, err := g.runtime.Acquire(ctx)
	if err != nil {
		return integrationtmdb.RuntimeSnapshot{}, nil, err
	}
	return snapshot, g.client(snapshot), nil
}

func (g *tmdbRuntimeGateway) client(snapshot integrationtmdb.RuntimeSnapshot) tmdbOperations {
	g.mu.Lock()
	defer g.mu.Unlock()
	for index, cached := range g.cached {
		if cached.revision == snapshot.Revision && cached.config == snapshot.Config {
			if index != len(g.cached)-1 {
				copy(g.cached[index:], g.cached[index+1:])
				g.cached[len(g.cached)-1] = cached
			}
			return cached.client
		}
	}
	cached := &cachedTMDBOperations{
		revision: snapshot.Revision,
		config:   snapshot.Config,
		client:   g.factory(snapshot.Config),
	}
	if len(g.cached) == 2 {
		g.cached[0] = g.cached[1]
		g.cached[1] = cached
	} else {
		g.cached = append(g.cached, cached)
	}
	return cached.client
}

func (g *tmdbRuntimeGateway) classify(
	ctx context.Context,
	snapshot integrationtmdb.RuntimeSnapshot,
	err error,
) error {
	if !errors.Is(err, errTMDBAuthentication) {
		return err
	}
	applied, recordErr := g.runtime.AuthenticationRejected(context.WithoutCancel(ctx), snapshot)
	if applied && g.onReject != nil {
		g.onReject(ctx, snapshot, recordErr)
	}
	return integrationtmdb.ErrAPIKeyRejected
}

func (g *tmdbRuntimeGateway) Search(ctx context.Context, query string) ([]tmdbMovie, error) {
	snapshot, client, err := g.acquire(ctx)
	if err != nil {
		return nil, err
	}
	result, err := client.Search(ctx, query)
	return result, g.classify(ctx, snapshot, err)
}

func (g *tmdbRuntimeGateway) DiscoverPopularPosters(ctx context.Context) ([]string, error) {
	snapshot, client, err := g.acquire(ctx)
	if err != nil {
		return nil, err
	}
	result, err := client.DiscoverPopularPosters(ctx)
	return result, g.classify(ctx, snapshot, err)
}

type tmdbRuntimeEnricher struct {
	gateway    *tmdbRuntimeGateway
	movies     enrichmentMovieStore
	candidates enrichmentCandidateStore
}

func newTMDBRuntimeEnricher(
	gateway *tmdbRuntimeGateway,
	movies enrichmentMovieStore,
	candidates enrichmentCandidateStore,
) *tmdbRuntimeEnricher {
	return &tmdbRuntimeEnricher{gateway: gateway, movies: movies, candidates: candidates}
}

func (e *tmdbRuntimeEnricher) NeedsEnrichment(
	ctx context.Context,
	staleBefore time.Time,
	limit int,
) ([]domain.EnrichmentCandidate, error) {
	return e.candidates.NeedsEnrichment(ctx, staleBefore, limit)
}

func (e *tmdbRuntimeEnricher) EnrichOne(ctx context.Context, movieID int) (enrichResult, error) {
	snapshot, client, err := e.gateway.acquire(ctx)
	if err != nil {
		return enrichResult{}, err
	}
	service := newEnrichmentService(e.movies, e.candidates, client, snapshot.Config.CastLimit)
	result, err := service.EnrichOne(ctx, movieID)
	return result, e.gateway.classify(ctx, snapshot, err)
}

var _ Enricher = (*tmdbRuntimeEnricher)(nil)

type tmdbSnapshotRunEnricher struct {
	gateway    *tmdbRuntimeGateway
	movies     enrichmentMovieStore
	candidates enrichmentCandidateStore
}

func (e *tmdbSnapshotRunEnricher) EnrichOne(
	ctx context.Context,
	snapshot integrationtmdb.RuntimeSnapshot,
	movieID int,
) (enrichResult, error) {
	service := newEnrichmentService(
		e.movies,
		e.candidates,
		e.gateway.client(snapshot),
		snapshot.Config.CastLimit,
	)
	return service.EnrichOne(ctx, movieID)
}

type tmdbRepositoryRunCandidates struct {
	candidates enrichmentCandidateStore
}

func (s *tmdbRepositoryRunCandidates) RefreshStale(
	ctx context.Context,
	staleBefore time.Time,
	limit int,
) ([]tmdbRunSubject, error) {
	candidates, err := s.candidates.NeedsEnrichment(ctx, staleBefore, limit)
	if err != nil {
		return nil, err
	}
	return toTMDBRunSubjects(candidates), nil
}

func (s *tmdbRepositoryRunCandidates) ReEnrichAll(ctx context.Context) ([]tmdbRunSubject, error) {
	candidates, err := s.candidates.NeedsEnrichment(
		ctx,
		time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC),
		int(^uint(0)>>1),
	)
	if err != nil {
		return nil, err
	}
	return toTMDBRunSubjects(candidates), nil
}

func toTMDBRunSubjects(candidates []domain.EnrichmentCandidate) []tmdbRunSubject {
	subjects := make([]tmdbRunSubject, 0, len(candidates))
	for _, candidate := range candidates {
		subjects = append(subjects, tmdbRunSubject{
			MovieID: candidate.MovieID,
			Label:   fmt.Sprintf("movie:%d", candidate.MovieID),
		})
	}
	return subjects
}
