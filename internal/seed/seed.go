// Package seed holds the app's one bootstrap into itself: the env-seeded
// break-glass admin. Onboarding is otherwise invite-only, so a fresh deploy
// would have no way in without this. The seed runs once per boot, between
// migrate and serve, and is idempotent while the named member remains active,
// so leaving the env vars set across those restarts is safe.
package seed

import (
	"context"
	"fmt"
	"os"
	"strings"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/domain"

	"github.com/rs/zerolog"
)

// Password bounds for the seeded login, matching the min-8/max-128 rule the
// local-login flow applies at the HTTP edge (the max closes an unbounded-input
// argon2id DoS). auth.HashPassword defers length validation to its caller, and
// the seed is a caller, so it enforces the bounds here rather than hashing an
// out-of-range password unchecked.
const (
	minPasswordLen = 8
	maxPasswordLen = 128
)

// AdminConfig is the break-glass admin trio. Name is matched against an
// existing member (case-insensitively) to adopt rather than duplicate;
// Username/Password become the seeded local login.
type AdminConfig struct {
	Name     string
	Username string
	Password string
}

// complete reports whether all three fields are present. The trio is
// all-or-nothing: a partial set is a misconfiguration, not a valid seed.
func (c AdminConfig) complete() bool {
	return c.Name != "" && c.Username != "" && c.Password != ""
}

// validate checks the fields a configured trio must satisfy before the seed
// touches the database. Only the password bound is enforced here; presence is
// already guaranteed by complete(). A violation fails boot loudly, which is the
// right outcome for a misconfigured seed.
func (c AdminConfig) validate() error {
	if n := len(c.Password); n < minPasswordLen || n > maxPasswordLen {
		return fmt.Errorf("MPA_ADMIN_PASSWORD must be %d-%d characters, got %d", minPasswordLen, maxPasswordLen, n)
	}
	return nil
}

// AdminConfigFromEnv reads the MPA_ADMIN_* trio, trimming surrounding
// whitespace so a stray space in a compose file doesn't silently change the
// seeded name or credentials. ok is true only when all three are set; a partial
// set is logged as a warning and reported as not-configured, so boot skips the
// seed (and, if no admin exists, falls through to the zero-admins warning).
func AdminConfigFromEnv(log zerolog.Logger) (AdminConfig, bool) {
	cfg := AdminConfig{
		Name:     strings.TrimSpace(os.Getenv("MPA_ADMIN_NAME")),
		Username: strings.TrimSpace(os.Getenv("MPA_ADMIN_USERNAME")),
		Password: strings.TrimSpace(os.Getenv("MPA_ADMIN_PASSWORD")),
	}
	if cfg.complete() {
		return cfg, true
	}

	// Some but not all set: call it out so a typo'd var name isn't mistaken for
	// a deliberate "no seed" deployment.
	if cfg.Name != "" || cfg.Username != "" || cfg.Password != "" {
		log.Warn().
			Bool("MPA_ADMIN_NAME", cfg.Name != "").
			Bool("MPA_ADMIN_USERNAME", cfg.Username != "").
			Bool("MPA_ADMIN_PASSWORD", cfg.Password != "").
			Msg("break-glass admin seed partially configured; all three of MPA_ADMIN_NAME/USERNAME/PASSWORD are required, skipping seed")
	}
	return cfg, false
}

// BreakGlassAdmin is the seed step in the migrate → seed → serve boot sequence.
//
// When the trio is configured it makes sure an admin member with a working
// local login exists, and returns a non-nil error on any failure so boot dies
// loudly. A broken seed must be obvious, not a silently login-less deploy. When
// the trio is not configured it is a no-op except for one guard: if the DB has
// zero admins it warns loudly, because nobody can perform admin actions.
//
// The step is idempotent. An existing member matching the name is adopted and
// ensured admin, and an already-present local login is never overwritten, so
// re-running boot with the same env changes nothing.
func BreakGlassAdmin(ctx context.Context, repo domain.AdminSeedRepo, cfg AdminConfig, configured bool, log zerolog.Logger) error {
	if !configured {
		warnIfNoAdmins(ctx, repo, log)
		return nil
	}

	if err := cfg.validate(); err != nil {
		return fmt.Errorf("break-glass admin seed: %w", err)
	}
	if err := seedAdmin(ctx, repo, cfg, log); err != nil {
		return fmt.Errorf("break-glass admin seed: %w", err)
	}
	return nil
}

