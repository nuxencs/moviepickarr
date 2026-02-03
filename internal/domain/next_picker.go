package domain

import "context"

type NextPickerRepo interface {
	Get(ctx context.Context) (*User, error)
	Set(ctx context.Context, userID int) error
}
