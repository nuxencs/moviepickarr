package server

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
	"moviepickarr/internal/repository"

	"github.com/gofiber/fiber/v2"
)

func putCorrection(t *testing.T, app *fiber.App, actorID int, role domain.Role, movieID int, body string) *http.Response {
	t.Helper()
	return doAs(t, app, jsonReq(http.MethodPut, fmt.Sprintf("/api/v1/movies/%d/correction", movieID), body), actorID, role)
}

func seedCorrectionMovie(
	t *testing.T,
	ctx context.Context,
	userRepo *repository.SqliteUserRepository,
	movieRepo *repository.SqliteMoviesRepository,
	status string,
	tmdbID int,
) (adderID int, movie *domain.Movie) {
	t.Helper()
	adder, err := userRepo.Create(ctx, "Adder")
	if err != nil {
		t.Fatalf("create adder: %v", err)
	}
	added, err := movieRepo.Add(ctx, "Wrong pick", status, adder.ID)
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}
	if err := movieRepo.SetExternalIDs(ctx, added.ID, &tmdbID, new("tt0000777")); err != nil {
		t.Fatalf("set identity: %v", err)
	}
	return adder.ID, added
}

func TestHandleAddMovie_RequiresTMDBID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)
	user, err := userRepo.Create(ctx, "Adder")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	resp := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/movies",
		`{"title":"Custom","link":"https://www.imdb.com/title/tt0133093/"}`,
	), user.ID, "member")
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("link-only add: status %d, want 400", resp.StatusCode)
	}

	movies, err := movieRepo.List(ctx)
	if err != nil {
		t.Fatalf("list movies: %v", err)
	}
	if len(movies) != 0 {
		t.Fatalf("link-only add created %d movies, want 0", len(movies))
	}
}

func TestCorrectMovie_AdminOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)
	adderID, movie := seedCorrectionMovie(t, ctx, userRepo, movieRepo, "stash", 100)

	// The adder has no edit right either: members cannot change a movie.
	resp := putCorrection(t, app, adderID, "member", movie.ID, `{"tmdbId":200,"title":"Right pick"}`)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("member correction: status %d, want 403", resp.StatusCode)
	}
	if code := problemCode(t, resp); code != "admin_required" {
		t.Fatalf("member correction: code %q, want admin_required", code)
	}

	got, err := movieRepo.FindByID(ctx, movie.ID)
	if err != nil {
		t.Fatalf("read movie: %v", err)
	}
	if got.Title != "Wrong pick" || got.TMDBID == nil || *got.TMDBID != 100 {
		t.Fatalf("refused correction changed the movie: %+v", got)
	}
}

