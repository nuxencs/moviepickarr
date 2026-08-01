-- 010: one library row per IMDb identity.
--
-- Existing duplicate rows are retained. The lowest movie id keeps the IMDb id;
-- later rows lose only that identifier and are recorded for manual review.
-- Matching and the final constraint both use NOCASE, so cleanup and runtime
-- enforcement cannot disagree about case variants.

CREATE TABLE movie_imdb_conflicts (
    movie_id           INTEGER PRIMARY KEY
        REFERENCES movies (id) ON DELETE CASCADE,
    imdb_id            TEXT    NOT NULL,
    canonical_movie_id INTEGER
        REFERENCES movies (id) ON DELETE SET NULL,
    detected_at        INTEGER NOT NULL DEFAULT (unixepoch())
) STRICT;

CREATE INDEX movie_imdb_conflicts_canonical_movie_id_index
    ON movie_imdb_conflicts (canonical_movie_id);

-- Canonicalize before ranking so padding and case cannot split one IMDb id
-- into separate groups. Skip already-canonical values to avoid needless row
-- writes on the common path.
UPDATE movies
SET imdb_id = lower(trim(imdb_id))
WHERE imdb_id IS NOT NULL
  AND imdb_id != lower(trim(imdb_id));

-- Blank legacy values carry no usable identity and would otherwise consume a
-- unique-index key.
UPDATE movies
SET imdb_id = NULL
WHERE imdb_id IS NOT NULL
  AND trim(imdb_id) = '';

WITH ranked AS (
    SELECT
        id AS movie_id,
        imdb_id,
        MIN(id) OVER (
            PARTITION BY imdb_id COLLATE NOCASE
        ) AS canonical_movie_id
    FROM movies
    WHERE imdb_id IS NOT NULL
)
INSERT INTO movie_imdb_conflicts (movie_id, imdb_id, canonical_movie_id)
SELECT movie_id, imdb_id, canonical_movie_id
FROM ranked
WHERE movie_id != canonical_movie_id
ORDER BY movie_id;

UPDATE movies
SET imdb_id = NULL
WHERE id IN (SELECT movie_id FROM movie_imdb_conflicts);

CREATE UNIQUE INDEX movies_imdb_id_unique
    ON movies (imdb_id COLLATE NOCASE)
    WHERE imdb_id IS NOT NULL;
