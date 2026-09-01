package repository

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"moviepickarr/internal/domain"
)

func TestWatchCurrentAndAdvanceNextUp_RotatesValidHolder(t *testing.T) {
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
			members := make([]*domain.User, 0, 3)
			for _, name := range []string{"Ana", "Ben", "Cai"} {
				member, err := e.users.Create(e.ctx, name)
				if err != nil {
					t.Fatalf("create member %q: %v", name, err)
				}
				members = append(members, member)
			}
			if err := e.nextUp.Set(e.ctx, members[tt.holder].ID); err != nil {
				t.Fatalf("set next up: %v", err)
			}
			if _, err := e.movies.Add(e.ctx, "Heat", "current", members[0].ID); err != nil {
				t.Fatalf("add current movie: %v", err)
			}
			if _, err := e.movies.Add(e.ctx, "Thief", "pool", members[0].ID); err != nil {
				t.Fatalf("add pooled movie: %v", err)
			}

			_, next, changed, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, time.Now().UTC())
			if err != nil {
				t.Fatalf("watch and rotate: %v", err)
			}
			if !changed || next == nil || next.ID != members[tt.wantHolder].ID {
				t.Fatalf(
					"handoff = changed=%v next=%+v, want member %d",
					changed,
					next,
					members[tt.wantHolder].ID,
				)
			}
		})
	}
}

func TestWatchCurrentAndAdvanceNextUp_SeedsThenRotatesFreshInstall(t *testing.T) {
	e := setupUserRemoveEnv(t)
	first, err := e.users.Create(e.ctx, "Ana")
	if err != nil {
		t.Fatalf("create first member: %v", err)
	}
	second, err := e.users.Create(e.ctx, "Ben")
	if err != nil {
		t.Fatalf("create second member: %v", err)
	}
	current, err := e.movies.Add(e.ctx, "Heat", "current", first.ID)
	if err != nil {
		t.Fatalf("add current movie: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Thief", "pool", first.ID); err != nil {
		t.Fatalf("add pooled movie: %v", err)
	}

	watchedAt := time.Date(2026, 7, 30, 20, 0, 0, 0, time.UTC)
	watched, next, changed, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, watchedAt)
	if err != nil {
		t.Fatalf("watch and rotate: %v", err)
	}
	if watched.ID != current.ID || watched.Status != "watched" || watched.WatchedAt == nil {
		t.Fatalf("watched movie = %+v, want movie %d watched", watched, current.ID)
	}
	if !changed || next == nil || next.ID != second.ID {
		t.Fatalf("handoff = changed=%v next=%+v, want member %d", changed, next, second.ID)
	}

	stored, err := e.nextUp.Get(e.ctx)
	if err != nil {
		t.Fatalf("get stored next up: %v", err)
	}
	if stored.ID != second.ID {
		t.Fatalf("stored next up = %d, want %d", stored.ID, second.ID)
	}
}