func TestCorrectMovie_IdentityReplacesMovieAndClearsDerivedData(t *testing.T) {
	t.Setenv("TMDB_API_KEY", "")

	ctx := context.Background()
	h, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)
	metaRepo := repository.NewSqliteMovieMetadataRepository(dbConn)
	creditsRepo := repository.NewSqliteMovieCreditsRepository(dbConn)
	adderID, movie := seedCorrectionMovie(t, ctx, userRepo, movieRepo, "pool", 100)
	admin, err := userRepo.Create(ctx, "Admin")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	watchedAt := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	if err := movieRepo.MarkAsWatched(ctx, movie.ID, watchedAt); err != nil {
		t.Fatalf("mark watched: %v", err)
	}
	if err := metaRepo.UpsertMetadata(ctx, domain.MovieMetadata{MovieID: movie.ID, Overview: "wrong film"}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	const personID = 98
	if err := creditsRepo.ReplaceCredits(ctx, movie.ID, []domain.MovieCredit{{
		MovieID: movie.ID,
		Person:  domain.Person{ID: personID, Name: "Shared Person"},
		Kind:    domain.CreditKindCast,
	}}); err != nil {
		t.Fatalf("seed credits: %v", err)
	}
	cacheNow := time.Now().UTC()
	h.setCachedStats("warm", statsResponse{SelectedWindow: "all-time"}, cacheNow)
	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	resp := putCorrection(t, app, admin.ID, "admin", movie.ID, `{"tmdbId":200,"title":"Right pick"}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("correction: status %d, want 200", resp.StatusCode)
	}

	got, err := movieRepo.FindByID(ctx, movie.ID)
	if err != nil {
		t.Fatalf("read movie: %v", err)
	}
	if got.Title != "Right pick" || got.TMDBID == nil || *got.TMDBID != 200 || got.IMDbID != nil {
		t.Fatalf("movie after correction = %q %v/%v, want Right pick 200/nil", got.Title, got.TMDBID, got.IMDbID)
	}
	if got.AddedByID != adderID {
		t.Fatalf("adder after correction = %d, want %d", got.AddedByID, adderID)
	}
	if got.WatchedAt == nil || !got.WatchedAt.Equal(watchedAt) {
		t.Fatalf("watchedAt after identity correction = %v, want %v", got.WatchedAt, watchedAt)
	}
	if _, err := metaRepo.GetMetadata(ctx, movie.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("metadata after correction = %v, want ErrNotFound", err)
	}
	credits, err := creditsRepo.GetCreditsByMovieIDs(ctx, []int{movie.ID})
	if err != nil {
		t.Fatalf("read credits: %v", err)
	}
	if len(credits[movie.ID]) != 0 {
		t.Fatalf("credits after correction = %+v, want none", credits[movie.ID])
	}
	var people int
	if err := dbConn.Read.QueryRowContext(ctx, "SELECT COUNT(*) FROM people WHERE id = ?", personID).Scan(&people); err != nil {
		t.Fatalf("count people: %v", err)
	}
	if people != 1 {
		t.Fatalf("shared people after correction = %d, want 1", people)
	}
	candidates, err := metaRepo.NeedsEnrichment(ctx, time.Time{}, 100)
	if err != nil {
		t.Fatalf("needs enrichment: %v", err)
	}
	if !candidateIncludes(candidates, movie.ID) {
		t.Fatal("corrected movie is not in the enrichment backlog")
	}
	if _, ok := h.getCachedStats("warm", cacheNow); ok {
		t.Fatal("watched correction left the stats cache warm")
	}
	assertOnlyEvent(t, client, "movie:updated")
}

func TestCorrectMovie_SameTMDBIDKeepsDerivedData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)
	metaRepo := repository.NewSqliteMovieMetadataRepository(dbConn)
	adderID, movie := seedCorrectionMovie(t, ctx, userRepo, movieRepo, "stash", 100)
	if err := metaRepo.UpsertMetadata(ctx, domain.MovieMetadata{MovieID: movie.ID, Overview: "keep"}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	resp := putCorrection(t, app, adderID, "admin", movie.ID, `{"tmdbId":100,"title":"Ignored"}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("correction: status %d, want 200", resp.StatusCode)
	}

	got, err := movieRepo.FindByID(ctx, movie.ID)
	if err != nil {
		t.Fatalf("read movie: %v", err)
	}
	if got.Title != "Wrong pick" || got.IMDbID == nil {
		t.Fatalf("same-identity correction changed the movie: %q imdb %v", got.Title, got.IMDbID)
	}
	md, err := metaRepo.GetMetadata(ctx, movie.ID)
	if err != nil || md.Overview != "keep" {
		t.Fatalf("metadata after same-identity correction = %+v, %v", md, err)
	}
}

