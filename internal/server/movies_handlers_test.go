package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
	integrationtmdb "moviepickarr/internal/integration/tmdb"
	"moviepickarr/internal/movie"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
)

const (
	testMemberHeader = "X-Test-Member"
	testRoleHeader   = "X-Test-Role"
)

// mountTestV1 mounts the v1 routes behind a middleware that injects the session
// actor from test headers, standing in for the real csrfGuard → requireSession
// chain. Role defaults to admin so the draw/reveal/watch guard passes without
// having to wire next-up; the adder-only checks compare the member id, which the
// adder tests set explicitly via testMemberHeader.
func mountTestV1(app *fiber.App, h *handler) {
	v1 := app.Group("/api/v1")
	v1.Use(func(c *fiber.Ctx) error {
		if raw := c.Get(testMemberHeader); raw != "" {
			if id, err := strconv.Atoi(raw); err == nil {
				c.Locals(localsMemberID, id)
			}
		}
		role := c.Get(testRoleHeader)
		if role == "" {
			role = "admin"
		}
		c.Locals(localsRole, role)
		return c.Next()
	})
	registerV1Routes(v1, h)
}

func setupEditMovieTest(t *testing.T) (*handler, *fiber.App, *repository.SqliteUserRepository, *repository.SqliteMoviesRepository) {
	t.Helper()

	h, app, userRepo, movieRepo, _ := setupEditMovieTestWithDB(t)
	return h, app, userRepo, movieRepo
}

func setupEditMovieTestWithDB(t *testing.T) (*handler, *fiber.App, *repository.SqliteUserRepository, *repository.SqliteMoviesRepository, *db.Pool) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "moviepickarr-test.db")
	dbConn, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.RunMigrations(ctx, dbConn.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	h := newHandler(dbConn, zerolog.Nop())
	t.Cleanup(func() {
		h.Close()
		if err := dbConn.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})

	app := fiber.New()
	mountTestV1(app, h)

	return h, app, repository.NewSqliteUserRepository(dbConn), repository.NewSqliteMoviesRepository(dbConn), dbConn
}

func TestHandleGetPooledMovies_ShipsLeanTiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)
	metaRepo := repository.NewSqliteMovieMetadataRepository(dbConn)
	creditsRepo := repository.NewSqliteMovieCreditsRepository(dbConn)

	user, err := userRepo.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	pooled, err := movieRepo.Add(ctx, "The Matrix", "pool", user.ID)
	if err != nil {
		t.Fatalf("create pooled movie: %v", err)
	}

	poster, backdrop := "/poster.jpg", "/backdrop.jpg"
	if err := metaRepo.UpsertMetadata(ctx, domain.MovieMetadata{
		MovieID:      pooled.ID,
		PosterPath:   &poster,
		BackdropPath: &backdrop,
		Runtime:      136,
		VoteAverage:  8.2,
		Tagline:      "Free your mind.",
		Overview:     "A hacker learns the truth.",
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	if err := creditsRepo.ReplaceCredits(ctx, pooled.ID, []domain.MovieCredit{{
		MovieID: pooled.ID,
		Person:  domain.Person{ID: 9340, Name: "Lana Wachowski"},
		Kind:    domain.CreditKindCrew,
		Job:     "Director",
	}}); err != nil {
		t.Fatalf("seed credits: %v", err)
	}

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/movies/pool", nil), -1)
	if err != nil {
		t.Fatalf("get pooled movies: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("get pooled movies status = %d, want %d", resp.StatusCode, fiber.StatusOK)
	}

	var payload []map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode pooled movies: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("pooled movie count = %d, want 1", len(payload))
	}
	for _, key := range []string{"movieID", "title", "posterPath", "runtime", "voteAverage"} {
		if _, ok := payload[0][key]; !ok {
			t.Errorf("pooled tile missing %q: %v", key, payload[0])
		}
	}
	for _, key := range []string{"status", "backdropPath", "tagline", "overview", "cast", "crew"} {
		if _, ok := payload[0][key]; ok {
			t.Errorf("pooled tile includes detail field %q: %v", key, payload[0])
		}
	}
}

func TestHandleEditMovie_RejectsWatchedAtForNonWatchedMovie(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Alice")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Before", "pool", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}
	imdbID := "tt0133093"
	if err := movieRepo.SetExternalIDs(ctx, movie.ID, nil, &imdbID); err != nil {
		t.Fatalf("set movie identity: %v", err)
	}

	body := `{"title":"After","link":"https://www.imdb.com/title/tt0133093/","watchedAt":"2026-02-08T18:30:00Z"}`
	req := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/movies/%d", movie.ID),
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}

	unchanged, err := movieRepo.FindByID(ctx, movie.ID)
	if err != nil {
		t.Fatalf("fetch movie: %v", err)
	}
	if unchanged.Title != "Before" {
		t.Fatalf("expected title unchanged, got %q", unchanged.Title)
	}
}

func TestHandleEditMovie_UpdatesWatchedMovieWithWatchedAt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Bob")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Before", "pool", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}
	imdbID := "tt0133093"
	if err := movieRepo.SetExternalIDs(ctx, movie.ID, nil, &imdbID); err != nil {
		t.Fatalf("set movie identity: %v", err)
	}

	initialWatchedAt := time.Date(2026, 2, 7, 13, 0, 0, 0, time.UTC)
	if err := movieRepo.MarkAsWatched(ctx, movie.ID, initialWatchedAt); err != nil {
		t.Fatalf("mark watched: %v", err)
	}

	body := `{"title":"After","link":"https://www.imdb.com/title/tt0133093/","watchedAt":"2026-02-08T16:45:00Z"}`
	req := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/movies/%d", movie.ID),
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	updated, err := movieRepo.FindByID(ctx, movie.ID)
	if err != nil {
		t.Fatalf("fetch updated movie: %v", err)
	}
	if updated.Title != "After" {
		t.Fatalf("expected title updated, got %q", updated.Title)
	}
	if updated.WatchedAt == nil {
		t.Fatalf("expected watchedAt to be set")
	}

	expectedWatchedAt := time.Date(2026, 2, 8, 16, 45, 0, 0, time.UTC)
	if !updated.WatchedAt.Equal(expectedWatchedAt) {
		t.Fatalf("expected watchedAt %v, got %v", expectedWatchedAt, updated.WatchedAt)
	}
}

