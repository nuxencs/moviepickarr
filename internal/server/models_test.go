package server

import (
	"testing"

	"moviepickarr/internal/domain"
)

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
		{"imdb preferred over tmdb", domain.Movie{IMDbID: &imdb, TMDBID: &tmdb, Link: "raw"}, "https://www.imdb.com/title/tt0133093/"},
		{"tmdb when no imdb", domain.Movie{TMDBID: &tmdb, Link: "raw"}, "https://www.themoviedb.org/movie/603"},
		{"empty imdb falls through to tmdb", domain.Movie{IMDbID: &empty, TMDBID: &tmdb}, "https://www.themoviedb.org/movie/603"},
		{"fallback to stored link", domain.Movie{Link: "https://example.com/x"}, "https://example.com/x"},
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
