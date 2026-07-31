package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
)

type identityEditFixture struct {
	ctx          context.Context
	h            *handler
	app          *fiber.App
	userID       int
	movieID      int
	movies       *repository.SqliteMoviesRepository
	metadata     *repository.SqliteMovieMetadataRepository
	credits      *repository.SqliteMovieCreditsRepository
	creditPerson int
}

func setupIdentityEditFixture(t *testing.T, tmdbID *int, imdbID *string) identityEditFixture {
	t.Helper()
	t.Setenv("TMDB_API_KEY", "")

	ctx := context.Background()
	h, app, users, movies, pool := setupEditMovieTestWithDB(t)
	userRecord, err := users.Create(ctx, "Identity owner")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movieRecord, err := movies.Add(ctx, "Before", string(domain.MovieStatusStash), userRecord.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}
	if err := movies.SetExternalIDs(ctx, movieRecord.ID, tmdbID, imdbID); err != nil {
		t.Fatalf("set movie identity: %v", err)
	}

	metadata := repository.NewSqliteMovieMetadataRepository(pool)
	if err := metadata.UpsertMetadata(ctx, domain.MovieMetadata{
		MovieID:  movieRecord.ID,
		Overview: "derived marker",
		Genres:   []string{"Science Fiction"},
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	credits := repository.NewSqliteMovieCreditsRepository(pool)
	const personID = 4242
	if err := credits.ReplaceCredits(ctx, movieRecord.ID, []domain.MovieCredit{{
		MovieID: movieRecord.ID,
		Person:  domain.Person{ID: personID, Name: "Derived Person"},
		Kind:    domain.CreditKindCast,
	}}); err != nil {
		t.Fatalf("seed credits: %v", err)
	}

	return identityEditFixture{
		ctx:          ctx,
		h:            h,
		app:          app,
		userID:       userRecord.ID,
		movieID:      movieRecord.ID,
		movies:       movies,
		metadata:     metadata,
		credits:      credits,
		creditPerson: personID,
	}
}

func (f identityEditFixture) put(t *testing.T, title, link string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/movies/%d", f.movieID),
		strings.NewReader(fmt.Sprintf(`{"title":%q,"link":%q}`, title, link)),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(f.userID))
	resp, err := f.app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func (f identityEditFixture) assertDerivedPresent(t *testing.T) {
	t.Helper()
	metadata, err := f.metadata.GetMetadata(f.ctx, f.movieID)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if metadata.Overview != "derived marker" {
		t.Fatalf("metadata overview = %q, want derived marker", metadata.Overview)
	}
	credits, err := f.credits.GetCreditsByMovieIDs(f.ctx, []int{f.movieID})
	if err != nil {
		t.Fatalf("read credits: %v", err)
	}
	if got := credits[f.movieID]; len(got) != 1 || got[0].Person.ID != f.creditPerson {
		t.Fatalf("credits = %+v, want seeded credit", got)
	}
}

func TestHandleEditMovieRejectsInvalidLinkWithoutMutation(t *testing.T) {
	tmdbID := 603
	imdbID := "tt0133093"
	f := setupIdentityEditFixture(t, &tmdbID, &imdbID)
	client, _ := f.h.broker.Subscribe()
	defer f.h.broker.Unsubscribe(client)

	for _, link := range []string{
		"https://example.com/after",
		"https://example.com/title/tt9999999/",
		"https://www.imdb.com.evil.test/title/tt9999999/",
	} {
		resp := f.put(t, "After", link)
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("edit link %q status = %d, want 400", link, resp.StatusCode)
		}

		unchanged, err := f.movies.FindByID(f.ctx, f.movieID)
		if err != nil {
			t.Fatalf("read movie: %v", err)
		}
		if unchanged.Title != "Before" || unchanged.TMDBID == nil || *unchanged.TMDBID != tmdbID ||
			unchanged.IMDbID == nil || *unchanged.IMDbID != imdbID {
			t.Fatalf("movie changed after invalid link: %+v", unchanged)
		}
		f.assertDerivedPresent(t)
	}

	select {
	case got := <-client:
		t.Fatalf("invalid edit broadcast event %q", got.Type)
	default:
	}
}

func TestHandleEditMovieMatchingTMDBLinkPreservesDerivedData(t *testing.T) {
	tmdbID := 603
	f := setupIdentityEditFixture(t, &tmdbID, nil)

	resp := f.put(t, "After", "https://www.themoviedb.org/movie/603-the-matrix?language=en-US")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("edit status = %d, want 200", resp.StatusCode)
	}

	updated, err := f.movies.FindByID(f.ctx, f.movieID)
	if err != nil {
		t.Fatalf("read movie: %v", err)
	}
	if updated.Title != "After" || updated.TMDBID == nil || *updated.TMDBID != tmdbID || updated.IMDbID != nil {
		t.Fatalf("movie after same-TMDB edit = %+v", updated)
	}
	f.assertDerivedPresent(t)
}

