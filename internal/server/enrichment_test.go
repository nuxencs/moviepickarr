package server

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"moviepickarr/internal/domain"
)

// --- fakes -----------------------------------------------------------------

type fakeTMDB struct {
	findFn    func(ctx context.Context, imdbID string) (tmdbMovie, error)
	detailsFn func(ctx context.Context, tmdbID int) (tmdbMovieDetails, error)
}

func (f *fakeTMDB) FindByIMDb(ctx context.Context, imdbID string) (tmdbMovie, error) {
	return f.findFn(ctx, imdbID)
}

func (f *fakeTMDB) MovieDetails(ctx context.Context, tmdbID int) (tmdbMovieDetails, error) {
	return f.detailsFn(ctx, tmdbID)
}

type setIDsCall struct {
	id     int
	tmdbID *int
	imdbID *string
}

type fakeMovieRepo struct {
	movies  map[int]*domain.Movie
	setCall *setIDsCall
}

func (r *fakeMovieRepo) FindByID(_ context.Context, id int) (*domain.Movie, error) {
	m, ok := r.movies[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	cp := *m
	return &cp, nil
}

func (r *fakeMovieRepo) SetExternalIDs(_ context.Context, id int, tmdbID *int, imdbID *string) error {
	r.setCall = &setIDsCall{id: id, tmdbID: tmdbID, imdbID: imdbID}
	return nil
}

func (r *fakeMovieRepo) List(context.Context) ([]*domain.Movie, error) { panic("unexpected call") }
func (r *fakeMovieRepo) FindByUserID(context.Context, int) ([]*domain.Movie, error) {
	panic("unexpected call")
}

func (r *fakeMovieRepo) FindByStatus(context.Context, string) ([]*domain.Movie, error) {
	panic("unexpected call")
}

func (r *fakeMovieRepo) FindByUserIDAndStatus(context.Context, int, string) ([]*domain.Movie, error) {
	panic("unexpected call")
}
func (r *fakeMovieRepo) CountByStatus(context.Context, string) (int, error) { panic("unexpected call") }
func (r *fakeMovieRepo) CountByUserIDAndStatus(context.Context, int, string) (int, error) {
	panic("unexpected call")
}

func (r *fakeMovieRepo) Add(context.Context, string, string, int) (*domain.Movie, error) {
	panic("unexpected call")
}

func (r *fakeMovieRepo) UpdateTitle(context.Context, int, string) error {
	panic("unexpected call")
}

func (r *fakeMovieRepo) UpdateWatchedAt(context.Context, int, time.Time) error {
	panic("unexpected call")
}
func (r *fakeMovieRepo) UpdateStatus(context.Context, int, string) error { panic("unexpected call") }
func (r *fakeMovieRepo) MarkAsWatched(context.Context, int, time.Time) error {
	panic("unexpected call")
}

func (r *fakeMovieRepo) GetRandomPooled(context.Context) (*domain.Movie, error) {
	panic("unexpected call")
}
func (r *fakeMovieRepo) GetCurrent(context.Context) (*domain.Movie, error) { panic("unexpected call") }
func (r *fakeMovieRepo) Delete(context.Context, int) error                 { panic("unexpected call") }

type fakeMetaRepo struct {
	upserts   []domain.MovieMetadata
	upsertErr error
}

func (r *fakeMetaRepo) UpsertMetadata(_ context.Context, md domain.MovieMetadata) error {
	if r.upsertErr != nil {
		return r.upsertErr
	}
	r.upserts = append(r.upserts, md)
	return nil
}

func (r *fakeMetaRepo) GetMetadata(context.Context, int) (*domain.MovieMetadata, error) {
	panic("unexpected call")
}

func (r *fakeMetaRepo) GetMetadataByMovieIDs(context.Context, []int) (map[int]*domain.MovieMetadata, error) {
	panic("unexpected call")
}

func (r *fakeMetaRepo) NeedsEnrichment(context.Context, time.Time, int) ([]domain.EnrichmentCandidate, error) {
	panic("unexpected call")
}

func newTestEnricher(m *domain.Movie, tmdb tmdbAPI) (*enrichmentService, *fakeMetaRepo) {
	movies := &fakeMovieRepo{movies: map[int]*domain.Movie{m.ID: m}}
	meta := &fakeMetaRepo{}
	return newEnrichmentService(movies, meta, tmdb), meta
}

// --- tests -----------------------------------------------------------------

func TestEnrichOne_HappyPath(t *testing.T) {
	t.Parallel()
	m := &domain.Movie{ID: 7, IMDbID: new("tt0133093")}
	runtime := 136
	backdrop := "/bd.jpg"
	tmdb := &fakeTMDB{
		findFn: func(_ context.Context, imdbID string) (tmdbMovie, error) {
			if imdbID != "tt0133093" {
				t.Fatalf("unexpected imdb id %q", imdbID)
			}
			return tmdbMovie{ID: 603}, nil
		},
		detailsFn: func(_ context.Context, id int) (tmdbMovieDetails, error) {
			if id != 603 {
				t.Fatalf("unexpected tmdb id %d", id)
			}
			return tmdbMovieDetails{
				ID:           603,
				IMDbID:       "tt0133093",
				Overview:     "ov",
				PosterPath:   nil,
				BackdropPath: &backdrop,
				ReleaseDate:  "1999-03-30",
				Runtime:      &runtime,
				Genres:       []tmdbGenre{{ID: 28, Name: "Action"}},
				VoteAverage:  8.2,
				VoteCount:    100,
				Tagline:      "tg",
			}, nil
		},
	}

	movies := &fakeMovieRepo{movies: map[int]*domain.Movie{m.ID: m}}
	meta := &fakeMetaRepo{}
	svc := newEnrichmentService(movies, meta, tmdb)
	res, err := svc.EnrichOne(context.Background(), 7)
	if err != nil {
		t.Fatalf("EnrichOne: %v", err)
	}
	if res.TMDBID != 603 || res.Genres != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}

	// Identity persisted on the movie row.
	if movies.setCall == nil || movies.setCall.tmdbID == nil || *movies.setCall.tmdbID != 603 {
		t.Fatalf("expected SetExternalIDs with tmdb 603, got %+v", movies.setCall)
	}
	if movies.setCall.imdbID == nil || *movies.setCall.imdbID != "tt0133093" {
		t.Fatalf("expected SetExternalIDs with imdb tt0133093, got %+v", movies.setCall)
	}

	// Display fields persisted to metadata (ids are NOT on metadata anymore).
	if len(meta.upserts) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(meta.upserts))
	}
	md := meta.upserts[0]
	if md.MovieID != 7 || md.Runtime != 136 {
		t.Fatalf("mapping mismatch: %+v", md)
	}
	if md.PosterPath != nil {
		t.Fatalf("expected nil poster, got %v", *md.PosterPath)
	}
	if md.BackdropPath == nil || *md.BackdropPath != "/bd.jpg" {
		t.Fatalf("backdrop mismatch: %v", md.BackdropPath)
	}
	if len(md.Genres) != 1 || md.Genres[0] != "Action" {
		t.Fatalf("genres mismatch: %v", md.Genres)
	}
}

