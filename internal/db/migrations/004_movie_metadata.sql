-- Stable TMDB/IMDb identity lives on the movie row; the displayed link is
-- derived from these (the `link` column remains only as a fallback).
ALTER TABLE movies ADD COLUMN tmdb_id INTEGER;
ALTER TABLE movies ADD COLUMN imdb_id TEXT;

-- movie_metadata holds only enriched display fields, 1:1 with a movie.
CREATE TABLE IF NOT EXISTS movie_metadata (
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
    enriched_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (movie_id) REFERENCES movies(id)
        ON UPDATE CASCADE ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS movie_metadata_enriched_at_index
    ON movie_metadata (enriched_at);