func TestHandleEditMovieMatchingTMDBLinkPreservesIMDbAndDerivedData(t *testing.T) {
	tmdbID := 603
	imdbID := "tt0133093"
	f := setupIdentityEditFixture(t, &tmdbID, &imdbID)

	resp := f.put(t, "After", "https://www.themoviedb.org/movie/603-the-matrix")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("edit status = %d, want 200", resp.StatusCode)
	}

	updated, err := f.movies.FindByID(f.ctx, f.movieID)
	if err != nil {
		t.Fatalf("read movie: %v", err)
	}
	if updated.TMDBID == nil || *updated.TMDBID != tmdbID ||
		updated.IMDbID == nil || *updated.IMDbID != imdbID {
		t.Fatalf("movie after same-TMDB edit = %+v", updated)
	}
	f.assertDerivedPresent(t)
}

func TestHandleEditMovieMatchingIMDbLinkPreservesTMDBAndDerivedData(t *testing.T) {
	tmdbID := 603
	imdbID := "tt0133093"
	f := setupIdentityEditFixture(t, &tmdbID, &imdbID)

	resp := f.put(t, "After", "https://www.imdb.com/title/TT0133093/")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("edit status = %d, want 200", resp.StatusCode)
	}

	updated, err := f.movies.FindByID(f.ctx, f.movieID)
	if err != nil {
		t.Fatalf("read movie: %v", err)
	}
	if updated.TMDBID == nil || *updated.TMDBID != tmdbID ||
		updated.IMDbID == nil || *updated.IMDbID != imdbID {
		t.Fatalf("movie after same-IMDb edit = %+v", updated)
	}
	f.assertDerivedPresent(t)
}

func TestHandleEditMovieCanRepointToTMDB(t *testing.T) {
	originalTMDB := 603
	originalIMDb := "tt0133093"
	f := setupIdentityEditFixture(t, &originalTMDB, &originalIMDb)

	resp := f.put(t, "Heat", "https://www.themoviedb.org/movie/949-heat")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("edit status = %d, want 200", resp.StatusCode)
	}

	updated, err := f.movies.FindByID(f.ctx, f.movieID)
	if err != nil {
		t.Fatalf("read movie: %v", err)
	}
	if updated.Title != "Heat" || updated.TMDBID == nil || *updated.TMDBID != 949 || updated.IMDbID != nil {
		t.Fatalf("movie after TMDB re-point = %+v", updated)
	}
	if _, err := f.metadata.GetMetadata(f.ctx, f.movieID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("metadata after re-point = %v, want ErrNotFound", err)
	}
	credits, err := f.credits.GetCreditsByMovieIDs(f.ctx, []int{f.movieID})
	if err != nil {
		t.Fatalf("read credits: %v", err)
	}
	if len(credits[f.movieID]) != 0 {
		t.Fatalf("credits after re-point = %+v, want none", credits[f.movieID])
	}
	candidates, err := f.metadata.NeedsEnrichment(f.ctx, time.Time{}, 100)
	if err != nil {
		t.Fatalf("needs enrichment: %v", err)
	}
	if !candidateIncludes(candidates, f.movieID) {
		t.Fatal("TMDB re-point disappeared from the enrichment backlog")
	}
}

func TestHandleEditMovieDuplicateTMDBRollsBackEdit(t *testing.T) {
	originalTMDB := 603
	originalIMDb := "tt0133093"
	f := setupIdentityEditFixture(t, &originalTMDB, &originalIMDb)

	duplicate, err := f.movies.Add(f.ctx, "Duplicate", string(domain.MovieStatusStash), f.userID)
	if err != nil {
		t.Fatalf("create duplicate movie: %v", err)
	}
	duplicateTMDB := 949
	if err := f.movies.SetExternalIDs(f.ctx, duplicate.ID, &duplicateTMDB, nil); err != nil {
		t.Fatalf("set duplicate identity: %v", err)
	}
	client, _ := f.h.broker.Subscribe()
	defer f.h.broker.Unsubscribe(client)

	resp := f.put(t, "After", "https://www.themoviedb.org/movie/949-heat")
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("edit status = %d, want 409", resp.StatusCode)
	}

	unchanged, err := f.movies.FindByID(f.ctx, f.movieID)
	if err != nil {
		t.Fatalf("read movie: %v", err)
	}
	if unchanged.Title != "Before" || unchanged.TMDBID == nil || *unchanged.TMDBID != originalTMDB ||
		unchanged.IMDbID == nil || *unchanged.IMDbID != originalIMDb {
		t.Fatalf("movie changed after duplicate TMDB edit: %+v", unchanged)
	}
	f.assertDerivedPresent(t)
	select {
	case got := <-client:
		t.Fatalf("failed edit broadcast event %q", got.Type)
	default:
	}
}