func TestHandleEditMovie_IdentityChangeRemovesDerivedDataAndStaysQueued(t *testing.T) {
	t.Setenv("TMDB_API_KEY", "")

	ctx := context.Background()
	h, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)
	metaRepo := repository.NewSqliteMovieMetadataRepository(dbConn)
	creditsRepo := repository.NewSqliteMovieCreditsRepository(dbConn)
	if _, err := h.tmdbIntegration.Acquire(ctx); !errors.Is(err, integrationtmdb.ErrRuntimeDisabled) {
		t.Fatalf("TMDB runtime error = %v, want disabled", err)
	}

	user, err := userRepo.Create(ctx, "Cleanup owner")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	target, err := movieRepo.Add(ctx, "Before", "pool", user.ID)
	if err != nil {
		t.Fatalf("create target movie: %v", err)
	}
	originalTMDB := 199
	originalIMDb := "tt0000098"
	if err := movieRepo.SetExternalIDs(ctx, target.ID, &originalTMDB, &originalIMDb); err != nil {
		t.Fatalf("set target identity: %v", err)
	}
	watchedAt := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	if err := movieRepo.MarkAsWatched(ctx, target.ID, watchedAt); err != nil {
		t.Fatalf("mark target watched: %v", err)
	}
	if err := metaRepo.UpsertMetadata(ctx, domain.MovieMetadata{
		MovieID:  target.ID,
		Overview: "old metadata",
		Genres:   []string{"Old"},
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	const personID = 98
	if err := creditsRepo.ReplaceCredits(ctx, target.ID, []domain.MovieCredit{
		{
			MovieID:   target.ID,
			Person:    domain.Person{ID: personID, Name: "Shared Person"},
			Kind:      domain.CreditKindCast,
			Character: "Old",
		},
	}); err != nil {
		t.Fatalf("seed credits: %v", err)
	}

	cacheNow := time.Now().UTC()
	h.setCachedStats("warm", statsResponse{SelectedWindow: "all-time"}, cacheNow)
	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	req := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/movies/%d", target.ID),
		strings.NewReader(
			`{"title":"After","link":"https://www.imdb.com/title/tt0000099/"}`,
		),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	updated, err := movieRepo.FindByID(ctx, target.ID)
	if err != nil {
		t.Fatalf("fetch updated target: %v", err)
	}
	if updated.Title != "After" ||
		updated.TMDBID != nil ||
		updated.IMDbID == nil ||
		*updated.IMDbID != "tt0000099" {
		t.Fatalf("movie after identity edit = %+v", updated)
	}
	if _, err := metaRepo.GetMetadata(ctx, target.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("metadata after identity edit = %v, want ErrNotFound", err)
	}
	gotCredits, err := creditsRepo.GetCreditsByMovieIDs(ctx, []int{target.ID})
	if err != nil {
		t.Fatalf("read credits after identity edit: %v", err)
	}
	if len(gotCredits[target.ID]) != 0 {
		t.Fatalf("credits after identity edit = %+v, want none", gotCredits[target.ID])
	}
	var people int
	if err := dbConn.Read.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM people WHERE id = ?",
		personID,
	).Scan(&people); err != nil {
		t.Fatalf("count shared people: %v", err)
	}
	if people != 1 {
		t.Fatalf("shared people rows after identity edit = %d, want 1", people)
	}
	candidates, err := metaRepo.NeedsEnrichment(ctx, time.Time{}, 100)
	if err != nil {
		t.Fatalf("needs enrichment: %v", err)
	}
	if !candidateIncludes(candidates, target.ID) {
		t.Fatal("identity edit disappeared from enrichment backlog with enrichment disabled")
	}
	if _, ok := h.getCachedStats("warm", cacheNow); ok {
		t.Fatal("successful watched edit left the stats cache warm")
	}
	select {
	case got := <-client:
		if got.Type != "movie:updated" {
			t.Fatalf("edit broadcast event %q, want movie:updated", got.Type)
		}
	default:
		t.Fatal("successful identity edit did not broadcast movie:updated")
	}
	select {
	case got := <-client:
		t.Fatalf("successful identity edit broadcast extra event %q", got.Type)
	default:
	}
}

