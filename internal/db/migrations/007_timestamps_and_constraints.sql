-- migrate:fk_off
-- 007: epoch timestamps + integrity constraints.
--
-- Timestamps were stored as TEXT in three incompatible shapes: bare
-- CURRENT_TIMESTAMP ("YYYY-MM-DD HH:MM:SS", UTC) and Go's default time.Time
-- string ("YYYY-MM-DD HH:MM:SS[.frac] +HHMM TZ") in both local time and UTC —
-- so ORDER BY watched_at (a text sort) missorted rows across formats and DST
-- changes. Convert every timestamp to INTEGER unix epoch seconds (db.ToUnix
-- is the Go-side counterpart) and rebuild the owning tables as STRICT, so a
-- stray raw time.Time bind is rejected by the type system instead of being
-- silently stored as text. The movies rebuild also locks in:
--   * added_by FK becomes ON DELETE RESTRICT: deleting a member must not
--     silently erase group watch history (tombstone reassignment is a
--     possible future feature)
--   * status <-> watched_at coupling: watched rows always carry a time
--   * partial UNIQUE on tmdb_id: the same movie can't be added twice
--     (a rewatch re-picks the existing row)
--   * partial UNIQUE on status='current': at most one current movie, so the
--     pick check-then-act race can't yield two
-- The rebuilds need the fk_off marker above: DROP TABLE with foreign_keys=ON
-- performs an implicit DELETE that would cascade across the FK graph.

-- --- Step 1: normalize legacy Go-formatted TEXT values ---------------------
-- substr(x, 1, 19) is the wall-clock prefix (any fractional seconds are
-- dropped; second precision is plenty here). The ±HHMM token after the last
-- space-sign becomes an ISO ±HH:MM suffix, which unixepoch()/datetime() fold
-- into UTC. Bare CURRENT_TIMESTAMP values need no rewrite — unixepoch()
-- (step 2) already reads them as UTC.
UPDATE movies
SET added_at = substr(added_at, 1, 19)
        || substr(added_at, instr(added_at, ' +') + 1, 3)
        || ':' || substr(added_at, instr(added_at, ' +') + 4, 2)
WHERE added_at LIKE '% +%';

UPDATE movies
SET added_at = substr(added_at, 1, 19)
        || substr(added_at, instr(added_at, ' -') + 1, 3)
        || ':' || substr(added_at, instr(added_at, ' -') + 4, 2)
WHERE added_at LIKE '% -%';

UPDATE movies
SET watched_at = substr(watched_at, 1, 19)
        || substr(watched_at, instr(watched_at, ' +') + 1, 3)
        || ':' || substr(watched_at, instr(watched_at, ' +') + 4, 2)
WHERE watched_at LIKE '% +%';

UPDATE movies
SET watched_at = substr(watched_at, 1, 19)
        || substr(watched_at, instr(watched_at, ' -') + 1, 3)
        || ':' || substr(watched_at, instr(watched_at, ' -') + 4, 2)
WHERE watched_at LIKE '% -%';

UPDATE users
SET created_at = substr(created_at, 1, 19)
        || substr(created_at, instr(created_at, ' +') + 1, 3)
        || ':' || substr(created_at, instr(created_at, ' +') + 4, 2)
WHERE created_at LIKE '% +%';

UPDATE users
SET created_at = substr(created_at, 1, 19)
        || substr(created_at, instr(created_at, ' -') + 1, 3)
        || ':' || substr(created_at, instr(created_at, ' -') + 4, 2)
WHERE created_at LIKE '% -%';

UPDATE users
SET updated_at = substr(updated_at, 1, 19)
        || substr(updated_at, instr(updated_at, ' +') + 1, 3)
        || ':' || substr(updated_at, instr(updated_at, ' +') + 4, 2)
WHERE updated_at LIKE '% +%';

UPDATE users
SET updated_at = substr(updated_at, 1, 19)
        || substr(updated_at, instr(updated_at, ' -') + 1, 3)
        || ':' || substr(updated_at, instr(updated_at, ' -') + 4, 2)
WHERE updated_at LIKE '% -%';

-- Pre-clean rows that would fail the new status <-> watched_at CHECK.
-- Production data was verified clean (0 violations), but other installs may
-- carry legacy Bolt-imported rows where the pairing drifted. The rule below is
-- lossless where it matters: watched history keeps a time (falling back to the
-- added time), non-watched rows drop a meaningless leftover watched_at.
UPDATE movies SET watched_at = NULL
WHERE status != 'watched' AND watched_at IS NOT NULL;

UPDATE movies SET watched_at = added_at
WHERE status = 'watched' AND watched_at IS NULL;

-- --- Step 2: rebuild users (epoch, STRICT) ----------------------------------
CREATE TABLE users_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()), -- unix epoch seconds, UTC
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
) STRICT;

INSERT INTO users_new (id, name, created_at, updated_at)
SELECT id, name, unixepoch(created_at), unixepoch(updated_at)
FROM users;