func TestCorrectMovie_WatchedDate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo := setupEditMovieTest(t)
	adderID, movie := seedCorrectionMovie(t, ctx, userRepo, movieRepo, "pool", 100)

	t.Run("refused_for_unwatched_movie", func(t *testing.T) {
		resp := putCorrection(t, app, adderID, "admin", movie.ID, `{"watchedAt":"2026-02-08T16:45:00Z"}`)
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("status %d, want 400", resp.StatusCode)
		}
		got, err := movieRepo.FindByID(ctx, movie.ID)
		if err != nil {
			t.Fatalf("read movie: %v", err)
		}
		if got.WatchedAt != nil {
			t.Fatalf("unwatched movie got watchedAt %v", got.WatchedAt)
		}
	})

	t.Run("updates_watched_movie", func(t *testing.T) {
		if err := movieRepo.MarkAsWatched(ctx, movie.ID, time.Date(2026, 2, 7, 13, 0, 0, 0, time.UTC)); err != nil {
			t.Fatalf("mark watched: %v", err)
		}
		cacheNow := time.Now().UTC()
		h.setCachedStats("warm", statsResponse{SelectedWindow: "all-time"}, cacheNow)

		resp := putCorrection(t, app, adderID, "admin", movie.ID, `{"watchedAt":"2026-02-08T16:45:00Z"}`)
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("status %d, want 200", resp.StatusCode)
		}
		got, err := movieRepo.FindByID(ctx, movie.ID)
		if err != nil {
			t.Fatalf("read movie: %v", err)
		}
		want := time.Date(2026, 2, 8, 16, 45, 0, 0, time.UTC)
		if got.WatchedAt == nil || !got.WatchedAt.Equal(want) || got.Title != "Wrong pick" {
			t.Fatalf("movie after date correction = %q %v, want Wrong pick %v", got.Title, got.WatchedAt, want)
		}
		if _, ok := h.getCachedStats("warm", cacheNow); ok {
			t.Fatal("watched date correction left the stats cache warm")
		}
	})
}

func TestCorrectMovie_RejectsEmptyCorrection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)
	adderID, movie := seedCorrectionMovie(t, ctx, userRepo, movieRepo, "stash", 100)

	for name, body := range map[string]string{
		"nothing":          `{}`,
		"tmdb_no_title":    `{"tmdbId":200}`,
		"negative_tmdb":    `{"tmdbId":-1,"title":"X"}`,
		"invalid_date":     `{"watchedAt":"yesterday"}`,
		"blank_title_tmdb": `{"tmdbId":200,"title":"   "}`,
	} {
		t.Run(name, func(t *testing.T) {
			resp := putCorrection(t, app, adderID, "admin", movie.ID, body)
			if resp.StatusCode != fiber.StatusBadRequest {
				t.Fatalf("status %d, want 400", resp.StatusCode)
			}
		})
	}
}

func TestCorrectMovie_DuplicateTMDBRollsBack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)
	adderID, movie := seedCorrectionMovie(t, ctx, userRepo, movieRepo, "pool", 100)
	owner, err := movieRepo.Add(ctx, "Owner", "stash", adderID)
	if err != nil {
		t.Fatalf("add owner: %v", err)
	}
	ownedTMDB := 200
	if err := movieRepo.SetExternalIDs(ctx, owner.ID, &ownedTMDB, nil); err != nil {
		t.Fatalf("set owner identity: %v", err)
	}
	const marker = int64(1_700_000_000)
	if _, err := dbConn.Write.ExecContext(ctx,
		`INSERT INTO movie_metadata (movie_id, credits_refreshed_at) VALUES (?, ?)`, movie.ID, marker,
	); err != nil {
		t.Fatalf("seed metadata marker: %v", err)
	}
	cacheNow := time.Now().UTC()
	h.setCachedStats("warm", statsResponse{SelectedWindow: "all-time"}, cacheNow)
	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	resp := putCorrection(t, app, adderID, "admin", movie.ID, `{"tmdbId":200,"title":"Right pick"}`)
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status %d, want 409", resp.StatusCode)
	}

	assertUncorrected(t, ctx, movieRepo, movie.ID)
	var got int64
	if err := dbConn.Read.QueryRowContext(ctx,
		`SELECT credits_refreshed_at FROM movie_metadata WHERE movie_id = ?`, movie.ID,
	).Scan(&got); err != nil {
		t.Fatalf("read metadata marker: %v", err)
	}
	if got != marker {
		t.Fatalf("metadata marker after conflict = %d, want %d", got, marker)
	}
	if _, ok := h.getCachedStats("warm", cacheNow); !ok {
		t.Fatal("failed correction invalidated the stats cache")
	}
	assertNoEvent(t, client)
}

