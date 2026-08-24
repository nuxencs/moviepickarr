// Package devfixtures loads a coherent, deterministic set of developer data
// (a roster with real logins, movies across every lifecycle state, watched
// history spread over time, an active turn holder) into a local DB.
//
// It is dev-only tooling driven by the cmd/devfixtures command, not part of the
// shipped server. It is deliberately distinct from the break-glass admin *seed*
// (internal/seed), which bootstraps a single admin on every boot: fixtures wipe
// and reload a whole developer world, and never run in production.
package devfixtures

import (
	_ "embed"
	"encoding/json/v2"
	"fmt"
)

//go:embed data/movies.json
var moviesJSON []byte

// MovieIdentity is one real TMDB title the fixtures draw from. Only the identity fields
// are stored: tmdb_id links the seeded movie so the enrichment worker fills its
// poster/metadata live on the next boot, and title/year render before that
// lands (and offline, with no TMDB key).
type MovieIdentity struct {
	TMDBID int    `json:"tmdb_id"`
	Title  string `json:"title"`
	Year   int    `json:"year"`
}

// LoadMovies returns the embedded movie dataset. It fails loudly if the embedded
// file is malformed, since a broken dataset means every fixtures run is broken.
func LoadMovies() ([]MovieIdentity, error) {
	var movies []MovieIdentity
	if err := json.Unmarshal(moviesJSON, &movies); err != nil {
		return nil, fmt.Errorf("decode embedded movies dataset: %w", err)
	}
	if len(movies) == 0 {
		return nil, fmt.Errorf("embedded movies dataset is empty")
	}
	return movies, nil
}
