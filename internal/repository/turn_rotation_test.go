package repository

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"moviepickarr/internal/domain"
)

// startTestDraw pools a movie for adderID and draws it, so it holds the
// concealed Acquisition a Reveal needs.
func startTestDraw(t *testing.T, e *userRemoveEnv, title string, adderID int) *domain.Movie {
	t.Helper()
	movie, err := e.movies.Add(e.ctx, title, "pool", adderID)
	if err != nil {
		t.Fatalf("add pooled movie %q: %v", title, err)
	}
	drawnAt := time.Now().UTC().Truncate(time.Millisecond)
	if err := e.movies.StartDraw(e.ctx, movie.ID, drawnAt, drawnAt.Add(time.Minute), "drawer"); err != nil {
		t.Fatalf("StartDraw %q: %v", title, err)
	}
	return movie
}

func createTestMembers(t *testing.T, e *userRemoveEnv, names ...string) []*domain.User {
	t.Helper()
	members := make([]*domain.User, 0, len(names))
	for _, name := range names {
		member, err := e.users.Create(e.ctx, name)
		if err != nil {
			t.Fatalf("create member %q: %v", name, err)
		}
		members = append(members, member)
	}
	return members
}

func assertStoredNextUp(t *testing.T, e *userRemoveEnv, wantID int) {
	t.Helper()
	stored, err := e.nextUp.Get(e.ctx)
	if err != nil {
		t.Fatalf("get stored next up: %v", err)
	}
	if stored.ID != wantID {
		t.Fatalf("stored next up = %d, want %d", stored.ID, wantID)
	}
}

func TestRevealDrawAndAdvanceNextUp_RotatesValidHolder(t *testing.T) {
	tests := []struct {
		name       string
		holder     int
		wantHolder int
	}{
		{name: "advances", holder: 0, wantHolder: 1},
		{name: "wraps", holder: 2, wantHolder: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setupUserRemoveEnv(t)
			members := createTestMembers(t, e, "Ana", "Ben", "Cai")
			if err := e.nextUp.Set(e.ctx, members[tt.holder].ID); err != nil {
				t.Fatalf("set next up: %v", err)
			}
			drawn := startTestDraw(t, e, "Heat", members[0].ID)

			next, err := e.movies.RevealDrawAndAdvanceNextUp(e.ctx, drawn.ID, time.Now().UTC())
			if err != nil {
				t.Fatalf("reveal and rotate: %v", err)
			}
			if next == nil || next.ID != members[tt.wantHolder].ID {
				t.Fatalf("handoff = %+v, want member %d", next, members[tt.wantHolder].ID)
			}
			assertStoredNextUp(t, e, members[tt.wantHolder].ID)
		})
	}
}

func TestRevealDrawAndAdvanceNextUp_SeedsThenRotatesFreshInstall(t *testing.T) {
	e := setupUserRemoveEnv(t)
	members := createTestMembers(t, e, "Ana", "Ben")
	drawn := startTestDraw(t, e, "Heat", members[0].ID)

	next, err := e.movies.RevealDrawAndAdvanceNextUp(e.ctx, drawn.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("reveal and rotate: %v", err)
	}
	if next == nil || next.ID != members[1].ID {
		t.Fatalf("handoff = %+v, want member %d", next, members[1].ID)
	}
	assertStoredNextUp(t, e, members[1].ID)
}

func TestRevealDrawAndAdvanceNextUp_SkipsGuests(t *testing.T) {
	e := setupUserRemoveEnv(t)
	members := createTestMembers(t, e, "Ana", "Guest", "Cai")
	if _, err := e.users.SetRole(e.ctx, domain.RoleChange{MemberID: members[1].ID, Role: domain.RoleGuest}); err != nil {
		t.Fatalf("set guest role: %v", err)
	}
	if err := e.nextUp.Set(e.ctx, members[0].ID); err != nil {
		t.Fatalf("set next up: %v", err)
	}
	drawn := startTestDraw(t, e, "Heat", members[0].ID)

	next, err := e.movies.RevealDrawAndAdvanceNextUp(e.ctx, drawn.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("reveal and rotate: %v", err)
	}
	if next == nil || next.ID != members[2].ID {
		t.Fatalf("handoff = %+v, want member %d", next, members[2].ID)
	}
}