func TestEnrichOne_SkipsFindWhenTMDBIDKnown(t *testing.T) {
	t.Parallel()
	tmdbID := 603
	m := &domain.Movie{ID: 8, TMDBID: &tmdbID}
	tmdb := &fakeTMDB{
		findFn: func(context.Context, string) (tmdbMovie, error) {
			t.Fatal("FindByIMDb must not be called when the TMDB id is known")
			return tmdbMovie{}, nil
		},
		detailsFn: func(_ context.Context, id int) (tmdbMovieDetails, error) {
			if id != 603 {
				t.Fatalf("unexpected tmdb id %d", id)
			}
			return tmdbMovieDetails{ID: 603, IMDbID: "tt0133093", Genres: []tmdbGenre{{Name: "Action"}}}, nil
		},
	}

	movies := &fakeMovieRepo{movies: map[int]*domain.Movie{m.ID: m}}
	meta := &fakeMetaRepo{}
	svc := newEnrichmentService(movies, meta, tmdb)
	if _, err := svc.EnrichOne(context.Background(), 8); err != nil {
		t.Fatalf("EnrichOne: %v", err)
	}
	if len(meta.upserts) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(meta.upserts))
	}
}

func TestEnrichOne_NoIMDbID(t *testing.T) {
	t.Parallel()
	m := &domain.Movie{ID: 1} // no identity at all
	called := false
	tmdb := &fakeTMDB{
		findFn:    func(context.Context, string) (tmdbMovie, error) { called = true; return tmdbMovie{}, nil },
		detailsFn: func(context.Context, int) (tmdbMovieDetails, error) { called = true; return tmdbMovieDetails{}, nil },
	}

	svc, meta := newTestEnricher(m, tmdb)
	_, err := svc.EnrichOne(context.Background(), 1)
	if !errors.Is(err, ErrEnrichNoIMDbID) {
		t.Fatalf("expected ErrEnrichNoIMDbID, got %v", err)
	}
	if called {
		t.Fatalf("tmdb should not be called when there is no imdb id")
	}
	if len(meta.upserts) != 0 {
		t.Fatalf("expected no upsert, got %d", len(meta.upserts))
	}
}