// warnIfNoAdmins logs a loud warning when the roster holds no admin. A failed
// count here is not fatal, since the paths that call it are deliberately
// non-blocking, but it is surfaced so the operator sees it.
func warnIfNoAdmins(ctx context.Context, repo domain.AdminSeedRepo, log zerolog.Logger) {
	admins, err := repo.CountAdmins(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("break-glass admin seed: could not count admins")
		return
	}
	if admins == 0 {
		log.Warn().Msg("no admin members exist and no break-glass seed took effect; set MPA_ADMIN_NAME/USERNAME/PASSWORD so an admin can be created on boot")
	}
}

// seedAdmin runs the actual bootstrap once the trio is known good. Any returned
// error propagates to a loud boot failure.
func seedAdmin(ctx context.Context, repo domain.AdminSeedRepo, cfg AdminConfig, log zerolog.Logger) error {
	matches, err := repo.FindUsersByNameFold(ctx, cfg.Name)
	if err != nil {
		return fmt.Errorf("look up member %q: %w", cfg.Name, err)
	}

	switch len(matches) {
	case 0:
		return createAdmin(ctx, repo, cfg, log)
	case 1:
		if matches[0].Archived {
			return fmt.Errorf(
				"member %q is archived; choose unused MPA_ADMIN_NAME and MPA_ADMIN_USERNAME values, then restore this member explicitly",
				matches[0].Name,
			)
		}
		return adoptAdmin(ctx, repo, matches[0], cfg, log)
	default:
		// Ambiguous: two members fold to the same name, so there is no single
		// row to adopt. Skip and log rather than guess or fail boot; the spec
		// treats this as a deliberate no-op, not a seed error. Still run the
		// zero-admins guard so a skipped seed on a fresh DB gets the same loud
		// signal the no-seed path would give.
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, m.Name)
		}
		log.Warn().
			Str("MPA_ADMIN_NAME", cfg.Name).
			Strs("matches", names).
			Msg("break-glass admin seed: multiple members match the name case-insensitively, skipping to avoid adopting the wrong one")
		warnIfNoAdmins(ctx, repo, log)
		return nil
	}
}

// createAdmin is the fresh-DB path: no member by that name, so create one as an
// admin and give it the seeded local login.
func createAdmin(ctx context.Context, repo domain.AdminSeedRepo, cfg AdminConfig, log zerolog.Logger) error {
	id, err := repo.CreateAdmin(ctx, cfg.Name)
	if err != nil {
		return fmt.Errorf("create admin member %q: %w", cfg.Name, err)
	}
	if err := attachLocalLogin(ctx, repo, id, cfg); err != nil {
		return err
	}
	log.Info().Int("user_id", id).Str("name", cfg.Name).Str("username", cfg.Username).
		Msg("break-glass admin seeded: created admin member with local login")
	return nil
}

// adoptAdmin is the existing-member path: adopt the row so history stays
// attached, make sure it is an admin, and attach a local login only if the
// member does not already have one (non-clobber: an existing password is never
// overwritten).
func adoptAdmin(ctx context.Context, repo domain.AdminSeedRepo, member domain.SeedUser, cfg AdminConfig, log zerolog.Logger) error {
	if member.Role != "admin" {
		if err := repo.PromoteToAdmin(ctx, member.ID); err != nil {
			return fmt.Errorf("promote member %d to admin: %w", member.ID, err)
		}
		log.Info().Int("user_id", member.ID).Str("name", member.Name).
			Msg("break-glass admin seed: promoted existing member to admin")
	}

	hasLogin, err := repo.HasLocalAccount(ctx, member.ID)
	if err != nil {
		return fmt.Errorf("check local login for member %d: %w", member.ID, err)
	}
	if hasLogin {
		log.Info().Int("user_id", member.ID).Str("name", member.Name).
			Msg("break-glass admin seed: member already has a local login, leaving the existing password untouched")
		return nil
	}

	if err := attachLocalLogin(ctx, repo, member.ID, cfg); err != nil {
		return err
	}
	log.Info().Int("user_id", member.ID).Str("name", member.Name).Str("username", cfg.Username).
		Msg("break-glass admin seed: attached local login to existing admin member")
	return nil
}

// attachLocalLogin hashes the seeded password with the argon2id wrapper and
// writes the local_accounts row.
func attachLocalLogin(ctx context.Context, repo domain.AdminSeedRepo, userID int, cfg AdminConfig) error {
	hash, err := auth.HashPassword(cfg.Password)
	if err != nil {
		return fmt.Errorf("hash seeded password: %w", err)
	}
	if err := repo.CreateLocalAccount(ctx, userID, cfg.Username, hash); err != nil {
		return fmt.Errorf("create local login for member %d: %w", userID, err)
	}
	return nil
}