func TestHandleEditMovie_DuplicateIMDbRollsBackWholeEdit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)

	user, err := userRepo.Create(ctx, "IMDb owner")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	duplicate, err := movieRepo.Add(ctx, "Duplicate", "stash", user.ID)
	if err != nil {
		t.Fatalf("create duplicate movie: %v", err)
	}
	target, err := movieRepo.Add(ctx, "Before", "pool", user.ID)
	if err != nil {
		t.Fatalf("create target movie: %v", err)
	}

	duplicateIMDb := "tt0000001"
	if err := movieRepo.SetExternalIDs(ctx, duplicate.ID, nil, &duplicateIMDb); err != nil {
		t.Fatalf("set duplicate identity: %v", err)
	}
	originalTMDB := 200
	originalIMDb := "tt0000002"
	if err := movieRepo.SetExternalIDs(ctx, target.ID, &originalTMDB, &originalIMDb); err != nil {
		t.Fatalf("set target identity: %v", err)
	}
	originalWatchedAt := time.Date(2026, 2, 7, 13, 0, 0, 0, time.UTC)
	if err := movieRepo.MarkAsWatched(ctx, target.ID, originalWatchedAt); err != nil {
		t.Fatalf("mark target watched: %v", err)
	}
	const originalMarker = int64(1_700_000_000)
	if _, err := dbConn.Write.ExecContext(ctx,
		`INSERT INTO movie_metadata (movie_id, credits_refreshed_at) VALUES (?, ?)`,
		target.ID, originalMarker,
	); err != nil {
		t.Fatalf("seed metadata marker: %v", err)
	}
	cacheNow := time.Now().UTC()
	h.setCachedStats("warm", statsResponse{SelectedWindow: "all-time"}, cacheNow)
	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	body := fmt.Sprintf(
		`{"title":"After","link":"https://www.imdb.com/title/%s/","watchedAt":"2026-02-08T16:45:00Z"}`,
		duplicateIMDb,
	)
	req := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/movies/%d", target.ID),
		strings.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("expected status 409, got %d", resp.StatusCode)
	}

	unchanged, err := movieRepo.FindByID(ctx, target.ID)
	if err != nil {
		t.Fatalf("fetch target after conflict: %v", err)
	}
	if unchanged.Title != "Before" {
		t.Fatalf("title after conflict = %q, want Before", unchanged.Title)
	}
	if unchanged.WatchedAt == nil || !unchanged.WatchedAt.Equal(originalWatchedAt) {
		t.Fatalf("watchedAt after conflict = %v, want %v", unchanged.WatchedAt, originalWatchedAt)
	}
	if unchanged.TMDBID == nil || *unchanged.TMDBID != originalTMDB ||
		unchanged.IMDbID == nil || *unchanged.IMDbID != originalIMDb {
		t.Fatalf("identity after conflict = %v/%v, want %d/%s",
			unchanged.TMDBID, unchanged.IMDbID, originalTMDB, originalIMDb)
	}

	var marker int64
	if err := dbConn.Read.QueryRowContext(ctx,
		`SELECT credits_refreshed_at FROM movie_metadata WHERE movie_id = ?`, target.ID,
	).Scan(&marker); err != nil {
		t.Fatalf("read metadata marker: %v", err)
	}
	if marker != originalMarker {
		t.Fatalf("metadata marker after conflict = %d, want %d", marker, originalMarker)
	}
	if _, ok := h.getCachedStats("warm", cacheNow); !ok {
		t.Fatal("failed edit invalidated the stats cache")
	}
	select {
	case got := <-client:
		t.Fatalf("failed edit broadcast event %q", got.Type)
	default:
	}
}

