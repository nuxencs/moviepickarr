package server

import (
	"testing"

	"moviepickarr/internal/domain"
)

func TestToAPIMovieMeta_FoldsMetadata(t *testing.T) {
	t.Parallel()

	tmdb := 603
	movie := domain.Movie{ID: 7, Title: "The Matrix", TMDBID: &tmdb}

	// Without metadata: enriched fields stay zero and serialize as omitted.
	bare := toAPIMovieMeta(&movie, nil)
	if bare.PosterPath != "" || bare.Runtime != 0 || bare.Genres != nil {
		t.Fatalf("expected no enriched fields, got %+v", bare)
	}
	if bare.Link != "https://www.themoviedb.org/movie/603" {
		t.Fatalf("link mismatch: %q", bare.Link)
	}
	// Stable ids are exposed for IMDb/TMDB/Letterboxd links.
	if bare.TMDBID == nil || *bare.TMDBID != 603 {
		t.Fatalf("expected tmdbId 603, got %v", bare.TMDBID)
	}
	if bare.IMDbID != "" {
		t.Fatalf("expected empty imdbId, got %q", bare.IMDbID)
	}

	poster := "/p.jpg"
	md := &domain.MovieMetadata{
		MovieID:     7,
		PosterPath:  &poster,
		ReleaseDate: "1999-03-30",
		Runtime:     136,
		Genres:      []string{"Action", "Sci-Fi"},
		VoteAverage: 8.2,
		Tagline:     "Free your mind.",
		Overview:    "A hacker learns the truth.",
	}
	got := toAPIMovieMeta(&movie, md)
	if got.PosterPath != "/p.jpg" || got.Runtime != 136 || got.VoteAverage != 8.2 {
		t.Fatalf("scalar fold mismatch: %+v", got)
	}
	if got.BackdropPath != "" {
		t.Fatalf("expected empty backdrop for nil path, got %q", got.BackdropPath)
	}
	if len(got.Genres) != 2 || got.Tagline != "Free your mind." {
		t.Fatalf("genres/tagline fold mismatch: %+v", got)
	}
}

func TestMovieLinkDerivation(t *testing.T) {
	t.Parallel()

	imdb := "tt0133093"
	tmdb := 603
	empty := ""

	cases := []struct {
		name  string
		movie domain.Movie
		want  string
	}{
		{"imdb preferred over tmdb", domain.Movie{IMDbID: &imdb, TMDBID: &tmdb}, "https://www.imdb.com/title/tt0133093/"},
		{"tmdb when no imdb", domain.Movie{TMDBID: &tmdb}, "https://www.themoviedb.org/movie/603"},
		{"empty imdb falls through to tmdb", domain.Movie{IMDbID: &empty, TMDBID: &tmdb}, "https://www.themoviedb.org/movie/603"},
		{"no identity yields empty", domain.Movie{}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.movie
			if got := movieLink(&m); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
