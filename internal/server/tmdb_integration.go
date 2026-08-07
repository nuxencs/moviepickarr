package server

import (
	"context"
	"errors"
	"net/http"

	integrationtmdb "moviepickarr/internal/integration/tmdb"
)

type tmdbConnectionTester struct {
	baseURL string
	http    *http.Client
}

func newTMDBConnectionTester(baseURL string, httpClient *http.Client) *tmdbConnectionTester {
	return &tmdbConnectionTester{baseURL: baseURL, http: httpClient}
}

func (t *tmdbConnectionTester) TestConnection(ctx context.Context, config integrationtmdb.RuntimeConfig) error {
	client := &tmdbClient{
		apiKey:      config.APIKey,
		baseURL:     t.baseURL,
		http:        t.http,
		limiter:     newRateLimiter(config.MinInterval),
		maxRetries:  config.MaxRetries,
		backoffBase: config.Backoff,
	}
	var response struct {
		Images map[string]any `json:"images"`
	}
	err := client.doRequest(ctx, t.baseURL+"/configuration", &response)
	if errors.Is(err, errTMDBAuthentication) {
		return integrationtmdb.ErrAuthentication
	}
	return err
}
