package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"moviepickarr/internal/db"
	"moviepickarr/internal/domain"
)

func setupCreditsRepos(t *testing.T) (context.Context, *SqliteMovieCreditsRepository, *SqliteMovieMetadataRepository, *SqliteMoviesRepository, *SqliteUserRepository) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "credits-test.db")
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
		NewSqliteMovieCreditsRepository(dbConn),
		NewSqliteMovieMetadataRepository(dbConn),
		NewSqliteMoviesRepository(dbConn),
		NewSqliteUserRepository(dbConn)
}

func castOf(movieID, personID int, name, character string, order int) domain.MovieCredit {
	return domain.MovieCredit{
		MovieID:   movieID,
		Person:    domain.Person{ID: personID, Name: name},
		Kind:      domain.CreditKindCast,
		Character: character,
		CastOrder: order,
	}
}

func crewOf(movieID, personID int, name, job, department string) domain.MovieCredit {
	return domain.MovieCredit{
		MovieID:    movieID,
		Person:     domain.Person{ID: personID, Name: name},
		Kind:       domain.CreditKindCrew,
		Job:        job,
		Department: department,
	}
}

func TestReplaceAndGetCredits_RoundTripAndOrder(t *testing.T) {
	t.Parallel()
	ctx, credits, _, movies, users := setupCreditsRepos(t)
	movieID := seedMovie(t, ctx, users, movies, "Alice")

	profile := "/kr.jpg"
	in := []domain.MovieCredit{
		// Inserted shuffled on purpose: the read must order cast (by billing)
		// before crew (by name).
		crewOf(movieID, 9340, "Lana Wachowski", "Director", "Directing"),
		castOf(movieID, 530, "Carrie-Anne Moss", "Trinity", 1),
		crewOf(movieID, 9339, "Lilly Wachowski", "Director", "Directing"),
		castOf(movieID, 6384, "Keanu Reeves", "Neo", 0),
	}
	in[3].Person.ProfilePath = &profile
	if err := credits.ReplaceCredits(ctx, movieID, in); err != nil {
		t.Fatalf("replace: %v", err)
	}

	got, err := credits.GetCreditsByMovieIDs(ctx, []int{movieID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	rows := got[movieID]
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(rows))
	}

	// Cast first, in billing order…
	if rows[0].Person.Name != "Keanu Reeves" || rows[0].Kind != domain.CreditKindCast || rows[0].Character != "Neo" {
		t.Fatalf("row 0 mismatch: %+v", rows[0])
	}
	if rows[0].Person.ProfilePath == nil || *rows[0].Person.ProfilePath != "/kr.jpg" {
		t.Fatalf("profile mismatch: %v", rows[0].Person.ProfilePath)
	}
	if rows[1].Person.Name != "Carrie-Anne Moss" || rows[1].CastOrder != 1 {
		t.Fatalf("row 1 mismatch: %+v", rows[1])
	}
	// …then crew alphabetically.
	if rows[2].Person.Name != "Lana Wachowski" || rows[2].Kind != domain.CreditKindCrew || rows[2].Job != "Director" {
		t.Fatalf("row 2 mismatch: %+v", rows[2])
	}
	if rows[3].Person.Name != "Lilly Wachowski" || rows[3].Department != "Directing" {
		t.Fatalf("row 3 mismatch: %+v", rows[3])
	}
}

