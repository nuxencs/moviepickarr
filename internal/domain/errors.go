package domain

import "errors"

var (
	ErrNotFound           = errors.New("not found")
	ErrInvalidInput       = errors.New("invalid input")
	ErrForbidden          = errors.New("forbidden")
	ErrPoolLimitReached   = errors.New("pool limit reached")
	ErrPoolLocked         = errors.New("pool is locked")
	ErrNoCurrentDraw      = errors.New("no current draw")
	ErrCurrentDrawExists  = errors.New("current draw exists")
	ErrDrawInProgress     = errors.New("a draw is in progress")
	ErrNoActiveWildcard   = errors.New("no active wildcard")
	ErrActiveWildcard     = errors.New("an active wildcard exists")
	ErrDrawNotRevealed    = errors.New("the current draw is not revealed")
	ErrCurrentDrawChanged = errors.New("the current draw changed")
	ErrWildcardChanged    = errors.New("the active wildcard changed")
	ErrInvalidState       = errors.New("invalid state")
	ErrConflict           = errors.New("conflict")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrNoLocalLogin       = errors.New("no local login")
	ErrInviteInvalid      = errors.New("invite invalid")
	ErrInviteUsed         = errors.New("invite already used")
	ErrSessionInvalid     = errors.New("session invalid")
)
