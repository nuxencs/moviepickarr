package server

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
	TMDBID  int
	Genres  int
	Credits int
}

type enrichmentService struct {
	movies    domain.MovieRepo
	meta      domain.MovieMetadataRepo
	credits   domain.MovieCreditsRepo
	tmdb      tmdbAPI
	castLimit int // cast rows kept per movie (0 = full cast)
}

func newEnrichmentService(
	movies domain.MovieRepo,
	meta domain.MovieMetadataRepo,
	credits domain.MovieCreditsRepo,
	tmdb tmdbAPI,
	castLimit int,
) *enrichmentService {
	return &enrichmentService{movies: movies, meta: meta, credits: credits, tmdb: tmdb, castLimit: castLimit}
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

	// Credits go in after the metadata upsert — the credits_refreshed_at stamp
	// lives on the metadata row. Replace also when the mapped credits are
	// empty, so credit-less titles still get stamped and leave the backlog.
	credits := mapCredits(movieID, details.Credits, s.castLimit)
	if err := s.credits.ReplaceCredits(ctx, movieID, credits); err != nil {
		return enrichResult{}, err
	}

	return enrichResult{TMDBID: tmdbID, Genres: len(details.Genres), Credits: len(credits)}, nil
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

// crewJobWhitelist keeps only the crew roles the UI surfaces; everything else
// (lighting, sound, ...) is dropped at ingest.
var crewJobWhitelist = map[string]struct{}{
	"Director":                {},
	"Writer":                  {},
	"Screenplay":              {},
	"Original Music Composer": {},
	"Director of Photography": {},
}

// mapCredits trims TMDB credits to what we persist: the top castLimit cast
// members by billing order (0 = full cast; duplicate persons merged, their
// characters joined with " / ") plus whitelisted crew jobs, deduped per
// (person, job) so re-listed roles can't violate the movie_credits PK.
func mapCredits(movieID int, credits tmdbCredits, castLimit int) []domain.MovieCredit {
	cast := make([]tmdbCastMember, len(credits.Cast))
	copy(cast, credits.Cast)
	slices.SortStableFunc(cast, func(a, b tmdbCastMember) int { return a.Order - b.Order })

	out := make([]domain.MovieCredit, 0, len(cast)+len(credits.Crew))
	castIndex := make(map[int]int, len(cast)) // person id -> index into out
	for i := range cast {
		m := cast[i]
		if idx, ok := castIndex[m.ID]; ok {
			// Same person billed more than once (e.g. dual roles): merge the
			// characters into the existing row instead of duplicating it.
			switch {
			case m.Character == "":
			case out[idx].Character == "":
				out[idx].Character = m.Character
			default:
				out[idx].Character += " / " + m.Character
			}
			continue
		}
		castIndex[m.ID] = len(out)
		out = append(out, domain.MovieCredit{
			MovieID:   movieID,
			Person:    domain.Person{ID: m.ID, Name: m.Name, ProfilePath: m.ProfilePath},
			Kind:      domain.CreditKindCast,
			Character: m.Character,
			CastOrder: m.Order,
		})
	}
	// Trim after deduping, so a duplicate row never costs a billing slot.
	if castLimit > 0 && len(out) > castLimit {
		out = out[:castLimit:castLimit]
	}

	type crewKey struct {
		personID int
		job      string
	}
	seenCrew := make(map[crewKey]struct{}, len(credits.Crew))
	for i := range credits.Crew {
		m := credits.Crew[i]
		if _, ok := crewJobWhitelist[m.Job]; !ok {
			continue
		}
		key := crewKey{personID: m.ID, job: m.Job}
		if _, ok := seenCrew[key]; ok {
			continue
		}
		seenCrew[key] = struct{}{}
		out = append(out, domain.MovieCredit{
			MovieID:    movieID,
			Person:     domain.Person{ID: m.ID, Name: m.Name, ProfilePath: m.ProfilePath},
			Kind:       domain.CreditKindCrew,
			Job:        m.Job,
			Department: m.Department,
		})
	}

	return out
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
