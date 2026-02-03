PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS movies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    link TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pool', 'stash', 'current', 'watched')),
    added_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    added_by_id INTEGER NOT NULL,
    watched_at TIMESTAMP,
    FOREIGN KEY (added_by_id) REFERENCES users(id)
        ON UPDATE CASCADE ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS next_picker (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    user_id INTEGER,
    FOREIGN KEY (user_id) REFERENCES users(id)
        ON UPDATE CASCADE ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS movies_added_by_id_status_index ON movies (added_by_id, status);
CREATE INDEX IF NOT EXISTS movies_status_index ON movies (status);

INSERT OR IGNORE INTO next_picker (id, user_id) VALUES (1, NULL);
INSERT OR IGNORE INTO settings (key, value) VALUES ('pool_locked', 'false');
