package domain

import "context"

// AdminSeedResult describes one completed seed decision. A password-less probe
// sets NeedsPasswordHash and makes no changes. AmbiguousNames likewise reports
// the deliberate skip without a write. Every other result is returned only
// after its writer transaction commits, so callers can log it as durable.
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

// AdminSeedRepo is the persistence port the break-glass admin seed runs over.
// It is a boot-only surface. SeedAdmin owns the users + local_accounts unit of
// work instead of exposing a transaction to the seed package.
type AdminSeedRepo interface {
	// SeedAdmin resolves case-insensitive name matches and local-login presence
	// inside one writer transaction. A nil passwordHash is a cheap probe: when
	// creating a login would be necessary it returns NeedsPasswordHash without
	// mutating the member or login tables. The caller hashes outside the writer
	// transaction and retries with the result. Existing passwords are ignored
	// and never overwritten.
	SeedAdmin(ctx context.Context, name, username string, passwordHash *string) (AdminSeedResult, error)
	// CountAdmins returns how many active members currently hold role='admin',
	// so boot can warn loudly when there are none and no seed is configured.
	CountAdmins(ctx context.Context) (int, error)
}
