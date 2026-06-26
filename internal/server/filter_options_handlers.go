package server

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"moviepickarr/internal/domain"

	"github.com/gofiber/fiber/v2"
)

// filterPersonOption is one selectable person (actor, crew member, or picker) in
// the Stats filter bar — id plus display name.
type filterPersonOption struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// filterOptionsResponse is the full set of Stats filter choices, derived
// server-side from the watched library. It replaces the client-side
// filterOptionsFrom() that used to rebuild these from the credits embedded in
// every /movies/watched payload — so that payload can now ship lean.
type filterOptionsResponse struct {
	Genres  []string             `json:"genres"`  // A→Z
	Actors  []filterPersonOption `json:"actors"`  // A→Z by name
	Crew    []filterPersonOption `json:"crew"`    // A→Z by name
	Years   []int                `json:"years"`   // release years, newest first
	Pickers []filterPersonOption `json:"pickers"` // A→Z by name
}

func (h *handler) handleGetFilterOptions(c *fiber.Ctx) error {
	now := time.Now()
	if cached, ok := h.getCachedFilterOptions(now); ok {
		return c.Status(fiber.StatusOK).JSON(cached)
	}

	ctx := c.UserContext()
	watched, err := h.movieService.Watched(ctx)
	if err != nil {
		return writeError(c, err)
	}

	ids := make([]int, len(watched))
	for i := range watched {
		ids[i] = watched[i].ID
	}
	meta, err := h.movieMetadata.GetMetadataByMovieIDs(ctx, ids)
	if err != nil {
		return writeError(c, err)
	}
	credits, err := h.movieCredits.GetCreditsByMovieIDs(ctx, ids)
	if err != nil {
		return writeError(c, err)
	}

	payload := buildFilterOptions(watched, meta, credits)
	h.setCachedFilterOptions(payload, now)

	return c.Status(fiber.StatusOK).JSON(payload)
}

// buildFilterOptions mirrors the old client-side filterOptionsFrom(watched):
// unique genres, the distinct cast and crew people (split, matching the
// actors/crew filter split), the distinct pickers, and the distinct release
// years — each in the same display order the UI expects.
func buildFilterOptions(watched []*domain.Movie, meta metaByID, credits creditsByID) filterOptionsResponse {
	genres := make(map[string]struct{})
	actors := make(map[int]string)
	crew := make(map[int]string)
	pickers := make(map[int]string)
	years := make(map[int]struct{})

	for i := range watched {
		md := meta[watched[i].ID]
		if md != nil {
			for _, genre := range md.Genres {
				genres[genre] = struct{}{}
			}
			if y := releaseYearOf(md); y > 0 {
				years[y] = struct{}{}
			}
		}
		if watched[i].AddedByID != 0 {
			if _, ok := pickers[watched[i].AddedByID]; !ok {
				pickers[watched[i].AddedByID] = watched[i].AddedByName
			}
		}
		for _, credit := range credits[watched[i].ID] {
			switch credit.Kind {
			case domain.CreditKindCast:
				if _, ok := actors[credit.Person.ID]; !ok {
					actors[credit.Person.ID] = credit.Person.Name
				}
			case domain.CreditKindCrew:
				if _, ok := crew[credit.Person.ID]; !ok {
					crew[credit.Person.ID] = credit.Person.Name
				}
			}
		}
	}

	return filterOptionsResponse{
		Genres:  sortedStrings(genres),
		Actors:  sortedPeople(actors),
		Crew:    sortedPeople(crew),
		Years:   sortedYearsDesc(years),
		Pickers: sortedPeople(pickers),
	}
}

func sortedStrings(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	return out
}

func sortedPeople(byID map[int]string) []filterPersonOption {
	out := make([]filterPersonOption, 0, len(byID))
	for id, name := range byID {
		out = append(out, filterPersonOption{ID: id, Name: name})
	}
	// Name A→Z, id as a stable tiebreak for distinct people sharing a name.
	slices.SortFunc(out, func(a, b filterPersonOption) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.ID, b.ID),
		)
	})
	return out
}

func sortedYearsDesc(set map[int]struct{}) []int {
	out := make([]int, 0, len(set))
	for y := range set {
		out = append(out, y)
	}
	slices.Sort(out)
	slices.Reverse(out)
	return out
}

func (h *handler) getCachedFilterOptions(now time.Time) (filterOptionsResponse, bool) {
	h.filterOptionsMu.RLock()
	defer h.filterOptionsMu.RUnlock()
	if h.filterOptionsCache == nil || now.After(h.filterOptionsExpiry) {
		return filterOptionsResponse{}, false
	}
	return *h.filterOptionsCache, true
}

func (h *handler) setCachedFilterOptions(payload filterOptionsResponse, now time.Time) {
	h.filterOptionsMu.Lock()
	defer h.filterOptionsMu.Unlock()
	h.filterOptionsCache = &payload
	h.filterOptionsExpiry = now.Add(h.statsCacheTTL)
}

func (h *handler) invalidateFilterOptionsCache() {
	h.filterOptionsMu.Lock()
	defer h.filterOptionsMu.Unlock()
	h.filterOptionsCache = nil
}
