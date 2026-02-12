package domain

import (
	"context"
	"time"
)

type MovieRepo interface {
	FindByID(ctx context.Context, id int) (*Movie, error)
	List(ctx context.Context) ([]*Movie, error)
	FindByUserID(ctx context.Context, userID int) ([]*Movie, error)
	FindByStatus(ctx context.Context, status string) ([]*Movie, error)
	FindByUserIDAndStatus(ctx context.Context, userID int, status string) ([]*Movie, error)
	CountByStatus(ctx context.Context, status string) (int, error)
	CountByUserIDAndStatus(ctx context.Context, userID int, status string) (int, error)
	Add(ctx context.Context, title, link, status string, userID int) (*Movie, error)
	UpdateTitleAndLink(ctx context.Context, id int, title, link string) error
	UpdateWatchedAt(ctx context.Context, id int, watchedAt time.Time) error
	UpdateStatus(ctx context.Context, id int, status string) error
	MarkAsWatched(ctx context.Context, id int, time time.Time) error
	GetRandomPooled(ctx context.Context) (*Movie, error)
	GetCurrent(ctx context.Context) (*Movie, error)
	Delete(ctx context.Context, id int) error
}

type MovieStatus string

const (
	MovieStatusPool    MovieStatus = "pool"
	MovieStatusStash   MovieStatus = "stash"
	MovieStatusCurrent MovieStatus = "current"
	MovieStatusWatched MovieStatus = "watched"
)

type Movie struct {
	ID          int
	Title       string
	Link        string
	Status      string
	AddedAt     *time.Time
	AddedByID   int
	AddedByName string
	WatchedAt   *time.Time
}
