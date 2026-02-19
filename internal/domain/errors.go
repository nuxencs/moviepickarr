package domain

import "errors"

var (
	ErrNotFound          = errors.New("not found")
	ErrInvalidInput      = errors.New("invalid input")
	ErrForbidden         = errors.New("forbidden")
	ErrUnauthorized      = errors.New("unauthorized")
	ErrUnauthenticated   = errors.New("unauthenticated")
	ErrPoolLimitReached  = errors.New("pool limit reached")
	ErrPoolLocked        = errors.New("pool is locked")
	ErrNoCurrentMovie    = errors.New("no current movie")
	ErrCurrentMovieExist = errors.New("current movie exists")
	ErrInvalidState      = errors.New("invalid state")
)
