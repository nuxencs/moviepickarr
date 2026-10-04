// Package seed creates the env-seeded break-glass admin. Onboarding is
// invite-only, so a fresh deploy has no other way in. Idempotent per boot.
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

// Password bounds: keep in step with the local-login HTTP rule. The max closes
// an argon2id DoS, and auth.HashPassword leaves length checks to its callers.
const (
	minPasswordLen = 8
	maxPasswordLen = 128
)

// AdminConfig is the break-glass admin trio. Name adopts an existing member
// (case-insensitive) instead of a duplicate.
type AdminConfig struct {
	Name     string
	Username string
	Password string
}

// complete reports whether all three fields are set; a partial set is invalid.
func (c AdminConfig) complete() bool {
	return c.Name != "" && c.Username != "" && c.Password != ""
}

// missingVars returns the env var names of the unset fields.
func (c AdminConfig) missingVars() []string {
	var missing []string
	for _, v := range []struct {
		name  string
		value string
	}{
		{"MPA_ADMIN_NAME", c.Name},
		{"MPA_ADMIN_USERNAME", c.Username},
		{"MPA_ADMIN_PASSWORD", c.Password},
	} {
		if v.value == "" {
			missing = append(missing, v.name)
		}
	}
	return missing
}

// validate checks the password bounds; complete() already checked presence.
func (c AdminConfig) validate() error {
	if n := len(c.Password); n < minPasswordLen || n > maxPasswordLen {
		return fmt.Errorf("MPA_ADMIN_PASSWORD must be %d-%d characters, got %d", minPasswordLen, maxPasswordLen, n)
	}
	return nil
}

func seedLogger(log zerolog.Logger) zerolog.Logger {
	return log.With().Str("component", "seed").Logger()
}

// AdminConfigFromEnv reads the trimmed MPA_ADMIN_* trio. ok is true only when
// all three are set; a partial set logs a warning and skips the seed.
func AdminConfigFromEnv(log zerolog.Logger) (AdminConfig, bool) {
	log = seedLogger(log)
	cfg := AdminConfig{
		Name:     strings.TrimSpace(os.Getenv("MPA_ADMIN_NAME")),
		Username: strings.TrimSpace(os.Getenv("MPA_ADMIN_USERNAME")),
		Password: strings.TrimSpace(os.Getenv("MPA_ADMIN_PASSWORD")),
	}
	if cfg.complete() {
		return cfg, true
	}

	// Warn, so a typo'd var name is not mistaken for a deliberate "no seed".
	if cfg.Name != "" || cfg.Username != "" || cfg.Password != "" {
		log.Warn().
			Strs("missing", cfg.missingVars()).
			Msg("break-glass admin seed partially configured, skipping; MPA_ADMIN_NAME, MPA_ADMIN_USERNAME and MPA_ADMIN_PASSWORD are all required")
	}
	return cfg, false
}

// BreakGlassAdmin ensures an admin with a local login exists, between migrate
// and serve. Any failure fails boot, so a broken seed is obvious. Unconfigured,
// it only warns when no admin exists. An existing local login is never overwritten.
func BreakGlassAdmin(ctx context.Context, repo domain.AdminSeedRepo, cfg AdminConfig, configured bool, log zerolog.Logger) error {
	log = seedLogger(log)
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

// warnIfNoAdmins warns when no admin exists. A failed count is logged, not fatal.
func warnIfNoAdmins(ctx context.Context, repo domain.AdminSeedRepo, log zerolog.Logger) {
	admins, err := repo.CountAdmins(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("counting admin members failed")
		return
	}
	if admins == 0 {
		log.Warn().Msg("no admin members exist and no break-glass seed took effect; set MPA_ADMIN_NAME, MPA_ADMIN_USERNAME and MPA_ADMIN_PASSWORD so an admin is created on boot")
	}
}

func seedAdmin(ctx context.Context, repo domain.AdminSeedRepo, cfg AdminConfig, log zerolog.Logger) error {
	result, err := repo.SeedAdmin(ctx, cfg.Name, cfg.Username, nil)
	if err != nil {
		return err
	}

	if result.NeedsPasswordHash {
		hash, err := auth.HashPassword(cfg.Password)
		if err != nil {
			return fmt.Errorf("hash seeded password: %w", err)
		}
		result, err = repo.SeedAdmin(ctx, cfg.Name, cfg.Username, &hash)
		if err != nil {
			return err
		}
		if result.NeedsPasswordHash {
			return fmt.Errorf("%w: seed store requested a password hash twice", domain.ErrInvalidState)
		}
	}

	if len(result.AmbiguousNames) > 0 {
		// Several members fold to the name: skip rather than guess or fail boot,
		// but still warn if no admin exists.
		log.Warn().
			Str("configured_name", cfg.Name).
			Strs("matches", result.AmbiguousNames).
			Msg("break-glass seed skipped: MPA_ADMIN_NAME matches several members case-insensitively, refusing to guess which one to adopt")
		warnIfNoAdmins(ctx, repo, log)
		return nil
	}

	if result.Created {
		log.Info().
			Int("member_id", result.UserID).
			Str("name", result.Name).
			Str("username", cfg.Username).
			Msg("break-glass seed created an admin member with a local login")
		return nil
	}
	if result.Promoted {
		log.Info().Int("member_id", result.UserID).Str("name", result.Name).
			Msg("break-glass seed promoted an existing member to admin")
	}
	if result.LoginPreserved {
		log.Info().Int("member_id", result.UserID).Str("name", result.Name).
			Msg("break-glass seed left an existing local login untouched")
		return nil
	}
	if result.LoginCreated {
		log.Info().
			Int("member_id", result.UserID).
			Str("name", result.Name).
			Str("username", cfg.Username).
			Msg("break-glass seed attached a local login to an existing admin member")
		return nil
	}
	return fmt.Errorf("%w: seed store returned no committed outcome", domain.ErrInvalidState)
}
