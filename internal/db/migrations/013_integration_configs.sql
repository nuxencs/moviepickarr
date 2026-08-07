CREATE TABLE integration_configs (
    integration TEXT PRIMARY KEY,
    revision INTEGER NOT NULL CHECK (revision > 0),
    admin_config TEXT NOT NULL DEFAULT '{}',
    encrypted_secret BLOB,
    state TEXT NOT NULL DEFAULT 'disabled' CHECK (
        state IN ('disabled', 'connected', 'could_not_verify', 'error', 'credential_unavailable')
    ),
    state_reason TEXT NOT NULL DEFAULT '',
    last_checked_at INTEGER,
    next_check_at INTEGER,
    last_successful_refresh_at INTEGER,
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
) STRICT;

INSERT INTO integration_configs (integration, revision)
VALUES ('tmdb', 1);
