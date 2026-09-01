-- migrate:fk_off
-- 018: add the Guest membership role.
--
-- SQLite cannot widen the users.role CHECK in place. Rebuild users while
-- foreign keys are disabled on the pinned migration connection, preserving
-- every id and the AUTOINCREMENT high-water mark. Child tables keep their
-- existing references to the replacement users table.

CREATE TABLE users_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
    role TEXT NOT NULL DEFAULT 'member'
        CHECK (role IN ('member', 'guest', 'admin')),
    archived_at INTEGER
) STRICT;

INSERT INTO users_new (id, name, created_at, updated_at, role, archived_at)
SELECT id, name, created_at, updated_at, role, archived_at
FROM users;

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

CREATE TRIGGER users_touch_updated_at
AFTER UPDATE OF name ON users
FOR EACH ROW
BEGIN
    UPDATE users SET updated_at = unixepoch() WHERE id = NEW.id;
END;