func TestWatchCurrentAndAdvanceNextUp_SkipsGuests(t *testing.T) {
	e := setupUserRemoveEnv(t)
	first, err := e.users.Create(e.ctx, "Ana")
	if err != nil {
		t.Fatal(err)
	}
	guest, err := e.users.Create(e.ctx, "Guest")
	if err != nil {
		t.Fatal(err)
	}
	last, err := e.users.Create(e.ctx, "Cai")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.users.SetRole(e.ctx, domain.RoleChange{MemberID: guest.ID, Role: domain.RoleGuest}); err != nil {
		t.Fatalf("set guest role: %v", err)
	}
	if err := e.nextUp.Set(e.ctx, first.ID); err != nil {
		t.Fatalf("set next up: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Heat", "current", first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.movies.Add(e.ctx, "Thief", "pool", first.ID); err != nil {
		t.Fatal(err)
	}

	_, next, changed, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !changed || next == nil || next.ID != last.ID {
		t.Fatalf("handoff = changed=%v next=%+v, want %d", changed, next, last.ID)
	}
}

func TestWatchCurrentAndAdvanceNextUp_HandsArchivedTurnToFirstActiveMember(t *testing.T) {
	e := setupUserRemoveEnv(t)
	departing, err := e.users.Create(e.ctx, "Departing")
	if err != nil {
		t.Fatalf("create departing member: %v", err)
	}
	firstActive, err := e.users.Create(e.ctx, "First active")
	if err != nil {
		t.Fatalf("create first active member: %v", err)
	}
	if _, err := e.users.Create(e.ctx, "Second active"); err != nil {
		t.Fatalf("create second active member: %v", err)
	}
	if err := e.nextUp.Set(e.ctx, departing.ID); err != nil {
		t.Fatalf("set departing member next up: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Heat", "current", departing.ID); err != nil {
		t.Fatalf("add current movie: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Thief", "pool", firstActive.ID); err != nil {
		t.Fatalf("add pooled movie: %v", err)
	}
	if outcome, err := e.users.Remove(e.ctx, departing.ID); err != nil || outcome != domain.OutcomeArchived {
		t.Fatalf("archive departing member: outcome=%q err=%v", outcome, err)
	}

	_, next, changed, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("watch and rotate: %v", err)
	}
	if !changed || next == nil || next.ID != firstActive.ID {
		t.Fatalf("handoff = changed=%v next=%+v, want first active member %d", changed, next, firstActive.ID)
	}
}

func TestWatchCurrentAndAdvanceNextUp_KeepsTurnWhenPoolIsEmpty(t *testing.T) {
	e := setupUserRemoveEnv(t)
	first, err := e.users.Create(e.ctx, "Ana")
	if err != nil {
		t.Fatalf("create first member: %v", err)
	}
	if _, err := e.users.Create(e.ctx, "Ben"); err != nil {
		t.Fatalf("create second member: %v", err)
	}
	if err := e.nextUp.Set(e.ctx, first.ID); err != nil {
		t.Fatalf("set next up: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Heat", "current", first.ID); err != nil {
		t.Fatalf("add current movie: %v", err)
	}

	_, next, changed, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("watch without handoff: %v", err)
	}
	if changed || next != nil {
		t.Fatalf("handoff = changed=%v next=%+v, want no change", changed, next)
	}

	stored, err := e.nextUp.Get(e.ctx)
	if err != nil {
		t.Fatalf("get stored next up: %v", err)
	}
	if stored.ID != first.ID {
		t.Fatalf("stored next up = %d, want %d", stored.ID, first.ID)
	}
}

func TestWatchCurrentAndAdvanceNextUp_KeepsTurnWithOneMember(t *testing.T) {
	e := setupUserRemoveEnv(t)
	only, err := e.users.Create(e.ctx, "Only")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := e.nextUp.Set(e.ctx, only.ID); err != nil {
		t.Fatalf("set next up: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Heat", "current", only.ID); err != nil {
		t.Fatalf("add current movie: %v", err)
	}
	if _, err := e.movies.Add(e.ctx, "Thief", "pool", only.ID); err != nil {
		t.Fatalf("add pooled movie: %v", err)
	}

	_, next, changed, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("watch without handoff: %v", err)
	}
	if changed || next != nil {
		t.Fatalf("handoff = changed=%v next=%+v, want no change", changed, next)
	}
}

func TestWatchCurrentAndAdvanceNextUp_RequiresCurrentMovie(t *testing.T) {
	e := setupUserRemoveEnv(t)

	_, _, _, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, time.Now().UTC())
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
	if err := e.movies.RevealDraw(e.ctx, movie.ID, revealedAtTime); err == nil {
		t.Fatal("RevealDraw succeeded despite webhook outbox failure")
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

	if err := e.movies.RevealDraw(e.ctx, movie.ID, revealedAtTime); err != nil {
		t.Fatalf("RevealDraw: %v", err)
	}
	if err := e.movies.RevealDraw(e.ctx, movie.ID, revealedAtTime.Add(time.Second)); err != nil {
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

	if _, _, _, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, drawnAt.Add(2*time.Second)); err == nil {
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
	if _, _, _, err := e.movies.WatchCurrentAndAdvanceNextUp(e.ctx, watchedAt); err != nil {
		t.Fatalf("WatchCurrentAndAdvanceNextUp: %v", err)
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