func TestRevealDrawAndAdvanceNextUp_HandsArchivedTurnToFirstActiveMember(t *testing.T) {
	e := setupUserRemoveEnv(t)
	members := createTestMembers(t, e, "Departing", "First active", "Second active")
	departing, firstActive := members[0], members[1]
	if err := e.nextUp.Set(e.ctx, departing.ID); err != nil {
		t.Fatalf("set departing member next up: %v", err)
	}
	drawn := startTestDraw(t, e, "Heat", departing.ID)
	if outcome, err := e.users.Remove(e.ctx, departing.ID); err != nil || outcome != domain.OutcomeArchived {
		t.Fatalf("archive departing member: outcome=%q err=%v", outcome, err)
	}

	next, err := e.movies.RevealDrawAndAdvanceNextUp(e.ctx, drawn.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("reveal and rotate: %v", err)
	}
	if next == nil || next.ID != firstActive.ID {
		t.Fatalf("handoff = %+v, want first active member %d", next, firstActive.ID)
	}
}

// The next member still owes the watch of the last pooled movie, so an empty
// pool does not hold the turn back.
func TestRevealDrawAndAdvanceNextUp_RotatesWhenPoolIsEmpty(t *testing.T) {
	e := setupUserRemoveEnv(t)
	members := createTestMembers(t, e, "Ana", "Ben")
	if err := e.nextUp.Set(e.ctx, members[0].ID); err != nil {
		t.Fatalf("set next up: %v", err)
	}
	drawn := startTestDraw(t, e, "Heat", members[0].ID)
	if pooled, err := e.movies.CountByStatus(e.ctx, "pool"); err != nil || pooled != 0 {
		t.Fatalf("pool after draw = %d, err=%v, want empty", pooled, err)
	}

	next, err := e.movies.RevealDrawAndAdvanceNextUp(e.ctx, drawn.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("reveal and rotate: %v", err)
	}
	if next == nil || next.ID != members[1].ID {
		t.Fatalf("handoff = %+v, want member %d", next, members[1].ID)
	}
	assertStoredNextUp(t, e, members[1].ID)
}

func TestRevealDrawAndAdvanceNextUp_KeepsTurnWithOneMember(t *testing.T) {
	e := setupUserRemoveEnv(t)
	only := createTestMembers(t, e, "Only")[0]
	if err := e.nextUp.Set(e.ctx, only.ID); err != nil {
		t.Fatalf("set next up: %v", err)
	}
	drawn := startTestDraw(t, e, "Heat", only.ID)

	next, err := e.movies.RevealDrawAndAdvanceNextUp(e.ctx, drawn.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("reveal without handoff: %v", err)
	}
	if next != nil {
		t.Fatalf("handoff = %+v, want no change", next)
	}
	assertStoredNextUp(t, e, only.ID)
}

func TestRevealDrawAndAdvanceNextUp_RollsBackRevealWhenHandoffFails(t *testing.T) {
	e := setupUserRemoveEnv(t)
	members := createTestMembers(t, e, "Ana", "Ben")
	if err := e.nextUp.Set(e.ctx, members[0].ID); err != nil {
		t.Fatalf("set next up: %v", err)
	}
	drawn := startTestDraw(t, e, "Heat", members[0].ID)
	if _, err := e.pool.Write.ExecContext(e.ctx, `
		CREATE TRIGGER fail_next_up_rotation
		BEFORE UPDATE ON next_up
		BEGIN
		    SELECT RAISE(ABORT, 'rotation unavailable');
		END
	`); err != nil {
		t.Fatalf("create rotation failure trigger: %v", err)
	}

	if _, err := e.movies.RevealDrawAndAdvanceNextUp(e.ctx, drawn.ID, time.Now().UTC()); err == nil {
		t.Fatal("Reveal succeeded despite handoff failure")
	}
	if _, _, _, _, found, err := e.movies.ConcealedCurrentDraw(e.ctx); err != nil || !found {
		t.Fatalf("failed handoff exposed the draw: found=%v err=%v", found, err)
	}
	assertStoredNextUp(t, e, members[0].ID)
}

