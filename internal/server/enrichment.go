package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"moviepickarr/internal/domain"
)

// tmdbAPI is the slice of the TMDB client the enrichment service depends on.
// *tmdbClient satisfies it; tests substitute a fake.
type tmdbAPI interface {
	FindByIMDb(ctx context.Context, imdbID string) (tmdbMovie, error)
	MovieDetails(ctx context.Context, tmdbID int) (tmdbMovieDetails, error)
}

var _ tmdbAPI = (*tmdbClient)(nil)

var (
	// ErrEnrichNoIMDbID means the movie's link had no extractable IMDb id.
	ErrEnrichNoIMDbID = errors.New("no imdb id in link")
	// ErrEnrichNotFound means TMDB had no match for the movie.
	ErrEnrichNotFound = errors.New("no tmdb match")
)

// Enricher enriches movies with TMDB data and reports which movies still need it.
type Enricher interface {
	EnrichOne(ctx context.Context, movieID int) (enrichResult, error)
	NeedsEnrichment(ctx context.Context, staleBefore time.Time, limit int) ([]domain.EnrichmentCandidate, error)
}

// enrichResult summarizes a successful enrichment, for logging.
type enrichResult struct {
	TMDBID int
	Genres int
}

type enrichmentService struct {
	movies domain.MovieRepo
	meta   domain.MovieMetadataRepo
	tmdb   tmdbAPI
}

func newEnrichmentService(movies domain.MovieRepo, meta domain.MovieMetadataRepo, tmdb tmdbAPI) *enrichmentService {
	return &enrichmentService{movies: movies, meta: meta, tmdb: tmdb}
}

var _ Enricher = (*enrichmentService)(nil)

// extractIMDbID pulls the tt-id out of a link, reusing the shared regex.
func extractIMDbID(link string) string {
	return imdbIDRegex.FindString(link)
}

func (s *enrichmentService) NeedsEnrichment(ctx context.Context, staleBefore time.Time, limit int) ([]domain.EnrichmentCandidate, error) {
	return s.meta.NeedsEnrichment(ctx, staleBefore, limit)
}

func (s *enrichmentService) EnrichOne(ctx context.Context, movieID int) (enrichResult, error) {
	m, err := s.movies.FindByID(ctx, movieID)
	if err != nil {
		return enrichResult{}, err // e.g. domain.ErrNotFound if the movie was deleted
	}

	// Prefer the TMDB id already on the movie (search adds, prior enrichment) so
	// we go straight to details. Otherwise reverse-look-up from the IMDb id.
	tmdbID, ok := movieTMDBID(m)
	if !ok {
		imdbID := movieIMDbID(m)
		if imdbID == "" {
			return enrichResult{}, fmt.Errorf("%w (movie %d %q)", ErrEnrichNoIMDbID, m.ID, m.Title)
		}
		found, err := s.tmdb.FindByIMDb(ctx, imdbID)
		if err != nil {
			if errors.Is(err, errTMDBNotFound) {
				return enrichResult{}, fmt.Errorf("%w (imdb %s)", ErrEnrichNotFound, imdbID)
			}
			return enrichResult{}, err // rate-limit / transient / not-configured bubble to the worker
		}
		tmdbID = found.ID
	}

	details, err := s.tmdb.MovieDetails(ctx, tmdbID)
	if err != nil {
		if errors.Is(err, errTMDBNotFound) {
			return enrichResult{}, fmt.Errorf("%w (tmdb %d)", ErrEnrichNotFound, tmdbID)
		}
		return enrichResult{}, err
	}

	// Persist the stable identity on the movie row (idempotent). Prefer the
	// authoritative imdb_id from details; fall back to what we already had.
	imdbID := details.IMDbID
	if imdbID == "" {
		imdbID = movieIMDbID(m)
	}
	var imdbPtr *string
	if imdbID != "" {
		imdbPtr = &imdbID
	}
	if err := s.movies.SetExternalIDs(ctx, movieID, &tmdbID, imdbPtr); err != nil {
		return enrichResult{}, err
	}

	if err := s.meta.UpsertMetadata(ctx, mapDetailsToMetadata(movieID, details)); err != nil {
		return enrichResult{}, err
	}

	return enrichResult{TMDBID: tmdbID, Genres: len(details.Genres)}, nil
}

// movieTMDBID returns the stored TMDB id, if any.
func movieTMDBID(m *domain.Movie) (int, bool) {
	if m.TMDBID != nil {
		return *m.TMDBID, true
	}
	return 0, false
}

// movieIMDbID returns the stored IMDb id, if any.
func movieIMDbID(m *domain.Movie) string {
	if m.IMDbID != nil && *m.IMDbID != "" {
		return *m.IMDbID
	}
	return ""
}

func mapDetailsToMetadata(movieID int, d tmdbMovieDetails) domain.MovieMetadata {
	genres := make([]string, 0, len(d.Genres))
	for _, g := range d.Genres {
		genres = append(genres, g.Name)
	}

	runtime := 0
	if d.Runtime != nil {
		runtime = *d.Runtime
	}

	return domain.MovieMetadata{
		MovieID:      movieID,
		Overview:     d.Overview,
		PosterPath:   d.PosterPath,
		BackdropPath: d.BackdropPath,
		ReleaseDate:  d.ReleaseDate,
		Runtime:      runtime,
		Genres:       genres,
		VoteAverage:  d.VoteAverage,
		VoteCount:    d.VoteCount,
		Tagline:      d.Tagline,
		// EnrichedAt is set by SQL (CURRENT_TIMESTAMP) on upsert.
	}
}
