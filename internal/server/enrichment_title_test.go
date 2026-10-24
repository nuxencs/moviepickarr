package server

import (
	"context"
	"testing"
)

func TestEnrichOne_TMDBTitleReplacesCustomTitle(t *testing.T) {
	t.Parallel()
	ctx, repos := setupEnrichmentTestRepos(t)
	_, movieID := seedEnrichmentMovie(t, ctx, repos, "My custom name", "tt0245429")

	tmdb := &fakeTMDB{
		findFn: func(context.Context, string) (tmdbMovie, error) {
			return tmdbMovie{ID: 129}, nil
		},
		detailsFn: func(context.Context, int) (tmdbMovieDetails, error) {
			return tmdbMovieDetails{ID: 129, IMDbID: "tt0245429", Title: "Spirited Away"}, nil
		},
	}
	svc := newEnrichmentService(repos.movies, repos.meta, tmdb, 15)

	if _, err := svc.EnrichOne(ctx, movieID); err != nil {
		t.Fatalf("EnrichOne: %v", err)
	}

	got, err := repos.movies.FindByID(ctx, movieID)
	if err != nil {
		t.Fatalf("read movie: %v", err)
	}
	if got.Title != "Spirited Away" {
		t.Fatalf("title = %q, want Spirited Away", got.Title)
	}
}

func TestEnrichOne_StoresOriginalTitle(t *testing.T) {
	t.Parallel()
	ctx, repos := setupEnrichmentTestRepos(t)
	_, movieID := seedEnrichmentMovie(t, ctx, repos, "Spirited Away", "tt0245429")

	tmdb := &fakeTMDB{
		findFn: func(context.Context, string) (tmdbMovie, error) {
			return tmdbMovie{ID: 129}, nil
		},
		detailsFn: func(context.Context, int) (tmdbMovieDetails, error) {
			return tmdbMovieDetails{
				ID:            129,
				IMDbID:        "tt0245429",
				Title:         "Spirited Away",
				OriginalTitle: "千と千尋の神隠し",
			}, nil
		},
	}
	svc := newEnrichmentService(repos.movies, repos.meta, tmdb, 15)

	if _, err := svc.EnrichOne(ctx, movieID); err != nil {
		t.Fatalf("EnrichOne: %v", err)
	}

	md, err := repos.meta.GetMetadata(ctx, movieID)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if md.OriginalTitle != "千と千尋の神隠し" {
		t.Fatalf("original title = %q, want 千と千尋の神隠し", md.OriginalTitle)
	}
}

func TestEnrichOne_TitleLandsWhenAnotherMovieOwnsResolvedIdentity(t *testing.T) {
	t.Parallel()
	ctx, repos := setupEnrichmentTestRepos(t)
	userID, targetID := seedEnrichmentMovie(t, ctx, repos, "Custom duplicate", "tt0000411")
	owner, err := repos.movies.Add(ctx, "Identity owner", "stash", userID)
	if err != nil {
		t.Fatalf("add identity owner: %v", err)
	}
	ownedTMDB := 511
	if err := repos.movies.SetExternalIDs(ctx, owner.ID, &ownedTMDB, nil); err != nil {
		t.Fatalf("set owned identity: %v", err)
	}

	tmdb := &fakeTMDB{
		findFn: func(context.Context, string) (tmdbMovie, error) {
			return tmdbMovie{ID: ownedTMDB}, nil
		},
		detailsFn: func(context.Context, int) (tmdbMovieDetails, error) {
			return tmdbMovieDetails{ID: ownedTMDB, IMDbID: "tt0000411", Title: "Official"}, nil
		},
	}
	svc := newEnrichmentService(repos.movies, repos.meta, tmdb, 15)

	if _, err := svc.EnrichOne(ctx, targetID); err != nil {
		t.Fatalf("EnrichOne: %v", err)
	}

	got, err := repos.movies.FindByID(ctx, targetID)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if got.Title != "Official" || got.TMDBID != nil {
		t.Fatalf("target = title %q, tmdb %v; want Official with no TMDB id", got.Title, got.TMDBID)
	}
}

func TestEnrichOne_BlankTMDBTitleKeepsStoredTitle(t *testing.T) {
	t.Parallel()
	ctx, repos := setupEnrichmentTestRepos(t)
	_, movieID := seedEnrichmentMovie(t, ctx, repos, "Stored title", "tt0000412")

	tmdb := &fakeTMDB{
		findFn: func(context.Context, string) (tmdbMovie, error) {
			return tmdbMovie{ID: 512}, nil
		},
		detailsFn: func(context.Context, int) (tmdbMovieDetails, error) {
			return tmdbMovieDetails{ID: 512, IMDbID: "tt0000412", Title: "  "}, nil
		},
	}
	svc := newEnrichmentService(repos.movies, repos.meta, tmdb, 15)

	if _, err := svc.EnrichOne(ctx, movieID); err != nil {
		t.Fatalf("EnrichOne: %v", err)
	}

	got, err := repos.movies.FindByID(ctx, movieID)
	if err != nil {
		t.Fatalf("read movie: %v", err)
	}
	if got.Title != "Stored title" {
		t.Fatalf("title = %q, want Stored title", got.Title)
	}
}
