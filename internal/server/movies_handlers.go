package server

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"moviepickarr/internal/domain"
	"moviepickarr/internal/integration"

	"github.com/gofiber/fiber/v2"
)

// metaFor batch-loads metadata. A failure logs and returns an empty map, so the
// response still renders.
func (h *handler) metaFor(c *fiber.Ctx, movies []*domain.Movie) metaByID {
	if h.movieMetadata == nil || len(movies) == 0 {
		return metaByID{}
	}
	ids := make([]int, len(movies))
	for i := range movies {
		ids[i] = movies[i].ID
	}
	meta, err := h.movieMetadata.GetMetadataByMovieIDs(c.UserContext(), ids)
	if err != nil {
		h.reqLog(c).Warn().Err(err).Int("count", len(ids)).
			Msg("loading movie metadata failed, responding without it")
		return metaByID{}
	}
	return meta
}

// creditsFor batch-loads credits, with metaFor's failure contract.
func (h *handler) creditsFor(c *fiber.Ctx, movies []*domain.Movie) creditsByID {
	if h.movieCredits == nil || len(movies) == 0 {
		return creditsByID{}
	}
	ids := make([]int, len(movies))
	for i := range movies {
		ids[i] = movies[i].ID
	}
	credits, err := h.movieCredits.GetCreditsByMovieIDs(c.UserContext(), ids)
	if err != nil {
		h.reqLog(c).Warn().Err(err).Int("count", len(ids)).
			Msg("loading movie credits failed, responding without them")
		return creditsByID{}
	}
	return credits
}

func (h *handler) getPooledMovies(c *fiber.Ctx) ([]leanMovieTile, error) {
	movies, err := h.movieService.Pooled(c.UserContext())
	if err != nil {
		return nil, err
	}
	return toLeanTiles(movies, h.metaFor(c, movies)), nil
}

// writeNotAdder is the 403 for a non-adder. No admin override, on purpose.
func writeNotAdder(c *fiber.Ctx) error {
	return writeProblem(c, fiber.StatusForbidden, "not_adder", "only the member who added this movie can change it")
}

