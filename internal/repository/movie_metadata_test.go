package repository

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

func setupMetadataRepos(t *testing.T) (context.Context, *SqliteMovieMetadataRepository, *SqliteMoviesRepository, *SqliteUserRepository) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "metadata-test.db")
	dbConn, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.RunMigrations(ctx, dbConn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() {
		if err := dbConn.Close(); err != nil {
			t.Fatalf("close sqlite: %v", err)
		}
	})

	return ctx,
		NewSqliteMovieMetadataRepository(dbConn),
		NewSqliteMoviesRepository(dbConn),
		NewSqliteUserRepository(dbConn)
}

func seedMovie(t *testing.T, ctx context.Context, users *SqliteUserRepository, movies *SqliteMoviesRepository, name string) int {
	t.Helper()
	user, err := users.Create(ctx, name)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	movie, err := movies.Add(ctx, name+" movie", "pool", user.ID)
	if err != nil {
		t.Fatalf("add movie: %v", err)
	}
	return movie.ID
}

func TestUpsertAndGetMetadata_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx, meta, movies, users := setupMetadataRepos(t)
	movieID := seedMovie(t, ctx, users, movies, "Alice")

	in := domain.MovieMetadata{
		MovieID:      movieID,
		Overview:     "A hacker learns the truth.",
		PosterPath:   new("/poster.jpg"),
		BackdropPath: nil, // exercises SQL NULL round-trip
		ReleaseDate:  "1999-03-30",
		Runtime:      136,
		Genres:       []string{"Action", "Science Fiction"},
		VoteAverage:  8.2,
		VoteCount:    25000,
		Tagline:      "Free your mind.",
	}
	if err := meta.UpsertMetadata(ctx, in); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := meta.GetMetadata(ctx, movieID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.Runtime != 136 || got.ReleaseDate != "1999-03-30" {
		t.Fatalf("scalar mismatch: %+v", got)
	}
	if got.PosterPath == nil || *got.PosterPath != "/poster.jpg" {
		t.Fatalf("poster mismatch: %v", got.PosterPath)
	}
	if got.BackdropPath != nil {
		t.Fatalf("expected nil backdrop, got %v", *got.BackdropPath)
	}
	if len(got.Genres) != 2 || got.Genres[0] != "Action" || got.Genres[1] != "Science Fiction" {
		t.Fatalf("genres mismatch: %v", got.Genres)
	}
	if got.VoteAverage != 8.2 || got.VoteCount != 25000 || got.Tagline != "Free your mind." {
		t.Fatalf("vote/tagline mismatch: %+v", got)
	}
	if got.EnrichedAt == nil {
		t.Fatalf("expected enriched_at to be set")
	}
}

