-- Normalized, trimmed TMDB credits. People are stored once, ever, keyed by
-- their TMDB person id; movie_credits joins them to movies as cast or crew.
CREATE TABLE IF NOT EXISTS people (
    id           INTEGER PRIMARY KEY,   -- TMDB person id
    name         TEXT NOT NULL,
    profile_path TEXT
);

-- The PK handles a person appearing as both cast and crew on the same movie
-- (kind differs) and as crew with several jobs (job differs).
CREATE TABLE IF NOT EXISTS movie_credits (
    movie_id   INTEGER NOT NULL,
    person_id  INTEGER NOT NULL,
    kind       TEXT    NOT NULL CHECK (kind IN ('cast', 'crew')),
    character  TEXT    NOT NULL DEFAULT '',
    job        TEXT    NOT NULL DEFAULT '',
    department TEXT    NOT NULL DEFAULT '',
    cast_order INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (movie_id, person_id, kind, job),
    FOREIGN KEY (movie_id) REFERENCES movies(id) ON UPDATE CASCADE ON DELETE CASCADE,
    FOREIGN KEY (person_id) REFERENCES people(id) ON UPDATE CASCADE ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS movie_credits_person_id_index ON movie_credits (person_id);

-- NULL = credits never ingested; enrichment drain re-picks these up (backfill).
ALTER TABLE movie_metadata ADD COLUMN credits_refreshed_at TIMESTAMP;