func (h *handler) handleAddMovie(c *fiber.Ctx) error {
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

	// No link is stored; it is derived from the id.
	var tmdbID *int
	var imdbID *string
	if body.TMDBID != nil && *body.TMDBID > 0 {
		tmdbID = body.TMDBID
	} else if sanitizeInput(body.Link) != "" {
		target, err := parseMovieLink(body.Link)
		if err != nil {
			return writeError(c, err)
		}
		tmdbID = target.TMDBID
		imdbID = target.IMDbID
	}
	if title == "" || (tmdbID == nil && imdbID == nil) {
		return writeError(c, fmt.Errorf("%w: title and a tmdbId or movie link are required", domain.ErrInvalidInput))
	}

	ctx := c.UserContext()

	// Identity is in the same INSERT, so a duplicate fails before any row exists.
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

	return c.Status(fiber.StatusOK).JSON(toLeanTiles(movies, h.metaFor(c, movies)))
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
	link := sanitizeInput(body.Link)
	if title == "" || link == "" {
		return writeError(c, fmt.Errorf("%w: title and link are required", domain.ErrInvalidInput))
	}
	target, err := parseMovieLink(link)
	if err != nil {
		return writeError(c, err)
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
	updatedMovie, identityChanged, err := h.movieService.Edit(
		ctx,
		movieID,
		actorID,
		title,
		target,
		watchedAt,
	)
	if err != nil {
		if errors.Is(err, domain.ErrForbidden) {
			return writeNotAdder(c)
		}
		return writeError(c, err)
	}

	// Watched stats include the title, so any watched edit invalidates them.
	if updatedMovie.Status == string(domain.MovieStatusWatched) {
		h.invalidateStatsCache()
	}

	payload := toFullMovieBare(updatedMovie)
	h.broker.Broadcast(event{Type: "movie:updated", Data: payload})

	// Publish the committed edit before background work can publish its
	// movies:enriched-batch follow-up.
	if identityChanged && h.enrichRunner != nil {
		h.enrichRunner.EnqueueWithTrigger(movieID, integration.RunTriggerMovieUpdated)
	}

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

	// The service owns the state rules: refusing the lock here would treat the
	// held winner differently from its neighbors and reveal it.
	err = h.runPoolStateCommand(func() error {
		poolLocked, err := h.settingsService.GetPoolLock(ctx)
		if err != nil {
			return err
		}
		if err := h.movieService.Delete(ctx, movieID, poolLocked); err != nil {
			return err
		}

		h.broker.Broadcast(event{
			Type: "movie:deleted",
			Data: fiber.Map{"userID": actorID, "movieID": movieID},
		})
		return nil
	})
	if err != nil {
		return writeError(c, err)
	}

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

	return c.Status(fiber.StatusOK).JSON(toLeanTiles(movies, h.metaFor(c, movies)))
}

func (h *handler) handleMove(c *fiber.Ctx) error {
	movieID, err := resolveMovieID(c)
	if err != nil {
		return writeError(c, err)
	}
	actorID := actorMemberID(c)

	ctx := c.UserContext()

	// A named destination, not a toggle, so a duplicate click cannot reverse it.
	var body struct {
		Target string `json:"target"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeError(c, fmt.Errorf("%w: invalid request body", domain.ErrInvalidInput))
	}
	if body.Target != "pool" && body.Target != "stash" {
		return writeError(c, fmt.Errorf("%w: target must be \"pool\" or \"stash\"", domain.ErrInvalidInput))
	}
	if body.Target == "pool" {
		if ok, err := h.requireTurnParticipant(c); !ok {
			return err
		}
	}

	movieRecord, err := h.movieService.Get(ctx, movieID)
	if err != nil {
		return writeError(c, err)
	}
	if movieRecord.AddedByID != actorID {
		return writeNotAdder(c)
	}

	// The service moves atomically, so duplicates are no-ops and promotions
	// cannot overshoot the cap. Only a real move (changed) broadcasts. The payload
	// is ids only: clients refetch, so no fallible read runs inside the
	// pool-state lock after the commit.
	err = h.runPoolStateCommand(func() error {
		poolLocked, err := h.settingsService.GetPoolLock(ctx)
		if err != nil {
			return err
		}
		if poolLocked {
			return domain.ErrPoolLocked
		}

		var changed bool
		switch body.Target {
		case "pool":
			changed, err = h.movieService.MoveToPool(ctx, movieID)
		case "stash":
			changed, err = h.movieService.MoveToStash(ctx, movieID)
		}
		if err != nil {
			return err
		}

		if changed {
			h.broker.Broadcast(event{Type: "movie:moved", Data: fiber.Map{
				"userID":  actorID,
				"movieID": movieID,
			}})
		}
		return nil
	})
	if err != nil {
		return writeError(c, err)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) handleGetPooledMovies(c *fiber.Ctx) error {
	movies, err := h.getPooledMovies(c)
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(movies)
}

// drawnPayload carries the reel candidates, so every client spins the same reel
// without its own pool cache.
type drawnPayload struct {
	fullMovie
	Candidates []leanMovieTile `json:"candidates"`
}

func (h *handler) handleGetRandomMovie(c *fiber.Ctx) error {
	ctx := c.UserContext()

	// Optional: without a client id, every reel auto-reveals on its countdown.
	var body struct {
		ClientID string `json:"clientId"`
	}
	_ = c.BodyParser(&body)

	var drawn drawnPayload
	ran, err := h.runDrawCommand(c, func() error {
		drawResult, drawErr := h.movieService.DrawRandom(ctx, sanitizeInput(body.ClientID))
		if drawErr != nil {
			return drawErr
		}

		selectedMovie := drawResult.Movie
		activeDraw := drawResult.ActiveDraw
		published := false
		defer func() {
			if !published {
				// An early return or panic still announces the persisted draw
				// before its timer can reveal it.
				drawn.ServerNow = formatTimePrecise(time.Now().UTC())
				h.broker.Broadcast(event{Type: "movie:drawn", Data: drawn})
			}
			h.movieService.StartAutoReveal(activeDraw.MovieID, activeDraw.Generation)
		}()

		payload := toFullMovieBare(selectedMovie)
		payload.DrawnAt = formatTime(&activeDraw.DrawnAt)
		payload.RevealAt = formatTimePrecise(activeDraw.RevealAt)
		payload.DrawClientID = activeDraw.DrawClientID
		drawn = drawnPayload{fullMovie: payload}

		candidateMovies := drawResult.Candidates
		candidateMeta := h.metaFor(c, candidateMovies)
		drawn.Candidates = toLeanTiles(candidateMovies, candidateMeta)

		// Reveal needs the winner's backdrop; reuse the candidate metadata batch.
		payload = toFullMovie(selectedMovie, candidateMeta[selectedMovie.ID], nil)
		payload.DrawnAt = formatTime(&activeDraw.DrawnAt)
		payload.RevealAt = formatTimePrecise(activeDraw.RevealAt)
		payload.DrawClientID = activeDraw.DrawClientID
		drawn.fullMovie = payload

		// After candidate I/O, so revealAt - serverNow is the real remaining window.
		drawn.ServerNow = formatTimePrecise(time.Now().UTC())

		// Publish before arming the timer, so movie:drawn always precedes reveal.
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

	meta := h.metaFor(c, []*domain.Movie{movieRecord})
	credits := h.creditsFor(c, []*domain.Movie{movieRecord})
	resp := toFullMovie(movieRecord, meta[movieRecord.ID], credits[movieRecord.ID])
	// The active draw carries its timing, so a reload resumes the reveal spin.
	if ap, ok := h.movieService.ActiveDraw(); ok && ap.MovieID == movieRecord.ID {
		resp.DrawnAt = formatTime(&ap.DrawnAt)
		resp.RevealAt = formatTimePrecise(ap.RevealAt)
		// Precise, like revealAt, or the countdown bar jitters.
		resp.ServerNow = formatTimePrecise(time.Now().UTC())
		resp.DrawClientID = ap.DrawClientID
		resp.Revealed = ap.Revealed
	}
	return c.Status(fiber.StatusOK).JSON(resp)
}

// handleRevealCurrentMovie confirms the active draw. The movie service owns the
// reveal. Idempotent, so racing clients do not reveal twice.
func (h *handler) handleRevealCurrentMovie(c *fiber.Ctx) error {
	ran, err := h.runDrawCommand(c, func() error {
		_, _, revealErr := h.movieService.RevealCurrentDrawContext(c.UserContext())
		return revealErr
	})
	if !ran {
		return err
	}
	if err != nil {
		return writeError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *handler) handleWatchMovie(c *fiber.Ctx) error {
	ctx := c.UserContext()
	var payload fullMovie
	ran, err := h.runDrawCommand(c, func() error {
		watched, watchErr := h.movieService.MarkCurrentAsWatched(ctx)
		if watchErr != nil {
			if !errors.Is(watchErr, domain.ErrNoCurrentDraw) {
				h.reqLog(c).Error().
					Err(watchErr).
					Msg("watching the current movie failed")
			}
			return watchErr
		}

		// The turn passes on Reveal, not here; the service already revealed an
		// unrevealed draw.
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

type wildcardResponse struct {
	ID          int64     `json:"id"`
	HostMovieID int       `json:"hostMovieId"`
	SelectedAt  string    `json:"selectedAt"`
	Movie       fullMovie `json:"movie"`
}

func (h *handler) wildcardResponse(c *fiber.Ctx, wildcard *domain.Wildcard) wildcardResponse {
	meta := h.metaFor(c, []*domain.Movie{wildcard.Movie})
	credits := h.creditsFor(c, []*domain.Movie{wildcard.Movie})
	return wildcardResponse{
		ID:          wildcard.ID,
		HostMovieID: wildcard.HostMovieID,
		SelectedAt:  formatTimeValue(wildcard.SelectedAt),
		Movie:       toFullMovie(wildcard.Movie, meta[wildcard.Movie.ID], credits[wildcard.Movie.ID]),
	}
}

func (h *handler) handleGetActiveWildcard(c *fiber.Ctx) error {
	wildcard, err := h.movieService.ActiveWildcard(c.UserContext())
	if errors.Is(err, domain.ErrNoActiveWildcard) {
		return c.Status(fiber.StatusOK).JSON(nil)
	}
	if err != nil {
		return writeError(c, err)
	}
	return c.Status(fiber.StatusOK).JSON(h.wildcardResponse(c, wildcard))
}

func (h *handler) handleSelectWildcard(c *fiber.Ctx) error {
	if ok, err := h.requireTurnParticipant(c); !ok {
		return err
	}
	var body struct {
		HostMovieID int     `json:"hostMovieId"`
		MovieID     *int    `json:"movieId"`
		Title       string  `json:"title"`
		TMDBID      *int    `json:"tmdbId"`
		IMDbID      *string `json:"imdbId"`
	}
	if err := c.BodyParser(&body); err != nil {
		return writeError(c, fmt.Errorf("%w: invalid request body", domain.ErrInvalidInput))
	}
	if body.HostMovieID <= 0 {
		return writeError(c, fmt.Errorf("%w: hostMovieId is required", domain.ErrInvalidInput))
	}
	if body.MovieID != nil && (sanitizeInput(body.Title) != "" || body.TMDBID != nil || body.IMDbID != nil) {
		return writeError(c, fmt.Errorf("%w: movieId cannot be combined with movie identity fields", domain.ErrInvalidInput))
	}

	ctx := c.UserContext()
	var wildcard *domain.Wildcard
	err := h.runPoolStateCommand(func() error {
		poolLocked, err := h.settingsService.GetPoolLock(ctx)
		if err != nil {
			return err
		}
		wildcard, err = h.movieService.SelectWildcard(ctx, actorMemberID(c), domain.WildcardSelection{
			ExpectedHostMovieID: body.HostMovieID,
			ExistingMovieID:     body.MovieID,
			Title:               sanitizeInput(body.Title),
			TMDBID:              body.TMDBID,
			IMDbID:              body.IMDbID,
		}, poolLocked)
		return err
	})
	if err != nil {
		return writeError(c, err)
	}
	if h.enrichRunner != nil {
		h.enrichRunner.Enqueue(wildcard.Movie.ID)
	}
	payload := h.wildcardResponse(c, wildcard)
	h.broker.Broadcast(event{Type: "wildcard:selected", Data: payload})
	return c.Status(fiber.StatusCreated).JSON(payload)
}

func (h *handler) handleCancelWildcard(c *fiber.Ctx) error {
	if ok, err := h.requireTurnParticipant(c); !ok {
		return err
	}
	expectedWildcardID, err := strconv.ParseInt(c.Query("wildcardId"), 10, 64)
	if err != nil || expectedWildcardID <= 0 {
		return writeError(c, fmt.Errorf("%w: wildcardId is required", domain.ErrInvalidInput))
	}
	ctx := c.UserContext()
	var wildcard *domain.Wildcard
	err = h.runPoolStateCommand(func() error {
		var err error
		wildcard, err = h.movieService.CancelActiveWildcard(ctx, actorMemberID(c), expectedWildcardID)
		return err
	})
	if err != nil {
		return writeError(c, err)
	}
	payload := fiber.Map{"id": wildcard.ID, "movieId": wildcard.Movie.ID}
	h.broker.Broadcast(event{Type: "wildcard:canceled", Data: payload})
	return c.Status(fiber.StatusOK).JSON(payload)
}

func (h *handler) handleWatchWildcard(c *fiber.Ctx) error {
	if ok, err := h.requireTurnParticipant(c); !ok {
		return err
	}
	var body struct {
		WildcardID int64 `json:"wildcardId"`
	}
	if err := c.BodyParser(&body); err != nil || body.WildcardID <= 0 {
		return writeError(c, fmt.Errorf("%w: wildcardId is required", domain.ErrInvalidInput))
	}
	wildcard, err := h.movieService.MarkActiveWildcardWatched(c.UserContext(), body.WildcardID)
	if err != nil {
		return writeError(c, err)
	}
	h.invalidateStatsCache()
	payload := h.wildcardResponse(c, wildcard)
	h.broker.Broadcast(event{Type: "wildcard:watched", Data: payload})
	return c.Status(fiber.StatusOK).JSON(payload)
}

func (h *handler) handleGetWatchedMovies(c *fiber.Ctx) error {
	ctx := c.UserContext()
	movies, err := h.movieService.Watched(ctx)
	if err != nil {
		return writeError(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(toLeanTiles(movies, h.metaFor(c, movies)))
}

// handleGetMovie returns one full record for the detail modal.
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
	meta := h.metaFor(c, one)
	credits := h.creditsFor(c, one)
	return c.Status(fiber.StatusOK).JSON(toFullMovie(movieRecord, meta[movieRecord.ID], credits[movieRecord.ID]))
}
