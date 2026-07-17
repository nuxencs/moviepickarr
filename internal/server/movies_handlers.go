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

func (h *handler) getPooledMovies(ctx context.Context) ([]movieResponse, error) {
	movies, err := h.movieService.Pooled(ctx)
	if err != nil {
		return nil, err
	}
	return toAPIMoviesMeta(movies, h.metaFor(ctx, movies), h.creditsFor(ctx, movies)), nil
}

func (h *handler) advanceNextUp(ctx context.Context) error {
	next, changed, err := h.nextUpService.Advance(ctx)
	if err != nil {
		return err
	}
	if changed {
		h.broker.Broadcast(event{
			Type: "settings:next-up-changed",
			Data: map[string]any{
				"id":   next.ID,
				"name": next.Name,
			},
		})
	}
	return nil
}

func (h *handler) handleAddMovie(c *fiber.Ctx) error {
	userID, err := h.resolveUserID(c)
	if err != nil {
		return writeError(c, err)
	}

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
	if _, err := h.userService.Get(ctx, userID); err != nil {
		return writeError(c, err)
	}

	// Adds always land in the stash. Reaching the pool is a separate, explicit
	// promotion (the move endpoint), so "Add to <user>'s stash" does exactly that.
	movieRecord, err := h.movieService.AddToStash(ctx, title, userID)
	if err != nil {
		return writeError(c, err)
	}

	if err := h.movieService.SetExternalIDs(ctx, movieRecord.ID, tmdbID, imdbID); err != nil {
		// The movies_tmdb_id_unique index rejects a second row for the same
		// film. The stash row was already inserted above, so remove it before
		// reporting — otherwise every duplicate add leaves an orphan behind.
		_ = h.movieService.Delete(ctx, movieRecord.ID)
		if errors.Is(err, domain.ErrConflict) {
			return writeError(c, fmt.Errorf("%w: movie is already in the library", domain.ErrConflict))
		}
		return writeError(c, err)
	}
	movieRecord.TMDBID = tmdbID
	movieRecord.IMDbID = imdbID

	payload := toAPIMovie(movieRecord)
	h.broker.Broadcast(event{Type: "movie:added", Data: payload})

	if h.enrichRunner != nil {
		h.enrichRunner.Enqueue(movieRecord.ID) // fire-and-forget background enrichment
	}

	return c.Status(fiber.StatusCreated).JSON(payload)
}

func (h *handler) handleGetPool(c *fiber.Ctx) error {
	userID, err := h.resolveUserID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()
	if _, err := h.userService.Get(ctx, userID); err != nil {
		return writeError(c, err)
	}

	movies, err := h.movieService.PooledByUserID(ctx, userID)
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(toAPIMoviesMeta(movies, h.metaFor(ctx, movies), h.creditsFor(ctx, movies)))
}

