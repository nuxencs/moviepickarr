package server

import (
	"testing"

	"moviepickarr/internal/domain"
)

func TestParseMovieLink(t *testing.T) {
	t.Parallel()

	tmdb603 := 603
	imdbMatrix := "tt0133093"
	tests := []struct {
		name  string
		input string
		want  domain.MovieIdentityTarget
	}{
		{
			name:  "canonical IMDb",
			input: "https://www.imdb.com/title/tt0133093/",
			want:  domain.MovieIdentityTarget{IMDbID: &imdbMatrix},
		},
		{
			name:  "bare IMDb host and query",
			input: " https://imdb.com/title/TT0133093?ref_=fn_all_ttl_1 ",
			want:  domain.MovieIdentityTarget{IMDbID: &imdbMatrix},
		},
		{
			name:  "uppercase HTTPS scheme",
			input: "HTTPS://www.imdb.com/title/tt0133093/",
			want:  domain.MovieIdentityTarget{IMDbID: &imdbMatrix},
		},
		{
			name:  "encoded separator in query",
			input: "https://www.imdb.com/title/tt0133093/?next=%2Ffoo#jump",
			want:  domain.MovieIdentityTarget{IMDbID: &imdbMatrix},
		},
		{
			name:  "canonical TMDB",
			input: "https://www.themoviedb.org/movie/603",
			want:  domain.MovieIdentityTarget{TMDBID: &tmdb603},
		},
		{
			name:  "TMDB slug and query",
			input: "https://themoviedb.org/movie/603-the-matrix?language=en-US",
			want:  domain.MovieIdentityTarget{TMDBID: &tmdb603},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMovieLink(tt.input)
			if err != nil {
				t.Fatalf("parseMovieLink: %v", err)
			}
			assertMovieIdentity(t, got, tt.want)
		})
	}
}

func TestParseMovieLinkRejectsNonMovieURLs(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"",
		"//www.imdb.com/title/tt0133093/",
		"https://example.com/title/tt0133093/",
		"https://www.imdb.com.evil.test/title/tt0133093/",
		"http://www.imdb.com/title/tt0133093/",
		"https://user@www.imdb.com/title/tt0133093/",
		"https://www.imdb.com:443/title/tt0133093/",
		"https://www.imdb.com:/title/tt0133093/",
		"https://www.imdb.com/name/tt0133093/",
		"https://www.imdb.com/title/tt0133093/reviews",
		"https://www.imdb.com/title/x/../tt0133093/",
		"https://www.imdb.com/%2e%2e/title/tt0133093/",
		"https://www.imdb.com/\ttitle/tt0133093/",
		"https://www.imdb.com/\ntitle/tt0133093/",
		"https://www.imdb.com/title/tt013309/",
		"https://www.imdb.com/title/tt013309399/",
		"https://www.themoviedb.org/person/603",
		"https://www.themoviedb.org/movie/0",
		"https://www.themoviedb.org/movie/-603",
		"https://www.themoviedb.org/en/movie/603-the-matrix",
		"https://www.themoviedb.org/movie/603-the%2Fmatrix",
		"https://www.themoviedb.org/movie/603-the%5Cmatrix",
		"https://www.themoviedb.org/movie/9007199254740992",
		"https://www.themoviedb.org/movie/999999999999999999999999999999",
		"javascript:alert(1)",
	}

	for _, input := range invalid {
		t.Run(input, func(t *testing.T) {
			if got, err := parseMovieLink(input); err == nil {
				t.Fatalf("parseMovieLink(%q) = %+v, want error", input, got)
			}
		})
	}
}

func assertMovieIdentity(t *testing.T, got, want domain.MovieIdentityTarget) {
	t.Helper()

	if (got.TMDBID == nil) != (want.TMDBID == nil) ||
		(got.TMDBID != nil && *got.TMDBID != *want.TMDBID) {
		t.Fatalf("TMDB identity = %v, want %v", got.TMDBID, want.TMDBID)
	}
	if (got.IMDbID == nil) != (want.IMDbID == nil) ||
		(got.IMDbID != nil && *got.IMDbID != *want.IMDbID) {
		t.Fatalf("IMDb identity = %v, want %v", got.IMDbID, want.IMDbID)
	}
}
