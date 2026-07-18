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
	bare := toFullMovie(&movie, nil, nil)
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
	for _, key := range []string{"posterPath", "backdropPath", "releaseDate", "runtime", "genres", "voteAverage", "tagline", "overview", "imdbId", "cast", "crew"} {
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
	got := toFullMovie(&movie, md, nil)
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

func TestToAPIMovieMeta_FoldsCredits(t *testing.T) {
	t.Parallel()

	movie := domain.Movie{ID: 7, Title: "The Matrix"}
	keanuProfile := "/kr.jpg"
	credits := []domain.MovieCredit{
		// Repo order: cast first (billing order), then crew.
		{
			MovieID:   7,
			Person:    domain.Person{ID: 6384, Name: "Keanu Reeves", ProfilePath: &keanuProfile},
			Kind:      domain.CreditKindCast,
			Character: "Neo",
			CastOrder: 0,
		},
		{
			MovieID:   7,
			Person:    domain.Person{ID: 530, Name: "Carrie-Anne Moss"},
			Kind:      domain.CreditKindCast,
			Character: "Trinity",
			CastOrder: 1,
		},
		{
			MovieID:    7,
			Person:     domain.Person{ID: 9340, Name: "Lana Wachowski"},
			Kind:       domain.CreditKindCrew,
			Job:        "Director",
			Department: "Directing",
		},
	}

	got := toFullMovie(&movie, nil, credits)
	if len(got.Cast) != 2 || len(got.Crew) != 1 {
		t.Fatalf("expected 2 cast + 1 crew, got %d/%d", len(got.Cast), len(got.Crew))
	}
	if got.Cast[0].ID != 6384 || got.Cast[0].Name != "Keanu Reeves" || got.Cast[0].Character != "Neo" {
		t.Fatalf("cast[0] mismatch: %+v", got.Cast[0])
	}
	if got.Cast[0].ProfilePath != "/kr.jpg" || got.Cast[1].ProfilePath != "" {
		t.Fatalf("profile fold mismatch: %+v", got.Cast)
	}
	if got.Cast[1].Name != "Carrie-Anne Moss" || got.Cast[1].Character != "Trinity" {
		t.Fatalf("cast[1] mismatch (input order must be preserved): %+v", got.Cast[1])
	}
	if got.Crew[0].ID != 9340 || got.Crew[0].Job != "Director" {
		t.Fatalf("crew[0] mismatch: %+v", got.Crew[0])
	}
	// Cast rows never carry a job, crew rows never a character.
	if got.Cast[0].Job != "" || got.Crew[0].Character != "" {
		t.Fatalf("kind fields leaked across: %+v / %+v", got.Cast[0], got.Crew[0])
	}

	// Exact wire keys per the TS contract (web/src/types/Response.ts).
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"cast":[{"id":6384,"name":"Keanu Reeves","profilePath":"/kr.jpg","character":"Neo"}`,
		`{"id":530,"name":"Carrie-Anne Moss","character":"Trinity"}]`,
		`"crew":[{"id":9340,"name":"Lana Wachowski","job":"Director"}]`,
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

// The Members boards ship lean tiles: even a fully enriched, credited movie
// must serialize without the modal-only fields (backdrop/tagline/overview/
// cast/crew). The board grids read tile data only; the modal lazy-loads the
// full record from GET /movies/:id. Assert against the marshaled userResponse
// so a type regressed back to fullMovie is caught here, not on the wire.
func TestToAPIUserMeta_ShipsLeanTiles(t *testing.T) {
	t.Parallel()

	tmdb := 603
	user := &domain.User{ID: 1, Name: "Alice"}
	pool := []*domain.Movie{{ID: 7, Title: "The Matrix", TMDBID: &tmdb, AddedByID: 1}}

	poster, backdrop := "/p.jpg", "/b.jpg"
	meta := metaByID{7: &domain.MovieMetadata{
		MovieID:      7,
		PosterPath:   &poster,
		BackdropPath: &backdrop,
		ReleaseDate:  "1999-03-30",
		Runtime:      136,
		Genres:       []string{"Action", "Sci-Fi"},
		VoteAverage:  8.2,
		Tagline:      "Free your mind.",
		Overview:     "A hacker learns the truth.",
	}}

	resp := toAPIUserMeta(user, pool, nil, meta)
	respJSON, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Tile fields survive.
	for _, want := range []string{`"posterPath":"/p.jpg"`, `"runtime":136`, `"voteAverage":8.2`} {
		if !strings.Contains(string(respJSON), want) {
			t.Fatalf("expected tile field %s in %s", want, respJSON)
		}
	}
	// Modal-only fields are structurally absent from the board payload.
	for _, key := range []string{"backdropPath", "tagline", "overview", "cast", "crew"} {
		if strings.Contains(string(respJSON), `"`+key+`"`) {
			t.Fatalf("expected %q omitted from lean board tile, got %s", key, respJSON)
		}
	}
}