// Watching rotates only when it is also the draw's Reveal. A revealed draw
// already handed the turn on.
func TestWatchCurrentDraw_RotatesOnlyWhenItRevealsDraw(t *testing.T) {
	tests := []struct {
		name        string
		revealsDraw bool
		wantHolder  int
	}{
		{name: "already revealed", revealsDraw: false, wantHolder: 0},
		{name: "reveals draw", revealsDraw: true, wantHolder: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setupUserRemoveEnv(t)
			members := createTestMembers(t, e, "Ana", "Ben")
			if err := e.nextUp.Set(e.ctx, members[0].ID); err != nil {
				t.Fatalf("set next up: %v", err)
			}
			drawn := startTestDraw(t, e, "Heat", members[0].ID)

			watched, next, err := e.movies.WatchCurrentDraw(e.ctx, time.Now().UTC().Add(time.Second), tt.revealsDraw)
			if err != nil {
				t.Fatalf("watch: %v", err)
			}
			if watched.ID != drawn.ID || watched.Status != "watched" || watched.WatchedAt == nil {
				t.Fatalf("watched movie = %+v, want movie %d watched", watched, drawn.ID)
			}
			if tt.revealsDraw != (next != nil) {
				t.Fatalf("handoff = %+v, want handoff=%v", next, tt.revealsDraw)
			}
			assertStoredNextUp(t, e, members[tt.wantHolder].ID)
		})
	}
}

func TestWatchCurrentDraw_RequiresCurrentMovie(t *testing.T) {
	e := setupUserRemoveEnv(t)

	_, _, err := e.movies.WatchCurrentDraw(e.ctx, time.Now().UTC(), false)
	if !errors.Is(err, domain.ErrNoCurrentDraw) {
		t.Fatalf("watch without current movie: got %v, want ErrNoCurrentDraw", err)
	}
}