func TestHandleEditMovie_TitleOnlyNormalizesIMDbWithoutClearingDerivedData(t *testing.T) {
	t.Setenv("TMDB_API_KEY", "")

	ctx := context.Background()
	_, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)
	metaRepo := repository.NewSqliteMovieMetadataRepository(dbConn)
	creditsRepo := repository.NewSqliteMovieCreditsRepository(dbConn)

	user, err := userRepo.Create(ctx, "Legacy IMDb owner")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	target, err := movieRepo.Add(ctx, "Before", "stash", user.ID)
	if err != nil {
		t.Fatalf("create target movie: %v", err)
	}
	if _, err := dbConn.Write.ExecContext(ctx,
		`UPDATE movies SET imdb_id = '  TT7654000  ' WHERE id = ?`,
		target.ID,
	); err != nil {
		t.Fatalf("seed legacy IMDb identity: %v", err)
	}
	if err := metaRepo.UpsertMetadata(ctx, domain.MovieMetadata{
		MovieID:  target.ID,
		Overview: "keep metadata",
		Genres:   []string{"Drama"},
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	if err := creditsRepo.ReplaceCredits(ctx, target.ID, []domain.MovieCredit{{
		MovieID:   target.ID,
		Person:    domain.Person{ID: 7654, Name: "Keep Person"},
		Kind:      domain.CreditKindCast,
		Character: "Lead",
	}}); err != nil {
		t.Fatalf("seed credits: %v", err)
	}

	before, err := movieRepo.FindByID(ctx, target.ID)
	if err != nil {
		t.Fatalf("read target before edit: %v", err)
	}
	body, err := json.Marshal(map[string]string{
		"title": "After",
		"link":  movieLink(before),
	})
	if err != nil {
		t.Fatalf("marshal edit request: %v", err)
	}
	req := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/movies/%d", target.ID),
		strings.NewReader(string(body)),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	updated, err := movieRepo.FindByID(ctx, target.ID)
	if err != nil {
		t.Fatalf("read target after edit: %v", err)
	}
	if updated.Title != "After" || updated.IMDbID == nil || *updated.IMDbID != "tt7654000" {
		t.Fatalf("movie after title-only edit = title %q, IMDb %v", updated.Title, updated.IMDbID)
	}
	md, err := metaRepo.GetMetadata(ctx, target.ID)
	if err != nil {
		t.Fatalf("read metadata after title-only edit: %v", err)
	}
	if md.Overview != "keep metadata" {
		t.Fatalf("metadata after title-only edit = %q", md.Overview)
	}
	gotCredits, err := creditsRepo.GetCreditsByMovieIDs(ctx, []int{target.ID})
	if err != nil {
		t.Fatalf("read credits after title-only edit: %v", err)
	}
	if rows := gotCredits[target.ID]; len(rows) != 1 || rows[0].Person.Name != "Keep Person" {
		t.Fatalf("credits after title-only edit = %+v", rows)
	}
}

func TestHandleEditMovie_SecondDerivedCleanupFailureRollsBackWholeEdit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)
	creditsRepo := repository.NewSqliteMovieCreditsRepository(dbConn)

	user, err := userRepo.Create(ctx, "Marker owner")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	target, err := movieRepo.Add(ctx, "Before", "pool", user.ID)
	if err != nil {
		t.Fatalf("create target movie: %v", err)
	}
	originalTMDB := 201
	originalIMDb := "tt0000003"
	if err := movieRepo.SetExternalIDs(ctx, target.ID, &originalTMDB, &originalIMDb); err != nil {
		t.Fatalf("set target identity: %v", err)
	}
	originalWatchedAt := time.Date(2026, 2, 7, 14, 0, 0, 0, time.UTC)
	if err := movieRepo.MarkAsWatched(ctx, target.ID, originalWatchedAt); err != nil {
		t.Fatalf("mark target watched: %v", err)
	}
	const originalMarker = int64(1_700_000_001)
	if _, err := dbConn.Write.ExecContext(ctx,
		`INSERT INTO movie_metadata (movie_id, credits_refreshed_at) VALUES (?, ?)`,
		target.ID, originalMarker,
	); err != nil {
		t.Fatalf("seed metadata marker: %v", err)
	}
	const personID = 103
	if err := creditsRepo.ReplaceCredits(ctx, target.ID, []domain.MovieCredit{
		{
			MovieID:   target.ID,
			Person:    domain.Person{ID: personID, Name: "Rollback Person"},
			Kind:      domain.CreditKindCast,
			Character: "Before",
		},
	}); err != nil {
		t.Fatalf("seed credits: %v", err)
	}
	if _, err := dbConn.Write.ExecContext(ctx,
		"UPDATE movie_metadata SET credits_refreshed_at = ? WHERE movie_id = ?",
		originalMarker,
		target.ID,
	); err != nil {
		t.Fatalf("restore metadata marker: %v", err)
	}
	if _, err := dbConn.Write.ExecContext(ctx, fmt.Sprintf(`
		CREATE TRIGGER fail_movie_edit_metadata_cleanup
		BEFORE DELETE ON movie_metadata
		WHEN OLD.movie_id = %d
		BEGIN
			SELECT RAISE(ABORT, 'forced metadata cleanup failure');
		END
	`, target.ID)); err != nil {
		t.Fatalf("install metadata cleanup failure: %v", err)
	}

	cacheNow := time.Now().UTC()
	h.setCachedStats("warm", statsResponse{SelectedWindow: "all-time"}, cacheNow)
	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	req := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/movies/%d", target.ID),
		strings.NewReader(
			`{"title":"After","link":"https://www.imdb.com/title/tt0000004/","watchedAt":"2026-02-08T17:45:00Z"}`,
		),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", resp.StatusCode)
	}

	unchanged, err := movieRepo.FindByID(ctx, target.ID)
	if err != nil {
		t.Fatalf("fetch target after cleanup failure: %v", err)
	}
	if unchanged.Title != "Before" {
		t.Fatalf("title after cleanup failure = %q, want Before", unchanged.Title)
	}
	if unchanged.WatchedAt == nil || !unchanged.WatchedAt.Equal(originalWatchedAt) {
		t.Fatalf("watchedAt after cleanup failure = %v, want %v", unchanged.WatchedAt, originalWatchedAt)
	}
	if unchanged.TMDBID == nil || *unchanged.TMDBID != originalTMDB ||
		unchanged.IMDbID == nil || *unchanged.IMDbID != originalIMDb {
		t.Fatalf("identity after cleanup failure = %v/%v, want %d/%s",
			unchanged.TMDBID, unchanged.IMDbID, originalTMDB, originalIMDb)
	}

	var marker int64
	if err := dbConn.Read.QueryRowContext(ctx,
		`SELECT credits_refreshed_at FROM movie_metadata WHERE movie_id = ?`, target.ID,
	).Scan(&marker); err != nil {
		t.Fatalf("read metadata marker: %v", err)
	}
	if marker != originalMarker {
		t.Fatalf("metadata marker after failure = %d, want %d", marker, originalMarker)
	}
	gotCredits, err := creditsRepo.GetCreditsByMovieIDs(ctx, []int{target.ID})
	if err != nil {
		t.Fatalf("read credits after cleanup failure: %v", err)
	}
	if rows := gotCredits[target.ID]; len(rows) != 1 || rows[0].Person.ID != personID {
		t.Fatalf("credits after cleanup failure = %+v, want original row", rows)
	}
	var people int
	if err := dbConn.Read.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM people WHERE id = ?",
		personID,
	).Scan(&people); err != nil {
		t.Fatalf("count people after cleanup failure: %v", err)
	}
	if people != 1 {
		t.Fatalf("people after cleanup failure = %d, want 1", people)
	}
	if _, ok := h.getCachedStats("warm", cacheNow); !ok {
		t.Fatal("failed edit invalidated the stats cache")
	}
	select {
	case got := <-client:
		t.Fatalf("failed edit broadcast event %q", got.Type)
	default:
	}
}

