package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
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

type tmdbExternalIDsResponse struct {
	IMDbID string `json:"imdb_id"`
}

type tmdbClient struct {
	apiKey string
	http   *http.Client
}

func newTMDBClient() *tmdbClient {
	return &tmdbClient{
		apiKey: os.Getenv("TMDB_API_KEY"),
		http: &http.Client{
			Timeout: 8 * time.Second,
		},
	}
}

func (c *tmdbClient) Search(ctx context.Context, query string) ([]tmdbMovie, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("tmdb api key not configured")
	}

	u := fmt.Sprintf("https://api.themoviedb.org/3/search/movie?query=%s", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tmdb api request failed: status %d", resp.StatusCode)
	}

	var payload tmdbSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return payload.Results, nil
}

func (c *tmdbClient) ExternalLink(ctx context.Context, movieID string) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("tmdb api key not configured")
	}

	u := fmt.Sprintf("https://api.themoviedb.org/3/movie/%s/external_ids", movieID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tmdb api request failed: status %d", resp.StatusCode)
	}

	var payload tmdbExternalIDsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}

	if payload.IMDbID == "" {
		return fmt.Sprintf("https://www.themoviedb.org/movie/%s", movieID), nil
	}

	return fmt.Sprintf("https://www.imdb.com/title/%s/", payload.IMDbID), nil
}
