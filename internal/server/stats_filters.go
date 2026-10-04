package server

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"moviepickarr/internal/domain"
)

// statsFilters narrows stats to a subset of the watched library. Zero values
// mean no filter. Cache key, matcher and echo all live here so the same key
// always means the same match set.
type statsFilters struct {
	Genre         string // case-insensitive genre name (display casing kept for the echo)
	ActorIDs      []int  // TMDB person ids, sorted+deduped; matched against cast credits
	CrewIDs       []int  // TMDB person ids, sorted+deduped; matched against crew credits
	ReleaseYear   int    // exact release year; mutually exclusive with ReleaseDecade
	ReleaseDecade int    // decade floor (1990 ⇒ [1990, 1999]); mutually exclusive with ReleaseYear
	AddedByIDs    []int  // user ids of the movie's adder, sorted+deduped (any-of)
}

func parseStatsFilters(genreRaw, actorsRaw, crewRaw, yearRaw, decadeRaw, addedByRaw string) (statsFilters, error) {
	// Clone: the raw value is fiber's zero-copy view of the request buffer,
	// but the genre echo outlives the handler inside the stats cache.
	filters := statsFilters{Genre: strings.Clone(strings.TrimSpace(genreRaw))}
	if len(filters.Genre) > statsMaxGenreLength {
		return statsFilters{}, fmt.Errorf("%w: genre exceeds %d characters", domain.ErrInvalidInput, statsMaxGenreLength)
	}

	var err error
	if filters.ActorIDs, err = parseIDList("actorIds", actorsRaw); err != nil {
		return statsFilters{}, err
	}
	if filters.CrewIDs, err = parseIDList("crewIds", crewRaw); err != nil {
		return statsFilters{}, err
	}
	if filters.AddedByIDs, err = parseIDList("addedByIds", addedByRaw); err != nil {
		return statsFilters{}, err
	}

	yearRaw = strings.TrimSpace(yearRaw)
	if yearRaw != "" {
		v, err := strconv.Atoi(yearRaw)
		if err != nil || v < statsMinReleaseYear || v > statsMaxReleaseYear {
			return statsFilters{}, fmt.Errorf("%w: invalid releaseYear %q (expected %d-%d)",
				domain.ErrInvalidInput, yearRaw, statsMinReleaseYear, statsMaxReleaseYear)
		}
		filters.ReleaseYear = v
	}

	// The UI offers a year or a decade, so reject both at once instead of guessing.
	decadeRaw = strings.TrimSpace(decadeRaw)
	if decadeRaw != "" {
		if filters.ReleaseYear != 0 {
			return statsFilters{}, fmt.Errorf("%w: releaseYear and decade are mutually exclusive", domain.ErrInvalidInput)
		}
		v, err := strconv.Atoi(decadeRaw)
		if err != nil || v%10 != 0 || v < statsMinReleaseYear || v > statsMaxReleaseYear {
			return statsFilters{}, fmt.Errorf("%w: invalid decade %q (expected a multiple of 10 in %d-%d)",
				domain.ErrInvalidInput, decadeRaw, statsMinReleaseYear, statsMaxReleaseYear)
		}
		filters.ReleaseDecade = v
	}

	return filters, nil
}

// genreFold is the only genre case-folding; key, matcher and echo must all use
// it, or a cache hit can return the wrong match set. ToLower, not EqualFold:
// they differ on runes such as U+0130.
func (f statsFilters) genreFold() string {
	return strings.ToLower(f.Genre)
}

// cacheKeySegment serializes the filters for the stats cache key. Equivalent
// selections serialize identically.
func (f statsFilters) cacheKeySegment() string {
	return fmt.Sprintf("%s|%s|%s|%d|%d|%s",
		f.genreFold(), joinIDs(f.ActorIDs), joinIDs(f.CrewIDs),
		f.ReleaseYear, f.ReleaseDecade, joinIDs(f.AddedByIDs))
}