func TestHandleEditMovie_ResponseReadFailureRollsBack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)

	user, err := userRepo.Create(ctx, "Read owner")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	target, err := movieRepo.Add(ctx, "Before", "stash", user.ID)
	if err != nil {
		t.Fatalf("create target movie: %v", err)
	}
	imdbID := "tt0000005"
	if err := movieRepo.SetExternalIDs(ctx, target.ID, nil, &imdbID); err != nil {
		t.Fatalf("set target identity: %v", err)
	}
	if _, err := dbConn.Write.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE movie_edit_read_failures (movie_id INTEGER NOT NULL);
		CREATE TRIGGER fail_movie_edit_response_read
		AFTER UPDATE OF title ON movies
		WHEN NEW.id = %d AND NEW.title = 'Unreadable edit'
		BEGIN
			INSERT INTO movie_edit_read_failures (movie_id) VALUES (NEW.id);
			DELETE FROM movies WHERE id = NEW.id;
		END
	`, target.ID)); err != nil {
		t.Fatalf("install response-read failure: %v", err)
	}

	cacheNow := time.Now().UTC()
	h.setCachedStats("warm", statsResponse{SelectedWindow: "all-time"}, cacheNow)
	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	req := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/movies/%d", target.ID),
		strings.NewReader(
			`{"title":"Unreadable edit","link":"https://www.imdb.com/title/tt0000005/"}`,
		),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}

	unchanged, err := movieRepo.FindByID(ctx, target.ID)
	if err != nil {
		t.Fatalf("fetch target after response-read failure: %v", err)
	}
	if unchanged.Title != "Before" {
		t.Fatalf("title after response-read failure = %q, want Before", unchanged.Title)
	}
	if unchanged.IMDbID == nil || *unchanged.IMDbID != imdbID {
		t.Fatalf("IMDb id after response-read failure = %v, want %s", unchanged.IMDbID, imdbID)
	}

	var markers int
	if err := dbConn.Read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM movie_edit_read_failures`,
	).Scan(&markers); err != nil {
		t.Fatalf("count response-read markers: %v", err)
	}
	if markers != 0 {
		t.Fatalf("response-read failure left %d markers, want 0", markers)
	}
	if _, ok := h.getCachedStats("warm", cacheNow); !ok {
		t.Fatal("failed edit invalidated the stats cache")
	}
	select {
	case got := <-client:
		t.Fatalf("failed edit broadcast event %q", got.Type)
	default:
	}
}

func TestHandleEditMovie_WatchedTitleInvalidatesStats(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)
	metaRepo := repository.NewSqliteMovieMetadataRepository(dbConn)
	creditsRepo := repository.NewSqliteMovieCreditsRepository(dbConn)

	user, err := userRepo.Create(ctx, "Stats owner")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	target, err := movieRepo.Add(ctx, "Before", "pool", user.ID)
	if err != nil {
		t.Fatalf("create target movie: %v", err)
	}
	imdbID := "tt0000006"
	if err := movieRepo.SetExternalIDs(ctx, target.ID, nil, &imdbID); err != nil {
		t.Fatalf("set target identity: %v", err)
	}
	originalWatchedAt := time.Date(2026, 2, 7, 15, 0, 0, 0, time.UTC)
	if err := movieRepo.MarkAsWatched(ctx, target.ID, originalWatchedAt); err != nil {
		t.Fatalf("mark target watched: %v", err)
	}
	if err := metaRepo.UpsertMetadata(ctx, domain.MovieMetadata{
		MovieID:  target.ID,
		Overview: "same identity metadata",
		Genres:   []string{"Drama"},
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	if err := creditsRepo.ReplaceCredits(ctx, target.ID, []domain.MovieCredit{
		{
			MovieID:   target.ID,
			Person:    domain.Person{ID: 106, Name: "Same Identity Person"},
			Kind:      domain.CreditKindCast,
			Character: "Lead",
		},
	}); err != nil {
		t.Fatalf("seed credits: %v", err)
	}

	cacheNow := time.Now().UTC()
	h.setCachedStats("warm", statsResponse{SelectedWindow: "all-time"}, cacheNow)
	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	req := httptest.NewRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/movies/%d", target.ID),
		strings.NewReader(
			`{"title":"After","link":"https://www.imdb.com/title/tt0000006/"}`,
		),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if _, ok := h.getCachedStats("warm", cacheNow); ok {
		t.Fatal("watched title edit left the stats cache warm")
	}

	updated, err := movieRepo.FindByID(ctx, target.ID)
	if err != nil {
		t.Fatalf("fetch updated target: %v", err)
	}
	if updated.Title != "After" {
		t.Fatalf("title after edit = %q, want After", updated.Title)
	}
	if updated.WatchedAt == nil || !updated.WatchedAt.Equal(originalWatchedAt) {
		t.Fatalf("watchedAt after title edit = %v, want %v", updated.WatchedAt, originalWatchedAt)
	}
	md, err := metaRepo.GetMetadata(ctx, target.ID)
	if err != nil {
		t.Fatalf("read same-identity metadata: %v", err)
	}
	if md.Overview != "same identity metadata" {
		t.Fatalf("same-identity metadata = %q, want preserved", md.Overview)
	}
	gotCredits, err := creditsRepo.GetCreditsByMovieIDs(ctx, []int{target.ID})
	if err != nil {
		t.Fatalf("read same-identity credits: %v", err)
	}
	if rows := gotCredits[target.ID]; len(rows) != 1 || rows[0].Person.Name != "Same Identity Person" {
		t.Fatalf("same-identity credits = %+v, want preserved", rows)
	}

	select {
	case got := <-client:
		if got.Type != "movie:updated" {
			t.Fatalf("edit broadcast event %q, want movie:updated", got.Type)
		}
		payload, ok := got.Data.(fullMovie)
		if !ok {
			t.Fatalf("movie:updated payload type = %T, want fullMovie", got.Data)
		}
		if payload.Title != "After" {
			t.Fatalf("movie:updated title = %q, want After", payload.Title)
		}
	default:
		t.Fatal("successful edit did not broadcast movie:updated")
	}
	select {
	case got := <-client:
		t.Fatalf("successful edit broadcast extra event %q", got.Type)
	default:
	}
}

// postMove moves a movie as the given actor (userID). The actor is passed as the
// session member header, since the endpoint no longer carries a user path id.
func postMove(t *testing.T, app *fiber.App, userID, movieID int, target string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/movies/%d/move", movieID),
		strings.NewReader(fmt.Sprintf(`{"target":%q}`, target)),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(userID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

type failMoveProjectionStore struct {
	*repository.SqliteMoviesRepository
	failReads bool
}

func (s *failMoveProjectionStore) PromoteToPoolIfRoom(
	ctx context.Context,
	id, maxPool int,
) (int64, error) {
	changed, err := s.SqliteMoviesRepository.PromoteToPoolIfRoom(ctx, id, maxPool)
	if err == nil && changed == 1 {
		s.failReads = true
	}
	return changed, err
}

func (s *failMoveProjectionStore) FindByUserIDAndStatus(
	ctx context.Context,
	userID int,
	status string,
) ([]*domain.Movie, error) {
	if s.failReads {
		return nil, errors.New("forced post-commit projection failure")
	}
	return s.SqliteMoviesRepository.FindByUserIDAndStatus(ctx, userID, status)
}

func movieStatus(t *testing.T, ctx context.Context, movieRepo *repository.SqliteMoviesRepository, id int) string {
	t.Helper()

	m, err := movieRepo.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("fetch movie: %v", err)
	}
	return m.Status
}

// A rapid duplicate click used to cancel itself out: the move endpoint blind-
// toggled on live status, so two clicks did stash→pool→stash and the movie
// appeared to move then snap back. The directional endpoint treats a move to
// the current location as an idempotent no-op, so duplicates can't reverse it.
func TestHandleMove_DirectionalIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Cara")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Heat", "stash", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}

	if resp := postMove(t, app, user.ID, movie.ID, "pool"); resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("first promote: expected 204, got %d", resp.StatusCode)
	}
	if got := movieStatus(t, ctx, movieRepo, movie.ID); got != "pool" {
		t.Fatalf("after first promote: expected pool, got %q", got)
	}

	// The regression: a second promote to the same target must NOT revert it.
	if resp := postMove(t, app, user.ID, movie.ID, "pool"); resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("duplicate promote: expected 204, got %d", resp.StatusCode)
	}
	if got := movieStatus(t, ctx, movieRepo, movie.ID); got != "pool" {
		t.Fatalf("after duplicate promote: expected pool (no revert), got %q", got)
	}

	if resp := postMove(t, app, user.ID, movie.ID, "stash"); resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("demote: expected 204, got %d", resp.StatusCode)
	}
	if got := movieStatus(t, ctx, movieRepo, movie.ID); got != "stash" {
		t.Fatalf("after demote: expected stash, got %q", got)
	}
}