func TestCorrectMovie_CleanupFailureRollsBack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)
	adderID, movie := seedCorrectionMovie(t, ctx, userRepo, movieRepo, "pool", 100)
	if _, err := dbConn.Write.ExecContext(ctx,
		`INSERT INTO movie_metadata (movie_id, overview) VALUES (?, 'kept')`, movie.ID,
	); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	if _, err := dbConn.Write.ExecContext(ctx, fmt.Sprintf(`
		CREATE TRIGGER fail_correction_metadata_cleanup
		BEFORE DELETE ON movie_metadata
		WHEN OLD.movie_id = %d
		BEGIN
			SELECT RAISE(ABORT, 'forced metadata cleanup failure');
		END
	`, movie.ID)); err != nil {
		t.Fatalf("install cleanup failure: %v", err)
	}
	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	resp := putCorrection(t, app, adderID, "admin", movie.ID, `{"tmdbId":200,"title":"Right pick"}`)
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status %d, want 500", resp.StatusCode)
	}

	assertUncorrected(t, ctx, movieRepo, movie.ID)
	assertNoEvent(t, client)
}

func TestCorrectMovie_ResponseReadFailureRollsBack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)
	adderID, movie := seedCorrectionMovie(t, ctx, userRepo, movieRepo, "stash", 100)
	if _, err := dbConn.Write.ExecContext(ctx, fmt.Sprintf(`
		CREATE TRIGGER fail_correction_response_read
		AFTER UPDATE OF title ON movies
		WHEN NEW.id = %d AND NEW.title = 'Unreadable'
		BEGIN
			DELETE FROM movies WHERE id = NEW.id;
		END
	`, movie.ID)); err != nil {
		t.Fatalf("install response-read failure: %v", err)
	}
	client, _ := h.broker.Subscribe()
	defer h.broker.Unsubscribe(client)

	resp := putCorrection(t, app, adderID, "admin", movie.ID, `{"tmdbId":200,"title":"Unreadable"}`)
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status %d, want 404", resp.StatusCode)
	}

	assertUncorrected(t, ctx, movieRepo, movie.ID)
	assertNoEvent(t, client)
}

func TestCorrectMovie_RefusedWhileDrawUnrevealed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)
	adderID, movie := seedCorrectionMovie(t, ctx, userRepo, movieRepo, "stash", 100)
	seedPoolAndDraw(t, app, movieRepo, adderID, "Pooled")

	resp := putCorrection(t, app, adderID, "admin", movie.ID, `{"tmdbId":200,"title":"Right pick"}`)
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status %d, want 409", resp.StatusCode)
	}
	if code := problemCode(t, resp); code != "draw_in_progress" {
		t.Fatalf("code %q, want draw_in_progress", code)
	}
	assertUncorrected(t, ctx, movieRepo, movie.ID)
}

func TestCorrectMovie_RetargetsUnlockedAcquisition(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)
	current := seedRevealedDraw(t, ctx, app, userRepo, movieRepo)
	acquisitionID := acquisitionIDFor(t, ctx, dbConn, current.ID)
	if _, err := dbConn.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET status = 'action_needed', action_reason = 'identity_required',
		    identity_source = 'override', identity_override_tmdb_id = 999
		WHERE id = ?
	`, acquisitionID); err != nil {
		t.Fatalf("seed identity wait: %v", err)
	}
	before := readAcquisition(t, ctx, dbConn, acquisitionID)

	resp := putCorrection(t, app, current.AddedByID, "admin", current.ID, `{"tmdbId":200,"title":"Right pick","year":1999}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}

	got := readAcquisition(t, ctx, dbConn, acquisitionID)
	if got.MovieYear != 1999 {
		t.Fatalf("acquisition year = %d, want 1999 from the TMDB pick", got.MovieYear)
	}
	if got.MovieTitle != "Right pick" || got.TMDBID == nil || *got.TMDBID != 200 || got.IMDbID != "" {
		t.Fatalf("acquisition identity = %q %v/%q, want Right pick 200/empty", got.MovieTitle, got.TMDBID, got.IMDbID)
	}
	if got.IdentitySource != "tmdb" || got.IdentityOverrideTMDBID != nil {
		t.Fatalf("acquisition identity source = %q override %v, want tmdb and none", got.IdentitySource, got.IdentityOverrideTMDBID)
	}
	if got.Status != "waiting_for_radarr" || got.ActionReason != "" {
		t.Fatalf("acquisition state = %s/%s, want waiting_for_radarr with no reason", got.Status, got.ActionReason)
	}
	if got.Revision <= before.Revision {
		t.Fatalf("acquisition revision = %d, want above %d so a stale review fails", got.Revision, before.Revision)
	}
}

