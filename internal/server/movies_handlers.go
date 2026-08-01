package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

// metaFor batch-loads enriched metadata for the given movies. A lookup failure
// is non-fatal — it logs and returns an empty map so responses still render
// (without enriched fields) rather than failing the whole request.
func (h *handler) metaFor(ctx context.Context, movies []*domain.Movie) metaByID {
	if h.movieMetadata == nil || len(movies) == 0 {
		return metaByID{}
	}
	ids := make([]int, len(movies))
	for i := range movies {
		ids[i] = movies[i].ID
	}
	meta, err := h.movieMetadata.GetMetadataByMovieIDs(ctx, ids)
	if err != nil {
		h.log.Warn().Err(err).Msg("failed to load movie metadata (using empty)")
		return metaByID{}
	}
	return meta
}

// creditsFor batch-loads ingested credits for the given movies. Same contract
// as metaFor: a lookup failure is non-fatal — it logs and returns an empty map
// so responses still render (without cast/crew) rather than failing the whole
// request.
func (h *handler) creditsFor(ctx context.Context, movies []*domain.Movie) creditsByID {
	if h.movieCredits == nil || len(movies) == 0 {
		return creditsByID{}
	}
	ids := make([]int, len(movies))
	for i := range movies {
		ids[i] = movies[i].ID
	}
	credits, err := h.movieCredits.GetCreditsByMovieIDs(ctx, ids)
	if err != nil {
		h.log.Warn().Err(err).Msg("failed to load movie credits (using empty)")
		return creditsByID{}
	}
	return credits
}

func (h *handler) getPooledMovies(ctx context.Context) ([]fullMovie, error) {
	movies, err := h.movieService.Pooled(ctx)
	if err != nil {
		return nil, err
	}
	return toFullMovies(movies, h.metaFor(ctx, movies), h.creditsFor(ctx, movies)), nil
}

// writeNotAdder is the uniform 403 for a member trying to change a movie they
// did not add. There is deliberately no admin override: edits, deletes and moves
// are the adder's alone.
func writeNotAdder(c *fiber.Ctx) error {
	return writeProblem(c, fiber.StatusForbidden, "not_adder", "only the member who added this movie can change it")
}