func TestHandleMove_RejectsMissingTarget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Dev")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Drive", "stash", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/movies/%d/move", movie.ID),
		strings.NewReader(`{}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400 for missing target, got %d", resp.StatusCode)
	}
	if got := movieStatus(t, ctx, movieRepo, movie.ID); got != "stash" {
		t.Fatalf("movie should be unchanged, got %q", got)
	}
}

// A fast double-click fires two near-simultaneous /move POSTs (the pending-guard
// can't block the second before React re-renders). With a blind toggle the
// even count cancelled out; the directional endpoint must instead leave the
// movie at the target and return 2xx for every duplicate — no spurious 409
// (pool-limit counting the movie against its own promotion) or 400
// (ErrInvalidState from a lost read-modify-write race).
func TestHandleMove_ConcurrentDuplicatePromote_NoSpuriousError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Eve")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	// Fill two of three pool slots so the duplicates also exercise the pool-limit
	// check, which must not count the movie against its own in-flight promotion.
	if _, err := movieRepo.Add(ctx, "Alien", "pool", user.ID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	if _, err := movieRepo.Add(ctx, "Aliens", "pool", user.ID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Predator", "stash", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}

	const clicks = 12
	results := make([]struct {
		status int
		err    error
	}, clicks)
	var wg sync.WaitGroup
	for i := range clicks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/api/v1/movies/%d/move", movie.ID),
				strings.NewReader(`{"target":"pool"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))
			resp, err := app.Test(req, -1)
			if err != nil {
				results[i].err = err
				return
			}
			results[i].status = resp.StatusCode
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			t.Fatalf("click %d: app.Test: %v", i, r.err)
		}
		if r.status < 200 || r.status >= 300 {
			t.Fatalf("click %d: expected 2xx (idempotent), got %d", i, r.status)
		}
	}
	if got := movieStatus(t, ctx, movieRepo, movie.ID); got != "pool" {
		t.Fatalf("after concurrent promotes: expected pool, got %q", got)
	}
	pooled, err := movieRepo.FindByUserIDAndStatus(ctx, user.ID, "pool")
	if err != nil {
		t.Fatalf("fetch pool: %v", err)
	}
	if len(pooled) != 3 {
		t.Fatalf("expected pool of 3 after concurrent promotes, got %d", len(pooled))
	}
}

// countMovedEvents drains a broker client for "movie:moved" frames until the
// channel goes quiet for `within`.
func countMovedEvents(client chan event, within time.Duration) int {
	count := 0
	for {
		select {
		case e, ok := <-client:
			if !ok {
				return count
			}
			if e.Type == "movie:moved" {
				count++
			}
		case <-time.After(within):
			return count
		}
	}
}

// A real move broadcasts movie:moved so other clients refetch; a no-op duplicate
// must NOT — otherwise every redundant click triggers an invalidation storm. The
// `changed` flag gates the broadcast; this asserts the gate holds.
func TestHandleMove_NoOpDuplicateSuppressesBroadcast(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Faye")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Fargo", "stash", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}

	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	// First promote is a real transition → exactly one movie:moved.
	if resp := postMove(t, app, user.ID, movie.ID, "pool"); resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("promote: expected 204, got %d", resp.StatusCode)
	}
	if got := countMovedEvents(client, 100*time.Millisecond); got != 1 {
		t.Fatalf("real move: expected 1 movie:moved broadcast, got %d", got)
	}

	// Duplicate promote finds the movie already pooled → no-op, no broadcast.
	if resp := postMove(t, app, user.ID, movie.ID, "pool"); resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("duplicate promote: expected 204, got %d", resp.StatusCode)
	}
	if got := countMovedEvents(client, 100*time.Millisecond); got != 0 {
		t.Fatalf("no-op duplicate: expected 0 broadcasts (suppression), got %d", got)
	}
}