func TestEditMovieRejectsInvalidIdentityTargetsWithoutMutation(t *testing.T) {
	tmdbID := 603
	imdbID := "tt0133093"
	f := setupIdentityEditFixture(t, &tmdbID, &imdbID)

	zeroTMDB := 0
	blankIMDb := " "
	malformedIMDb := "tt123"
	otherTMDB := 949
	otherIMDb := "tt0113277"
	tests := []struct {
		name   string
		target domain.MovieIdentityTarget
	}{
		{name: "neither provider"},
		{name: "both providers", target: domain.MovieIdentityTarget{TMDBID: &otherTMDB, IMDbID: &otherIMDb}},
		{name: "TMDB plus blank IMDb", target: domain.MovieIdentityTarget{TMDBID: &otherTMDB, IMDbID: &blankIMDb}},
		{name: "zero TMDB", target: domain.MovieIdentityTarget{TMDBID: &zeroTMDB}},
		{name: "blank IMDb", target: domain.MovieIdentityTarget{IMDbID: &blankIMDb}},
		{name: "malformed IMDb", target: domain.MovieIdentityTarget{IMDbID: &malformedIMDb}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated, changed, err := f.movies.EditMovie(
				f.ctx,
				f.movieID,
				f.userID,
				"After",
				tt.target,
				nil,
			)
			if !errors.Is(err, domain.ErrInvalidInput) {
				t.Fatalf("EditMovie error = %v, want ErrInvalidInput", err)
			}
			if updated != nil || changed {
				t.Fatalf("EditMovie result = %+v, changed %v; want nil, false", updated, changed)
			}

			unchanged, err := f.movies.FindByID(f.ctx, f.movieID)
			if err != nil {
				t.Fatalf("read movie: %v", err)
			}
			if unchanged.Title != "Before" || unchanged.TMDBID == nil || *unchanged.TMDBID != tmdbID ||
				unchanged.IMDbID == nil || *unchanged.IMDbID != imdbID {
				t.Fatalf("movie changed after invalid target: %+v", unchanged)
			}
			f.assertDerivedPresent(t)
		})
	}
}

func TestHandleAddMovieRejectsForeignIMDbSubstring(t *testing.T) {
	t.Setenv("TMDB_API_KEY", "")
	ctx := context.Background()
	_, app, users, _, pool := setupEditMovieTestWithDB(t)
	userRecord, err := users.Create(ctx, "Manual adder")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/movies",
		strings.NewReader(`{"title":"Fake","link":"https://example.com/title/tt0133093/"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(userRecord.ID))
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("add status = %d, want 400", resp.StatusCode)
	}

	var count int
	if err := pool.Read.QueryRowContext(ctx, "SELECT COUNT(*) FROM movies").Scan(&count); err != nil {
		t.Fatalf("count movies: %v", err)
	}
	if count != 0 {
		t.Fatalf("movie count = %d, want 0", count)
	}
}

func TestHandleAddMovieAcceptsTMDBMovieLink(t *testing.T) {
	t.Setenv("TMDB_API_KEY", "")
	ctx := context.Background()
	_, app, users, movies, _ := setupEditMovieTestWithDB(t)
	userRecord, err := users.Create(ctx, "Manual TMDB adder")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/movies",
		strings.NewReader(`{"title":"The Matrix","link":"https://www.themoviedb.org/movie/603-the-matrix"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(userRecord.ID))
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("add status = %d, want 201", resp.StatusCode)
	}

	stashed, err := movies.FindByUserIDAndStatus(ctx, userRecord.ID, string(domain.MovieStatusStash))
	if err != nil {
		t.Fatalf("read stash: %v", err)
	}
	if len(stashed) != 1 || stashed[0].TMDBID == nil || *stashed[0].TMDBID != 603 ||
		stashed[0].IMDbID != nil {
		t.Fatalf("stashed movie = %+v, want TMDB 603 only", stashed)
	}
}
