package server

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
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

type fakeMovieRepo struct {
	movies      map[int]*domain.Movie
	applyCalls  []domain.MovieEnrichmentWrite
	applyErr    error
	applyResult *bool
}

func (r *fakeMovieRepo) FindByID(_ context.Context, id int) (*domain.Movie, error) {
	m, ok := r.movies[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	cp := *m
	return &cp, nil
}

func (r *fakeMovieRepo) ApplyEnrichment(
	_ context.Context,
	write domain.MovieEnrichmentWrite,
) (bool, error) {
	if r.applyErr != nil {
		return false, r.applyErr
	}
	r.applyCalls = append(r.applyCalls, write)
	if r.applyResult != nil {
		return *r.applyResult, nil
	}
	return true, nil
}

type fakeCandidateRepo struct{}

func (*fakeCandidateRepo) NeedsEnrichment(context.Context, time.Time, int) ([]domain.EnrichmentCandidate, error) {
	panic("unexpected call")
}

func newTestEnricher(m *domain.Movie, tmdb tmdbAPI) (*enrichmentService, *fakeMovieRepo) {
	movies := &fakeMovieRepo{movies: map[int]*domain.Movie{m.ID: m}}
	return newEnrichmentService(movies, &fakeCandidateRepo{}, tmdb, 15), movies
}

func TestIMDbExtractionNormalizesCase(t *testing.T) {
	t.Parallel()

	link := "HTTPS://WWW.IMDB.COM/title/TT0133093/?ref_=test"
	if got := extractIMDbID(link); got != "tt0133093" {
		t.Fatalf("extract IMDb id = %q, want tt0133093", got)
	}
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
				Credits: tmdbCredits{
					Cast: []tmdbCastMember{{ID: 6384, Name: "Keanu Reeves", Character: "Neo", Order: 0}},
					Crew: []tmdbCrewMember{
						{ID: 9340, Name: "Lana Wachowski", Job: "Director", Department: "Directing"},
						{ID: 1234, Name: "Some Gaffer", Job: "Gaffer", Department: "Lighting"}, // dropped by whitelist
					},
				},
			}, nil
		},
	}

	movies := &fakeMovieRepo{movies: map[int]*domain.Movie{m.ID: m}}
	svc := newEnrichmentService(movies, &fakeCandidateRepo{}, tmdb, 15)
	res, err := svc.EnrichOne(context.Background(), 7)
	if err != nil {
		t.Fatalf("EnrichOne: %v", err)
	}
	if res.TMDBID != 603 || res.Genres != 1 || res.Credits != 2 {
		t.Fatalf("unexpected result: %+v", res)
	}

	if len(movies.applyCalls) != 1 {
		t.Fatalf("expected one atomic enrichment write, got %d", len(movies.applyCalls))
	}
	write := movies.applyCalls[0]
	if write.MovieID != 7 {
		t.Fatalf("write movie id = %d, want 7", write.MovieID)
	}
	if write.Expected.TMDBID != nil || write.Expected.IMDbID == nil || *write.Expected.IMDbID != "tt0133093" {
		t.Fatalf("expected identity = %+v", write.Expected)
	}
	if write.Resolved.TMDBID == nil || *write.Resolved.TMDBID != 603 ||
		write.Resolved.IMDbID == nil || *write.Resolved.IMDbID != "tt0133093" {
		t.Fatalf("resolved identity = %+v", write.Resolved)
	}

	md := write.Metadata
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

	if len(write.Credits) != 2 {
		t.Fatalf("mapped credits = %+v", write.Credits)
	}
	if write.Credits[0].Kind != domain.CreditKindCast || write.Credits[0].Person.Name != "Keanu Reeves" {
		t.Fatalf("cast mismatch: %+v", write.Credits[0])
	}
	if write.Credits[1].Kind != domain.CreditKindCrew || write.Credits[1].Job != "Director" {
		t.Fatalf("crew mismatch: %+v", write.Credits[1])
	}
}