func (h *handler) handleAddMovie(c *fiber.Ctx) error {
	// The adder is the session member, never a path id: no target user to spoof.
	actorID := actorMemberID(c)

	var body struct {
		Title  string `json:"title"`
		Link   string `json:"link"`
		TMDBID *int   `json:"tmdbId"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeError(c, fmt.Errorf("%w: invalid request body", domain.ErrInvalidInput))
	}

	title := sanitizeInput(body.Title)

	// Identity-first: a search add carries the TMDB id; a manual add carries an
	// IMDb link we extract the id from. No link is stored — it's derived.
	var tmdbID *int
	var imdbID *string
	if body.TMDBID != nil && *body.TMDBID > 0 {
		tmdbID = body.TMDBID
	} else if id := extractIMDbID(sanitizeLink(body.Link)); id != "" {
		imdbID = &id
	}
	if title == "" || (tmdbID == nil && imdbID == nil) {
		return writeError(c, fmt.Errorf("%w: title and a tmdbId or imdb link are required", domain.ErrInvalidInput))
	}

	ctx := c.UserContext()

	// Adds always land in the stash. Reaching the pool is a separate, explicit
	// promotion (the move endpoint), so "Add to your stash" does exactly that.
	// Identity lands in the same INSERT, so a duplicate fails before any row
	// exists instead of relying on a second write and best-effort cleanup.
	movieRecord, err := h.movieService.AddToStash(ctx, title, actorID, tmdbID, imdbID)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return writeError(c, fmt.Errorf("%w: movie is already in the library", domain.ErrConflict))
		}
		return writeError(c, err)
	}

	payload := toFullMovieBare(movieRecord)
	h.broker.Broadcast(event{Type: "movie:added", Data: payload})

	if h.enrichRunner != nil {
		h.enrichRunner.Enqueue(movieRecord.ID) // fire-and-forget background enrichment
	}

	return c.Status(fiber.StatusCreated).JSON(payload)
}

func (h *handler) handleGetPool(c *fiber.Ctx) error {
	memberID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()
	if _, err := h.userService.Get(ctx, memberID); err != nil {
		return writeError(c, err)
	}

	movies, err := h.movieService.PooledByUserID(ctx, memberID)
	if err != nil {
		return writeError(c, err)
	}

	// A Members board path: tile-level data only, so ship lean tiles and skip
	// the credits batch-load (the modal lazy-loads its full record instead).
	return c.Status(fiber.StatusOK).JSON(toLeanTiles(movies, h.metaFor(ctx, movies)))
}

func (h *handler) handleEditMovie(c *fiber.Ctx) error {
	movieID, err := resolveMovieID(c)
	if err != nil {
		return writeError(c, err)
	}
	actorID := actorMemberID(c)

	var body struct {
		Title     string  `json:"title"`
		Link      string  `json:"link"`
		WatchedAt *string `json:"watchedAt"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeError(c, fmt.Errorf("%w: invalid request body", domain.ErrInvalidInput))
	}

	title := sanitizeInput(body.Title)
	link := sanitizeLink(body.Link)
	if title == "" || link == "" {
		return writeError(c, fmt.Errorf("%w: title and link are required", domain.ErrInvalidInput))
	}

	var watchedAt *time.Time
	if body.WatchedAt != nil {
		raw := sanitizeInput(*body.WatchedAt)
		if raw == "" {
			return writeError(c, fmt.Errorf("%w: watchedAt must be a valid RFC3339 timestamp", domain.ErrInvalidInput))
		}

		parsed, err := time.Parse(timeFormat, raw)
		if err != nil {
			return writeError(c, fmt.Errorf("%w: watchedAt must be a valid RFC3339 timestamp", domain.ErrInvalidInput))
		}

		parsedUTC := parsed.UTC()
		watchedAt = &parsedUTC
	}

	ctx := c.UserContext()
	movieRecord, err := h.movieService.Get(ctx, movieID)
	if err != nil {
		return writeError(c, err)
	}
	if movieRecord.AddedByID != actorID {
		return writeNotAdder(c)
	}

	updatedMovie, err := h.movieService.Update(ctx, movieID, title, watchedAt)
	if err != nil {
		return writeError(c, err)
	}

	if watchedAt != nil {
		h.invalidateStatsCache()
	}

	// If the link's IMDb identity changed, reset the ids (forcing a fresh
	// reverse lookup) and re-enrich. Comparing extracted ids — not raw link
	// strings — avoids re-enriching when an unchanged derived link is resubmitted.
	newIMDb := extractIMDbID(link)
	curIMDb := ""
	if movieRecord.IMDbID != nil {
		curIMDb = *movieRecord.IMDbID
	}
	if newIMDb != curIMDb {
		var imdbPtr *string
		if newIMDb != "" {
			imdbPtr = &newIMDb
		}
		if err := h.movieService.SetExternalIDs(ctx, movieID, nil, imdbPtr); err != nil {
			return writeError(c, err)
		}
		// The enqueue below is in-memory; clearing the credits marker makes the
		// periodic drain a reliable backstop if it is lost (queue full or
		// restart) — otherwise the movie would keep serving and tallying the
		// previous film's metadata and credits until the refresh TTL.
		if err := h.movieMetadata.MarkEnrichmentStale(ctx, movieID); err != nil {
			return writeError(c, err)
		}
		updatedMovie.TMDBID = nil
		updatedMovie.IMDbID = imdbPtr
		if h.enrichRunner != nil {
			h.enrichRunner.Enqueue(movieID)
		}
	}

	payload := toFullMovieBare(updatedMovie)
	h.broker.Broadcast(event{Type: "movie:updated", Data: payload})

	return c.Status(fiber.StatusOK).JSON(payload)
}