func TestHandleMove_PublishesCommittedMoveWithoutResponseProjection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)
	h.movieService.Close()
	h.movieService = movie.NewService(&failMoveProjectionStore{
		SqliteMoviesRepository: movieRepo,
	}, movie.DrawConfig{})

	user, err := userRepo.Create(ctx, "Projection failure owner")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movieRecord, err := movieRepo.Add(ctx, "Committed move", "stash", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}

	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	resp := postMove(t, app, user.ID, movieRecord.ID, "pool")
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("move: expected 204, got %d", resp.StatusCode)
	}
	if got := movieStatus(t, ctx, movieRepo, movieRecord.ID); got != "pool" {
		t.Fatalf("committed move status = %q, want pool", got)
	}
	if got := countMovedEvents(client, 100*time.Millisecond); got != 1 {
		t.Fatalf("committed move: expected 1 movie:moved broadcast, got %d", got)
	}
}

// Two DIFFERENT stashed movies promoted concurrently into a one-free-slot pool
// must not overshoot maxPoolSize: the pool-count check is folded into the same
// atomic UPDATE as the status flip, so exactly one wins and the rest get 409.
// Before the atomic refactor both could read count<cap and both commit -> pool of 4.
func TestHandleMove_ConcurrentDistinctPromotes_RespectPoolCap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Gus")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	// Seed 2 of 3 pool slots → exactly one free slot for the contenders below.
	if _, err := movieRepo.Add(ctx, "Fargo", "pool", user.ID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	if _, err := movieRepo.Add(ctx, "Brick", "pool", user.ID); err != nil {
		t.Fatalf("seed pool: %v", err)
	}

	const contenders = 4
	ids := make([]int, contenders)
	for i := range contenders {
		m, err := movieRepo.Add(ctx, fmt.Sprintf("Contender %d", i), "stash", user.ID)
		if err != nil {
			t.Fatalf("seed contender %d: %v", i, err)
		}
		ids[i] = m.ID
	}

	results := make([]struct {
		status int
		err    error
	}, contenders)
	var wg sync.WaitGroup
	for i := range contenders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/api/v1/movies/%d/move", ids[i]),
				strings.NewReader(`{"target":"pool"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))
			resp, err := app.Test(req, -1)
			if err != nil {
				results[i].err = err
				return
			}
			results[i].status = resp.StatusCode
		}(i)
	}
	wg.Wait()

	var promoted, rejected int
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("contender %d: app.Test: %v", i, r.err)
		}
		switch r.status {
		case fiber.StatusNoContent:
			promoted++
		case fiber.StatusConflict:
			rejected++
		default:
			t.Fatalf("contender %d: expected 204 or 409, got %d", i, r.status)
		}
	}
	if promoted != 1 {
		t.Fatalf("expected exactly 1 promotion to win the free slot, got %d", promoted)
	}
	if rejected != contenders-1 {
		t.Fatalf("expected %d pool-limit rejections, got %d", contenders-1, rejected)
	}

	pooled, err := movieRepo.FindByUserIDAndStatus(ctx, user.ID, "pool")
	if err != nil {
		t.Fatalf("fetch pool: %v", err)
	}
	if len(pooled) != 3 {
		t.Fatalf("pool cap breached: expected 3, got %d", len(pooled))
	}
}

// The demote path is directional + idempotent too: many concurrent demotes of
// the same pooled movie all succeed (2xx) and leave it stashed, never erroring
// on the lost-race ErrInvalidState.
func TestHandleMove_ConcurrentDemoteIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Hank")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movie, err := movieRepo.Add(ctx, "Holes", "pool", user.ID)
	if err != nil {
		t.Fatalf("create movie: %v", err)
	}

	const clicks = 8
	results := make([]struct {
		status int
		err    error
	}, clicks)
	var wg sync.WaitGroup
	for i := range clicks {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(
				http.MethodPost,
				fmt.Sprintf("/api/v1/movies/%d/move", movie.ID),
				strings.NewReader(`{"target":"stash"}`),
			)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))
			resp, err := app.Test(req, -1)
			if err != nil {
				results[i].err = err
				return
			}
			results[i].status = resp.StatusCode
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			t.Fatalf("click %d: app.Test: %v", i, r.err)
		}
		if r.status < 200 || r.status >= 300 {
			t.Fatalf("click %d: expected 2xx (idempotent demote), got %d", i, r.status)
		}
	}
	if got := movieStatus(t, ctx, movieRepo, movie.ID); got != "stash" {
		t.Fatalf("after concurrent demotes: expected stash, got %q", got)
	}
}

// Regression: adding a movie lands it in the stash, never the pool — even when
// the pool has free slots. The button says "Add to <user>'s stash", so the add
// must not silently auto-promote; reaching the pool is a separate explicit move.
func TestHandleAddMovie_LandsInStashEvenWithPoolRoom(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Casey")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/movies",
		strings.NewReader(`{"title":"Dune","tmdbId":438631}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("expected status 201, got %d", resp.StatusCode)
	}

	pooled, err := movieRepo.FindByUserIDAndStatus(ctx, user.ID, "pool")
	if err != nil {
		t.Fatalf("fetch pool: %v", err)
	}
	if len(pooled) != 0 {
		t.Fatalf("expected empty pool after add, got %d pooled", len(pooled))
	}

	stashed, err := movieRepo.FindByUserIDAndStatus(ctx, user.ID, "stash")
	if err != nil {
		t.Fatalf("fetch stash: %v", err)
	}
	if len(stashed) != 1 || stashed[0].Title != "Dune" {
		t.Fatalf("expected Dune stashed, got %#v", stashed)
	}
}

