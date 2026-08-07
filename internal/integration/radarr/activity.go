package radarr

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

type qualityPayload struct {
	Quality struct {
		ID         int    `json:"id"`
		Name       string `json:"name"`
		Source     string `json:"source"`
		Resolution int    `json:"resolution"`
	} `json:"quality"`
	Revision struct {
		Version  int  `json:"version"`
		Real     int  `json:"real"`
		IsRepack bool `json:"isRepack"`
	} `json:"revision"`
}

func qualityFromPayload(payload qualityPayload) Quality {
	return Quality{Name: payload.Quality.Name, Revision: payload.Revision.Version, Repack: payload.Revision.IsRepack}
}

type queuePayload struct {
	ID                      int            `json:"id"`
	MovieID                 int            `json:"movieId"`
	Title                   string         `json:"title"`
	Status                  string         `json:"status"`
	TrackedDownloadStatus   string         `json:"trackedDownloadStatus"`
	TrackedDownloadState    string         `json:"trackedDownloadState"`
	Protocol                string         `json:"protocol"`
	Indexer                 string         `json:"indexer"`
	Size                    float64        `json:"size"`
	SizeRemaining           float64        `json:"sizeleft"`
	EstimatedCompletionTime *time.Time     `json:"estimatedCompletionTime"`
	Quality                 qualityPayload `json:"quality"`
}

func (c *HTTPClient) Queue(ctx context.Context, movieID int) ([]QueueItem, error) {
	if movieID <= 0 {
		return nil, fmt.Errorf("%w: Radarr movie ID must be positive", ErrInvalidInput)
	}
	query := url.Values{"movieId": []string{strconv.Itoa(movieID)}}
	var payload []queuePayload
	if err := c.get(ctx, "queue/details", query, &payload); err != nil {
		return nil, err
	}
	items := make([]QueueItem, 0, len(payload))
	for _, item := range payload {
		if item.MovieID != movieID {
			return nil, fmt.Errorf("%w: queue returned an item for another movie", ErrInvalidResponse)
		}
		items = append(items, QueueItem{
			ID: item.ID, MovieID: item.MovieID, Title: item.Title, Status: item.Status,
			TrackedDownloadStatus: item.TrackedDownloadStatus, TrackedDownloadState: item.TrackedDownloadState,
			Protocol: item.Protocol, Indexer: item.Indexer, Size: item.Size, SizeRemaining: item.SizeRemaining,
			EstimatedCompletionTime: item.EstimatedCompletionTime, Quality: qualityFromPayload(item.Quality),
		})
	}
	return items, nil
}

type historyPayload struct {
	ID                int            `json:"id"`
	MovieID           int            `json:"movieId"`
	EventType         string         `json:"eventType"`
	SourceTitle       string         `json:"sourceTitle"`
	Date              time.Time      `json:"date"`
	Quality           qualityPayload `json:"quality"`
	CustomFormatScore int            `json:"customFormatScore"`
}

func (c *HTTPClient) History(ctx context.Context, movieID int) ([]HistoryItem, error) {
	if movieID <= 0 {
		return nil, fmt.Errorf("%w: Radarr movie ID must be positive", ErrInvalidInput)
	}
	query := url.Values{"movieId": []string{strconv.Itoa(movieID)}}
	var payload []historyPayload
	if err := c.get(ctx, "history/movie", query, &payload); err != nil {
		return nil, err
	}
	items := make([]HistoryItem, 0, len(payload))
	for _, item := range payload {
		if item.MovieID != movieID {
			return nil, fmt.Errorf("%w: history returned an item for another movie", ErrInvalidResponse)
		}
		items = append(items, HistoryItem{
			ID: item.ID, MovieID: item.MovieID, EventType: item.EventType,
			SourceTitle: item.SourceTitle, Date: item.Date, Quality: qualityFromPayload(item.Quality),
			CustomFormatScore: item.CustomFormatScore,
		})
	}
	return items, nil
}
