package devfixtures

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"moviepickarr/internal/auth"
	"moviepickarr/internal/db"
)

// querier is the read surface IsEmpty needs: satisfied by both *sql.DB and
// *sql.Tx, so the guard can run on either the pool or inside a transaction.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// IsEmpty reports whether the DB holds no developer data yet. A freshly
// migrated DB is "empty" despite carrying the next_up singleton and the
// pool_locked setting (both seeded by migration 001), so the guard keys off the
// two tables fixtures actually own: users and movies.
func IsEmpty(ctx context.Context, q querier) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx,
		"SELECT (SELECT COUNT(*) FROM users) + (SELECT COUNT(*) FROM movies)").Scan(&n)
	if err != nil {
		return false, fmt.Errorf("count existing rows: %w", err)
	}
	return n == 0, nil
}

// Wipe clears all developer data inside tx so a reset reloads from empty.
//
// Deletes run child-before-parent because foreign_keys is ON and movies.added_by
// is ON DELETE RESTRICT (deleting a member with movies would otherwise error).
// The autoincrement high-water marks for users and movies are reset too, so a
// reseed always produces the same ids: the determinism the fixtures promise.
// The next_up singleton row is kept (its user_id is nulled) and pool_locked is
// left for Apply to set, matching the post-migration baseline.
func Wipe(ctx context.Context, tx *sql.Tx) error {
	stmts := []string{
		"DELETE FROM movie_credits",
		"DELETE FROM movie_metadata",
		"DELETE FROM movies",
		"DELETE FROM sessions",
		"DELETE FROM invites",
		"DELETE FROM oidc_identities",
		"DELETE FROM local_accounts",
		"UPDATE next_up SET user_id = NULL WHERE id = 1",
		"DELETE FROM users",
		"DELETE FROM people",
		"DELETE FROM sqlite_sequence WHERE name IN ('users', 'movies')",
	}
	for _, s := range stmts {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("wipe (%s): %w", s, err)
		}
	}
	return nil
}

// Apply writes the whole plan inside tx. Members come first so their ids exist
// for the movie, login, and next-up foreign keys; movies, logins, the turn
// holder, and the pool lock follow. now stamps the archived member's
// archived_at. Passwords are hashed here (not in the plan) because hashing is
// an app concern the pure builder should not carry.
func Apply(ctx context.Context, tx *sql.Tx, plan Plan, now time.Time) error {
	memberIDs := make([]int64, len(plan.Members))

	for i, m := range plan.Members {
		var archivedAt any
		if m.Archived {
			archivedAt = db.ToUnix(now)
		}
		res, err := tx.ExecContext(ctx,
			"INSERT INTO users (name, role, archived_at) VALUES (?, ?, ?)",
			m.Name, m.Role, archivedAt)
		if err != nil {
			return fmt.Errorf("insert member %q: %w", m.Name, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("member %q id: %w", m.Name, err)
		}
		memberIDs[i] = id

		if m.Login != nil {
			hash, err := auth.HashPassword(m.Login.Password)
			if err != nil {
				return fmt.Errorf("hash password for %q: %w", m.Name, err)
			}
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO local_accounts (user_id, username, password_hash) VALUES (?, ?, ?)",
				id, m.Login.Username, hash); err != nil {
				return fmt.Errorf("insert login for %q: %w", m.Name, err)
			}
		}
	}

	for _, mv := range plan.Movies {
		if mv.AdderIndex < 0 || mv.AdderIndex >= len(memberIDs) {
			return fmt.Errorf("movie %q references member index %d out of range", mv.Title, mv.AdderIndex)
		}
		_, err := tx.ExecContext(ctx,
			"INSERT INTO movies (title, status, added_at, added_by_id, watched_at, tmdb_id) VALUES (?, ?, ?, ?, ?, ?)",
			mv.Title, string(mv.Status), db.ToUnix(mv.AddedAt), memberIDs[mv.AdderIndex],
			db.ToUnixPtr(mv.WatchedAt), mv.TMDBID)
		if err != nil {
			return fmt.Errorf("insert movie %q: %w", mv.Title, err)
		}
	}

	if plan.NextUpIndex >= 0 && plan.NextUpIndex < len(memberIDs) {
		if _, err := tx.ExecContext(ctx,
			"UPDATE next_up SET user_id = ? WHERE id = 1", memberIDs[plan.NextUpIndex]); err != nil {
			return fmt.Errorf("set next-up: %w", err)
		}
	}

	locked := "false"
	if plan.PoolLocked {
		locked = "true"
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO settings (key, value) VALUES ('pool_locked', ?) "+
			"ON CONFLICT(key) DO UPDATE SET value = excluded.value", locked); err != nil {
		return fmt.Errorf("set pool_locked: %w", err)
	}

	return nil
}