func (h *handler) handleDeleteMovie(c *fiber.Ctx) error {
	movieID, err := resolveMovieID(c)
	if err != nil {
		return writeError(c, err)
	}
	actorID := actorMemberID(c)

	ctx := c.UserContext()
	movieRecord, err := h.movieService.Get(ctx, movieID)
	if err != nil {
		return writeError(c, err)
	}

	if movieRecord.AddedByID != actorID {
		return writeNotAdder(c)
	}

	poolLocked, err := h.settingsService.GetPoolLock(ctx)
	if err != nil {
		return writeError(c, err)
	}

	// The state rules (deletable statuses, the freeze while a draw is unrevealed,
	// and which of the two refusals the lock yields to) live in the service,
	// which owns the active draw. Refusing the lock here instead would answer
	// differently for the held winner than for the tile beside it, which is
	// exactly the tell the freeze exists to remove.
	if err := h.movieService.Delete(ctx, movieID, poolLocked); err != nil {
		return writeError(c, err)
	}

	h.broker.Broadcast(event{
		Type: "movie:deleted",
		Data: fiber.Map{"userID": actorID, "movieID": movieID},
	})

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) handleGetStash(c *fiber.Ctx) error {
	memberID, err := resolveMemberID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()
	if _, err := h.userService.Get(ctx, memberID); err != nil {
		return writeError(c, err)
	}

	movies, err := h.movieService.StashedByUserID(ctx, memberID)
	if err != nil {
		return writeError(c, err)
	}

	// A Members board path: tile-level data only, so ship lean tiles and skip
	// the credits batch-load (the modal lazy-loads its full record instead).
	return c.Status(fiber.StatusOK).JSON(toLeanTiles(movies, h.metaFor(ctx, movies)))
}

