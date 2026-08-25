-- migrate:fk_off
-- 017: Wildcard watches beside a revealed Current draw.

-- The movie status identifies the one selected Active wildcard. The separate
-- wildcards table preserves its host draw and its terminal history.
CREATE TABLE movies_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    status TEXT NOT NULL CHECK (
        status IN ('pool', 'stash', 'current', 'wildcard', 'watched')
    ),
    added_at INTEGER NOT NULL DEFAULT (unixepoch()),
    added_by_id INTEGER NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    watched_at INTEGER,
    tmdb_id INTEGER,
    imdb_id TEXT,
    CHECK ((status = 'watched') = (watched_at IS NOT NULL))
) STRICT;

INSERT INTO movies_new (
    id, title, status, added_at, added_by_id, watched_at, tmdb_id, imdb_id
)
SELECT id, title, status, added_at, added_by_id, watched_at, tmdb_id, imdb_id
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
CREATE UNIQUE INDEX movies_tmdb_id_unique
    ON movies (tmdb_id) WHERE tmdb_id IS NOT NULL;
CREATE UNIQUE INDEX movies_imdb_id_unique
    ON movies (imdb_id COLLATE NOCASE) WHERE imdb_id IS NOT NULL;
CREATE UNIQUE INDEX movies_single_current
    ON movies (status) WHERE status = 'current';
CREATE UNIQUE INDEX movies_single_wildcard
    ON movies (status) WHERE status = 'wildcard';

CREATE TABLE wildcards (
    id                   INTEGER PRIMARY KEY,
    host_movie_id        INTEGER NOT NULL REFERENCES movies (id) ON DELETE RESTRICT,
    movie_id             INTEGER NOT NULL REFERENCES movies (id) ON DELETE RESTRICT,
    selected_by_id       INTEGER REFERENCES users (id) ON DELETE SET NULL,
    canceled_by_id       INTEGER REFERENCES users (id) ON DELETE SET NULL,
    source_status        TEXT    NOT NULL CHECK (source_status IN ('pool', 'stash')),
    created_for_wildcard INTEGER NOT NULL DEFAULT 0 CHECK (created_for_wildcard IN (0, 1)),
    status               TEXT    NOT NULL DEFAULT 'active' CHECK (
        status IN ('active', 'watched', 'canceled')
    ),
    selected_at          INTEGER NOT NULL,
    watched_at           INTEGER,
    canceled_at          INTEGER,
    CHECK (host_movie_id != movie_id),
    CHECK ((status = 'active') = (watched_at IS NULL AND canceled_at IS NULL)),
    CHECK ((status = 'watched') = (watched_at IS NOT NULL)),
    CHECK ((status = 'canceled') = (canceled_at IS NOT NULL))
) STRICT;

CREATE UNIQUE INDEX wildcards_single_active
    ON wildcards (status) WHERE status = 'active';
CREATE INDEX wildcards_host_history_index
    ON wildcards (host_movie_id, selected_at DESC, id DESC);
CREATE INDEX wildcards_movie_history_index
    ON wildcards (movie_id, selected_at DESC, id DESC);

-- Existing Acquisition code keeps its terminal mechanics. A canceled Wildcard
-- stores status='abandoned' internally so every worker, target lock, and webhook
-- safety gate remains closed. canceled_at distinguishes the member cancellation
-- from an Admin abandonment on the API and in history.
ALTER TABLE radarr_acquisitions
    ADD COLUMN source TEXT NOT NULL DEFAULT 'draw'
        CHECK (source IN ('draw', 'wildcard'));
ALTER TABLE radarr_acquisitions
    ADD COLUMN wildcard_id INTEGER REFERENCES wildcards (id) ON DELETE RESTRICT;
ALTER TABLE radarr_acquisitions ADD COLUMN canceled_at INTEGER;
ALTER TABLE radarr_acquisitions
    ADD COLUMN canceled_by INTEGER REFERENCES users (id) ON DELETE SET NULL;

CREATE UNIQUE INDEX radarr_acquisitions_wildcard_unique
    ON radarr_acquisitions (wildcard_id) WHERE wildcard_id IS NOT NULL;
