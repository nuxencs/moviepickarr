-- 005_drop_movie_link.sql
-- The displayed link is now derived from tmdb_id/imdb_id; the link column is no
-- longer authoritative. Backfill imdb_id from any legacy IMDb link that still
-- carries a movie's only identity, then drop the column. Runs in one tx, so the
-- backfill always precedes the drop atomically (rolled back together on error).
--
-- Pre-ship check (run against production data first; should return no rows):
--   SELECT id, title, link FROM movies
--   WHERE tmdb_id IS NULL AND imdb_id IS NULL
--     AND link NOT LIKE '%imdb.com/title/tt%';
-- Any such "orphan" row would lose its only reference when the column drops.

-- Links were normalized by sanitizeLink to ".../title/{ttid}/", so the id sits
-- between "/title/" and the next "/". Cuts at the trailing slash, so it handles
-- both 7- and 8-digit ids.
UPDATE movies
SET imdb_id = substr(
        link,
        instr(link, '/title/') + 7,
        instr(substr(link, instr(link, '/title/') + 7), '/') - 1
    )
WHERE imdb_id IS NULL
  AND tmdb_id IS NULL
  AND link LIKE '%imdb.com/title/tt%';

ALTER TABLE movies DROP COLUMN link;
