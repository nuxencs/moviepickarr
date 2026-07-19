package domain

import "context"

// SeedUser is the slim member projection the break-glass seed reasons over:
// just the identity fields it decides on (id, name) plus the live role it may
// have to raise. It is deliberately narrower than User (the seed never touches
// timestamps) so the port stays focused on the bootstrap decision.
type SeedUser struct {
	ID   int
	Name string
	Role string
}

// AdminSeedRepo is the persistence port the break-glass admin seed runs over.
// It is a boot-only surface: the env-seeded admin bootstrap is the sole caller,
// so the methods map one-to-one to the seed's decision tree (match by name,
// create or adopt, ensure the admin role, attach a local login) rather than to
// a general member-management API.
type AdminSeedRepo interface {
	// FindUsersByNameFold returns every member whose name matches the given
	// name case-insensitively. More than one row means an ambiguous match the
	// seed refuses to act on; exactly one is the adopt path; none is create.
	FindUsersByNameFold(ctx context.Context, name string) ([]SeedUser, error)
	// CreateAdmin inserts a fresh member with role='admin' and returns its id.
	CreateAdmin(ctx context.Context, name string) (int, error)
	// PromoteToAdmin sets role='admin' on an existing member. Idempotent: a
	// member who is already an admin is left unchanged.
	PromoteToAdmin(ctx context.Context, id int) error
	// HasLocalAccount reports whether the member already holds a local login.
	// The seed reads this to honor its non-clobber rule: an existing password
	// is never overwritten.
	HasLocalAccount(ctx context.Context, userID int) (bool, error)
	// CreateLocalAccount attaches a local login (username + argon2id hash) to a
	// member. Only ever called when HasLocalAccount reported false.
	CreateLocalAccount(ctx context.Context, userID int, username, passwordHash string) error
	// CountAdmins returns how many members currently hold role='admin', so boot
	// can warn loudly when there are none and no seed is configured.
	CountAdmins(ctx context.Context) (int, error)
}
