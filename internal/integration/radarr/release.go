package radarr

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type namedPayload struct {
	Name string `json:"name"`
}

type languagePayload struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type releasePayload struct {
	GUID                string            `json:"guid"`
	IndexerID           int               `json:"indexerId"`
	Indexer             string            `json:"indexer"`
	Title               string            `json:"title"`
	Size                int64             `json:"size"`
	PublishDate         time.Time         `json:"publishDate"`
	Protocol            string            `json:"protocol"`
	Seeders             *int              `json:"seeders"`
	Leechers            *int              `json:"leechers"`
	Quality             qualityPayload    `json:"quality"`
	Languages           []languagePayload `json:"languages"`
	CustomFormats       []namedPayload    `json:"customFormats"`
	CustomFormatScore   int               `json:"customFormatScore"`
	ReleaseGroup        string            `json:"releaseGroup"`
	Edition             string            `json:"edition"`
	MappedMovieID       *int              `json:"mappedMovieId"`
	Approved            bool              `json:"approved"`
	Rejected            bool              `json:"rejected"`
	TemporarilyRejected bool              `json:"temporarilyRejected"`
	Rejections          []string          `json:"rejections"`
}

type cachedRelease struct {
	movieID   int
	guid      string
	indexer   int
	rejected  bool
	quality   qualityPayload
	languages []languagePayload
	expires   time.Time
}

func (c *HTTPClient) SearchReleases(ctx context.Context, movieID int) ([]Release, error) {
	if movieID <= 0 {
		return nil, fmt.Errorf("%w: Radarr movie ID must be positive", ErrInvalidInput)
	}
	query := url.Values{"movieId": []string{strconv.Itoa(movieID)}}
	var payload []releasePayload
	if err := c.get(ctx, "release", query, &payload); err != nil {
		return nil, err
	}
	matched := make([]releasePayload, 0, len(payload))
	for _, candidate := range payload {
		if candidate.MappedMovieID == nil || *candidate.MappedMovieID != movieID || candidate.GUID == "" || candidate.IndexerID <= 0 {
			continue
		}
		matched = append(matched, candidate)
	}
	sort.SliceStable(matched, func(i, j int) bool {
		return matched[i].Approved && !matched[j].Approved
	})

	now := time.Now().UTC()
	c.releaseMu.Lock()
	defer c.releaseMu.Unlock()
	c.pruneExpiredReleasesLocked(now)
	c.invalidateMovieReleasesLocked(movieID)
	result := make([]Release, 0, len(matched))
	for _, candidate := range matched {
		id, err := c.newReleaseIDLocked()
		if err != nil {
			c.invalidateMovieReleasesLocked(movieID)
			return nil, err
		}
		rejected := candidate.Rejected || candidate.TemporarilyRejected || !candidate.Approved
		release := Release{
			ID: id, Title: candidate.Title, Indexer: candidate.Indexer, Size: candidate.Size,
			PublishedAt: candidate.PublishDate, Protocol: candidate.Protocol,
			Seeders: candidate.Seeders, Leechers: candidate.Leechers, Quality: qualityFromPayload(candidate.Quality),
			Languages: languageNames(candidate.Languages), CustomFormats: names(candidate.CustomFormats),
			CustomFormatScore: candidate.CustomFormatScore, ReleaseGroup: candidate.ReleaseGroup,
			Edition: candidate.Edition, Approved: candidate.Approved, Rejected: rejected,
			RejectionReasons: append([]string(nil), candidate.Rejections...),
		}
		result = append(result, release)
		c.releases[id] = cachedRelease{
			movieID: movieID, guid: candidate.GUID, indexer: candidate.IndexerID,
			rejected: rejected, quality: candidate.Quality,
			languages: append([]languagePayload(nil), candidate.Languages...),
			expires:   now.Add(c.releaseCacheTTL),
		}
		if c.releasesByMovie[movieID] == nil {
			c.releasesByMovie[movieID] = make(map[string]struct{})
		}
		c.releasesByMovie[movieID][id] = struct{}{}
	}
	return result, nil
}

func languageNames(values []languagePayload) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if name := strings.TrimSpace(value.Name); name != "" {
			result = append(result, name)
		}
	}
	return result
}

func names(values []namedPayload) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if name := strings.TrimSpace(value.Name); name != "" {
			result = append(result, name)
		}
	}
	return result
}

func (c *HTTPClient) newReleaseIDLocked() (string, error) {
	for range 8 {
		buffer := make([]byte, 18)
		if _, err := cryptorand.Read(buffer); err != nil {
			return "", fmt.Errorf("create opaque release ID: %w", err)
		}
		id := "rr_" + base64.RawURLEncoding.EncodeToString(buffer)
		if _, exists := c.releases[id]; !exists {
			return id, nil
		}
	}
	return "", errors.New("could not allocate a unique opaque release ID")
}

func (c *HTTPClient) GrabRelease(ctx context.Context, request GrabReleaseRequest) error {
	id := strings.TrimSpace(request.ResultID)
	if id == "" {
		return fmt.Errorf("%w: release result ID is required", ErrInvalidInput)
	}
	now := time.Now().UTC()
	c.releaseMu.Lock()
	cached, exists := c.releases[id]
	if !exists || !cached.expires.After(now) {
		if exists {
			c.deleteReleaseLocked(id, cached.movieID)
		}
		c.releaseMu.Unlock()
		return ErrReleaseExpired
	}
	if cached.rejected && !request.AllowRejected {
		c.releaseMu.Unlock()
		return ErrRejectedRelease
	}
	c.releaseMu.Unlock()

	payload := struct {
		GUID           string             `json:"guid"`
		IndexerID      int                `json:"indexerId"`
		ShouldOverride *bool              `json:"shouldOverride,omitempty"`
		MovieID        *int               `json:"movieId,omitempty"`
		Quality        *qualityPayload    `json:"quality,omitempty"`
		Languages      *[]languagePayload `json:"languages,omitempty"`
	}{GUID: cached.guid, IndexerID: cached.indexer}
	if cached.rejected {
		override := true
		movieID := cached.movieID
		languages := append(make([]languagePayload, 0, len(cached.languages)), cached.languages...)
		payload.ShouldOverride = &override
		payload.MovieID = &movieID
		payload.Quality = &cached.quality
		payload.Languages = &languages
	}
	if err := c.post(ctx, "release", payload, nil); err != nil {
		if errors.Is(err, ErrNotFound) {
			c.releaseMu.Lock()
			c.deleteReleaseLocked(id, cached.movieID)
			c.releaseMu.Unlock()
			return fmt.Errorf("%w: Radarr no longer has the selected result", ErrReleaseExpired)
		}
		return err
	}
	c.releaseMu.Lock()
	c.invalidateMovieReleasesLocked(cached.movieID)
	c.releaseMu.Unlock()
	return nil
}

func (c *HTTPClient) pruneExpiredReleasesLocked(now time.Time) {
	for id, cached := range c.releases {
		if !cached.expires.After(now) {
			c.deleteReleaseLocked(id, cached.movieID)
		}
	}
}

func (c *HTTPClient) invalidateMovieReleasesLocked(movieID int) {
	for id := range c.releasesByMovie[movieID] {
		delete(c.releases, id)
	}
	delete(c.releasesByMovie, movieID)
}

func (c *HTTPClient) deleteReleaseLocked(id string, movieID int) {
	delete(c.releases, id)
	if ids := c.releasesByMovie[movieID]; ids != nil {
		delete(ids, id)
		if len(ids) == 0 {
			delete(c.releasesByMovie, movieID)
		}
	}
}