func TestStartDrawSnapshotsConcealedAcquisitionAndDefersWebhookUntilReveal(t *testing.T) {
	e := setupUserRemoveEnv(t)
	member, err := e.users.Create(e.ctx, "Ana")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	movie, err := e.movies.Add(e.ctx, "Heat", "pool", member.ID)
	if err != nil {
		t.Fatalf("add pooled movie: %v", err)
	}
	tmdbID := 949
	imdbID := "tt0113277"
	if err := e.movies.SetExternalIDs(e.ctx, movie.ID, &tmdbID, &imdbID); err != nil {
		t.Fatalf("set provider ids: %v", err)
	}
	if _, err := e.pool.Write.ExecContext(e.ctx, `
		INSERT INTO radarr_webhook_destinations (
		    id, name, kind, encrypted_url, reason_filters,
		    enabled, verified_at, revision
		) VALUES (
		    1, 'Discord', 'discord', ?, '["preset_required"]',
		    1, 100, 3
		)
	`, []byte{1}); err != nil {
		t.Fatalf("insert webhook destination: %v", err)
	}
	if _, err := e.pool.Write.ExecContext(e.ctx, `
		INSERT INTO radarr_webhook_destinations (
		    id, name, kind, encrypted_url, reason_filters,
		    enabled, verified_at
		) VALUES
		    (2, 'Different reason', 'generic', ?, '["identity_required"]', 1, 100),
		    (3, 'Disabled', 'generic', ?, '["preset_required"]', 0, 100)
	`, []byte{2}, []byte{3}); err != nil {
		t.Fatalf("insert filtered webhook destinations: %v", err)
	}

	drawnAt := time.Date(2026, 8, 7, 19, 30, 0, 123_000_000, time.UTC)
	revealAt := drawnAt.Add(16_500 * time.Millisecond)
	if err := e.movies.StartDraw(e.ctx, movie.ID, drawnAt, revealAt, "drawer-1"); err != nil {
		t.Fatalf("StartDraw: %v", err)
	}

	stored, err := e.movies.FindByID(e.ctx, movie.ID)
	if err != nil {
		t.Fatalf("find drawn movie: %v", err)
	}
	if stored.Status != "current" {
		t.Fatalf("drawn movie status = %q, want current", stored.Status)
	}

	var (
		status                 string
		actionReason           sql.NullString
		actionVersion          int
		title                  string
		storedTMDBID           sql.NullInt64
		storedIMDbID           sql.NullString
		identitySource         sql.NullString
		storedDrawnAt          int64
		storedRevealAt         int64
		clientID               string
		revealedAt             sql.NullInt64
		targetTags             string
		effectiveConfiguration string
	)
	err = e.pool.Read.QueryRowContext(e.ctx, `
		SELECT status, action_reason, action_version, movie_title,
		       tmdb_id, imdb_id, identity_source, drawn_at, reveal_at,
		       draw_client_id, revealed_at, target_tags, effective_configuration
		FROM radarr_acquisitions
		WHERE movie_id = ?
	`, movie.ID).Scan(
		&status,
		&actionReason,
		&actionVersion,
		&title,
		&storedTMDBID,
		&storedIMDbID,
		&identitySource,
		&storedDrawnAt,
		&storedRevealAt,
		&clientID,
		&revealedAt,
		&targetTags,
		&effectiveConfiguration,
	)
	if err != nil {
		t.Fatalf("read concealed acquisition: %v", err)
	}
	if status != "needs_preset" ||
		!actionReason.Valid || actionReason.String != "preset_required" ||
		actionVersion != 0 {
		t.Fatalf(
			"concealed action = status %q reason %v version %d",
			status,
			actionReason,
			actionVersion,
		)
	}
	if title != "Heat" ||
		!storedTMDBID.Valid || storedTMDBID.Int64 != 949 ||
		!storedIMDbID.Valid || storedIMDbID.String != "tt0113277" ||
		!identitySource.Valid || identitySource.String != "tmdb" {
		t.Fatalf(
			"snapshot = title %q tmdb %v imdb %v source %v",
			title,
			storedTMDBID,
			storedIMDbID,
			identitySource,
		)
	}
	if storedDrawnAt != drawnAt.UnixMilli() || storedRevealAt != revealAt.UnixMilli() {
		t.Fatalf(
			"stored draw times = %d/%d, want %d/%d",
			storedDrawnAt,
			storedRevealAt,
			drawnAt.UnixMilli(),
			revealAt.UnixMilli(),
		)
	}
	if clientID != "drawer-1" || revealedAt.Valid {
		t.Fatalf("concealed state = client %q revealed %v", clientID, revealedAt)
	}
	if targetTags != "[]" || effectiveConfiguration != "{}" {
		t.Fatalf("JSON defaults = tags %q effective %q", targetTags, effectiveConfiguration)
	}
	if got := e.countRow(t, `SELECT COUNT(*) FROM radarr_webhook_deliveries`); got != 0 {
		t.Fatalf("concealed acquisition queued %d webhook deliveries, want 0", got)
	}

	movieID, recoveredDrawnAt, recoveredRevealAt, recoveredClientID, found, err := e.movies.ConcealedCurrentDraw(e.ctx)
	if err != nil {
		t.Fatalf("ConcealedCurrentDraw: %v", err)
	}
	if !found ||
		movieID != movie.ID ||
		!recoveredDrawnAt.Equal(drawnAt) ||
		!recoveredRevealAt.Equal(revealAt) ||
		recoveredClientID != "drawer-1" {
		t.Fatalf(
			"recovered draw = found %v movie %d times %v/%v client %q",
			found,
			movieID,
			recoveredDrawnAt,
			recoveredRevealAt,
			recoveredClientID,
		)
	}

	if _, err := e.pool.Write.ExecContext(e.ctx, `
		CREATE TRIGGER fail_radarr_delivery
		BEFORE INSERT ON radarr_webhook_deliveries
		BEGIN
		    SELECT RAISE(ABORT, 'delivery unavailable');
		END
	`); err != nil {
		t.Fatalf("create delivery failure trigger: %v", err)
	}
	revealedAtTime := revealAt.Add(time.Second)
	if _, err := e.movies.RevealDrawAndAdvanceNextUp(e.ctx, movie.ID, revealedAtTime); err == nil {
		t.Fatal("RevealDrawAndAdvanceNextUp succeeded despite webhook outbox failure")
	}
	if err := e.pool.Read.QueryRowContext(e.ctx, `
		SELECT revealed_at, action_version
		FROM radarr_acquisitions
		WHERE movie_id = ?
	`, movie.ID).Scan(&revealedAt, &actionVersion); err != nil {
		t.Fatalf("read rolled-back Reveal: %v", err)
	}
	if revealedAt.Valid || actionVersion != 0 {
		t.Fatalf("failed outbox transaction persisted Reveal: revealed=%v version=%d", revealedAt, actionVersion)
	}
	if _, err := e.pool.Write.ExecContext(e.ctx, `DROP TRIGGER fail_radarr_delivery`); err != nil {
		t.Fatalf("drop delivery failure trigger: %v", err)
	}

	if _, err := e.movies.RevealDrawAndAdvanceNextUp(e.ctx, movie.ID, revealedAtTime); err != nil {
		t.Fatalf("RevealDraw: %v", err)
	}
	if _, err := e.movies.RevealDrawAndAdvanceNextUp(e.ctx, movie.ID, revealedAtTime.Add(time.Second)); err != nil {
		t.Fatalf("idempotent RevealDraw: %v", err)
	}
	var (
		destinationRevision int
		deliveryReason      string
		deliveryVersion     int
		nextAttemptAt       int64
		targetLabel         string
	)
	err = e.pool.Read.QueryRowContext(e.ctx, `
		SELECT destination_revision, reason, action_version, next_attempt_at, target_label
		FROM radarr_webhook_deliveries
	`).Scan(
		&destinationRevision,
		&deliveryReason,
		&deliveryVersion,
		&nextAttemptAt,
		&targetLabel,
	)
	if err != nil {
		t.Fatalf("read Reveal delivery: %v", err)
	}
	if destinationRevision != 3 ||
		deliveryReason != "preset_required" ||
		deliveryVersion != 1 ||
		nextAttemptAt != revealedAtTime.Unix() ||
		targetLabel != "" {
		t.Fatalf(
			"delivery = revision %d reason %q action %d next %d target %q",
			destinationRevision,
			deliveryReason,
			deliveryVersion,
			nextAttemptAt,
			targetLabel,
		)
	}
	if got := e.countRow(t, `SELECT COUNT(*) FROM radarr_webhook_deliveries`); got != 1 {
		t.Fatalf("idempotent Reveal queued %d deliveries, want 1", got)
	}
}