func TestReplaceCredits_ReplacesRowsAndRefreshesPerson(t *testing.T) {
	t.Parallel()
	ctx, credits, _, movies, users := setupCreditsRepos(t)
	movieID := seedMovie(t, ctx, users, movies, "Bob")

	first := []domain.MovieCredit{
		castOf(movieID, 1, "Old Name", "Hero", 0),
		castOf(movieID, 2, "Sidekick", "Pal", 1),
	}
	if err := credits.ReplaceCredits(ctx, movieID, first); err != nil {
		t.Fatalf("first replace: %v", err)
	}

	// Re-enrichment: the sidekick is gone, the lead's person row got renamed.
	second := []domain.MovieCredit{
		castOf(movieID, 1, "New Name", "Hero", 0),
	}
	if err := credits.ReplaceCredits(ctx, movieID, second); err != nil {
		t.Fatalf("second replace: %v", err)
	}

	got, err := credits.GetCreditsByMovieIDs(ctx, []int{movieID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	rows := got[movieID]
	if len(rows) != 1 {
		t.Fatalf("expected old rows replaced, got %d rows", len(rows))
	}
	if rows[0].Person.Name != "New Name" {
		t.Fatalf("expected person refreshed via upsert, got %q", rows[0].Person.Name)
	}
}

func TestReplaceCredits_PersonAsCastAndCrew(t *testing.T) {
	t.Parallel()
	ctx, credits, _, movies, users := setupCreditsRepos(t)
	movieID := seedMovie(t, ctx, users, movies, "Cara")

	// Same person acting and directing (plus a second crew job): three rows
	// against one people entry — the PK allows it because kind/job differ.
	in := []domain.MovieCredit{
		castOf(movieID, 190, "Clint Eastwood", "Walt", 0),
		crewOf(movieID, 190, "Clint Eastwood", "Director", "Directing"),
		crewOf(movieID, 190, "Clint Eastwood", "Writer", "Writing"),
	}
	if err := credits.ReplaceCredits(ctx, movieID, in); err != nil {
		t.Fatalf("replace: %v", err)
	}

	got, err := credits.GetCreditsByMovieIDs(ctx, []int{movieID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	rows := got[movieID]
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows for the dual-role person, got %d", len(rows))
	}
	if rows[0].Kind != domain.CreditKindCast || rows[1].Kind != domain.CreditKindCrew || rows[2].Kind != domain.CreditKindCrew {
		t.Fatalf("kind order mismatch: %+v", rows)
	}
}

func TestGetCreditsByMovieIDs_BatchAndEmptyInput(t *testing.T) {
	t.Parallel()
	ctx, credits, _, movies, users := setupCreditsRepos(t)
	idA := seedMovie(t, ctx, users, movies, "Mara")
	idB := seedMovie(t, ctx, users, movies, "Nils")
	idC := seedMovie(t, ctx, users, movies, "Omar") // intentionally credit-less

	if err := credits.ReplaceCredits(ctx, idA, []domain.MovieCredit{castOf(idA, 1, "A", "X", 0)}); err != nil {
		t.Fatalf("replace A: %v", err)
	}
	if err := credits.ReplaceCredits(ctx, idB, []domain.MovieCredit{crewOf(idB, 2, "B", "Director", "Directing")}); err != nil {
		t.Fatalf("replace B: %v", err)
	}

	got, err := credits.GetCreditsByMovieIDs(ctx, []int{idA, idB, idC})
	if err != nil {
		t.Fatalf("batch get: %v", err)
	}
	if len(got) != 2 || len(got[idA]) != 1 || len(got[idB]) != 1 {
		t.Fatalf("batch mismatch: %v", got)
	}
	if _, ok := got[idC]; ok {
		t.Fatalf("credit-less movie %d must be absent from the map", idC)
	}

	empty, err := credits.GetCreditsByMovieIDs(ctx, nil)
	if err != nil {
		t.Fatalf("empty get: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty map, got %v", empty)
	}
}

func TestReplaceCredits_StampsCreditsRefreshedAt(t *testing.T) {
	t.Parallel()
	ctx, credits, meta, movies, users := setupCreditsRepos(t)
	movieID := seedMovie(t, ctx, users, movies, "Dave")

	// Metadata enriched, but credits never ingested: the NULL marker keeps the
	// movie a NeedsEnrichment candidate (credits backfill for existing rows).
	if err := meta.UpsertMetadata(ctx, domain.MovieMetadata{MovieID: movieID}); err != nil {
		t.Fatalf("upsert metadata: %v", err)
	}
	candidates, err := meta.NeedsEnrichment(ctx, time.Time{}, 100)
	if err != nil {
		t.Fatalf("needs-enrichment: %v", err)
	}
	if !containsID(candidates, movieID) {
		t.Fatalf("movie without ingested credits should be a candidate")
	}

	// Replacing with EMPTY credits still stamps the marker — a genuinely
	// credit-less title must not stay in the backlog forever.
	if err := credits.ReplaceCredits(ctx, movieID, nil); err != nil {
		t.Fatalf("replace: %v", err)
	}
	candidates, err = meta.NeedsEnrichment(ctx, time.Time{}, 100)
	if err != nil {
		t.Fatalf("needs-enrichment 2: %v", err)
	}
	if containsID(candidates, movieID) {
		t.Fatalf("stamped movie should no longer be a candidate")
	}
}

func TestCredits_CascadeDeleteWithMovie(t *testing.T) {
	t.Parallel()
	ctx, credits, _, movies, users := setupCreditsRepos(t)
	movieID := seedMovie(t, ctx, users, movies, "Erin")

	in := []domain.MovieCredit{castOf(movieID, 1, "A", "X", 0)}
	if err := credits.ReplaceCredits(ctx, movieID, in); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if err := movies.Delete(ctx, movieID); err != nil {
		t.Fatalf("delete movie: %v", err)
	}

	// FK ON DELETE CASCADE (foreign_keys pragma is on via the DSN) removes the
	// credit rows; the shared people rows stay.
	got, err := credits.GetCreditsByMovieIDs(ctx, []int{movieID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected credits removed by cascade, got %v", got)
	}
}
