package radarr

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type commandPayload struct {
	ID      int        `json:"id"`
	Name    string     `json:"name"`
	Status  string     `json:"status"`
	Message string     `json:"message"`
	Queued  time.Time  `json:"queued"`
	Started *time.Time `json:"started"`
	Ended   *time.Time `json:"ended"`
}

type moviesSearchCommandPayload struct {
	commandPayload
	Body struct {
		MovieIDs []int `json:"movieIds"`
	} `json:"body"`
}

func (c *HTTPClient) StartMoviesSearch(ctx context.Context, movieID int) (Command, error) {
	if movieID <= 0 {
		return Command{}, fmt.Errorf("%w: Radarr movie ID must be positive", ErrInvalidInput)
	}
	payload := struct {
		Name     string `json:"name"`
		MovieIDs []int  `json:"movieIds"`
	}{Name: "MoviesSearch", MovieIDs: []int{movieID}}
	var command commandPayload
	if err := c.post(ctx, "command", payload, &command); err != nil {
		return Command{}, err
	}
	if command.ID <= 0 || command.Name != "MoviesSearch" {
		return Command{}, fmt.Errorf("%w: MoviesSearch returned an invalid command", ErrInvalidResponse)
	}
	return commandFromPayload(command), nil
}

func (c *HTTPClient) FindRecentMoviesSearchCommand(
	ctx context.Context,
	movieID int,
	queuedSince time.Time,
) (*Command, error) {
	if movieID <= 0 {
		return nil, fmt.Errorf("%w: Radarr movie ID must be positive", ErrInvalidInput)
	}
	if queuedSince.IsZero() {
		return nil, fmt.Errorf("%w: command search boundary is required", ErrInvalidInput)
	}
	var payloads []moviesSearchCommandPayload
	if err := c.get(ctx, "command", nil, &payloads); err != nil {
		return nil, err
	}
	var active, recent *commandPayload
	for i := range payloads {
		payload := &payloads[i]
		if payload.Name != "MoviesSearch" || len(payload.Body.MovieIDs) != 1 || payload.Body.MovieIDs[0] != movieID {
			continue
		}
		if payload.ID <= 0 || payload.Queued.IsZero() {
			return nil, fmt.Errorf("%w: MoviesSearch command list contained an invalid command", ErrInvalidResponse)
		}
		status := strings.ToLower(strings.TrimSpace(payload.Status))
		if status == "queued" || status == "started" {
			if active == nil || payload.Queued.After(active.Queued) || payload.Queued.Equal(active.Queued) && payload.ID > active.ID {
				copy := payload.commandPayload
				active = &copy
			}
			continue
		}
		if payload.Queued.Before(queuedSince) && (payload.Ended == nil || payload.Ended.Before(queuedSince)) {
			continue
		}
		if recent == nil || payload.Queued.After(recent.Queued) || payload.Queued.Equal(recent.Queued) && payload.ID > recent.ID {
			copy := payload.commandPayload
			recent = &copy
		}
	}
	selected := active
	if selected == nil {
		selected = recent
	}
	if selected == nil {
		return nil, nil
	}
	command := commandFromPayload(*selected)
	return &command, nil
}

func (c *HTTPClient) GetCommand(ctx context.Context, commandID int) (Command, error) {
	if commandID <= 0 {
		return Command{}, fmt.Errorf("%w: Radarr command ID must be positive", ErrInvalidInput)
	}
	var command commandPayload
	if err := c.get(ctx, "command/"+strconv.Itoa(commandID), nil, &command); err != nil {
		return Command{}, err
	}
	if command.ID != commandID {
		return Command{}, fmt.Errorf("%w: command response ID does not match the request", ErrInvalidResponse)
	}
	return commandFromPayload(command), nil
}

func commandFromPayload(command commandPayload) Command {
	return Command(command)
}
