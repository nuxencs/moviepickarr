package domain

import "context"

// AdminSeedResult describes one seed decision. NeedsPasswordHash and
// AmbiguousNames mean nothing was written; every other result is committed.
type AdminSeedResult struct {
	UserID            int
	Name              string
	NeedsPasswordHash bool
	AmbiguousNames    []string
	Created           bool
	Promoted          bool
	LoginCreated      bool
	LoginPreserved    bool
}

// AdminSeedRepo is the boot-only port for the break-glass admin seed.
type AdminSeedRepo interface {
	// SeedAdmin runs in one writer transaction. A nil passwordHash is a cheap
	// probe: it returns NeedsPasswordHash without writing, so the caller can hash
	// outside the writer. Existing passwords are never overwritten.
	SeedAdmin(ctx context.Context, name, username string, passwordHash *string) (AdminSeedResult, error)
	// CountAdmins lets boot warn when no admin exists and no seed is configured.
	CountAdmins(ctx context.Context) (int, error)
}
