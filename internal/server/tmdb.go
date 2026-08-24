package server

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type tmdbMovie struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	PosterPath  *string `json:"poster_path"`
	ReleaseDate string  `json:"release_date"`
	Overview    string  `json:"overview"`
}

type tmdbSearchResponse struct {
	Results []tmdbMovie `json:"results"`
}

type tmdbClient struct {
	apiKey      string
	http        *http.Client
	baseURL     string        // default "https://api.themoviedb.org/3"; overridable in tests
	limiter     *rateLimiter  // paces every enrichment request; nil-safe
	maxRetries  int           // retry attempts for the enrichment methods
	backoffBase time.Duration // base delay for exponential backoff
}

func (c *tmdbClient) Search(ctx context.Context, query string) ([]tmdbMovie, error) {
	u := fmt.Sprintf("%s/search/movie?query=%s", c.baseURL, url.QueryEscape(query))
	var payload tmdbSearchResponse
	if err := c.doRequestWithAdmission(ctx, u, &payload, true); err != nil {
		return nil, err
	}
	return payload.Results, nil
}

// --- Enrichment: reverse lookup + full details -----------------------------

var (
	errTMDBNotFound       = errors.New("tmdb: not found")
	errTMDBNotConfigured  = errors.New("tmdb: api key not configured")
	errTMDBAuthentication = errors.New("tmdb: API key rejected")
)

// tmdbRateLimitError is returned when retries are exhausted on HTTP 429.
type tmdbRateLimitError struct {
	RetryAfter time.Duration
}

func (e *tmdbRateLimitError) Error() string {
	return fmt.Sprintf("tmdb: rate limited, retry after %s", e.RetryAfter)
}

// tmdbTransientError is returned when retries are exhausted on a 5xx/network error.
type tmdbTransientError struct {
	StatusCode int
	Err        error
}

func (e *tmdbTransientError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("tmdb: transient error: %v", e.Err)
	}
	return fmt.Sprintf("tmdb: transient error: status %d", e.StatusCode)
}

func (e *tmdbTransientError) Unwrap() error { return e.Err }

type tmdbFindResponse struct {
	MovieResults []tmdbMovie `json:"movie_results"`
}

type tmdbGenre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type tmdbCastMember struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	ProfilePath *string `json:"profile_path"`
	Character   string  `json:"character"`
	Order       int     `json:"order"` // billing order
}

type tmdbCrewMember struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	ProfilePath *string `json:"profile_path"`
	Job         string  `json:"job"`
	Department  string  `json:"department"`
}

type tmdbCredits struct {
	Cast []tmdbCastMember `json:"cast"`
	Crew []tmdbCrewMember `json:"crew"`
}

type tmdbMovieDetails struct {
	ID           int         `json:"id"`
	IMDbID       string      `json:"imdb_id"`
	Overview     string      `json:"overview"`
	PosterPath   *string     `json:"poster_path"`
	BackdropPath *string     `json:"backdrop_path"`
	ReleaseDate  string      `json:"release_date"`
	Runtime      *int        `json:"runtime"` // null for some titles
	Genres       []tmdbGenre `json:"genres"`
	VoteAverage  float64     `json:"vote_average"`
	VoteCount    int         `json:"vote_count"`
	Tagline      string      `json:"tagline"`
	Credits      tmdbCredits `json:"credits"` // populated via append_to_response=credits
}

// FindByIMDb performs the TMDB reverse lookup: IMDb id -> TMDB movie.
func (c *tmdbClient) FindByIMDb(ctx context.Context, imdbID string) (tmdbMovie, error) {
	u := fmt.Sprintf("%s/find/%s?external_source=imdb_id", c.baseURL, url.PathEscape(imdbID))

	var payload tmdbFindResponse
	if err := c.doRequest(ctx, u, &payload); err != nil {
		return tmdbMovie{}, err
	}

	if len(payload.MovieResults) == 0 {
		return tmdbMovie{}, errTMDBNotFound
	}

	return payload.MovieResults[0], nil
}

// MovieDetails fetches the full detail record for a TMDB movie id, with the
// credits appended so it stays a single API call.
func (c *tmdbClient) MovieDetails(ctx context.Context, tmdbID int) (tmdbMovieDetails, error) {
	u := fmt.Sprintf("%s/movie/%d?append_to_response=credits", c.baseURL, tmdbID)

	var payload tmdbMovieDetails
	if err := c.doRequest(ctx, u, &payload); err != nil {
		return tmdbMovieDetails{}, err
	}

	return payload, nil
}

