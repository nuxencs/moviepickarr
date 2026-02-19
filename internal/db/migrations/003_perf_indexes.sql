WITH ranked_current AS (
    SELECT
        id,
        ROW_NUMBER() OVER (ORDER BY added_at DESC, id DESC) AS row_num
    FROM movies
    WHERE status = 'current'
)
UPDATE movies
SET status = 'pool'
WHERE id IN (
    SELECT id
    FROM ranked_current
    WHERE row_num > 1
);

DROP INDEX IF EXISTS movies_added_by_id_status_index;
DROP INDEX IF EXISTS movies_status_index;

CREATE INDEX IF NOT EXISTS movies_added_by_status_title_index ON movies (added_by_id, status, title);
CREATE INDEX IF NOT EXISTS movies_status_title_index ON movies (status, title);
CREATE INDEX IF NOT EXISTS movies_status_watched_at_title_index ON movies (status, watched_at DESC, title);
CREATE UNIQUE INDEX IF NOT EXISTS movies_current_unique_index ON movies (status) WHERE status = 'current';