func TestCorrectMovie_LockedAcquisitionRefusesIdentityButNotDate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo, dbConn := setupEditMovieTestWithDB(t)
	current := seedRevealedDraw(t, ctx, app, userRepo, movieRepo)
	acquisitionID := acquisitionIDFor(t, ctx, dbConn, current.ID)
	lockAcquisition(t, ctx, dbConn, acquisitionID)
	if err := movieRepo.MarkAsWatched(ctx, current.ID, time.Date(2026, 2, 7, 13, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("mark watched: %v", err)
	}
	before := readAcquisition(t, ctx, dbConn, acquisitionID)

	resp := putCorrection(t, app, current.AddedByID, "admin", current.ID, `{"tmdbId":200,"title":"Right pick"}`)
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("identity correction: status %d, want 409", resp.StatusCode)
	}
	if code := problemCode(t, resp); code != "acquisition_locked" {
		t.Fatalf("identity correction: code %q, want acquisition_locked", code)
	}
	got, err := movieRepo.FindByID(ctx, current.ID)
	if err != nil {
		t.Fatalf("read movie: %v", err)
	}
	if got.TMDBID == nil || *got.TMDBID != 100 {
		t.Fatalf("refused correction changed tmdb id to %v", got.TMDBID)
	}
	if after := readAcquisition(t, ctx, dbConn, acquisitionID); after.Revision != before.Revision {
		t.Fatalf("refused correction touched the acquisition: revision %d -> %d", before.Revision, after.Revision)
	}

	resp = putCorrection(t, app, current.AddedByID, "admin", current.ID, `{"watchedAt":"2026-02-08T16:45:00Z"}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("date correction: status %d, want 200", resp.StatusCode)
	}
}

// seedRevealedDraw draws and reveals a movie with TMDB id 100, which creates
// its Acquisition.
func seedRevealedDraw(
	t *testing.T,
	ctx context.Context,
	app *fiber.App,
	userRepo *repository.SqliteUserRepository,
	movieRepo *repository.SqliteMoviesRepository,
) *domain.Movie {
	t.Helper()
	adderID, movie := seedCorrectionMovie(t, ctx, userRepo, movieRepo, "pool", 100)
	resp := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/movies/random", `{"clientId":"c"}`), adderID, "member")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("draw: status %d", resp.StatusCode)
	}
	resp = doAs(t, app, httptest.NewRequest(http.MethodPost, "/api/v1/movies/current/reveal", nil), adderID, "member")
	if resp.StatusCode != fiber.StatusOK && resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("reveal: status %d", resp.StatusCode)
	}
	return movie
}

func acquisitionIDFor(t *testing.T, ctx context.Context, pool *db.Pool, movieID int) int64 {
	t.Helper()
	var id int64
	if err := pool.Read.QueryRowContext(ctx,
		"SELECT id FROM radarr_acquisitions WHERE movie_id = ?", movieID,
	).Scan(&id); err != nil {
		t.Fatalf("find acquisition: %v", err)
	}
	return id
}

func readAcquisition(t *testing.T, ctx context.Context, pool *db.Pool, id int64) repository.RadarrAcquisition {
	t.Helper()
	acquisition, err := repository.NewSqliteRadarrRepository(pool).GetVisibleAcquisition(ctx, id)
	if err != nil {
		t.Fatalf("read acquisition: %v", err)
	}
	return acquisition
}

// lockAcquisition stands in for a Radarr add: the minimum target columns the
// schema needs before target_locked_at.
func lockAcquisition(t *testing.T, ctx context.Context, pool *db.Pool, id int64) {
	t.Helper()
	if _, err := pool.Write.ExecContext(ctx, `
		INSERT INTO radarr_instances (name, base_url, encrypted_api_key, last_checked_at)
		VALUES ('Radarr', 'http://radarr.test', x'00', 0)
	`); err != nil {
		t.Fatalf("seed instance: %v", err)
	}
	if _, err := pool.Write.ExecContext(ctx, `
		UPDATE radarr_acquisitions
		SET preset_name = 'Preset', target_instance_id = (SELECT id FROM radarr_instances LIMIT 1),
		    target_instance_name = 'Radarr', target_root_folder_id = 1,
		    target_root_folder_path = '/movies', target_quality_profile_id = 1,
		    target_quality_profile_name = 'HD', target_minimum_availability = 'released',
		    target_acquisition_mode = 'automatic', target_selected_at = 1,
		    target_locked_at = 1, radarr_movie_id = 5
		WHERE id = ?
	`, id); err != nil {
		t.Fatalf("lock acquisition: %v", err)
	}
}

func assertUncorrected(t *testing.T, ctx context.Context, movieRepo *repository.SqliteMoviesRepository, movieID int) {
	t.Helper()
	got, err := movieRepo.FindByID(ctx, movieID)
	if err != nil {
		t.Fatalf("read movie: %v", err)
	}
	if got.Title != "Wrong pick" || got.TMDBID == nil || *got.TMDBID != 100 ||
		got.IMDbID == nil || *got.IMDbID != "tt0000777" {
		t.Fatalf("movie after failed correction = %q %v/%v, want Wrong pick 100/tt0000777",
			got.Title, got.TMDBID, got.IMDbID)
	}
}

func assertOnlyEvent(t *testing.T, client <-chan event, want string) {
	t.Helper()
	select {
	case got := <-client:
		if got.Type != want {
			t.Fatalf("broadcast %q, want %q", got.Type, want)
		}
	default:
		t.Fatalf("no %q broadcast", want)
	}
	assertNoEvent(t, client)
}

func assertNoEvent(t *testing.T, client <-chan event) {
	t.Helper()
	select {
	case got := <-client:
		t.Fatalf("unexpected broadcast %q", got.Type)
	default:
	}
}

func TestMovieLibrary_ListsEveryStatusAndHidesHeldDraw(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)
	adderID, stashed := seedCorrectionMovie(t, ctx, userRepo, movieRepo, "stash", 100)
	watched, err := movieRepo.Add(ctx, "Seen", "pool", adderID)
	if err != nil {
		t.Fatalf("add watched: %v", err)
	}
	if err := movieRepo.MarkAsWatched(ctx, watched.ID, time.Date(2026, 2, 7, 13, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("mark watched: %v", err)
	}
	// One pooled movie, so the draw holds it.
	seedPoolAndDraw(t, app, movieRepo, adderID, "Held")

	resp := doAs(t, app, jsonReq(http.MethodGet, "/api/v1/movies/library", ``), adderID, "admin")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	var rows []libraryMovie
	if err := json.UnmarshalRead(resp.Body, &rows); err != nil {
		t.Fatalf("decode library: %v", err)
	}

	got := map[string]domain.MovieStatus{}
	for _, row := range rows {
		got[row.Title] = row.Status
	}
	want := map[string]domain.MovieStatus{
		stashed.Title: domain.MovieStatusStash,
		"Seen":        domain.MovieStatusWatched,
		"Held":        domain.MovieStatusPool,
	}
	if !maps.Equal(got, want) {
		t.Fatalf("library statuses = %v, want %v", got, want)
	}
}

func TestSelectWildcard_NewMovieRequiresTMDBID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, app, userRepo, movieRepo := setupEditMovieTest(t)
	current := seedRevealedDraw(t, ctx, app, userRepo, movieRepo)

	resp := doAs(t, app, jsonReq(http.MethodPost, "/api/v1/movies/wildcard",
		fmt.Sprintf(`{"hostMovieId":%d,"title":"Custom","imdbId":"tt0133093"}`, current.ID),
	), current.AddedByID, "member")
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("IMDb-only wildcard: status %d, want 400", resp.StatusCode)
	}
	movies, err := movieRepo.List(ctx)
	if err != nil {
		t.Fatalf("list movies: %v", err)
	}
	if len(movies) != 1 {
		t.Fatalf("IMDb-only wildcard left %d movies, want 1", len(movies))
	}
}