func TestHandleAddMovie_DuplicateIMDbDoesNotCreateMovie(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)

	user, err := userRepo.Create(ctx, "IMDb adder")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	existing, err := movieRepo.Add(ctx, "Existing", "stash", user.ID)
	if err != nil {
		t.Fatalf("create existing movie: %v", err)
	}
	existingIMDb := "TT7654321"
	if err := movieRepo.SetExternalIDs(ctx, existing.ID, nil, &existingIMDb); err != nil {
		t.Fatalf("set existing identity: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/movies",
		strings.NewReader(`{"title":"Duplicate","link":"https://www.imdb.com/title/tt7654321/"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("expected status 409, got %d", resp.StatusCode)
	}

	var count int
	if err := dbConn.Read.QueryRowContext(ctx, `SELECT COUNT(*) FROM movies`).Scan(&count); err != nil {
		t.Fatalf("count movies: %v", err)
	}
	if count != 1 {
		t.Fatalf("movie count after duplicate IMDb add = %d, want 1", count)
	}
}

// Regression: a duplicate add used to insert an identityless stash row before
// the TMDB uniqueness check ran. If its compensating DELETE then failed, the
// handler still returned 409 and silently left that orphan behind.
func TestHandleAddMovie_DuplicateIdentityDoesNotLeaveOrphanWhenCleanupFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)

	user, err := userRepo.Create(ctx, "Casey")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	existing, err := movieRepo.Add(ctx, "Dune", "stash", user.ID)
	if err != nil {
		t.Fatalf("create existing movie: %v", err)
	}
	tmdbID := 438631
	if err := movieRepo.SetExternalIDs(ctx, existing.ID, &tmdbID, nil); err != nil {
		t.Fatalf("set existing identity: %v", err)
	}

	if _, err := dbConn.Write.ExecContext(ctx, `
		CREATE TRIGGER reject_movie_cleanup
		BEFORE DELETE ON movies
		BEGIN
			SELECT RAISE(ABORT, 'injected cleanup failure');
		END
	`); err != nil {
		t.Fatalf("install cleanup failure: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/movies",
		strings.NewReader(`{"title":"Dune duplicate","tmdbId":438631}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("expected status 409, got %d", resp.StatusCode)
	}

	var total, identityless int
	if err := dbConn.Read.QueryRowContext(ctx, "SELECT COUNT(*) FROM movies").Scan(&total); err != nil {
		t.Fatalf("count movies: %v", err)
	}
	if err := dbConn.Read.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM movies WHERE tmdb_id IS NULL AND imdb_id IS NULL",
	).Scan(&identityless); err != nil {
		t.Fatalf("count identityless movies: %v", err)
	}
	if total != 1 || identityless != 0 {
		t.Fatalf("movies after duplicate = %d total, %d identityless; want 1 total, 0 identityless", total, identityless)
	}
}

// Regression: the add response is read after the INSERT. If that read fails,
// every write caused by the INSERT must roll back and no event may claim that
// the movie was added.
func TestHandleAddMovie_ResponseReadFailureRollsBackInsert(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, _, dbConn := setupEditMovieTestWithDB(t)

	user, err := userRepo.Create(ctx, "Reader")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := dbConn.Write.ExecContext(ctx, `
		CREATE TABLE movie_add_read_failures (movie_id INTEGER NOT NULL);
		CREATE TRIGGER fail_movie_add_response_read
		AFTER INSERT ON movies
		WHEN NEW.title = 'Unreadable add'
		BEGIN
			INSERT INTO movie_add_read_failures (movie_id) VALUES (NEW.id);
			DELETE FROM movies WHERE id = NEW.id;
		END
	`); err != nil {
		t.Fatalf("install response-read failure: %v", err)
	}

	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/movies",
		strings.NewReader(`{"title":"Unreadable add","tmdbId":438632}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}

	var movies, markers int
	if err := dbConn.Read.QueryRowContext(ctx, "SELECT COUNT(*) FROM movies").Scan(&movies); err != nil {
		t.Fatalf("count movies: %v", err)
	}
	if err := dbConn.Read.QueryRowContext(ctx, "SELECT COUNT(*) FROM movie_add_read_failures").Scan(&markers); err != nil {
		t.Fatalf("count failure markers: %v", err)
	}
	if movies != 0 || markers != 0 {
		t.Fatalf("failed add left %d movies and %d failure markers; want 0 and 0", movies, markers)
	}

	select {
	case got := <-client:
		t.Fatalf("failed add broadcast event %q", got.Type)
	default:
	}
}

func TestHandleDeleteMovie_PoolLock(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)

	user, err := userRepo.Create(ctx, "Dana")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	pooled, err := movieRepo.Add(ctx, "Locked In", "pool", user.ID)
	if err != nil {
		t.Fatalf("create pooled movie: %v", err)
	}
	stashed, err := movieRepo.Add(ctx, "Just Parked", "stash", user.ID)
	if err != nil {
		t.Fatalf("create stashed movie: %v", err)
	}

	if err := h.settingsService.SetPoolLock(ctx, true); err != nil {
		t.Fatalf("lock the pool: %v", err)
	}

	del := func(id int) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/movies/%d", id), nil)
		req.Header.Set(testMemberHeader, strconv.Itoa(user.ID))
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		return resp.StatusCode
	}

	if status := del(pooled.ID); status != fiber.StatusForbidden {
		t.Fatalf("delete pooled movie while locked = %d, want 403", status)
	}
	if _, err := movieRepo.FindByID(ctx, pooled.ID); err != nil {
		t.Fatalf("expected the pooled movie to survive the refusal: %v", err)
	}

	// The lock is the pool's, not a member's list's.
	if status := del(stashed.ID); status != fiber.StatusNoContent {
		t.Fatalf("delete stashed movie while locked = %d, want 204", status)
	}

	if err := h.settingsService.SetPoolLock(ctx, false); err != nil {
		t.Fatalf("unlock the pool: %v", err)
	}
	if status := del(pooled.ID); status != fiber.StatusNoContent {
		t.Fatalf("delete pooled movie while unlocked = %d, want 204", status)
	}
}
