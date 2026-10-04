package domain

import "context"

type NextUpRepo interface {
	Get(ctx context.Context) (*User, error)
	Set(ctx context.Context, userID int) error
	SetFirstEligible(ctx context.Context) (*User, error)
	// Skip passes the turn from holderID to the next Turn participant. It
	// refuses a stale holder (ErrNextUpChanged), an unrevealed draw
	// (ErrDrawNotRevealed), and a roster with one participant (ErrConflict).
	Skip(ctx context.Context, holderID int) (*User, error)
}