func (h *handler) handleMove(c *fiber.Ctx) error {
	movieID, err := resolveMovieID(c)
	if err != nil {
		return writeError(c, err)
	}
	actorID := actorMemberID(c)

	ctx := c.UserContext()

	// The client names the destination ("pool" or "stash") instead of letting the
	// server toggle on live status — a directional move is idempotent, so a
	// duplicate click can only re-confirm the target, never reverse it.
	var body struct {
		Target string `json:"target"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeError(c, fmt.Errorf("%w: invalid request body", domain.ErrInvalidInput))
	}
	if body.Target != "pool" && body.Target != "stash" {
		return writeError(c, fmt.Errorf("%w: target must be \"pool\" or \"stash\"", domain.ErrInvalidInput))
	}

	// Authorize before touching state: only the movie's adder may move it, with no
	// admin override.
	movieRecord, err := h.movieService.Get(ctx, movieID)
	if err != nil {
		return writeError(c, err)
	}
	if movieRecord.AddedByID != actorID {
		return writeNotAdder(c)
	}

	poolLocked, err := h.settingsService.GetPoolLock(ctx)
	if err != nil {
		return writeError(c, err)
	}
	if poolLocked {
		return writeError(c, domain.ErrPoolLocked)
	}

	userRecord, err := h.userService.Get(ctx, actorID)
	if err != nil {
		return writeError(c, err)
	}

	// The transition is enforced atomically in the service: it moves the movie
	// only if it sits at the source (and, for a promotion, only if the owner's
	// pool has room), so a duplicate click is an idempotent no-op and concurrent
	// promotions can't overshoot the cap. `changed` reports whether a real move
	// happened, so a no-op duplicate doesn't broadcast.
	var changed bool
	switch body.Target {
	case "pool":
		changed, err = h.movieService.MoveToPool(ctx, movieID)
	case "stash":
		changed, err = h.movieService.MoveToStash(ctx, movieID)
	}
	if err != nil {
		return writeError(c, err)
	}

	updatedPool, err := h.movieService.PooledByUserID(ctx, actorID)
	if err != nil {
		return writeError(c, err)
	}
	updatedStash, err := h.movieService.StashedByUserID(ctx, actorID)
	if err != nil {
		return writeError(c, err)
	}

	combined := append(append([]*domain.Movie{}, updatedPool...), updatedStash...)
	updatedUser := toAPIUserMeta(userRecord, updatedPool, updatedStash, h.metaFor(ctx, combined))
	if changed {
		h.broker.Broadcast(event{Type: "movie:moved", Data: updatedUser})
	}

	return c.Status(fiber.StatusOK).JSON(updatedUser)
}

func (h *handler) handleGetPooledMovies(c *fiber.Ctx) error {
	movies, err := h.getPooledMovies(c.UserContext())
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(movies)
}

// drawnPayload is the movie:drawn wire shape: the winning movie plus the reel
// candidates (the pre-draw pool as lean tiles, winner included). Carrying the
// candidates makes the reel self-contained — every client renders the full spin
// regardless of whether it has the pool cached — and decouples the reel from each
// client's local pool snapshot.
type drawnPayload struct {
	fullMovie
	Candidates []leanMovieTile `json:"candidates"`
}

func (h *handler) handleGetRandomMovie(c *fiber.Ctx) error {
	ctx := c.UserContext()

	// The client identifies itself so only the drawer sees the reel's confirm
	// button. Optional + best-effort: a malformed/absent body just means no drawer
	// (every client's reel then auto-reveals on its countdown).
	var body struct {
		ClientID string `json:"clientId"`
	}
	_ = c.BodyParser(&body)

	var drawn drawnPayload
	ran, err := h.runMovieNightCommand(c, func() error {
		selectedMovie, drawErr := h.movieService.DrawRandom(ctx, sanitizeInput(body.ClientID))
		if drawErr != nil {
			return drawErr
		}

		activeDraw, ok := h.movieService.ActiveDraw()
		if !ok || activeDraw.MovieID != selectedMovie.ID {
			return domain.ErrInvalidState
		}
		payload := toFullMovieBare(selectedMovie)
		// Carry the authoritative draw time so the clicker (whose own SSE event may
		// drop) and every other client resume the reveal spin from the same instant,
		// the reveal deadline the server will enforce (clients time the confirm
		// countdown off it), plus the drawer id so each client knows whether to
		// show the confirm button.
		payload.DrawnAt = formatTime(&activeDraw.DrawnAt)
		payload.RevealAt = formatTimePrecise(activeDraw.RevealAt)
		payload.DrawClientID = activeDraw.DrawClientID
		drawn = drawnPayload{fullMovie: payload}

		published := false
		defer func() {
			if !published {
				// A future early return or panic still announces the persisted
				// draw before its timer can reveal it. The full reel ceremony
				// may be unavailable, but the lifecycle keeps progressing.
				drawn.ServerNow = formatTimePrecise(time.Now().UTC())
				h.broker.Broadcast(event{Type: "movie:drawn", Data: drawn})
			}
			h.movieService.StartAutoReveal(selectedMovie.ID, activeDraw.Generation)
		}()

		// Reel candidates = the pre-draw pool (winner + the rest) as lean tiles WITH
		// posters. That IS the pool view during an unrevealed draw: the service holds
		// the winner in it until the reveal, so no reconstruction is needed here. The
		// winner must carry a poster because the reel lands on it: toFullMovieBare(selected)
		// alone has none (no metadata), so the tiles — not the bare payload — supply it.
		// Best-effort: the draw already succeeded, so a pool-load failure must not fail
		// the response — it just omits candidates and the client falls back to its
		// local pool cache (the pre-self-contained behaviour).
		if candidateMovies, poolErr := h.movieService.Pooled(ctx); poolErr != nil {
			h.log.Warn().Err(poolErr).Msg("failed to load pool for draw candidates (reel falls back to client pool cache)")
		} else {
			drawn.Candidates = toLeanTiles(candidateMovies, h.metaFor(ctx, candidateMovies))
		}

		// Stamp the server clock after candidate I/O, immediately before
		// publication, so revealAt - serverNow is the actual remaining window.
		drawn.ServerNow = formatTimePrecise(time.Now().UTC())

		// Publish before arming the timer. If candidate construction consumed the
		// whole reveal window, StartAutoReveal schedules an immediate callback,
		// but movie:drawn has already entered the broker first.
		h.broker.Broadcast(event{Type: "movie:drawn", Data: drawn})
		published = true
		return nil
	})
	if !ran {
		return err
	}
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(drawn)
}

func (h *handler) handleGetCurrentMovie(c *fiber.Ctx) error {
	ctx := c.UserContext()
	movieRecord, err := h.movieService.Current(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.Status(fiber.StatusOK).JSON(nil)
		}
		return writeError(c, err)
	}

	meta := h.metaFor(ctx, []*domain.Movie{movieRecord})
	credits := h.creditsFor(ctx, []*domain.Movie{movieRecord})
	resp := toFullMovie(movieRecord, meta[movieRecord.ID], credits[movieRecord.ID])
	// When this movie is the active draw, hand the client the timing it needs to
	// resume the reveal spin after a reload: when it was drawn, plus the server
	// clock now (so elapsed is computed server-relative, free of client skew).
	if ap, ok := h.movieService.ActiveDraw(); ok && ap.MovieID == movieRecord.ID {
		resp.DrawnAt = formatTime(&ap.DrawnAt)
		resp.RevealAt = formatTimePrecise(ap.RevealAt)
		// Precise, like revealAt: the confirm countdown is revealAt − serverNow,
		// so truncating this to the second would jitter the bar by up to a second.
		resp.ServerNow = formatTimePrecise(time.Now().UTC())
		resp.DrawClientID = ap.DrawClientID
		resp.Revealed = ap.Revealed
	}
	return c.Status(fiber.StatusOK).JSON(resp)
}

// handleRevealCurrentMovie confirms the active draw — the drawer pressed the
// reel's OK button. The movie service owns the whole flip: reveal-once, the
// timer cancel, and the movie:revealed broadcast (via its OnRevealed hook).
// Idempotent: a second confirm (or a confirm with no active draw) is a quiet
// no-op, so racing clients don't double-fire the reveal.
func (h *handler) handleRevealCurrentMovie(c *fiber.Ctx) error {
	ran, err := h.runMovieNightCommand(c, func() error {
		h.movieService.RevealCurrentDraw()
		return nil
	})
	if !ran {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) handleWatchMovie(c *fiber.Ctx) error {
	ctx := c.UserContext()
	var payload fullMovie
	ran, err := h.runMovieNightCommand(c, func() error {
		watched, next, changed, watchErr := h.movieService.MarkCurrentAsWatchedAndAdvanceNextUp(ctx)
		if watchErr != nil {
			if !errors.Is(watchErr, domain.ErrNoCurrentDraw) {
				h.log.Error().
					Err(watchErr).
					Msg("watch current movie and advance next up failed")
			}
			return watchErr
		}

		// Rotation-on-watch (Model B): the turn passes only once the movie is
		// actually watched, so the same member holds it across the whole draw →
		// reveal → watch cycle. The service commits the watched row and handoff
		// together; publish only after both are durable.
		if changed {
			h.broker.Broadcast(event{
				Type: "settings:next-up-changed",
				Data: map[string]any{
					"id":   next.ID,
					"name": next.Name,
				},
			})
		}

		// The service revealed an unrevealed reel after commit, then cleared the
		// draw and its pending auto-reveal.
		h.invalidateStatsCache()

		payload = toFullMovieBare(watched)
		h.broker.Broadcast(event{Type: "movie:watched", Data: payload})
		return nil
	})
	if !ran {
		return err
	}
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(payload)
}

func (h *handler) handleGetWatchedMovies(c *fiber.Ctx) error {
	ctx := c.UserContext()
	movies, err := h.movieService.Watched(ctx)
	if err != nil {
		return writeError(c, err)
	}

	// Lean payload: the grid tiles render only poster/title/rating/adder, so
	// this drops the per-movie cast/crew (and backdrop/tagline/overview) that
	// made up the bulk of the bytes. The detail modal lazy-loads the full record
	// from GET /movies/:id; Stats reads its actor/crew filter options from
	// GET /movies/filter-options. Credits are no longer loaded here at all.
	return c.Status(fiber.StatusOK).JSON(toLeanTiles(movies, h.metaFor(ctx, movies)))
}

// handleGetMovie returns the full enriched record for one movie — backdrop,
// tagline, overview and cast/crew — for the detail modal, which lazy-loads it on
// open rather than carrying credits in every list payload.
func (h *handler) handleGetMovie(c *fiber.Ctx) error {
	movieID, ok := parseInt(c.Params("movieID"))
	if !ok {
		return writeError(c, fmt.Errorf("%w: movieID path parameter is required", domain.ErrInvalidInput))
	}

	ctx := c.UserContext()
	movieRecord, err := h.movieService.GetForDisplay(ctx, movieID)
	if err != nil {
		return writeError(c, err)
	}

	one := []*domain.Movie{movieRecord}
	meta := h.metaFor(ctx, one)
	credits := h.creditsFor(ctx, one)
	return c.Status(fiber.StatusOK).JSON(toFullMovie(movieRecord, meta[movieRecord.ID], credits[movieRecord.ID]))
}
