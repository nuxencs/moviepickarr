package radarr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultRequestTimeout  = 8 * time.Second
	defaultMaxResponseSize = 4 << 20
	defaultReleaseCacheTTL = 30 * time.Minute
)

type ClientConfig struct {
	BaseURL          string
	APIKey           string
	HTTPClient       *http.Client
	Timeout          time.Duration
	MaxResponseBytes int64
	ReleaseCacheTTL  time.Duration
}

// HTTPClient is a production Radarr v3 adapter. It owns one instance URL,
// credential, connection pool, and Interactive search cache.
type HTTPClient struct {
	baseURL          *url.URL
	apiKey           string
	http             *http.Client
	maxResponseBytes int64
	releaseCacheTTL  time.Duration
	releaseMu        sync.Mutex
	releases         map[string]cachedRelease
	releasesByMovie  map[int]map[string]struct{}
}

func NewHTTPClient(config ClientConfig) (*HTTPClient, error) {
	baseURL, err := parseBaseURL(config.BaseURL)
	if err != nil {
		return nil, err
	}
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("%w: API key is required", ErrInvalidInput)
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = defaultRequestTimeout
	}
	if timeout < 0 {
		return nil, fmt.Errorf("%w: timeout cannot be negative", ErrInvalidInput)
	}
	maxResponseBytes := config.MaxResponseBytes
	if maxResponseBytes == 0 {
		maxResponseBytes = defaultMaxResponseSize
	}
	if maxResponseBytes < 0 {
		return nil, fmt.Errorf("%w: response limit cannot be negative", ErrInvalidInput)
	}
	releaseCacheTTL := config.ReleaseCacheTTL
	if releaseCacheTTL == 0 {
		releaseCacheTTL = defaultReleaseCacheTTL
	}
	if releaseCacheTTL < 0 {
		return nil, fmt.Errorf("%w: release cache TTL cannot be negative", ErrInvalidInput)
	}

	httpClient := &http.Client{}
	if config.HTTPClient != nil {
		copy := *config.HTTPClient
		httpClient = &copy
	}
	httpClient.Timeout = timeout
	previousRedirect := httpClient.CheckRedirect
	httpClient.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 0 && !sameOrigin(request.URL, via[0].URL) {
			return fmt.Errorf("%w: cross-origin redirect refused", ErrRemote)
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}

	return &HTTPClient{
		baseURL:          baseURL,
		apiKey:           apiKey,
		http:             httpClient,
		maxResponseBytes: maxResponseBytes,
		releaseCacheTTL:  releaseCacheTTL,
		releases:         make(map[string]cachedRelease),
		releasesByMovie:  make(map[int]map[string]struct{}),
	}, nil
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func parseBaseURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil {
		return nil, fmt.Errorf("%w: Radarr URL is invalid", ErrInvalidInput)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%w: Radarr URL must use HTTP or HTTPS", ErrInvalidInput)
	}
	if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("%w: Radarr URL cannot contain credentials, query, or fragment", ErrInvalidInput)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

type systemStatusPayload struct {
	Version      string `json:"version"`
	InstanceName string `json:"instanceName"`
}

type rootFolderPayload struct {
	ID         int    `json:"id"`
	Path       string `json:"path"`
	Accessible bool   `json:"accessible"`
	FreeSpace  int64  `json:"freeSpace"`
}

type qualityProfilePayload struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type tagPayload struct {
	ID    int    `json:"id"`
	Label string `json:"label"`
}

func (c *HTTPClient) VerifyAndCatalog(ctx context.Context) (Catalog, error) {
	var status systemStatusPayload
	if err := c.get(ctx, "system/status", nil, &status); err != nil {
		return Catalog{}, err
	}
	var roots []rootFolderPayload
	if err := c.get(ctx, "rootfolder", nil, &roots); err != nil {
		return Catalog{}, err
	}
	var profiles []qualityProfilePayload
	if err := c.get(ctx, "qualityprofile", nil, &profiles); err != nil {
		return Catalog{}, err
	}
	var tags []tagPayload
	if err := c.get(ctx, "tag", nil, &tags); err != nil {
		return Catalog{}, err
	}

	catalog := Catalog{
		Version:         status.Version,
		InstanceName:    status.InstanceName,
		RootFolders:     make([]RootFolder, 0, len(roots)),
		QualityProfiles: make([]QualityProfile, 0, len(profiles)),
		Tags:            make([]Tag, 0, len(tags)),
	}
	for _, root := range roots {
		catalog.RootFolders = append(catalog.RootFolders, RootFolder(root))
	}
	for _, profile := range profiles {
		catalog.QualityProfiles = append(catalog.QualityProfiles, QualityProfile(profile))
	}
	for _, tag := range tags {
		catalog.Tags = append(catalog.Tags, Tag(tag))
	}
	return catalog, nil
}

func (c *HTTPClient) get(ctx context.Context, endpoint string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, endpoint, query, nil, out)
}

func (c *HTTPClient) post(ctx context.Context, endpoint string, body, out any) error {
	return c.do(ctx, http.MethodPost, endpoint, nil, body, out)
}

func (c *HTTPClient) put(ctx context.Context, endpoint string, body, out any) error {
	return c.do(ctx, http.MethodPut, endpoint, nil, body, out)
}

func (c *HTTPClient) do(
	ctx context.Context,
	method string,
	endpoint string,
	query url.Values,
	body any,
	out any,
) error {
	requestURL := *c.baseURL
	requestURL.Path = strings.TrimRight(requestURL.Path, "/") + "/api/v3/" + strings.TrimLeft(endpoint, "/")
	requestURL.RawPath = ""
	requestURL.RawQuery = query.Encode()

	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Radarr request: %w", err)
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), requestBody)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Api-Key", c.apiKey)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.http.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errors.Is(err, ErrRemote) {
			return &RequestError{Kind: ErrRemote}
		}
		return &RequestError{Kind: ErrTransient}
	}
	defer response.Body.Close()

	data, err := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBytes+1))
	if err != nil {
		return &RequestError{StatusCode: response.StatusCode, Kind: ErrTransient}
	}
	if int64(len(data)) > c.maxResponseBytes {
		return &RequestError{StatusCode: response.StatusCode, Kind: ErrResponseTooLarge}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return classifyStatus(response.StatusCode)
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return nil
}

func classifyStatus(status int) error {
	kind := ErrRemote
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		kind = ErrAuthentication
	case status == http.StatusNotFound:
		kind = ErrNotFound
	case status == http.StatusConflict:
		kind = ErrConflict
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		kind = ErrValidation
	case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500:
		kind = ErrTransient
	}
	return &RequestError{StatusCode: status, Kind: kind}
}