func TestUpsertMetadata_Idempotent(t *testing.T) {
	t.Parallel()
	ctx, meta, movies, users := setupMetadataRepos(t)
	movieID := seedMovie(t, ctx, users, movies, "Bob")

	first := domain.MovieMetadata{MovieID: movieID, Runtime: 142, Genres: []string{"Drama"}}
	if err := meta.UpsertMetadata(ctx, first); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	updated := first
	updated.Runtime = 999
	updated.Genres = []string{"Drama", "Crime"}
	if err := meta.UpsertMetadata(ctx, updated); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := meta.GetMetadata(ctx, movieID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Runtime != 999 {
		t.Fatalf("expected updated runtime 999, got %d", got.Runtime)
	}
	if len(got.Genres) != 2 {
		t.Fatalf("expected genres replaced, got %v", got.Genres)
	}

	// Still exactly one row for this movie (no longer a backfill candidate).
	candidates, err := meta.NeedsEnrichment(ctx, time.Time{}, 100)
	if err != nil {
		t.Fatalf("needs-enrichment: %v", err)
	}
	if containsID(candidates, movieID) {
		t.Fatalf("enriched movie %d should not be a backfill candidate", movieID)
	}
}

func TestUpsertMetadata_NilGenresStoredAsEmptyArray(t *testing.T) {
	t.Parallel()
	ctx, meta, movies, users := setupMetadataRepos(t)
	movieID := seedMovie(t, ctx, users, movies, "Carol")

	if err := meta.UpsertMetadata(ctx, domain.MovieMetadata{MovieID: movieID}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := meta.GetMetadata(ctx, movieID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Genres) != 0 {
		t.Fatalf("expected empty genres, got %v", got.Genres)
	}
}

func TestGetMetadataByMovieIDs_BatchAndPartial(t *testing.T) {
	t.Parallel()
	ctx, meta, movies, users := setupMetadataRepos(t)
	idA := seedMovie(t, ctx, users, movies, "Mara")
	idB := seedMovie(t, ctx, users, movies, "Nils")
	idC := seedMovie(t, ctx, users, movies, "Omar") // intentionally left un-enriched

	if err := meta.UpsertMetadata(ctx, domain.MovieMetadata{MovieID: idA, Runtime: 100, Genres: []string{"Action"}}); err != nil {
		t.Fatalf("upsert A: %v", err)
	}
	if err := meta.UpsertMetadata(ctx, domain.MovieMetadata{MovieID: idB, Runtime: 200, PosterPath: new("/b.jpg")}); err != nil {
		t.Fatalf("upsert B: %v", err)
	}

	got, err := meta.GetMetadataByMovieIDs(ctx, []int{idA, idB, idC})
	if err != nil {
		t.Fatalf("batch get: %v", err)
	}

	// Enriched ids present; the un-enriched id is simply absent (async enrichment).
	if len(got) != 2 {
		t.Fatalf("expected 2 enriched rows, got %d", len(got))
	}
	if got[idA] == nil || got[idA].Runtime != 100 {
		t.Fatalf("A mismatch: %+v", got[idA])
	}
	if got[idB] == nil || got[idB].PosterPath == nil || *got[idB].PosterPath != "/b.jpg" {
		t.Fatalf("B mismatch: %+v", got[idB])
	}
	if _, ok := got[idC]; ok {
		t.Fatalf("un-enriched movie %d must be absent from the map", idC)
	}
}

func TestGetMetadataByMovieIDs_EmptyInput(t *testing.T) {
	t.Parallel()
	ctx, meta, _, _ := setupMetadataRepos(t)

	got, err := meta.GetMetadataByMovieIDs(ctx, nil)
	if err != nil {
		t.Fatalf("batch get: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %v", got)
	}
}

func TestGetMetadata_NotFound(t *testing.T) {
	t.Parallel()
	ctx, meta, _, _ := setupMetadataRepos(t)

	_, err := meta.GetMetadata(ctx, 4242)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSetExternalIDs_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx, _, movies, users := setupMetadataRepos(t)
	movieID := seedMovie(t, ctx, users, movies, "Zed")

	// Fresh movie has no ids.
	m, err := movies.FindByID(ctx, movieID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if m.TMDBID != nil || m.IMDbID != nil {
		t.Fatalf("expected nil ids on a fresh movie, got %v / %v", m.TMDBID, m.IMDbID)
	}

	tmdbID := 603
	imdbID := "tt0133093"
	if err := movies.SetExternalIDs(ctx, movieID, &tmdbID, &imdbID); err != nil {
		t.Fatalf("set: %v", err)
	}

	m, err = movies.FindByID(ctx, movieID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if m.TMDBID == nil || *m.TMDBID != 603 || m.IMDbID == nil || *m.IMDbID != "tt0133093" {
		t.Fatalf("ids not persisted: %v / %v", m.TMDBID, m.IMDbID)
	}

	// Resetting to nil clears them (the edit-changed-link path).
	if err := movies.SetExternalIDs(ctx, movieID, nil, nil); err != nil {
		t.Fatalf("reset: %v", err)
	}
	m, err = movies.FindByID(ctx, movieID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if m.TMDBID != nil || m.IMDbID != nil {
		t.Fatalf("expected ids cleared, got %v / %v", m.TMDBID, m.IMDbID)
	}
}

func TestNeedsEnrichment_BackfillAndStale(t *testing.T) {
	t.Parallel()
	ctx, meta, movies, users := setupMetadataRepos(t)
	idA := seedMovie(t, ctx, users, movies, "Dave")
	idB := seedMovie(t, ctx, users, movies, "Erin")

	// Backfill (zero time): both un-enriched movies are candidates.
	backfill, err := meta.NeedsEnrichment(ctx, time.Time{}, 100)
	if err != nil {
		t.Fatalf("backfill query: %v", err)
	}
	if !containsID(backfill, idA) || !containsID(backfill, idB) {
		t.Fatalf("expected both movies as backfill candidates, got %v", backfill)
	}

	// Enrich A. Now backfill returns only B.
	if err := meta.UpsertMetadata(ctx, domain.MovieMetadata{MovieID: idA}); err != nil {
		t.Fatalf("upsert A: %v", err)
	}
	backfill, err = meta.NeedsEnrichment(ctx, time.Time{}, 100)
	if err != nil {
		t.Fatalf("backfill query 2: %v", err)
	}
	if containsID(backfill, idA) {
		t.Fatalf("enriched A should not be a backfill candidate")
	}
	if !containsID(backfill, idB) {
		t.Fatalf("B should still be a backfill candidate")
	}

	// Stale scan with a future cutoff: A's fresh enriched_at is < future, so A
	// is stale and returned again (plus B, which has no row).
	future, err := meta.NeedsEnrichment(ctx, time.Now().Add(time.Hour), 100)
	if err != nil {
		t.Fatalf("stale query (future): %v", err)
	}
	if !containsID(future, idA) {
		t.Fatalf("A should be stale against a future cutoff")
	}

	// Stale scan with a past cutoff: A's fresh enriched_at is NOT < past, so A
	// is not stale; only B (no row) is returned.
	past, err := meta.NeedsEnrichment(ctx, time.Now().Add(-time.Hour), 100)
	if err != nil {
		t.Fatalf("stale query (past): %v", err)
	}
	if containsID(past, idA) {
		t.Fatalf("freshly-enriched A should not be stale against a past cutoff")
	}
	if !containsID(past, idB) {
		t.Fatalf("B should always be a candidate (no metadata)")
	}
}

func TestNeedsEnrichment_RespectsLimit(t *testing.T) {
	t.Parallel()
	ctx, meta, movies, users := setupMetadataRepos(t)
	seedMovie(t, ctx, users, movies, "Fay")
	seedMovie(t, ctx, users, movies, "Gus")
	seedMovie(t, ctx, users, movies, "Ivy")

	got, err := meta.NeedsEnrichment(ctx, time.Time{}, 2)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected limit of 2, got %d", len(got))
	}
}

func TestFindByStatus_WatchedScansAllColumns(t *testing.T) {
	t.Parallel()
	ctx, _, movies, users := setupMetadataRepos(t)
	movieID := seedMovie(t, ctx, users, movies, "Wendy")
	if err := movies.MarkAsWatched(ctx, movieID, time.Now().UTC()); err != nil {
		t.Fatalf("mark watched: %v", err)
	}

	// Regression: the "watched" query variant must SELECT the same columns
	// scanMovie expects (tmdb_id/imdb_id included) or Scan fails with a 500.
	watched, err := movies.FindByStatus(ctx, "watched")
	if err != nil {
		t.Fatalf("find watched: %v", err)
	}
	if len(watched) != 1 || watched[0].ID != movieID {
		t.Fatalf("expected the watched movie, got %+v", watched)
	}
}

func TestMetadata_CascadeDeleteWithMovie(t *testing.T) {
	t.Parallel()
	ctx, meta, movies, users := setupMetadataRepos(t)
	movieID := seedMovie(t, ctx, users, movies, "Hank")

	if err := meta.UpsertMetadata(ctx, domain.MovieMetadata{MovieID: movieID}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := movies.Delete(ctx, movieID); err != nil {
		t.Fatalf("delete movie: %v", err)
	}

	// FK ON DELETE CASCADE (foreign_keys pragma is on via the DSN) removes the row.
	if _, err := meta.GetMetadata(ctx, movieID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected metadata removed by cascade, got %v", err)
	}
}

func containsID(candidates []domain.EnrichmentCandidate, id int) bool {
	for _, c := range candidates {
		if c.MovieID == id {
			return true
		}
	}
	return false
}
