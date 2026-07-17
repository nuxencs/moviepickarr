package domain

import "errors"

var (
	ErrNotFound          = errors.New("not found")
	ErrInvalidInput      = errors.New("invalid input")
	ErrForbidden         = errors.New("forbidden")
	ErrPoolLimitReached  = errors.New("pool limit reached")
	ErrPoolLocked        = errors.New("pool is locked")
	ErrNoCurrentDraw     = errors.New("no current draw")
	ErrCurrentDrawExists = errors.New("current draw exists")
	ErrInvalidState      = errors.New("invalid state")
	ErrConflict          = errors.New("conflict")
)
