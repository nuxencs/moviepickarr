# Database review — July 2026

Full review of the SQLite layer (schema, indexes, connection handling, query
paths) from both a brownfield and greenfield angle, and the changes shipped
from it (migration `007` + pool split). Verified against a copy of the
production DB (205 movies / 141 watched / 3 users / 3967 credits).

## The bug that motivated it: three timestamp formats

SQLite's `TIMESTAMP` is just TEXT. The DB had accumulated three shapes:

| source | example |
|---|---|
| `DEFAULT CURRENT_TIMESTAMP` | `2026-06-27 11:20:58` (UTC, bare) |
| Go `time.Time` bound directly (local) | `2025-11-17 15:08:44 +0100 CET` |
| Go `time.Time` bound directly (UTC) | `2026-03-06 21:28:41.97003533 +0000 UTC` |

`ORDER BY watched_at DESC` is a **text sort of local-time prefixes**, so the
watched list missorted rows across format boundaries and DST flips
(CET/CEST). `enriched_at` comparisons had already worked around this by hand;
the movies table never got the same care.

### Decision: INTEGER epoch, STRICT tables

Canonical storage is INTEGER unix epoch seconds (UTC by definition): sorts
correctly as a number and, with the owning tables rebuilt as STRICT, a stray
TEXT bind is rejected by the type system outright. TEXT UTC
(`YYYY-MM-DD HH:MM:SS` plus format CHECKs) was the first pick for ad-hoc
readability, but was revised before shipping: format CHECKs guard shape, not
zone, while STRICT + INTEGER turns the wrong bind into a hard type error. Go
side: `db.ToUnix` / `db.ToUnixPtr` are the **only** way timestamps get bound
and `db.FromUnix` the only way they are scanned; in-SQL stamps use
`unixepoch()`, not `CURRENT_TIMESTAMP`.

## Migration 007 (`007_timestamps_and_constraints.sql`)

1. **Normalize** all `movies` / `users` timestamps: wall-clock prefix +
   `±HHMM` token → ISO offset string → `unixepoch()` folds to UTC epoch
   seconds. Fractional seconds are dropped (second precision).
2. **Rebuild `movies`** as STRICT with:
   - INTEGER epoch columns for `added_at` / `watched_at`
   - `CHECK ((status = 'watched') = (watched_at IS NOT NULL))` — verified 0
     violations in prod before shipping
   - `added_by_id … ON DELETE RESTRICT` (was CASCADE): deleting a member must
     not erase group watch history. Repo maps the FK error (by driver code via
     `db.IsForeignKeyViolation`; RESTRICT surfaces as extended code 1811, not
     787) to `domain.ErrConflict` → 409. Tombstone-reassignment ("Deleted
     member" user inherits watched rows, pool/stash dropped, excluded from
     picker rotation) was designed but deferred until user deletion is a real
     feature.
   - partial `UNIQUE` on `tmdb_id` (NULLs free) — verified 0 dupes in prod.
     Application paths handle the conflict: a duplicate add deletes its
     just-inserted stash row and returns 409; enrichment treats a tmdb-id
     conflict as non-fatal so the row still gets metadata/credits and leaves
     the backlog.
   - partial `UNIQUE` on `status='current'` — at most one current movie,
     enforced at the DB so the pick check-then-act race (widened by the read
     pool) can't yield two currents.
   - the rebuild preserves the AUTOINCREMENT high-water mark via
     `sqlite_sequence` (the plain copy would reset it to max(id) and reuse
     deleted ids — a hazard for SSE/pick identity keyed on movieID).
   - the rebuilt tables are STRICT, so the driver never guesses a format:
     repos bind via `db.ToUnix` and scan via `db.FromUnix` (`unixTimePtr`).
3. `users_touch_updated_at` trigger — `updated_at` was write-once dead weight.
4. Dropped `movie_metadata_enriched_at_index` — `NeedsEnrichment`'s OR-shaped
   predicate forces a scan (EXPLAIN QUERY PLAN confirmed), so the index was
   pure write overhead.

**Runner support**: `-- migrate:fk_off` first-line marker. `PRAGMA
foreign_keys` is a silent no-op inside a transaction, and `DROP TABLE` with
FKs on would cascade into `movie_metadata`/`movie_credits`. The runner pins a
connection, disables FKs around the tx, and runs `foreign_key_check` before
commit.

## Connection pool split

`SetMaxOpenConns(1)` serialized every dashboard read behind any in-flight
enrichment write (`ReplaceCredits` is ~50 statements in one tx). WAL exists
precisely so readers don't wait: `db.Pool` now holds a 1-conn `Write` handle
and a 4-conn `Read` pool over the same file. `Pool.Close` runs
`PRAGMA optimize` (SQLite's recommended pre-close hygiene).

## Deliberately NOT changed

- `genres` as JSON TEXT — display-only, filtering happens in Go
  (see data-fetching findings: pagination/SQL-filtering is the wrong move at
  this scale).
- `ORDER BY RANDOM()` for picks — fine until ~100k rows.
- Singleton `next_picker` table — typed FK beats a settings string.
- `movie_metadata` as 1:1 side-table — isolates enrichment writes.
- `movies`↔`users` stays a plain FK, **not** a junction table: "added by" is
  genuinely 1:N (a junction with UNIQUE(movie_id) is a foreign key in
  costume). Future M:N facts (votes, attendance) get their own relation
  tables, like `movie_credits` already does.

## Verification

- `internal/db/migration_timestamps_test.go` — 007 backfill against seeded
  legacy formats + every new constraint.
- `scripts/verify_migration_007.go` — runs the chain against a **copy of a
  production DB**; asserts per-row Go-computed UTC truth, counts, watched
  order, `integrity_check`, `foreign_key_check`, index shape. Run it against
  a prod backup before deploying.
- Booted the real binary on the migrated copy: watched list strictly
  chronological, all endpoints healthy.