// matches reports whether a watched movie passes the active filters.
// Unenriched movies fail any active filter, as guessing would skew the stats.
func (f statsFilters) matches(md *domain.MovieMetadata, credits []domain.MovieCredit) bool {
	if f.Genre != "" {
		want := f.genreFold()
		if md == nil || !slices.ContainsFunc(md.Genres, func(genre string) bool {
			return strings.ToLower(genre) == want
		}) {
			return false
		}
	}
	if f.ReleaseYear != 0 && releaseYearOf(md) != f.ReleaseYear {
		return false
	}
	if f.ReleaseDecade != 0 {
		if y := releaseYearOf(md); y < f.ReleaseDecade || y >= f.ReleaseDecade+10 {
			return false
		}
	}
	// Any-of within a list, AND across lists. Ingest already whitelists crew
	// jobs, so no job check here.
	if len(f.ActorIDs) > 0 && !creditsContainPerson(credits, domain.CreditKindCast, f.ActorIDs) {
		return false
	}
	if len(f.CrewIDs) > 0 && !creditsContainPerson(credits, domain.CreditKindCrew, f.CrewIDs) {
		return false
	}
	return true
}

// echo returns the active filters with display names and canonical genre
// casing, so the echo is identical across cache hits.
func (f statsFilters) echo(meta metaByID, credits creditsByID) statsFiltersEcho {
	out := statsFiltersEcho{
		Genre:         f.Genre,
		Actors:        resolveFilterPeople(f.ActorIDs, credits),
		Crew:          resolveFilterPeople(f.CrewIDs, credits),
		ReleaseYear:   f.ReleaseYear,
		ReleaseDecade: f.ReleaseDecade,
	}
	if out.Genre != "" {
		want := f.genreFold()
	genres:
		for _, md := range meta {
			if md == nil {
				continue
			}
			for _, genre := range md.Genres {
				if strings.ToLower(genre) == want {
					out.Genre = genre
					break genres
				}
			}
		}
	}
	return out
}

// parseIDList parses comma-separated positive ids, sorted and deduped so
// equivalent selections share one cache key. Empty input returns nil so the
// echo omits the field.
func parseIDList(param, raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	if len(parts) > statsMaxPeopleFilterIDs {
		return nil, fmt.Errorf("%w: %s exceeds %d ids", domain.ErrInvalidInput, param, statsMaxPeopleFilterIDs)
	}
	ids := make([]int, 0, len(parts))
	for _, part := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("%w: invalid %s %q (expected comma-separated positive integers)", domain.ErrInvalidInput, param, raw)
		}
		ids = append(ids, v)
	}

	slices.Sort(ids)
	return slices.Compact(ids), nil
}

// joinIDs serializes a canonical id list for the cache key; empty → "".
func joinIDs(ids []int) string {
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",")
}

// creditsContainPerson reports whether any credit of kind references one of
// the sorted ids.
func creditsContainPerson(credits []domain.MovieCredit, kind string, ids []int) bool {
	return slices.ContainsFunc(credits, func(c domain.MovieCredit) bool {
		if c.Kind != kind {
			return false
		}
		_, found := slices.BinarySearch(ids, c.Person.ID)
		return found
	})
}

// releaseYearOf returns the release year, or 0 when unknown.
func releaseYearOf(md *domain.MovieMetadata) int {
	if md == nil || len(md.ReleaseDate) < 4 {
		return 0
	}
	year, err := strconv.Atoi(md.ReleaseDate[:4])
	if err != nil {
		return 0
	}
	return year
}

// resolveFilterPeople maps filter ids to display names. Ids with no credit row
// keep an empty name; the client has its own labels for those.
func resolveFilterPeople(ids []int, credits creditsByID) []statsFilterPerson {
	if len(ids) == 0 {
		return nil
	}

	names := make(map[int]string, len(ids))
	// Stop once every id has a name; the credits map has thousands of rows.
	for _, movieCredits := range credits {
		if len(names) == len(ids) {
			break
		}
		for i := range movieCredits {
			if _, found := slices.BinarySearch(ids, movieCredits[i].Person.ID); found {
				names[movieCredits[i].Person.ID] = movieCredits[i].Person.Name
			}
		}
	}

	people := make([]statsFilterPerson, len(ids))
	for i, id := range ids {
		people[i] = statsFilterPerson{PersonID: id, Name: names[id]}
	}
	return people
}