func TestEnrichOne_FindNotFound(t *testing.T) {
	t.Parallel()
	m := &domain.Movie{ID: 2, IMDbID: new("tt9999999")}
	tmdb := &fakeTMDB{
		findFn: func(context.Context, string) (tmdbMovie, error) { return tmdbMovie{}, errTMDBNotFound },
		detailsFn: func(context.Context, int) (tmdbMovieDetails, error) {
			t.Fatal("details must not be called")
			return tmdbMovieDetails{}, nil
		},
	}

	svc, meta := newTestEnricher(m, tmdb)
	_, err := svc.EnrichOne(context.Background(), 2)
	if !errors.Is(err, ErrEnrichNotFound) {
		t.Fatalf("expected ErrEnrichNotFound, got %v", err)
	}
	if len(meta.upserts) != 0 {
		t.Fatalf("expected no upsert")
	}
}

func TestEnrichOne_DetailsNotFound(t *testing.T) {
	t.Parallel()
	m := &domain.Movie{ID: 5, IMDbID: new("tt0133093")}
	tmdb := &fakeTMDB{
		findFn:    func(context.Context, string) (tmdbMovie, error) { return tmdbMovie{ID: 603}, nil },
		detailsFn: func(context.Context, int) (tmdbMovieDetails, error) { return tmdbMovieDetails{}, errTMDBNotFound },
	}

	svc, meta := newTestEnricher(m, tmdb)
	if _, err := svc.EnrichOne(context.Background(), 5); !errors.Is(err, ErrEnrichNotFound) {
		t.Fatalf("expected ErrEnrichNotFound, got %v", err)
	}
	if len(meta.upserts) != 0 {
		t.Fatalf("expected no upsert")
	}
}

func TestEnrichOne_RateLimitBubbles(t *testing.T) {
	t.Parallel()
	m := &domain.Movie{ID: 3, IMDbID: new("tt0133093")}
	rlErr := &tmdbRateLimitError{RetryAfter: 5 * time.Second}
	tmdb := &fakeTMDB{
		findFn:    func(context.Context, string) (tmdbMovie, error) { return tmdbMovie{}, rlErr },
		detailsFn: func(context.Context, int) (tmdbMovieDetails, error) { return tmdbMovieDetails{}, nil },
	}

	svc, meta := newTestEnricher(m, tmdb)
	_, err := svc.EnrichOne(context.Background(), 3)
	var rl *tmdbRateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("expected rate-limit error to bubble, got %v", err)
	}
	if len(meta.upserts) != 0 {
		t.Fatalf("expected no upsert on rate limit")
	}
}

func TestEnrichOne_NilRuntimeAndEmptyGenres(t *testing.T) {
	t.Parallel()
	m := &domain.Movie{ID: 4, IMDbID: new("tt0133093")}
	tmdb := &fakeTMDB{
		findFn: func(context.Context, string) (tmdbMovie, error) { return tmdbMovie{ID: 1}, nil },
		detailsFn: func(context.Context, int) (tmdbMovieDetails, error) {
			return tmdbMovieDetails{ID: 1, IMDbID: "tt0133093", Runtime: nil, Genres: nil}, nil
		},
	}

	svc, meta := newTestEnricher(m, tmdb)
	if _, err := svc.EnrichOne(context.Background(), 4); err != nil {
		t.Fatalf("EnrichOne: %v", err)
	}
	md := meta.upserts[0]
	if md.Runtime != 0 {
		t.Fatalf("expected runtime 0 for nil, got %d", md.Runtime)
	}
	if md.Genres == nil || len(md.Genres) != 0 {
		t.Fatalf("expected empty non-nil genres, got %v", md.Genres)
	}
}