func TestStartDrawRollsBackMovieWhenAcquisitionInsertFails(t *testing.T) {
	e := setupUserRemoveEnv(t)
	member, err := e.users.Create(e.ctx, "Ana")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	movie, err := e.movies.Add(e.ctx, "Heat", "pool", member.ID)
	if err != nil {
		t.Fatalf("add pooled movie: %v", err)
	}
	if _, err := e.pool.Write.ExecContext(e.ctx, `
		CREATE TRIGGER fail_radarr_acquisition_insert
		BEFORE INSERT ON radarr_acquisitions
		BEGIN
		    SELECT RAISE(ABORT, 'acquisition unavailable');
		END
	`); err != nil {
		t.Fatalf("create acquisition failure trigger: %v", err)
	}

	drawnAt := time.Now().UTC()
	if err := e.movies.StartDraw(e.ctx, movie.ID, drawnAt, drawnAt.Add(time.Second), "drawer"); err == nil {
		t.Fatal("StartDraw succeeded despite acquisition insert failure")
	}
	stored, err := e.movies.FindByID(e.ctx, movie.ID)
	if err != nil {
		t.Fatalf("find movie after rollback: %v", err)
	}
	if stored.Status != "pool" {
		t.Fatalf("movie status after rollback = %q, want pool", stored.Status)
	}
	if got := e.countRow(t, `SELECT COUNT(*) FROM radarr_acquisitions`); got != 0 {
		t.Fatalf("acquisitions after rollback = %d, want 0", got)
	}
}

