package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
	"moviepickarr/internal/repository"
)

type enrichmentTestRepos struct {
	db      *db.Pool
	movies  *repository.SqliteMoviesRepository
	meta    *repository.SqliteMovieMetadataRepository
	credits *repository.SqliteMovieCreditsRepository
	users   *repository.SqliteUserRepository
}

func setupEnrichmentTestRepos(t *testing.T) (context.Context, enrichmentTestRepos) {
	t.Helper()

	ctx := context.Background()
	pool, err := db.OpenSQLite(filepath.Join(t.TempDir(), "enrichment-test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.RunMigrations(ctx, pool.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})

	return ctx, enrichmentTestRepos{
		db:      pool,
		movies:  repository.NewSqliteMoviesRepository(pool),
		meta:    repository.NewSqliteMovieMetadataRepository(pool),
		credits: repository.NewSqliteMovieCreditsRepository(pool),
		users:   repository.NewSqliteUserRepository(pool),
	}
}

func seedEnrichmentMovie(
	t *testing.T,
	ctx context.Context,
	repos enrichmentTestRepos,
	name, imdbID string,
) (userID, movieID int) {
	t.Helper()

	user, err := repos.users.Create(ctx, name+" owner")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movie, err := repos.movies.Add(ctx, name, string(domain.MovieStatusStash), user.ID)
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}
	if err := repos.movies.SetExternalIDs(ctx, movie.ID, nil, &imdbID); err != nil {
		t.Fatalf("set movie identity: %v", err)
	}
	return user.ID, movie.ID
}

func waitForSignal(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func waitForEnrichmentResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for enrichment")
		return nil
	}
}

func candidateIncludes(candidates []domain.EnrichmentCandidate, movieID int) bool {
	for _, candidate := range candidates {
		if candidate.MovieID == movieID {
			return true
		}
	}
	return false
}