-- Preserve the AUTOINCREMENT high-water mark across the rebuild: the copy
-- seeds the new table's sequence with max(copied id), which is LOWER than the
-- old sequence whenever the highest-id row was deleted — new rows would then
-- reuse dead ids. The INSERT covers the empty-table edge (no seq row created
-- by the copy). Same pattern for movies below.
INSERT INTO sqlite_sequence (name, seq)
SELECT 'users_new', (SELECT seq FROM sqlite_sequence WHERE name = 'users')
WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = 'users_new')
  AND EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = 'users');

UPDATE sqlite_sequence
SET seq = (SELECT seq FROM sqlite_sequence WHERE name = 'users')
WHERE name = 'users_new'
  AND seq < (SELECT seq FROM sqlite_sequence WHERE name = 'users');

DROP TABLE users;
ALTER TABLE users_new RENAME TO users;

-- users.updated_at was write-once dead weight: nothing ever wrote it after
-- insert. Keep it honest via trigger, scoped to "OF name" so the trigger's
-- own UPDATE can't recurse. (Recreated here because DROP TABLE users above
-- would have removed any earlier version.)
CREATE TRIGGER users_touch_updated_at
AFTER UPDATE OF name ON users
FOR EACH ROW
BEGIN
    UPDATE users SET updated_at = unixepoch() WHERE id = NEW.id;
END;

-- --- Step 3: rebuild movies (epoch, STRICT, constraints) --------------------
CREATE TABLE movies_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pool', 'stash', 'current', 'watched')),
    added_at INTEGER NOT NULL DEFAULT (unixepoch()), -- unix epoch seconds, UTC
    added_by_id INTEGER NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    watched_at INTEGER,
    tmdb_id INTEGER,
    imdb_id TEXT,
    CHECK ((status = 'watched') = (watched_at IS NOT NULL))
) STRICT;

INSERT INTO movies_new (id, title, status, added_at, added_by_id, watched_at, tmdb_id, imdb_id)
SELECT id, title, status, unixepoch(added_at), added_by_id, unixepoch(watched_at), tmdb_id, imdb_id
FROM movies;

INSERT INTO sqlite_sequence (name, seq)
SELECT 'movies_new', (SELECT seq FROM sqlite_sequence WHERE name = 'movies')
WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = 'movies_new')
  AND EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = 'movies');

UPDATE sqlite_sequence
SET seq = (SELECT seq FROM sqlite_sequence WHERE name = 'movies')
WHERE name = 'movies_new'
  AND seq < (SELECT seq FROM sqlite_sequence WHERE name = 'movies');

DROP TABLE movies;
ALTER TABLE movies_new RENAME TO movies;

CREATE INDEX movies_added_by_id_status_index ON movies (added_by_id, status);
CREATE INDEX movies_status_index ON movies (status);
CREATE UNIQUE INDEX movies_tmdb_id_unique ON movies (tmdb_id) WHERE tmdb_id IS NOT NULL;
CREATE UNIQUE INDEX movies_single_current ON movies (status) WHERE status = 'current';

-- --- Step 4: rebuild movie_metadata (epoch, STRICT) -------------------------
-- Its timestamps were always bare CURRENT_TIMESTAMP text (single format), but
-- one storage convention beats two. NULL credits_refreshed_at keeps meaning
-- "credits never ingested" (backfill marker).
CREATE TABLE movie_metadata_new (
    movie_id      INTEGER PRIMARY KEY,
    overview      TEXT    NOT NULL DEFAULT '',
    poster_path   TEXT,
    backdrop_path TEXT,
    release_date  TEXT    NOT NULL DEFAULT '',
    runtime       INTEGER NOT NULL DEFAULT 0,
    genres        TEXT    NOT NULL DEFAULT '[]',
    vote_average  REAL    NOT NULL DEFAULT 0,
    vote_count    INTEGER NOT NULL DEFAULT 0,
    tagline       TEXT    NOT NULL DEFAULT '',
    enriched_at   INTEGER NOT NULL DEFAULT (unixepoch()), -- unix epoch seconds, UTC
    credits_refreshed_at INTEGER,
    FOREIGN KEY (movie_id) REFERENCES movies (id)
        ON UPDATE CASCADE ON DELETE CASCADE
) STRICT;

INSERT INTO movie_metadata_new (
    movie_id, overview, poster_path, backdrop_path, release_date, runtime,
    genres, vote_average, vote_count, tagline, enriched_at, credits_refreshed_at
)
SELECT
    movie_id, overview, poster_path, backdrop_path, release_date, runtime,
    genres, vote_average, vote_count, tagline,
    unixepoch(enriched_at), unixepoch(credits_refreshed_at)
FROM movie_metadata;

DROP TABLE movie_metadata;
ALTER TABLE movie_metadata_new RENAME TO movie_metadata;

-- The old movie_metadata_enriched_at_index is NOT recreated: NeedsEnrichment's
-- OR-shaped predicate forces a scan of movies with a PK probe into
-- movie_metadata (EXPLAIN QUERY PLAN confirms), so it was pure write overhead.