func (h *handler) handleEditMovie(c *fiber.Ctx) error {
	userID, movieID, err := h.resolveUserAndMovieID(c)
	if err != nil {
		return writeError(c, err)
	}

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
	if movieRecord.AddedByID != userID {
		return writeError(c, domain.ErrNotFound)
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

	payload := toAPIMovie(updatedMovie)
	h.broker.Broadcast(event{Type: "movie:updated", Data: payload})

	return c.Status(fiber.StatusOK).JSON(payload)
}

func (h *handler) handleDeleteMovie(c *fiber.Ctx) error {
	userID, movieID, err := h.resolveUserAndMovieID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()
	movieRecord, err := h.movieService.Get(ctx, movieID)
	if err != nil {
		return writeError(c, err)
	}

	if movieRecord.AddedByID != userID {
		return writeError(c, domain.ErrNotFound)
	}
	if movieRecord.Status != "pool" && movieRecord.Status != "stash" {
		return writeError(c, domain.ErrInvalidState)
	}

	if err := h.movieService.Delete(ctx, movieID); err != nil {
		return writeError(c, err)
	}

	h.broker.Broadcast(event{
		Type: "movie:deleted",
		Data: fiber.Map{"userID": userID, "movieID": movieID},
	})

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) handleGetStash(c *fiber.Ctx) error {
	userID, err := h.resolveUserID(c)
	if err != nil {
		return writeError(c, err)
	}

	ctx := c.UserContext()
	if _, err := h.userService.Get(ctx, userID); err != nil {
		return writeError(c, err)
	}

	movies, err := h.movieService.StashedByUserID(ctx, userID)
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(toAPIMoviesMeta(movies, h.metaFor(ctx, movies), h.creditsFor(ctx, movies)))
}

func (h *handler) handleMove(c *fiber.Ctx) error {
	userID, movieID, err := h.resolveUserAndMovieID(c)
	if err != nil {
		return writeError(c, err)
	}

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

	poolLocked, err := h.settingsService.GetPoolLock(ctx)
	if err != nil {
		return writeError(c, err)
	}
	if poolLocked {
		return writeError(c, domain.ErrPoolLocked)
	}

	userRecord, err := h.userService.Get(ctx, userID)
	if err != nil {
		return writeError(c, err)
	}

	movieRecord, err := h.movieService.Get(ctx, movieID)
	if err != nil {
		return writeError(c, err)
	}

	if movieRecord.AddedByID != userID {
		return writeError(c, domain.ErrNotFound)
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

	updatedPool, err := h.movieService.PooledByUserID(ctx, userID)
	if err != nil {
		return writeError(c, err)
	}
	updatedStash, err := h.movieService.StashedByUserID(ctx, userID)
	if err != nil {
		return writeError(c, err)
	}

	combined := append(append([]*domain.Movie{}, updatedPool...), updatedStash...)
	updatedUser := toAPIUserMeta(userRecord, updatedPool, updatedStash, h.metaFor(ctx, combined), h.creditsFor(ctx, combined))
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
	movieResponse
	Candidates []movieResponse `json:"candidates"`
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

	selectedMovie, err := h.movieService.DrawRandom(ctx, sanitizeInput(body.ClientID))
	if err != nil {
		return writeError(c, err)
	}

	if err := h.advanceNextUp(ctx); err != nil {
		h.log.Error().Err(err).Msg("failed to advance next up")
	}

	payload := toAPIMovie(selectedMovie)
	// Carry the authoritative draw time so the clicker (whose own SSE event may
	// drop) and every other client resume the reveal spin from the same instant,
	// the reveal deadline the server will enforce (clients time the confirm
	// countdown off it), plus the drawer id so each client knows whether to
	// show the confirm button.
	if ap, ok := h.movieService.ActiveDraw(); ok && ap.MovieID == selectedMovie.ID {
		payload.DrawnAt = formatTime(&ap.DrawnAt)
		payload.RevealAt = formatTimePrecise(ap.RevealAt)
		payload.DrawClientID = ap.DrawClientID
	}

	// Reel candidates = the pre-draw pool (winner + the rest) as lean tiles WITH
	// posters, reconstructed from the post-draw pool plus the winner. The winner
	// must carry a poster here because the reel lands on it: toAPIMovie(selected)
	// alone has none (no metadata), so the tiles — not the bare payload — supply it.
	// Best-effort: the draw already succeeded, so a pool-load failure must not fail
	// the response — it just omits candidates and the client falls back to its
	// local pool cache (the pre-self-contained behaviour).
	drawn := drawnPayload{movieResponse: payload}
	if pooled, err := h.movieService.Pooled(ctx); err != nil {
		h.log.Warn().Err(err).Msg("failed to load pool for draw candidates (reel falls back to client pool cache)")
	} else {
		candidateMovies := append([]*domain.Movie{selectedMovie}, pooled...)
		drawn.Candidates = toAPIMoviesLean(candidateMovies, h.metaFor(ctx, candidateMovies))
	}

	// The auto-reveal is armed inside DrawRandom: if no client confirms by the
	// payload's revealAt, the movie service reveals the draw itself and the
	// OnRevealed hook broadcasts once for everyone.
	h.broker.Broadcast(event{Type: "movie:drawn", Data: drawn})

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
	resp := toAPIMovieMeta(movieRecord, meta[movieRecord.ID], credits[movieRecord.ID])
	// When this movie is the active draw, hand the client the timing it needs to
	// resume the reveal spin after a reload: when it was drawn, plus the server
	// clock now (so elapsed is computed server-relative, free of client skew).
	if ap, ok := h.movieService.ActiveDraw(); ok && ap.MovieID == movieRecord.ID {
		now := time.Now().UTC()
		resp.DrawnAt = formatTime(&ap.DrawnAt)
		resp.RevealAt = formatTimePrecise(ap.RevealAt)
		resp.ServerNow = formatTime(&now)
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
	h.movieService.RevealCurrentDraw()
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) handleWatchMovie(c *fiber.Ctx) error {
	watched, err := h.movieService.MarkCurrentAsWatched(c.UserContext())
	if err != nil {
		return writeError(c, err)
	}

	// MarkCurrentAsWatched cleared the draw and its pending auto-reveal.
	h.invalidateStatsCache()

	payload := toAPIMovie(watched)
	h.broker.Broadcast(event{Type: "movie:watched", Data: payload})

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
	return c.Status(fiber.StatusOK).JSON(toAPIMoviesLean(movies, h.metaFor(ctx, movies)))
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
	movieRecord, err := h.movieService.Get(ctx, movieID)
	if err != nil {
		return writeError(c, err)
	}

	one := []*domain.Movie{movieRecord}
	meta := h.metaFor(ctx, one)
	credits := h.creditsFor(ctx, one)
	return c.Status(fiber.StatusOK).JSON(toAPIMovieMeta(movieRecord, meta[movieRecord.ID], credits[movieRecord.ID]))
}
