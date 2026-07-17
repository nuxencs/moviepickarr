package server

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"moviepickarr/internal/domain"
)

// statsFilters narrows the stats computation to a subset of the watched
// library. Zero values mean "no filter". The people lists are any-of within a
// list and AND-ed across lists (and with genre/year).
//
// This value is the whole filter module: the cache-key segment, the matcher,
// and the echo are all methods on it, and genre case-folding goes through the
// single genreFold() below — so "same cache key" ⇒ "same match set" holds by
// construction instead of by three call sites agreeing.
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

	// A decade selection ("1990s") is the alternative to an exact year — the UI
	// offers one or the other, so reject both at once rather than guess.
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

// genreFold is THE genre case-folding — the cache key, the matcher, and the
// echo all fold through here, never inline. ToLower (not EqualFold): the two
// relations differ on exotic runes (U+0130 "İ" lowercases to "i" but has no
// simple case folding), and a fold drift between the key and the matcher
// would let a cache hit return the wrong match set.
func (f statsFilters) genreFold() string {
	return strings.ToLower(f.Genre)
}

// cacheKeySegment serializes the filters for the stats cache key. Equivalent
// selections serialize identically: the genre is folded and the id lists are
// already sorted+deduped at parse time.
func (f statsFilters) cacheKeySegment() string {
	return fmt.Sprintf("%s|%s|%s|%d|%d|%s",
		f.genreFold(), joinIDs(f.ActorIDs), joinIDs(f.CrewIDs),
		f.ReleaseYear, f.ReleaseDecade, joinIDs(f.AddedByIDs))
}

// matches reports whether a watched movie passes the active filters.
// Unenriched movies (no metadata/credits) fail any active filter — their
// genre/year/people are unknown, and guessing would skew the stats.
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
	// People filters are any-of within a list, AND-ed across lists. Crew rows
	// are already whitelisted to a handful of jobs at ingest, so crewIds need
	// no job check here.
	if len(f.ActorIDs) > 0 && !creditsContainPerson(credits, domain.CreditKindCast, f.ActorIDs) {
		return false
	}
	if len(f.CrewIDs) > 0 && !creditsContainPerson(credits, domain.CreditKindCrew, f.CrewIDs) {
		return false
	}
	return true
}

// echo returns the active filters for the response, resolving each person
// filter to a display name from any credit row that references them and the
// genre to its stored canonical casing (matching is case-insensitive, so
// canonicalizing keeps the echo identical across cache hits).
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

// parseIDList parses a comma-separated list of positive TMDB person ids into
// the canonical sorted-and-deduped form — "6384,530" and "530,6384,530" both
// yield [530 6384], so equivalent selections share one cache key. Empty input
// returns nil (never an empty slice) so the filters echo can omit the field.
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

// creditsContainPerson reports whether any credit of the given kind references
// one of the (sorted) person ids.
func creditsContainPerson(credits []domain.MovieCredit, kind string, ids []int) bool {
	return slices.ContainsFunc(credits, func(c domain.MovieCredit) bool {
		if c.Kind != kind {
			return false
		}
		_, found := slices.BinarySearch(ids, c.Person.ID)
		return found
	})
}

// releaseYearOf extracts the year from the metadata's "YYYY-MM-DD" release
// date, or 0 when the metadata or date is absent/unparseable.
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

// resolveFilterPeople maps filter ids (already sorted, so the echo order is
// deterministic across cache hits) to display names from any credit row that
// references them. Ids with no credit row keep an empty name — the client
// carries its own labels for those.
func resolveFilterPeople(ids []int, credits creditsByID) []statsFilterPerson {
	if len(ids) == 0 {
		return nil
	}

	names := make(map[int]string, len(ids))
	// Stop as soon as every filter id has a name — this is a best-effort label
	// lookup over the full (~thousands of rows) credits map, and the handful of
	// filter people are usually found in the first few movies.
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
