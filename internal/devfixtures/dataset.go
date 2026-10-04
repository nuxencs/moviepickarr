// Package devfixtures loads a deterministic developer world into a local DB.
// Dev-only (cmd/devfixtures); unlike internal/seed it never runs in production.
package devfixtures

import (
	_ "embed"
	"encoding/json/v2"
	"fmt"
)

//go:embed data/movies.json
var moviesJSON []byte

// MovieIdentity is one real TMDB title. Only identity is stored: enrichment
// fills the rest on the next boot.
type MovieIdentity struct {
	TMDBID int    `json:"tmdb_id"`
	Title  string `json:"title"`
	Year   int    `json:"year"`
}

// LoadMovies returns the embedded movie dataset.
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
