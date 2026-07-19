-- 009: auth + per-member identity data model.
--
-- Lands the whole authentication schema in one forward-only step so a
-- production DB rolls forward safely and every existing member survives as a
-- login-less placeholder. Pure additions only: two ALTER TABLE ADD COLUMN on
-- users (STRICT-legal, no rebuild) plus four fresh CREATE tables. Nothing is
-- rebuilt and no table is dropped, so this migration needs no fk_off marker
-- (unlike 007); RunMigrationsWithBackup auto-snapshots the DB before applying.
--
-- Every table is STRICT and every timestamp is INTEGER unix-epoch seconds with
-- an unixepoch() default, matching the post-007 shape (db.ToUnix is the Go-side
-- counterpart). Link-state is derived, never stored: a member holds a local
-- login iff a local_accounts row exists and a linked identity iff an
-- oidc_identities row exists — there is no pending/can_login flag. archived_at
-- is a membership-lifecycle axis, distinct from login capability.
--
-- Numbering: this is 009. The 002 gap is the reverted auth schema (003 dropped
-- it); this is a clean fresh start, not a revival of that shape.

-- --- Existing members: role + archival, without a rebuild --------------------
-- Every current row becomes an active member (role='member', archived_at NULL)
-- with no credential rows: a credential-less placeholder, re-onboarded later
-- with an invite. role is app-owned and never derived from a credential.
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'member'
    CHECK (role IN ('member', 'admin'));
ALTER TABLE users ADD COLUMN archived_at INTEGER; -- null = active

-- --- local_accounts: username + password login -------------------------------
-- One row per member with a local login; user_id is the PK, so at most one
-- local login per member. Username is globally unique case-insensitively
-- (NOCASE) since login treats it as trimmed/case-folded. failed_attempts /
-- locked_until back the self-healing login-throttle lockout.
CREATE TABLE local_accounts (
    user_id         INTEGER PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    username        TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash   TEXT    NOT NULL,
    failed_attempts INTEGER NOT NULL DEFAULT 0,
    locked_until    INTEGER, -- null = not locked
    last_login_at   INTEGER,
    created_at      INTEGER NOT NULL DEFAULT (unixepoch()), -- unix epoch seconds, UTC
    updated_at      INTEGER NOT NULL DEFAULT (unixepoch())
) STRICT;

-- --- oidc_identities: linked SSO identity ------------------------------------
-- user_id UNIQUE enforces the 1:1 member <-> identity rule; (issuer, subject)
-- is the sole match key on login. email/preferred_username are informational
-- snapshots refreshed on each login, never a match or gate key.
CREATE TABLE oidc_identities (
    id                 INTEGER PRIMARY KEY,
    user_id            INTEGER NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    issuer             TEXT    NOT NULL,
    subject            TEXT    NOT NULL,
    email              TEXT, -- nullable snapshot
    preferred_username TEXT, -- nullable snapshot
    last_login_at      INTEGER,
    created_at         INTEGER NOT NULL DEFAULT (unixepoch()), -- unix epoch seconds, UTC
    updated_at         INTEGER NOT NULL DEFAULT (unixepoch()),
    UNIQUE (issuer, subject)
) STRICT;

-- --- sessions: server-side, revocable, one table for both login paths --------
-- token_hash is SHA-256 of the opaque cookie token (the raw token is never
-- stored). expires_at is the absolute cap; last_seen_at drives the idle slide.
-- Indexed on user_id (per-member revoke) and expires_at (expiry sweep).
CREATE TABLE sessions (
    id           INTEGER PRIMARY KEY,
    token_hash   TEXT    NOT NULL UNIQUE,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL DEFAULT (unixepoch()),
    user_agent   TEXT,
    ip           TEXT,
    created_at   INTEGER NOT NULL DEFAULT (unixepoch()) -- unix epoch seconds, UTC
) STRICT;

CREATE INDEX sessions_user_id_index ON sessions (user_id);
CREATE INDEX sessions_expires_at_index ON sessions (expires_at);

-- --- invites: single-use, expiring claim links -------------------------------
-- token_hash mirrors sessions (hash-only lookup, raw token lives only in the
-- claim URL). Validity is time-derived: used_at IS NULL AND revoked_at IS NULL
-- AND expires_at > now — no status column. created_by keeps the issuing admin
-- but SET NULL on their deletion so the invite outlives them. Indexed on
-- user_id (the one-valid-invite-per-member check is app-enforced).
CREATE TABLE invites (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash TEXT    NOT NULL UNIQUE,
    expires_at INTEGER NOT NULL,
    used_at    INTEGER,
    revoked_at INTEGER,
    created_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()) -- unix epoch seconds, UTC
) STRICT;

CREATE INDEX invites_user_id_index ON invites (user_id);
