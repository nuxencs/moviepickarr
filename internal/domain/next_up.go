package domain

import "context"

type NextUpRepo interface {
	Get(ctx context.Context) (*User, error)
	Set(ctx context.Context, userID int) error
}