func TestWatchRollsBackMovieWhenEarlyRevealFails(t *testing.T) {
	e := setupUserRemoveEnv(t)
	member, err := e.users.Create(e.ctx, "Ana")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	movie, err := e.movies.Add(e.ctx, "Heat", "pool", member.ID)
	if err != nil {
		t.Fatalf("add pooled movie: %v", err)
	}
	drawnAt := time.Now().UTC().Truncate(time.Millisecond)
	if err := e.movies.StartDraw(e.ctx, movie.ID, drawnAt, drawnAt.Add(time.Second), "drawer"); err != nil {
		t.Fatalf("StartDraw: %v", err)
	}
	if _, err := e.pool.Write.ExecContext(e.ctx, `
		CREATE TRIGGER fail_early_reveal
		BEFORE UPDATE OF revealed_at ON radarr_acquisitions
		WHEN NEW.revealed_at IS NOT OLD.revealed_at
		BEGIN
		    SELECT RAISE(ABORT, 'reveal unavailable');
		END
	`); err != nil {
		t.Fatalf("create Reveal failure trigger: %v", err)
	}

	if _, _, err := e.movies.WatchCurrentDraw(e.ctx, drawnAt.Add(2*time.Second), true); err == nil {
		t.Fatal("Watch succeeded despite early Reveal failure")
	}
	stored, err := e.movies.FindByID(e.ctx, movie.ID)
	if err != nil {
		t.Fatalf("find movie after failed Watch: %v", err)
	}
	if stored.Status != "current" || stored.WatchedAt != nil {
		t.Fatalf("failed Watch persisted movie state: %+v", stored)
	}
	_, _, _, _, found, err := e.movies.ConcealedCurrentDraw(e.ctx)
	if err != nil {
		t.Fatalf("ConcealedCurrentDraw: %v", err)
	}
	if !found {
		t.Fatal("failed Watch exposed the concealed Acquisition")
	}
}

func TestWatchRevealsAcquisitionAndQueuesWebhook(t *testing.T) {
	e := setupUserRemoveEnv(t)
	member, err := e.users.Create(e.ctx, "Ana")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	movie, err := e.movies.Add(e.ctx, "Heat", "pool", member.ID)
	if err != nil {
		t.Fatalf("add pooled movie: %v", err)
	}
	drawnAt := time.Date(2026, 8, 7, 19, 30, 0, 123_000_000, time.UTC)
	if err := e.movies.StartDraw(e.ctx, movie.ID, drawnAt, drawnAt.Add(time.Minute), "drawer"); err != nil {
		t.Fatalf("StartDraw: %v", err)
	}
	if _, err := e.pool.Write.ExecContext(e.ctx, `
		INSERT INTO radarr_webhook_destinations (
		    name, kind, encrypted_url, reason_filters, enabled, verified_at
		) VALUES ('Discord', 'discord', ?, '["preset_required"]', 1, 100)
	`, []byte{1}); err != nil {
		t.Fatalf("insert webhook destination: %v", err)
	}

	watchedAt := drawnAt.Add(10 * time.Second)
	if _, _, err := e.movies.WatchCurrentDraw(e.ctx, watchedAt, true); err != nil {
		t.Fatalf("WatchCurrentDraw: %v", err)
	}
	var (
		revealedAt    sql.NullInt64
		actionVersion int
	)
	if err := e.pool.Read.QueryRowContext(e.ctx, `
		SELECT revealed_at, action_version
		FROM radarr_acquisitions
		WHERE movie_id = ?
	`, movie.ID).Scan(&revealedAt, &actionVersion); err != nil {
		t.Fatalf("read watched acquisition: %v", err)
	}
	if !revealedAt.Valid || revealedAt.Int64 != watchedAt.UnixMilli() || actionVersion != 1 {
		t.Fatalf("early Reveal = revealed %v version %d", revealedAt, actionVersion)
	}
	if got := e.countRow(t, `SELECT COUNT(*) FROM radarr_webhook_deliveries`); got != 1 {
		t.Fatalf("early Reveal queued %d deliveries, want 1", got)
	}
}
