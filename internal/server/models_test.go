package server

import (
	"encoding/json"
	"strings"
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

	// The omitempty contract the frontend (web/src/types/Response.ts) relies on:
	// enriched keys are absent from the wire format when the movie isn't enriched,
	// while the stable tmdbId is present. Assert against the marshaled bytes so a
	// dropped omitempty tag or renamed json tag is caught here, not at runtime.
	bareJSON, err := json.Marshal(bare)
	if err != nil {
		t.Fatalf("marshal bare: %v", err)
	}
	for _, key := range []string{"posterPath", "backdropPath", "releaseDate", "runtime", "genres", "voteAverage", "tagline", "overview", "imdbId"} {
		if strings.Contains(string(bareJSON), `"`+key+`"`) {
			t.Fatalf("expected %q omitted for unenriched movie, got %s", key, bareJSON)
		}
	}
	if !strings.Contains(string(bareJSON), `"tmdbId":603`) {
		t.Fatalf("expected tmdbId present, got %s", bareJSON)
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

	// Enriched movies serialize the exact json tags the TS contract expects.
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	for _, want := range []string{
		`"posterPath":"/p.jpg"`,
		`"releaseDate":"1999-03-30"`,
		`"runtime":136`,
		`"genres":["Action","Sci-Fi"]`,
		`"voteAverage":8.2`,
		`"tagline":"Free your mind."`,
	} {
		if !strings.Contains(string(gotJSON), want) {
			t.Fatalf("expected %s in %s", want, gotJSON)
		}
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