// DiscoverPopularPosters fetches one /discover/movie page of the most popular
// movies and returns their poster paths in popularity order, dropping any result
// with no poster. It is instance-independent (it never reads the pool) so a fresh
// instance still gets a full wall and no member's real pool leaks pre-auth. The
// pinned filters keep the wall safe and recognizable: popularity-sorted, a
// vote-count floor to cut noise, no adult titles, English metadata. It rides the
// shared doRequest path, so it reuses the retry/backoff/rate-limit machinery and
// returns errTMDBNotConfigured when no key is set.
func (c *tmdbClient) DiscoverPopularPosters(ctx context.Context) ([]string, error) {
	q := url.Values{}
	q.Set("sort_by", "popularity.desc")
	q.Set("vote_count.gte", "200")
	q.Set("include_adult", "false")
	q.Set("language", "en-US")
	u := fmt.Sprintf("%s/discover/movie?%s", c.baseURL, q.Encode())

	var payload tmdbSearchResponse
	if err := c.doRequest(ctx, u, &payload); err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(payload.Results))
	for _, m := range payload.Results {
		if m.PosterPath == nil || *m.PosterPath == "" {
			continue
		}
		paths = append(paths, *m.PosterPath)
	}
	return paths, nil
}

// doRequest is the shared GET helper for the enrichment endpoints. It paces
// every request through the rate limiter, maps status codes to the error
// taxonomy, honors 429 Retry-After, and retries 5xx/network errors with
// exponential backoff + jitter until maxRetries is reached or ctx is done.
func (c *tmdbClient) doRequest(ctx context.Context, requestURL string, out any) error {
	return c.doRequestWithAdmission(ctx, requestURL, out, false)
}

func (c *tmdbClient) doRequestWithAdmission(
	ctx context.Context,
	requestURL string,
	out any,
	rejectWhenFull bool,
) error {
	if c.apiKey == "" {
		return errTMDBNotConfigured
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if c.limiter != nil {
			var err error
			if rejectWhenFull {
				err = c.limiter.wait(ctx)
			} else {
				err = c.limiter.waitReserved(ctx)
			}
			if err != nil {
				return err
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := c.http.Do(req)
		if err != nil { // network / timeout / ctx cancellation
			lastErr = &tmdbTransientError{Err: err}
			if attempt < c.maxRetries && !sleepBackoff(ctx, attempt, c.backoffBase) {
				return ctx.Err()
			}
			continue
		}

		switch {
		case resp.StatusCode == http.StatusOK:
			err := json.UnmarshalRead(resp.Body, out)
			// Drain the trailing bytes before Close so the keep-alive connection
			// is pooled for the next back-to-back enrichment request rather than
			// torn down and re-handshaked.
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return err
		case resp.StatusCode == http.StatusNotFound:
			_ = resp.Body.Close()
			return errTMDBNotFound
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			_ = resp.Body.Close()
			return errTMDBAuthentication
		case resp.StatusCode == http.StatusTooManyRequests:
			d := parseRetryAfter(resp.Header.Get("Retry-After"), c.backoffBase)
			_ = resp.Body.Close()
			lastErr = &tmdbRateLimitError{RetryAfter: d}
			if attempt < c.maxRetries && !sleepFor(ctx, d) {
				return ctx.Err()
			}
			continue
		case resp.StatusCode >= 500:
			_ = resp.Body.Close()
			lastErr = &tmdbTransientError{StatusCode: resp.StatusCode}
			if attempt < c.maxRetries && !sleepBackoff(ctx, attempt, c.backoffBase) {
				return ctx.Err()
			}
			continue
		default: // other 4xx (401 bad key, etc.) -> permanent
			_ = resp.Body.Close()
			return fmt.Errorf("tmdb api request failed: status %d", resp.StatusCode)
		}
	}

	return lastErr
}

const maxTMDBRetryBackoff = 30 * time.Second

// sleepBackoff waits base*2^attempt plus up to base of jitter, with the complete
// delay capped at 30 seconds. A zero base retries immediately.
func sleepBackoff(ctx context.Context, attempt int, base time.Duration) bool {
	return sleepFor(ctx, retryBackoffDuration(attempt, base))
}

func retryBackoffDuration(attempt int, base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	if base >= maxTMDBRetryBackoff {
		return maxTMDBRetryBackoff
	}

	delay := base
	for range attempt {
		if delay >= maxTMDBRetryBackoff/2 {
			delay = maxTMDBRetryBackoff
			break
		}
		delay *= 2
	}
	if delay >= maxTMDBRetryBackoff {
		return maxTMDBRetryBackoff
	}

	jitterLimit := min(base, maxTMDBRetryBackoff-delay)
	return delay + time.Duration(rand.Int63n(int64(jitterLimit)+1))
}

// sleepFor blocks for d, returning false if ctx is cancelled first.
func sleepFor(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// parseRetryAfter interprets a Retry-After header (delta-seconds or HTTP-date),
// falling back to the given duration when absent or unparseable.
func parseRetryAfter(header string, fallback time.Duration) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return fallback
	}
	if secs, err := strconv.Atoi(header); err == nil {
		if secs < 0 {
			return fallback
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(header); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return fallback
}
