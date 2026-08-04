-- 012: stable public invite handles and one current generation per member.
--
-- Integer row ids are store-local and SQLite may reuse them after deletion.
-- Admin actions therefore address an immutable random handle instead. The
-- partial unique index makes the current-generation rule a database invariant:
-- at most one unused, unrevoked invite exists for a member, expired or not.

-- Older builds could leave several unused, unrevoked generations after each
-- expiry. Retain the newest invite overall as the member's current generation.
-- If a newer used or revoked generation exists, every older outstanding row is
-- retired instead of allowing a stale invite to resurface.
UPDATE invites AS older
SET revoked_at = unixepoch()
WHERE older.used_at IS NULL
  AND older.revoked_at IS NULL
  AND EXISTS (
      SELECT 1
      FROM invites AS newer
      WHERE newer.user_id = older.user_id
        AND (
          newer.created_at > older.created_at
          OR (newer.created_at = older.created_at AND newer.id > older.id)
        )
  );

-- An OIDC identity created outside the claim flow made an onboarding invite
-- unnecessary, but older builds left it live and merely hid it from the
-- overview. Retire those links before enforcing the current-generation
-- invariant. A local-account invite must stay current: older builds also used
-- this row shape for an explicit password reset, and local-account presence is
-- what makes the claim password-only recovery.
UPDATE invites
SET revoked_at = unixepoch()
WHERE used_at IS NULL
  AND revoked_at IS NULL
  AND EXISTS (SELECT 1 FROM oidc_identities WHERE user_id = invites.user_id)
  AND NOT EXISTS (SELECT 1 FROM local_accounts WHERE user_id = invites.user_id);

CREATE TABLE invites_new (
    id         INTEGER PRIMARY KEY,
    public_id  TEXT    NOT NULL UNIQUE,
    user_id    INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash TEXT    NOT NULL UNIQUE,
    expires_at INTEGER NOT NULL,
    used_at    INTEGER,
    revoked_at INTEGER,
    created_by INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
) STRICT;

INSERT INTO invites_new (
    id, public_id, user_id, token_hash, expires_at, used_at, revoked_at,
    created_by, created_at
)
SELECT
    id, lower(hex(randomblob(16))), user_id, token_hash, expires_at, used_at,
    revoked_at, created_by, created_at
FROM invites;

DROP TABLE invites;
ALTER TABLE invites_new RENAME TO invites;

CREATE INDEX invites_user_id_index ON invites (user_id);
CREATE UNIQUE INDEX invites_one_current_per_user
    ON invites (user_id)
    WHERE used_at IS NULL AND revoked_at IS NULL;