func TestEnrichOne_IdentityEditDuringTMDBRequestRejectsOldResult(t *testing.T) {
	ctx, repos := setupEnrichmentTestRepos(t)
	const oldIMDb = "tt0000101"
	const newIMDb = "tt0000102"
	userID, movieID := seedEnrichmentMovie(t, ctx, repos, "Identity race", oldIMDb)

	if err := repos.meta.UpsertMetadata(ctx, domain.MovieMetadata{
		MovieID:  movieID,
		Overview: "metadata before the edit",
		Genres:   []string{"Before"},
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	if err := repos.credits.ReplaceCredits(ctx, movieID, []domain.MovieCredit{
		{
			MovieID:   movieID,
			Person:    domain.Person{ID: 101, Name: "Before Person"},
			Kind:      domain.CreditKindCast,
			Character: "Before",
		},
	}); err != nil {
		t.Fatalf("seed credits: %v", err)
	}

	detailsStarted := make(chan struct{})
	releaseDetails := make(chan struct{})
	tmdb := &fakeTMDB{
		findFn: func(context.Context, string) (tmdbMovie, error) {
			return tmdbMovie{ID: 201}, nil
		},
		detailsFn: func(ctx context.Context, _ int) (tmdbMovieDetails, error) {
			close(detailsStarted)
			select {
			case <-releaseDetails:
			case <-ctx.Done():
				return tmdbMovieDetails{}, ctx.Err()
			}
			return tmdbMovieDetails{
				ID:       201,
				IMDbID:   oldIMDb,
				Overview: "stale metadata from the old identity",
				Genres:   []tmdbGenre{{Name: "Stale"}},
				Credits: tmdbCredits{Cast: []tmdbCastMember{
					{ID: 202, Name: "Stale Person", Character: "Stale", Order: 0},
				}},
			}, nil
		},
	}
	svc := newEnrichmentService(repos.movies, repos.meta, tmdb, 15)

	enrichErr := make(chan error, 1)
	go func() {
		_, err := svc.EnrichOne(ctx, movieID)
		enrichErr <- err
	}()

	waitForSignal(t, detailsStarted, "old identity TMDB request")
	_, changed, err := repos.movies.EditMovie(ctx, movieID, userID, "Identity edited", newIMDb, nil)
	if err != nil {
		close(releaseDetails)
		t.Fatalf("edit movie identity: %v", err)
	}
	if !changed {
		close(releaseDetails)
		t.Fatal("identity edit was not reported as changed")
	}
	close(releaseDetails)

	if err := waitForEnrichmentResult(t, enrichErr); !errors.Is(err, ErrEnrichSuperseded) {
		t.Fatalf("stale enrichment error = %v, want ErrEnrichSuperseded", err)
	}

	movie, err := repos.movies.FindByID(ctx, movieID)
	if err != nil {
		t.Fatalf("read edited movie: %v", err)
	}
	if movie.TMDBID != nil || movie.IMDbID == nil || *movie.IMDbID != newIMDb {
		t.Fatalf("identity after stale enrichment = %v/%v, want nil/%s", movie.TMDBID, movie.IMDbID, newIMDb)
	}

	md, err := repos.meta.GetMetadata(ctx, movieID)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if md.Overview != "metadata before the edit" {
		t.Fatalf("metadata after stale enrichment = %q", md.Overview)
	}
	gotCredits, err := repos.credits.GetCreditsByMovieIDs(ctx, []int{movieID})
	if err != nil {
		t.Fatalf("read credits: %v", err)
	}
	if rows := gotCredits[movieID]; len(rows) != 1 || rows[0].Person.Name != "Before Person" {
		t.Fatalf("credits after stale enrichment = %+v", rows)
	}

	var refreshed sql.NullInt64
	if err := repos.db.Read.QueryRowContext(ctx,
		"SELECT credits_refreshed_at FROM movie_metadata WHERE movie_id = ?",
		movieID,
	).Scan(&refreshed); err != nil {
		t.Fatalf("read credits marker: %v", err)
	}
	if refreshed.Valid {
		t.Fatalf("stale enrichment stamped credits marker %d", refreshed.Int64)
	}
	candidates, err := repos.meta.NeedsEnrichment(ctx, time.Time{}, 100)
	if err != nil {
		t.Fatalf("needs enrichment: %v", err)
	}
	if !candidateIncludes(candidates, movieID) {
		t.Fatal("edited movie disappeared from enrichment backlog")
	}
}

func TestEnrichOne_CreditFailureRollsBackWholeEnrichment(t *testing.T) {
	ctx, repos := setupEnrichmentTestRepos(t)
	const oldIMDb = "tt0000201"
	_, movieID := seedEnrichmentMovie(t, ctx, repos, "Rollback", oldIMDb)

	if err := repos.meta.UpsertMetadata(ctx, domain.MovieMetadata{
		MovieID:  movieID,
		Overview: "metadata before enrichment",
		Genres:   []string{"Before"},
	}); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	if err := repos.credits.ReplaceCredits(ctx, movieID, []domain.MovieCredit{
		{
			MovieID:   movieID,
			Person:    domain.Person{ID: 301, Name: "Before Person"},
			Kind:      domain.CreditKindCast,
			Character: "Before",
		},
	}); err != nil {
		t.Fatalf("seed credits: %v", err)
	}

	const markerBefore = int64(1_700_000_123)
	if _, err := repos.db.Write.ExecContext(ctx,
		"UPDATE movie_metadata SET credits_refreshed_at = ? WHERE movie_id = ?",
		markerBefore,
		movieID,
	); err != nil {
		t.Fatalf("set marker before enrichment: %v", err)
	}
	if _, err := repos.db.Write.ExecContext(ctx, fmt.Sprintf(`
		CREATE TRIGGER fail_atomic_enrichment_credit
		BEFORE INSERT ON movie_credits
		WHEN NEW.movie_id = %d AND NEW.person_id = 302
		BEGIN
			SELECT RAISE(ABORT, 'forced enrichment credit failure');
		END
	`, movieID)); err != nil {
		t.Fatalf("install credit failure: %v", err)
	}

	tmdb := &fakeTMDB{
		findFn: func(context.Context, string) (tmdbMovie, error) {
			return tmdbMovie{ID: 301}, nil
		},
		detailsFn: func(context.Context, int) (tmdbMovieDetails, error) {
			return tmdbMovieDetails{
				ID:       301,
				IMDbID:   oldIMDb,
				Overview: "metadata after enrichment",
				Genres:   []tmdbGenre{{Name: "After"}},
				Credits: tmdbCredits{Cast: []tmdbCastMember{
					{ID: 302, Name: "After Person", Character: "After", Order: 0},
				}},
			}, nil
		},
	}
	svc := newEnrichmentService(repos.movies, repos.meta, tmdb, 15)

	if _, err := svc.EnrichOne(ctx, movieID); err == nil {
		t.Fatal("expected forced credit failure")
	}

	movie, err := repos.movies.FindByID(ctx, movieID)
	if err != nil {
		t.Fatalf("read movie: %v", err)
	}
	if movie.TMDBID != nil || movie.IMDbID == nil || *movie.IMDbID != oldIMDb {
		t.Fatalf("identity after rollback = %v/%v, want nil/%s", movie.TMDBID, movie.IMDbID, oldIMDb)
	}
	md, err := repos.meta.GetMetadata(ctx, movieID)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if md.Overview != "metadata before enrichment" {
		t.Fatalf("metadata after rollback = %q", md.Overview)
	}
	gotCredits, err := repos.credits.GetCreditsByMovieIDs(ctx, []int{movieID})
	if err != nil {
		t.Fatalf("read credits: %v", err)
	}
	if rows := gotCredits[movieID]; len(rows) != 1 || rows[0].Person.Name != "Before Person" {
		t.Fatalf("credits after rollback = %+v", rows)
	}

	var markerAfter int64
	if err := repos.db.Read.QueryRowContext(ctx,
		"SELECT credits_refreshed_at FROM movie_metadata WHERE movie_id = ?",
		movieID,
	).Scan(&markerAfter); err != nil {
		t.Fatalf("read marker after enrichment: %v", err)
	}
	if markerAfter != markerBefore {
		t.Fatalf("credits marker after rollback = %d, want %d", markerAfter, markerBefore)
	}
	var newPeople int
	if err := repos.db.Read.QueryRowContext(ctx, "SELECT COUNT(*) FROM people WHERE id = 302").Scan(&newPeople); err != nil {
		t.Fatalf("count rolled-back people: %v", err)
	}
	if newPeople != 0 {
		t.Fatalf("failed enrichment left %d new people rows", newPeople)
	}
}

func TestEnrichOne_AtomicSuccessWithEmptyCredits(t *testing.T) {
	ctx, repos := setupEnrichmentTestRepos(t)
	const imdbID = "tt0000301"
	_, movieID := seedEnrichmentMovie(t, ctx, repos, "Empty credits", imdbID)

	tmdb := &fakeTMDB{
		findFn: func(context.Context, string) (tmdbMovie, error) {
			return tmdbMovie{ID: 401}, nil
		},
		detailsFn: func(context.Context, int) (tmdbMovieDetails, error) {
			return tmdbMovieDetails{
				ID:       401,
				IMDbID:   imdbID,
				Overview: "credit-less movie",
				Genres:   []tmdbGenre{{Name: "Documentary"}},
			}, nil
		},
	}
	svc := newEnrichmentService(repos.movies, repos.meta, tmdb, 15)

	if _, err := svc.EnrichOne(ctx, movieID); err != nil {
		t.Fatalf("enrich: %v", err)
	}

	movie, err := repos.movies.FindByID(ctx, movieID)
	if err != nil {
		t.Fatalf("read movie: %v", err)
	}
	if movie.TMDBID == nil || *movie.TMDBID != 401 || movie.IMDbID == nil || *movie.IMDbID != imdbID {
		t.Fatalf("identity after enrichment = %v/%v", movie.TMDBID, movie.IMDbID)
	}
	md, err := repos.meta.GetMetadata(ctx, movieID)
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if md.Overview != "credit-less movie" || len(md.Genres) != 1 || md.Genres[0] != "Documentary" {
		t.Fatalf("metadata after enrichment = %+v", md)
	}
	gotCredits, err := repos.credits.GetCreditsByMovieIDs(ctx, []int{movieID})
	if err != nil {
		t.Fatalf("read credits: %v", err)
	}
	if len(gotCredits[movieID]) != 0 {
		t.Fatalf("empty enrichment stored credits: %+v", gotCredits[movieID])
	}
	candidates, err := repos.meta.NeedsEnrichment(ctx, time.Time{}, 100)
	if err != nil {
		t.Fatalf("needs enrichment: %v", err)
	}
	if candidateIncludes(candidates, movieID) {
		t.Fatal("credit-less movie stayed in enrichment backlog")
	}
}

func TestEnrichOne_DuplicateResolvedTMDBStillStoresMetadataAndCredits(t *testing.T) {
	ctx, repos := setupEnrichmentTestRepos(t)
	const ownerIMDb = "tt0000401"
	const targetIMDb = "tt0000402"
	userID, targetID := seedEnrichmentMovie(t, ctx, repos, "Duplicate target", targetIMDb)

	owner, err := repos.movies.Add(ctx, "Identity owner", string(domain.MovieStatusStash), userID)
	if err != nil {
		t.Fatalf("add identity owner: %v", err)
	}
	ownedTMDB := 501
	if err := repos.movies.SetExternalIDs(ctx, owner.ID, &ownedTMDB, new(ownerIMDb)); err != nil {
		t.Fatalf("set owned identity: %v", err)
	}

	tmdb := &fakeTMDB{
		findFn: func(context.Context, string) (tmdbMovie, error) {
			return tmdbMovie{ID: ownedTMDB}, nil
		},
		detailsFn: func(context.Context, int) (tmdbMovieDetails, error) {
			return tmdbMovieDetails{
				ID:       ownedTMDB,
				IMDbID:   targetIMDb,
				Overview: "duplicate metadata",
				Credits: tmdbCredits{Cast: []tmdbCastMember{
					{ID: 502, Name: "Duplicate Person", Character: "Duplicate", Order: 0},
				}},
			}, nil
		},
	}
	svc := newEnrichmentService(repos.movies, repos.meta, tmdb, 15)

	if _, err := svc.EnrichOne(ctx, targetID); err != nil {
		t.Fatalf("enrich duplicate identity: %v", err)
	}

	target, err := repos.movies.FindByID(ctx, targetID)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if target.TMDBID != nil || target.IMDbID == nil || *target.IMDbID != targetIMDb {
		t.Fatalf("duplicate target identity = %v/%v", target.TMDBID, target.IMDbID)
	}
	md, err := repos.meta.GetMetadata(ctx, targetID)
	if err != nil {
		t.Fatalf("read target metadata: %v", err)
	}
	if md.Overview != "duplicate metadata" {
		t.Fatalf("duplicate target metadata = %q", md.Overview)
	}
	gotCredits, err := repos.credits.GetCreditsByMovieIDs(ctx, []int{targetID})
	if err != nil {
		t.Fatalf("read target credits: %v", err)
	}
	if rows := gotCredits[targetID]; len(rows) != 1 || rows[0].Person.Name != "Duplicate Person" {
		t.Fatalf("duplicate target credits = %+v", rows)
	}
	candidates, err := repos.meta.NeedsEnrichment(ctx, time.Time{}, 100)
	if err != nil {
		t.Fatalf("needs enrichment: %v", err)
	}
	if candidateIncludes(candidates, targetID) {
		t.Fatal("duplicate target stayed in enrichment backlog")
	}
}

func TestEnrichOne_MissingMovieDuringPersistIsNotFound(t *testing.T) {
	ctx, repos := setupEnrichmentTestRepos(t)
	const imdbID = "tt0000501"
	_, movieID := seedEnrichmentMovie(t, ctx, repos, "Deleted race", imdbID)

	detailsStarted := make(chan struct{})
	releaseDetails := make(chan struct{})
	tmdb := &fakeTMDB{
		findFn: func(context.Context, string) (tmdbMovie, error) {
			return tmdbMovie{ID: 601}, nil
		},
		detailsFn: func(ctx context.Context, _ int) (tmdbMovieDetails, error) {
			close(detailsStarted)
			select {
			case <-releaseDetails:
			case <-ctx.Done():
				return tmdbMovieDetails{}, ctx.Err()
			}
			return tmdbMovieDetails{ID: 601, IMDbID: imdbID}, nil
		},
	}
	svc := newEnrichmentService(repos.movies, repos.meta, tmdb, 15)

	enrichErr := make(chan error, 1)
	go func() {
		_, err := svc.EnrichOne(ctx, movieID)
		enrichErr <- err
	}()

	waitForSignal(t, detailsStarted, "deleted movie TMDB request")
	if err := repos.movies.Delete(ctx, movieID); err != nil {
		close(releaseDetails)
		t.Fatalf("delete movie: %v", err)
	}
	close(releaseDetails)

	if err := waitForEnrichmentResult(t, enrichErr); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing movie error = %v, want ErrNotFound", err)
	}
}
