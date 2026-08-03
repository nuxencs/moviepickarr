-- 011: stable public handles for per-device session revocation.
--
-- SQLite may reuse an INTEGER PRIMARY KEY after its highest row is deleted.
-- Those row ids are store-local implementation details, so exposing one as a
-- revoke target lets a stale browser action delete a newer session that later
-- received the same id. Keep the integer key for internal joins and activity
-- updates, but address member-facing actions with a random, immutable handle.

CREATE TABLE sessions_new (
    id           INTEGER PRIMARY KEY,
    public_id    TEXT    NOT NULL UNIQUE,
    token_hash   TEXT    NOT NULL UNIQUE,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL DEFAULT (unixepoch()),
    user_agent   TEXT,
    created_at   INTEGER NOT NULL DEFAULT (unixepoch())
) STRICT;

INSERT INTO sessions_new (
    id, public_id, token_hash, user_id, expires_at, last_seen_at,
    user_agent, created_at
)
SELECT
    id, lower(hex(randomblob(16))), token_hash, user_id, expires_at,
    last_seen_at, user_agent, created_at
FROM sessions;

DROP TABLE sessions;
ALTER TABLE sessions_new RENAME TO sessions;

CREATE INDEX sessions_user_id_index ON sessions (user_id);
CREATE INDEX sessions_expires_at_index ON sessions (expires_at);