func TestEnrichOne_EmptyCreditsStillReplaced(t *testing.T) {
	t.Parallel()
	m := &domain.Movie{ID: 9, IMDbID: new("tt0133093")}
	tmdb := &fakeTMDB{
		findFn: func(context.Context, string) (tmdbMovie, error) { return tmdbMovie{ID: 1}, nil },
		detailsFn: func(context.Context, int) (tmdbMovieDetails, error) {
			return tmdbMovieDetails{ID: 1, IMDbID: "tt0133093"}, nil // no credits at all
		},
	}

	svc, movies := newTestEnricher(m, tmdb)
	res, err := svc.EnrichOne(context.Background(), 9)
	if err != nil {
		t.Fatalf("EnrichOne: %v", err)
	}
	if res.Credits != 0 {
		t.Fatalf("expected 0 credits, got %d", res.Credits)
	}
	if len(movies.applyCalls) != 1 || len(movies.applyCalls[0].Credits) != 0 {
		t.Fatalf("expected one atomic write with empty credits, got %+v", movies.applyCalls)
	}
}

func TestEnrichOne_ApplyErrorBubbles(t *testing.T) {
	t.Parallel()
	m := &domain.Movie{ID: 10, IMDbID: new("tt0133093")}
	tmdb := &fakeTMDB{
		findFn: func(context.Context, string) (tmdbMovie, error) { return tmdbMovie{ID: 1}, nil },
		detailsFn: func(context.Context, int) (tmdbMovieDetails, error) {
			return tmdbMovieDetails{ID: 1, IMDbID: "tt0133093"}, nil
		},
	}

	svc, movies := newTestEnricher(m, tmdb)
	boom := errors.New("enrichment write failed")
	movies.applyErr = boom
	if _, err := svc.EnrichOne(context.Background(), 10); !errors.Is(err, boom) {
		t.Fatalf("expected enrichment write error to bubble, got %v", err)
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
	svc := newEnrichmentService(movies, &fakeCandidateRepo{}, tmdb, 15)
	if _, err := svc.EnrichOne(context.Background(), 8); err != nil {
		t.Fatalf("EnrichOne: %v", err)
	}
	if len(movies.applyCalls) != 1 {
		t.Fatalf("expected one atomic write, got %d", len(movies.applyCalls))
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

	svc, movies := newTestEnricher(m, tmdb)
	_, err := svc.EnrichOne(context.Background(), 1)
	if !errors.Is(err, ErrEnrichNoIMDbID) {
		t.Fatalf("expected ErrEnrichNoIMDbID, got %v", err)
	}
	if called {
		t.Fatalf("tmdb should not be called when there is no imdb id")
	}
	if len(movies.applyCalls) != 0 {
		t.Fatalf("expected no atomic write, got %d", len(movies.applyCalls))
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

	svc, movies := newTestEnricher(m, tmdb)
	_, err := svc.EnrichOne(context.Background(), 2)
	if !errors.Is(err, ErrEnrichNotFound) {
		t.Fatalf("expected ErrEnrichNotFound, got %v", err)
	}
	if len(movies.applyCalls) != 0 {
		t.Fatal("expected no atomic write")
	}
}

func TestEnrichOne_DetailsNotFound(t *testing.T) {
	t.Parallel()
	m := &domain.Movie{ID: 5, IMDbID: new("tt0133093")}
	tmdb := &fakeTMDB{
		findFn:    func(context.Context, string) (tmdbMovie, error) { return tmdbMovie{ID: 603}, nil },
		detailsFn: func(context.Context, int) (tmdbMovieDetails, error) { return tmdbMovieDetails{}, errTMDBNotFound },
	}

	svc, movies := newTestEnricher(m, tmdb)
	if _, err := svc.EnrichOne(context.Background(), 5); !errors.Is(err, ErrEnrichNotFound) {
		t.Fatalf("expected ErrEnrichNotFound, got %v", err)
	}
	if len(movies.applyCalls) != 0 {
		t.Fatal("expected no atomic write")
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

	svc, movies := newTestEnricher(m, tmdb)
	_, err := svc.EnrichOne(context.Background(), 3)
	var rl *tmdbRateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("expected rate-limit error to bubble, got %v", err)
	}
	if len(movies.applyCalls) != 0 {
		t.Fatal("expected no atomic write on rate limit")
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

	svc, movies := newTestEnricher(m, tmdb)
	if _, err := svc.EnrichOne(context.Background(), 4); err != nil {
		t.Fatalf("EnrichOne: %v", err)
	}
	md := movies.applyCalls[0].Metadata
	if md.Runtime != 0 {
		t.Fatalf("expected runtime 0 for nil, got %d", md.Runtime)
	}
	if md.Genres == nil || len(md.Genres) != 0 {
		t.Fatalf("expected empty non-nil genres, got %v", md.Genres)
	}
}

func TestMapCredits(t *testing.T) {
	t.Parallel()

	castCredit := func(id int, name, character string, order int) domain.MovieCredit {
		return domain.MovieCredit{
			MovieID:   42,
			Person:    domain.Person{ID: id, Name: name},
			Kind:      domain.CreditKindCast,
			Character: character,
			CastOrder: order,
		}
	}
	crewCredit := func(id int, name, job, department string) domain.MovieCredit {
		return domain.MovieCredit{
			MovieID:    42,
			Person:     domain.Person{ID: id, Name: name},
			Kind:       domain.CreditKindCrew,
			Job:        job,
			Department: department,
		}
	}

	cases := []struct {
		name      string
		credits   tmdbCredits
		castLimit int
		want      []domain.MovieCredit
	}{
		{
			name:      "empty credits yield no rows",
			credits:   tmdbCredits{},
			castLimit: 15,
			want:      []domain.MovieCredit{},
		},
		{
			name: "cast sorted by billing order",
			credits: tmdbCredits{Cast: []tmdbCastMember{
				{ID: 3, Name: "Cara", Character: "C", Order: 2},
				{ID: 1, Name: "Abe", Character: "A", Order: 0},
				{ID: 2, Name: "Ben", Character: "B", Order: 1},
			}},
			castLimit: 15,
			want: []domain.MovieCredit{
				castCredit(1, "Abe", "A", 0),
				castCredit(2, "Ben", "B", 1),
				castCredit(3, "Cara", "C", 2),
			},
		},
		{
			name: "cast limit trims after sorting",
			credits: tmdbCredits{Cast: []tmdbCastMember{
				{ID: 3, Name: "Cara", Character: "C", Order: 2},
				{ID: 1, Name: "Abe", Character: "A", Order: 0},
				{ID: 2, Name: "Ben", Character: "B", Order: 1},
			}},
			castLimit: 2,
			want: []domain.MovieCredit{
				castCredit(1, "Abe", "A", 0),
				castCredit(2, "Ben", "B", 1),
			},
		},
		{
			name: "limit zero keeps the full cast",
			credits: tmdbCredits{Cast: []tmdbCastMember{
				{ID: 1, Name: "Abe", Character: "A", Order: 0},
				{ID: 2, Name: "Ben", Character: "B", Order: 1},
				{ID: 3, Name: "Cara", Character: "C", Order: 2},
			}},
			castLimit: 0,
			want: []domain.MovieCredit{
				castCredit(1, "Abe", "A", 0),
				castCredit(2, "Ben", "B", 1),
				castCredit(3, "Cara", "C", 2),
			},
		},
		{
			name: "duplicate cast merges characters",
			credits: tmdbCredits{Cast: []tmdbCastMember{
				{ID: 1, Name: "Abe", Character: "Neo", Order: 0},
				{ID: 1, Name: "Abe", Character: "", Order: 1},
				{ID: 1, Name: "Abe", Character: "Thomas Anderson", Order: 2},
			}},
			castLimit: 15,
			want: []domain.MovieCredit{
				castCredit(1, "Abe", "Neo / Thomas Anderson", 0),
			},
		},
		{
			name: "duplicate keeps the later character when the first is empty",
			credits: tmdbCredits{Cast: []tmdbCastMember{
				{ID: 1, Name: "Abe", Character: "", Order: 0},
				{ID: 1, Name: "Abe", Character: "Neo", Order: 1},
			}},
			castLimit: 15,
			want: []domain.MovieCredit{
				castCredit(1, "Abe", "Neo", 0),
			},
		},
		{
			name: "duplicate cast does not cost a billing slot",
			credits: tmdbCredits{Cast: []tmdbCastMember{
				{ID: 1, Name: "Abe", Character: "A", Order: 0},
				{ID: 1, Name: "Abe", Character: "A2", Order: 1},
				{ID: 2, Name: "Ben", Character: "B", Order: 2},
			}},
			castLimit: 2,
			want: []domain.MovieCredit{
				castCredit(1, "Abe", "A / A2", 0),
				castCredit(2, "Ben", "B", 2),
			},
		},
		{
			name: "crew filtered to the job whitelist",
			credits: tmdbCredits{Crew: []tmdbCrewMember{
				{ID: 1, Name: "Dora", Job: "Director", Department: "Directing"},
				{ID: 2, Name: "Walt", Job: "Writer", Department: "Writing"},
				{ID: 3, Name: "Sue", Job: "Screenplay", Department: "Writing"},
				{ID: 4, Name: "Omar", Job: "Original Music Composer", Department: "Sound"},
				{ID: 5, Name: "Phil", Job: "Director of Photography", Department: "Camera"},
				{ID: 6, Name: "Gabe", Job: "Gaffer", Department: "Lighting"},
				{ID: 7, Name: "Pam", Job: "Producer", Department: "Production"},
			}},
			castLimit: 15,
			want: []domain.MovieCredit{
				crewCredit(1, "Dora", "Director", "Directing"),
				crewCredit(2, "Walt", "Writer", "Writing"),
				crewCredit(3, "Sue", "Screenplay", "Writing"),
				crewCredit(4, "Omar", "Original Music Composer", "Sound"),
				crewCredit(5, "Phil", "Director of Photography", "Camera"),
			},
		},
		{
			name: "crew deduped per person and job, distinct jobs kept",
			credits: tmdbCredits{Crew: []tmdbCrewMember{
				{ID: 1, Name: "Dora", Job: "Director", Department: "Directing"},
				{ID: 1, Name: "Dora", Job: "Director", Department: "Directing"},
				{ID: 1, Name: "Dora", Job: "Writer", Department: "Writing"},
			}},
			castLimit: 15,
			want: []domain.MovieCredit{
				crewCredit(1, "Dora", "Director", "Directing"),
				crewCredit(1, "Dora", "Writer", "Writing"),
			},
		},
		{
			name: "person as both cast and crew yields two rows",
			credits: tmdbCredits{
				Cast: []tmdbCastMember{{ID: 1, Name: "Clint", Character: "Lead", Order: 0}},
				Crew: []tmdbCrewMember{{ID: 1, Name: "Clint", Job: "Director", Department: "Directing"}},
			},
			castLimit: 15,
			want: []domain.MovieCredit{
				castCredit(1, "Clint", "Lead", 0),
				crewCredit(1, "Clint", "Director", "Directing"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mapCredits(42, tc.credits, tc.castLimit)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("mapCredits mismatch:\n got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestMapCredits_CarriesProfilePath(t *testing.T) {
	t.Parallel()

	profile := "/keanu.jpg"
	got := mapCredits(42, tmdbCredits{
		Cast: []tmdbCastMember{{ID: 1, Name: "Keanu", ProfilePath: &profile, Character: "Neo", Order: 0}},
	}, 15)
	if len(got) != 1 || got[0].Person.ProfilePath == nil || *got[0].Person.ProfilePath != "/keanu.jpg" {
		t.Fatalf("expected profile path carried, got %+v", got)
	}
}
