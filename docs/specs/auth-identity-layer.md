# Auth & per-member identity layer — build spec

Handoff spec for the authentication + per-member identity layer. Every decision
here is locked; this is the assembled, buildable record of the wayfinder map
[Auth & per-member identity layer](https://github.com/nuxencs/moviepickarr/issues/71)
and its design tickets (#72–#83, #85–#87). Hand this to a build effort.

It does not re-argue anything. Where a choice needs its rationale, the source
ticket is linked. Read `CONTEXT.md` first for the identity vocabulary (member,
local login, linked identity, invite, claim, placeholder, session, admin).

Source tickets, one line each, at the end under [Ticket index](#ticket-index).

---

## 1. Scope and frame

Add real authentication to what is today a name-only roster:

- **Local login** (username + password) and **generic OIDC** (single configurable
  provider) side by side, both optional and additive per member.
- **Server-side, revocable sessions** carried by one opaque cookie, one store for
  both login paths.
- **App-owned roles** (`member` / `admin`), never derived from a credential.
- **Invite/claim onboarding**: an admin issues a single-use expiring claim link;
  the member redeems it to set a password and/or link SSO.
- **Env-seeded break-glass admin** as the only bootstrap.

Locked frame (settled during charting, not up for re-litigation):

- The app is a real OIDC relying party (discovery + PKCE), single generic
  provider. Multi-provider is out of scope.
- Member ↔ account is 1:1. A member may hold a local password and/or a linked
  OIDC identity. A member with neither is a roster **placeholder** who cannot log
  in yet.
- `role` is an app-owned enum `{member, admin}`, single-valued, not claim-derived.
  Link-state (is an identity bound to this member) is a separate axis from role.
- Auth is mandatory, always on. The Authelia forward-auth gate in front of the
  app is loosened so `/auth/*`, `/oidc/callback`, and the claim routes are
  reachable without Authelia (proxy config, not app code). The app therefore owns
  its own login defense standalone and never relies on a proxy for rate-limiting.
- No SMTP/email, no self-service registration. Onboarding is invite-only and
  admin-initiated.

Prior art: PR #26 shipped full local auth and was reverted wholesale in #32 as
half-baked. It is a reference, not a template.

---

## 2. Dependencies

| Concern | Choice | Notes |
|---|---|---|
| Password hashing | `github.com/alexedwards/argon2id` | argon2id, PHC encode/verify over `x/crypto/argon2`. Only transitive dep is `golang.org/x/crypto`. ([#72](https://github.com/nuxencs/moviepickarr/issues/72)) |
| OIDC RP | `github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2` | Discovery, PKCE S256, ID-token verification, JWKS cache/rotation. Outbound-only net/http, zero Fiber-adapter cost. ([#73](https://github.com/nuxencs/moviepickarr/issues/73)) |
| Sessions | Hand-rolled table in the existing modernc SQLite DB | Fiber's session/CSRF middleware rejected: can't enumerate/revoke per-member, sliding-only expiry, cgo adapter can't share our `*sql.DB`. ([#74](https://github.com/nuxencs/moviepickarr/issues/74)) |
| AEAD for `mpa_oidc_tx` | AES-256-GCM or nacl secretbox | The one symmetric key in the design. ([#83](https://github.com/nuxencs/moviepickarr/issues/83)) |

**argon2id params:** `m=19456 KiB, t=2, p=1, salt=16, key=32` (OWASP minimum). Store
the full PHC string. Rehash on login when stored params differ from configured
(see §7). Scale up memory if the host has headroom (benchmark for ~250–500 ms per
hash); the rehash-on-login path upgrades existing hashes transparently.

**No refresh tokens.** We mint our own session at the OIDC callback and never
touch provider tokens again. Don't request `offline_access`. No userinfo call;
claims come from the verified ID token only.

---

## 3. Data model

One forward-only migration **`009`**, pure additions + `ALTER TABLE ADD COLUMN`,
no `-- migrate:fk_off` marker, no 12-step rebuild. Everything is `STRICT` +
`INTEGER` unix-epoch (`unixepoch()` defaults, `db.ToUnix` on the Go side) to match
the post-007 DB. The keystone's loose `TIMESTAMP` sketch reconciles to epoch here
because `STRICT` forbids `TIMESTAMP`. `local_accounts`/`sessions` were dropped in
003, so the `CREATE`s don't collide. ([#75](https://github.com/nuxencs/moviepickarr/issues/75), [#81](https://github.com/nuxencs/moviepickarr/issues/81), [#83](https://github.com/nuxencs/moviepickarr/issues/83))

### `users` — two added columns

```sql
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'member'
    CHECK (role IN ('member','admin'));
ALTER TABLE users ADD COLUMN archived_at INTEGER;   -- null = active
```

Both are `ALTER ADD COLUMN` on a `STRICT` table (STRICT-legal, no rebuild). 007's
`users_touch_updated_at` trigger is scoped `AFTER UPDATE OF name`, so role/archive
writes don't bump `updated_at`; bump explicitly if wanted.

### `local_accounts` (STRICT)

```sql
CREATE TABLE local_accounts (
    user_id        INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    username       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash  TEXT NOT NULL,
    failed_attempts INTEGER NOT NULL DEFAULT 0,          -- §12 lockout
    locked_until   INTEGER,                              -- §12 lockout, null = unlocked
    last_login_at  INTEGER,
    created_at     INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at     INTEGER NOT NULL DEFAULT (unixepoch())
) STRICT;
```

`failed_attempts`/`locked_until` go in the initial CREATE (not a later ALTER)
because 009 creates the table fresh.

### `oidc_identities` (STRICT)

```sql
CREATE TABLE oidc_identities (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id            INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    issuer             TEXT NOT NULL,
    subject            TEXT NOT NULL,
    email              TEXT,                             -- nullable snapshot
    preferred_username TEXT,                             -- nullable snapshot
    last_login_at      INTEGER,
    created_at         INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at         INTEGER NOT NULL DEFAULT (unixepoch()),
    UNIQUE(issuer, subject)
) STRICT;
```

`user_id UNIQUE` enforces the 1:1 member↔identity rule. `(issuer, subject)` is the
sole match key. `email`/`preferred_username` are informational snapshots refreshed
on each login, never a match or gate key.

### `sessions` (STRICT)

```sql
CREATE TABLE sessions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash   TEXT NOT NULL UNIQUE,                   -- SHA-256 of the cookie token
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at   INTEGER NOT NULL,                       -- absolute cap
    last_seen_at INTEGER NOT NULL DEFAULT (unixepoch()), -- idle slide
    user_agent   TEXT,
    ip           TEXT,
    created_at   INTEGER NOT NULL DEFAULT (unixepoch())
) STRICT;
CREATE INDEX idx_sessions_user_id    ON sessions(user_id);     -- per-member revoke
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);  -- hourly sweep
```

Keystone shape: `id` PK + `token_hash` UNIQUE (not token-hash-as-PK).

### `invites` (STRICT)

```sql
CREATE TABLE invites (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  INTEGER NOT NULL,
    used_at     INTEGER,
    revoked_at  INTEGER,                                 -- #78 delta
    created_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at  INTEGER NOT NULL DEFAULT (unixepoch())
) STRICT;
CREATE INDEX idx_invites_user_id ON invites(user_id);
```

Validity is time-derived: `used_at IS NULL AND revoked_at IS NULL AND expires_at > now`.
The one-valid-invite-per-member invariant is app-enforced (not expressible as a
partial UNIQUE).

**Link-state is derived, never stored.** `hasLocalLogin` = a `local_accounts` row
exists; `hasLinkedIdentity` = an `oidc_identities` row exists. No `pending` or
`can_login` column anywhere. `archived_at` is a membership-lifecycle axis, distinct
from login capability.

---

## 4. Token & crypto primitives

**One token helper**, shared by sessions and invites (and OIDC state/nonce/PKCE):
32 bytes from `crypto/rand` → base64url unpadded (~43 chars, 256 bits) → store only
`SHA-256(token)`. The raw token is returned to the caller (cookie or claim URL) and
never persisted. One audited generator across the whole layer. ([#79](https://github.com/nuxencs/moviepickarr/issues/79), [#83](https://github.com/nuxencs/moviepickarr/issues/83))

Uses:
- Session cookie token.
- Invite claim-URL token.
- OIDC `state`, `nonce`, PKCE verifier (32 bytes → 43 chars satisfies RFC 7636).

The only symmetric key in the design is the `mpa_oidc_tx` AEAD key (§8). The session
cookie is opaque-random with a server-side hash, so it needs no secret.

---

## 5. Sessions

Durations, hardcoded constants (no env knobs): **30-day idle**, **90-day absolute**.
Both enforced. A session is valid iff `now < expires_at` AND `now < last_seen_at + 30d`,
whichever bites first. `expires_at = created_at + 90d`, fixed at creation. ([#79](https://github.com/nuxencs/moviepickarr/issues/79))

### Cookie

`mpa_session`, attributes `HttpOnly; SameSite=Lax; Path=/` + `Secure` (scheme-derived,
§12). **Persistent**: `Max-Age = 90d` (the absolute cap), set once at creation, not
refreshed per request. Survives browser restart. An idle-expired-but-not-absolute
cookie can linger client-side; the server rejects it and clears it anyway.

Lax, not Strict: the OIDC callback is a cross-site top-level GET and Strict drops
the cookie on it. The callback's own CSRF defense is `state`/PKCE, not SameSite.

### Mint helper

One helper, called by every login path (local §7, OIDC §8, claim §9):

1. Generate token (§4), return raw to caller for the cookie.
2. Insert row: `token_hash = SHA-256(token)`, `created_at = last_seen_at = now`,
   `expires_at = now + 90d`, `user_id`, plus `user_agent`/`ip`.
3. `Set-Cookie mpa_session=<token>` with the attributes above.

Login **always** mints a fresh row/token; an inbound `mpa_session` is never adopted
(fixation-safe by construction). On same-browser re-login the old token is
overwritten in the cookie and its row orphans, reaped by the sweep.

### Auth middleware (per request)

1. Read `mpa_session`; absent → unauthenticated (401 on protected routes).
2. `SHA-256` the token, read-pool `SELECT` the row (joined to `users` for `role`)
   by `token_hash`.
3. Reject (**401 + clear cookie** via `Set-Cookie mpa_session=; Max-Age=0`) if: no
   row, `now >= expires_at`, or `now >= last_seen_at + 30d`.
4. Attach `memberID` and live `role` from the joined row.
5. **Throttled slide**: if `last_seen_at` is older than 1h, `UPDATE last_seen_at = now`
   on the write connection; else skip. Caps writes to ≤1/hr per active member;
   idle accuracy ±1h is irrelevant at a 30d window.

Expiry is checked on every read, independent of the sweep.

### Logout

Single `POST /auth/logout`:
- Empty body / `{}` → delete the caller's session by `token_hash` (this session only).
- `{"all": true}` → delete every session `WHERE user_id = me` (this and all devices).
- Always clears the cookie, returns `204`, idempotent.

### Revocation triggers

| Event | Effect on sessions |
|---|---|
| Logout (self) | Current only, or all via `{"all":true}` |
| Password change (self, §7) | Revoke **other** sessions; rotate the current token |
| Password reset / admin set-password (§7), invite-reset (§9) | Revoke **all** |
| Member deletion | Rows removed via `user_id` FK `ON DELETE CASCADE` |
| Role change (member ↔ admin) | **Nothing** — role is read live each request |

### Cleanup

Lazy (middleware never accepts an expired row) plus a background sweep every **hour**
(and once on startup): `DELETE WHERE expires_at < now OR last_seen_at < now - 30d`.

---

## 6. CSRF

One middleware on **unsafe methods** (`POST`/`PUT`/`PATCH`/`DELETE`), mounted across
**both** `/api/v1/*` and `/auth/*` (and any member-mutation routes). ([#74](https://github.com/nuxencs/moviepickarr/issues/74), [#79](https://github.com/nuxencs/moviepickarr/issues/79))

Rule (OWASP current guidance): allow if `Sec-Fetch-Site` is `same-origin` or `none`;
else if an `Origin` header is present, allow iff it equals our origin; else **403**.
**Fail closed** when both signals are absent (every major browser has sent
`Sec-Fetch-Site` since March 2023; all inbound mutations come from our own SPA).

- Runs **before** `requireSession` so a forged cross-origin request is rejected on
  request-shape before a DB round-trip.
- Exempt: all safe methods; `GET /oidc/callback` (its own `state`/PKCE defense);
  `GET /auth/claim/{token}` (read-only validate).
- No CSRF token, no double-submit, no SPA or SSE changes. `EventSource` is a safe
  GET subresource, unaffected.
- **Invariant: no state-changing GET endpoints anywhere** (Lax still sends the
  cookie on cross-site top-level GETs).

---

## 7. Local auth flow

Local password path. ([#76](https://github.com/nuxencs/moviepickarr/issues/76), hardening in [#83](https://github.com/nuxencs/moviepickarr/issues/83))

### Login — `POST /api/v1/auth/login`

- JSON `{ "username", "password" }`. Identifier is the **username only** (NOCASE,
  trimmed), never the display name, never email. No email column exists.
- Success: `204 No Content` + `Set-Cookie` session cookie, no body. Client hydrates
  from `GET /api/v1/auth/me` (one code path for fresh login and page reload).
- Malformed request (missing field, bad JSON) → `400`.

**Anti-enumeration + timing.** Every credential failure returns the same
`401 { "error": "invalid credentials" }`: unknown username, wrong password, and
member-has-no-local-login are indistinguishable. Unknown username still runs one
argon2id verify against a fixed dummy hash so timing can't separate known from
unknown. The lockout short-circuit (§12) reuses that same dummy verify, so
wrong-password / no-user / locked all have flat timing and the identical `401`.

**Rehash on login.** On a verified login, decode the stored PHC hash's params; if
they differ from configured, recompute from the just-verified plaintext and write
the new hash back. Bump `last_login_at` on the same write. Failed logins never
touch the row. If the rehash write fails, log and continue (the session is valid).

### Who-am-I — `GET /api/v1/auth/me`

Returns `{ id, displayName, username|null, role, hasLocalLogin, hasLinkedIdentity }`
for a valid session, else `401`. Both `has*` flags derived from credential-row
presence. Lean identity+role+credential-presence; **no** `isNextUp` (derived
client-side from `/settings/next-up`).

### Self-service password change — `POST /api/v1/auth/password`

- `{ "currentPassword", "newPassword" }`, authenticated. Actor from the session.
- Verify current first; mismatch → generic `401`.
- Change-not-create: a member with no `local_accounts` row → `409`. Adding a first
  local login goes through `POST /auth/local-login` (§9) or the admin path below.
- On success: **revoke other sessions** (keep current) and **rotate the current
  session's token** (via the mint helper) — closes the "was my current token
  observed" gap at the moment the member is acting on a security concern.

### Admin set / reset / clear — `PUT`/`DELETE /api/v1/members/{id}/local-login`

- `PUT` (admin), upsert. Create requires `{ username, password }`; if a row exists,
  it replaces the password (this is both create-local-login and admin reset). An
  admin reset **revokes all** of that member's sessions and clears
  `failed_attempts`/`locked_until` (§12) — no separate unlock endpoint.
- `DELETE` (admin) removes the `local_accounts` row; a linked OIDC identity
  survives; a member left with neither falls back to placeholder (legitimate).
- Self-lockout guard: an admin cannot `DELETE` their own last remaining credential.
- Username set at create, immutable through this flow (delete + recreate to change).
  NOCASE collision → `409`. Charset: 3–32 chars `[a-zA-Z0-9._-]`, trimmed.

### Password validation

Applied wherever a password is set (self-service, admin, seed, claim): **min 8,
max 128** (the max closes an unbounded-input argon2id DoS; argon2id has no
bcrypt-style 72-byte limit). No composition rules, no zxcvbn, no HIBP. `400` with a
field-level message on violation.

### Bootstrap admin (env-seeded, break-glass)

Three env vars, all required or the step is skipped: `MPA_ADMIN_NAME` (display
name), `MPA_ADMIN_USERNAME` (login handle, charset-validated as above),
`MPA_ADMIN_PASSWORD`.

Seed-once / non-clobber, runs every boot after migrations and before serving:
- Match `MPA_ADMIN_NAME` against `users.name`, case-insensitively.
  - One match, no local login → create the login, set `role='admin'`.
  - One match, already has a local login → ensure `role='admin'`, leave handle and
    password untouched.
  - No match → create a fresh member with that login and admin.
  - Multiple name matches → ambiguous: skip the seed, log a loud error.
- Never overwrites an existing password. Safe to leave env vars set.
- Lost bootstrap password recovery: clear that member's `local_accounts` row (or
  delete the member) and restart; the seed re-creates from env.

---

## 8. OIDC relying-party flow

`go-oidc/v3` + `x/oauth2`. This flow owns the callback engine + the **login** and
**link-while-logged-in** intents; **claim-link** (`intent=claim`) reuses this
callback but its invite semantics live in §9. ([#77](https://github.com/nuxencs/moviepickarr/issues/77), hardening in [#83](https://github.com/nuxencs/moviepickarr/issues/83))

### Provider config

`MPA_OIDC_ISSUER`, `MPA_OIDC_CLIENT_ID`, `MPA_OIDC_CLIENT_SECRET`,
`MPA_OIDC_REDIRECT_URL` (exact registered callback, explicit not derived). Scopes
**hardcoded `openid profile email`** (not operator-configurable; the claim→column
mapping depends on exactly these). **Presence-derived enablement:** OIDC is on iff
all four are set; otherwise `/oidc/*` routes return `404` and the frontend hides
the SSO controls (learned from the config endpoint, §13).

### Transaction stash

Encrypted cookie `mpa_oidc_tx`, `HttpOnly; SameSite=Lax; Path=/` + scheme-derived
`Secure`, ~10 min TTL, holding `{ state, nonce, pkce_verifier, intent, member_id?,
invite_token_hash? }`. Cleared at callback. No server-side table — the transaction
is ephemeral and single-use. It is **AEAD** (AES-256-GCM or nacl secretbox), not
merely encrypted, so `intent` and the invite hash are authenticated against
tampering. Key source: auto-generated random 32-byte at boot by default, optional
`MPA_OIDC_TX_SECRET` override (§13). Rotation is a non-event.

### Routes

- `GET /api/v1/auth/oidc/login` — **unauthenticated**. Sets tx cookie `intent=login`,
  generates state/nonce/PKCE, `302` to the provider authorize endpoint (PKCE S256).
- `GET /api/v1/auth/oidc/link` — **authenticated**. Sets tx cookie `intent=link` +
  member id, `302` to provider.
- `GET /api/v1/auth/oidc/callback` — single callback, dispatches on `intent`.
- `GET /api/v1/auth/claim/{token}/oidc` — claim-link initiation (§9), `intent=claim`.
- `DELETE /api/v1/auth/linked-identity` (self) and
  `DELETE /api/v1/members/{id}/linked-identity` (admin) — unlink.

Initiation is GET (top-level navigation to the provider); the state change is at the
callback, whose CSRF defense is `state` bound to the tx cookie — so the §6 middleware
does not gate the initiation routes.

### Callback validation

1. Provider returned `error` query param → `oidc_denied`.
2. Load + decrypt `mpa_oidc_tx`; missing/expired → `oidc_expired`.
3. Compare callback `state` to tx `state`; mismatch → `oidc_failed` (log warn).
4. Exchange `code` (PKCE S256, confidential-client secret) via `oauth2.Config.Exchange`.
5. Verify the ID token (signature, `iss`, `aud` contains our client_id, `exp`/`nbf`),
   then compare `idToken.Nonce` to tx `nonce`.
6. Extract from the ID token: `iss`→`issuer`, `sub`→`subject`, `email`→`email`
   (nullable), `preferred_username`→`preferred_username` (nullable). `email_verified`
   ignored (we never match/gate on email). Missing profile/email → NULL, no userinfo.

### Dispatch by intent

**intent=login** — look up `oidc_identities` by `(issuer, subject)`:
- **Found** → refresh snapshots, bump `last_login_at`, mint a fresh session (§5
  helper), `302` → app home.
- **Not found** → the ephemeral reject: `302` → login view `?error=oidc_unlinked`.
  Nothing persisted; log `(iss, sub, email)` at **WARN** so an admin can grep for an
  expected arrival. (No persisted pending queue — out of scope, §16.)

**intent=link** — tx cookie carries the member id captured at initiation:
- Re-check a currently-valid `mpa_session` whose member matches the tx member id;
  logged-out/revoked/mismatch → `oidc_session_expired`, no new session.
- Insert `oidc_identities`. Collision matrix:
  - `(issuer,subject)` on **another** member → `UNIQUE(issuer,subject)` blocks →
    `oidc_link_conflict`, nothing written.
  - `(issuer,subject)` on **this** member → idempotent success, refresh snapshots.
  - This member already has a **different** identity → `user_id` UNIQUE blocks →
    `oidc_link_conflict`.
  - Otherwise → insert, `302` → settings view `?linked=1`.

**intent=claim** — §9 (invite validated, then same insert + same collision matrix).

### Unlink

`DELETE /auth/linked-identity` (self) / `DELETE /members/{id}/linked-identity`
(admin) remove the `oidc_identities` row. Any local login survives. **Self-lockout
guard**: a member cannot remove their own last remaining credential → `409`.

### Outcome semantics — redirects, not JSON

Every callback outcome is an HTTP `302` to a frontend route with a query param
(the deliberate opposite of local login's XHR `204`/`401`).

| Case | Outcome |
|---|---|
| Success, login | `302` → app home, `mpa_session` set |
| Success, link | `302` → settings view, `?linked=1` |
| Provider `error=` (cancelled/denied) | `302` → login view, `?error=oidc_denied` |
| tx cookie missing/expired | `302` → login view, `?error=oidc_expired` |
| state / exchange / ID-token / nonce failure | `302` → login view, `?error=oidc_failed` (real reason logged warn) |
| login, `(iss,sub)` not found | `302` → login view, `?error=oidc_unlinked` |
| link, `(iss,sub)` collision | `302` → settings view, `?error=oidc_link_conflict` |
| link, session invalid at callback | `302` → login view, `?error=oidc_session_expired` |

Four public buckets (`oidc_denied`, `oidc_expired`, `oidc_unlinked`, `oidc_failed`)
plus two link-specific ones. Enough for distinct UX copy without leaking which
internal check failed.

### Logout

Session-only. The provider `end_session_endpoint` is **not** wired (out of scope,
§16); the IdP SSO session stays alive.

---

## 9. Invite / claim flow

The onboarding front door for both login paths. Reuses the §5 session-mint helper
and the §8 OIDC callback. ([#78](https://github.com/nuxencs/moviepickarr/issues/78))

### The invite

- A one-time-shown **claim URL** `/claim/<token>`. DB stores only `token_hash`; the
  raw token is shown to the admin once at issuance and delivered out-of-band (no
  SMTP). **No resend** — a lost link means regenerate.
- Token: §4 helper (32-byte `crypto/rand` → base64url, hash-only lookup). No id in
  the URL.
- Validity: `used_at IS NULL AND revoked_at IS NULL AND expires_at > now`.
- Invariant: at most one valid invite per member (app-enforced).

### Issuance / regenerate / revoke (admin)

- `POST /api/v1/members` — combined create-placeholder + issue first invite, returns
  the claim URL (primary entry point).
- `POST /api/v1/members/{id}/invite` — (re)issue for an existing member: sets
  `revoked_at = now()` on any current valid invite, inserts a fresh row, returns the
  new claim URL. Serves both re-invite and regenerate.
- `DELETE /api/v1/members/{id}/invite` — revoke: `revoked_at = now()` on the current
  valid invite.
- Invites may target **any** member: placeholder = claim, credentialed = reset.

### Claim (redeem)

- `GET /api/v1/auth/claim/{token}` drives the SPA `/claim/<token>` page: returns the
  member **display name**, whether a username is needed (placeholder) vs already set
  (reset), which options to offer. Invalid/expired/revoked collapse to a single
  "no longer valid" state; **already-used returns a distinct "already set up"**.
- **Consume on first credential:** establishing *one* credential sets `used_at` and
  mints the session. The second credential is added later via authed self-serve.
- **Password path** — `POST /api/v1/auth/claim/{token}/password`:
  - Placeholder: takes **username + password**, inserts `local_accounts`.
  - Reset: takes **password only** (username immutable), upserts `local_accounts`,
    **revokes all existing sessions** (the invite doubles as the locked-out reset).
  - Both then consume the invite and mint `mpa_session` → **204 + cookie**, SPA
    hydrates via `/me`.
- **OIDC path** — `GET /api/v1/auth/claim/{token}/oidc` validates the token, kicks
  off §8's authorize flow with `intent=claim`, stashing the invite's `token_hash` in
  the AEAD `mpa_oidc_tx` cookie. On the shared callback the engine re-validates the
  invite is still valid, applies §8's link-collision matrix (reject and do **not**
  consume on collision), inserts `oidc_identities`, consumes the invite, mints the
  session → `302 ?error=`/`?linked=`.

### Credential-completeness

`POST /api/v1/auth/local-login` — authed, self-serve: a logged-in member with **no**
`local_accounts` row sets a first username + password (min-8). No current-password
check — the active session is the proof. `409` if a local login already exists (use
§7's change endpoint). Closes the OIDC-first-then-wants-a-password gap; the mirror
(password-first adds OIDC) is §8's `/oidc/link`.

### Fallbacks

- **Admin-set-password:** §7's `PUT /members/{id}/local-login`.
- **Admin-manual-link:** out of scope (§16) — the subject is unknowable without the
  member authenticating.

---

## 10. Authorization model & API reshape

The surface renames `/users` → `/members` (`:userID` → `:memberID`); the design
drops CONTEXT.md's "user survives in URLs" carve-out. ([#80](https://github.com/nuxencs/moviepickarr/issues/80))

### Middleware chain (`/api/v1`, blanket)

CSRF origin-check (§6) → `requireSession` (§5), both on the whole `/api/v1` group.
CSRF runs first. `requireSession` attaches `c.Locals("memberID", int)` +
`c.Locals("role", string)`; handlers that want name/username load the row.

Auth-*establishing* routes (`/auth/local-login`, `/auth/login`, `/oidc/login`,
`/oidc/callback`, `/claim/...` validate+submit) stay top-level, outside the
mandatory-auth group, declaring auth per-route (owned by §7–§9).

### 401 vs 403 vs 404

- **401** — not authenticated (no cookie, unknown/expired session). From `requireSession`.
- **403** — authenticated but not permitted. JSON body carries a machine `code`:
  `admin_required`, `not_next_up`, `not_adder`.
- **404** — only genuinely-missing resources (unknown `movieID`). Never a permission
  mask: every movie/pool/stash is any-authenticated readable, so an honest 403 leaks
  nothing a `GET` wouldn't.
- "Not your turn" is a **403** (`not_next_up`), not a 409 — it's a permission derived
  from rotating state, not a retryable conflict.
- Admin *pages* (SPA routes; the server serves `index.html`) render a **forbidden
  state, not a 404 mask** (§14).

### Endpoint matrix (reshaped `/api/v1`)

| Method + path | Authz |
|---|---|
| `GET /me` | authenticated — `{id, displayName, username\|null, role, hasLocalLogin, hasLinkedIdentity}` (no `isNextUp`) |
| `GET /members` | any authenticated read |
| `GET /members/:memberID/pool` · `/members/:memberID/stash` | any authenticated read |
| `GET /movies/pool` · `/movies/current` · `/movies/watched` · `/movies/filter-options` · `/movies/:movieID` | any authenticated read |
| `GET /stats` · `/settings/next-up` · `/settings/pool-lock` · `/tmdb/search` | any authenticated read |
| `GET /events` (SSE) | authenticated — cookie at handshake (401 before stream opens if none); revalidate session on the existing 15s heartbeat, close stream if gone (≤15s revocation window); no per-member payload filtering |
| `POST /movies` (add to own stash) | authenticated — adder = session member, no target id |
| `PUT /movies/:movieID` · `DELETE /movies/:movieID` · `POST /movies/:movieID/move` | adder-only (`403 not_adder`, **no admin override**) |
| `POST /movies/random` (draw) · `POST /movies/current/reveal` · `POST /movies/current/watch` | next-up-or-admin (`403 not_next_up`) |
| `POST /members` (create + invite) | admin |
| `DELETE /members/:memberID` (delete member) | admin (§11) |
| `PUT`/`DELETE /members/:memberID/local-login` | admin |
| `POST`/`DELETE /members/:memberID/invite` | admin |
| `DELETE /members/:memberID/linked-identity` (admin unlink) | admin |
| `PUT /settings/pool-lock` | admin |

Self-serve credential ops (`POST /auth/local-login`, `POST /auth/password`,
`/oidc/link`, self-unlink) stay top-level under `/auth`·`/oidc`.

### Route reshape (drop `:userID`)

- `POST /users/:userID/movies` → `POST /movies` (adder = session).
- `PUT`/`DELETE /users/:userID/movies/:movieID` and `.../move` → `/movies/:movieID`
  (+ `/move`), folded into the top-level `/movies` group; adder-only enforced by
  loading the movie and comparing its adder to `memberID`.
- `GET /users/:userID/pool` → `GET /members/:memberID/pool`.
- `GET /users/:userID/stash` → `GET /members/:memberID/stash` (stash "private" =
  write-scoped only; contents readable by any authenticated member).
- `/me/stash` dropped as redundant.

### Rotation moves to watch (Model B)

`advanceNextUp` moves from the draw handler (`handleGetRandomMovie`,
`movies_handlers.go:420`) to the watch handler (`handleWatchMovie`). So next-up ==
the current runner across the whole draw → reveal → watch cycle, and the authz check
is one uniform concept for all three actions. The hero's "Next up: X" holds on the
runner for the night and flips only when the movie is marked watched. There is **no**
manual next-up-set endpoint (rotation stays automatic).

### CONTEXT.md edits the build should make

- Drop the "user survives in code, API names and URLs" carve-out's URL/API clause
  (surface is now `/members`; `user` survives in code only).
- Clarify Stash: "private backlog" → private means only the adder can add; contents
  are readable by any authenticated member.
- Next up: enforced for draw/reveal/watch; rotates **on watch**, not on draw. Drop
  the "not enforced by the app" line for those three actions.

### Deferred vocabulary pass

Under Model B, "Next up" reads slightly present-tense during an active night. A
rename (to a tense-neutral role noun) is **deferred** to a dedicated vocabulary pass
after the auth layer lands — it's load-bearing across code, API, SSE events, the
glossary, and closed tickets. "Drawer" specifically rejected (inaccurate pre-draw,
homograph with the furniture). Not part of this build.

---

## 11. Roster migration & rollout

### Delete-member — one action, two outcomes

007 set `movies.added_by_id` to `ON DELETE RESTRICT` ("don't silently erase group
watch history"), so a hard delete of a contributor is impossible at the DB level.
`DELETE /members/:memberID` (admin) resolves to: ([#81](https://github.com/nuxencs/moviepickarr/issues/81))

- **Zero authored movies** → **hard `DELETE`**. `RESTRICT` passes; `CASCADE` clears
  any `local_accounts`/`oidc_identities`/`sessions`/`invites`; `next_up` → `SET NULL`.
  Row and display name freed. Covers junk/typo placeholders.
- **Authored movies** → **archive**. Set `users.archived_at` and explicitly delete
  the member's credential/session/invite rows so login dies (the `users` row
  survives, so `CASCADE` does not fire). Movies stay attributed — history still reads
  "Alice added Dune."

`archived_at` is a membership-lifecycle axis, distinct from login capability. Active
roster / draw / next-up / member-count reads filter `archived_at IS NULL`. Accepted
cost: an archived member squats their UNIQUE display name to preserve attribution
(why the zero-movie case hard-deletes instead).

### Un-archive / re-invite (folded-in loose end)

Restoring an archived member is **clear `archived_at`, then re-invite** — because
archiving stripped the credential/session/invite rows, the member has no login until
they claim a fresh invite. So restore is two steps, surfaced as one **Restore**
action on the roster (§14):

1. `UPDATE users SET archived_at = NULL` — the member reappears in active reads.
2. Issue a fresh invite (`POST /members/{id}/invite`, §9) and deliver the claim URL
   out-of-band. The member claims to set a password and/or link SSO again.

A restore that stops at step 1 leaves an active-but-credential-less placeholder,
which is a legitimate state (they show on the roster, usable as an adder, can't log
in until they claim). The roster **Restore** action should run both steps and
surface the one-time invite reveal (§14), so restore and re-onboard are one gesture.

### Existing members → placeholders

The migration leaves existing rows untouched: everyone gets `role='member'` via the
column default and has no credential rows, i.e. becomes a credential-less
placeholder. No data loss, no forced migration flow.

### Cutover (hard, runbook-documented)

Auth is mandatory, so on deploy the whole group is locked out except the member the
break-glass admin adopts. Accepted as a hard cutover, no invite pre-seeding:

1. Deploy 009.
2. Break-glass admin logs in (adopts their existing member row by `MPA_ADMIN_NAME`).
3. Admin issues invites to the remaining existing members (`POST /members/{id}/invite`).
4. Claim links delivered out-of-band; each member claims.

### Boot ordering & idempotency

- `RunMigrationsWithBackup` auto-snapshots the prod DB before applying 009 (no
  bespoke backup step).
- Boot order: **migrate → seed → serve.** The seed runs only after migrations and
  before traffic.
- Seed idempotent (#76's adopt-never-clobber makes re-runs no-ops). **Fail boot
  loudly** if seed env is set but seeding fails; **loud warning** if zero admins
  exist and no seed is configured (env-seed is the only admin path).

---

## 12. Security hardening & threat model

### Threat model

The app may run behind a proxy/Cloudflare or completely raw. The Authelia gate is
loosened so `/auth/*`, `/oidc/callback`, and claim endpoints are reachable without
Authelia, so the app owns its own login defense standalone and never relies on a
proxy for rate-limiting. In scope: opportunistic internet brute-force /
credential-stuffing against the loosened routes; a curious-but-not-malicious LAN
member. Out of scope: a targeted attacker with host access, a malicious admin,
side-channel/timing APTs, resource-exhaustion DoS. Posture: proportionate hardening
for a single-digit trusted roster, not bank-grade, no new operator config, no
signup/email surface. ([#83](https://github.com/nuxencs/moviepickarr/issues/83))

### Decisions

1. **Login lockout — per-account soft-threshold.** After 10 consecutive failed
   verifies, set `locked_until = now + 15min`; a successful verify resets
   `failed_attempts = 0, locked_until = NULL`. Auto-expiring (a past `locked_until`
   is ignored, accounts self-heal, break-glass admin stays recoverable). When
   `locked_until > now`, skip the real verify but run a dummy argon2id verify and
   return the **same uniform `401`** (silent lockout — a distinct "locked"/`429`
   would confirm the username exists). Admin `PUT /members/{id}/local-login` reset
   clears both columns (no separate unlock endpoint). Only real accounts have rows,
   so nonexistent-username attempts are untracked (consistent with anti-enumeration).
   Accepted cost: a low-stakes griefing lock, bounded by auto-expiry and the tiny
   roster.
2. **Timing-equalization.** Uniform `401` for wrong-password and no-such-user; dummy
   argon2id verify against a fixed hash so no-such-user costs the same. The locked
   short-circuit reuses that dummy verify, so wrong-password / no-user / locked all
   have flat timing and the identical `401`.
3. **Password strength.** min-8 floor + **max-128 cap** (closes the unbounded-input
   hashing DoS). No zxcvbn, no HIBP. Lockout + argon2id carry credential-stuffing
   defense.
4. **Token entropy.** One `crypto/rand` 32-byte → base64url → hash-only helper (§4)
   for sessions and invites. 256 bits, single-use, expiring; no scaling.
5. **Session fixation / mint rotation.** No anonymous pre-auth session (`mpa_session`
   is minted only after login or claim; `mpa_oidc_tx` is a separate short-lived
   encrypted transaction cookie). Fresh mint at login and claim. **No rotation on
   OIDC link** (adds a credential, doesn't elevate privilege or change the actor).
   **Rotate the current session's token on a successful password change** (§7).
6. **Secure-cookie — scheme-derived, no new env var.** `Secure` tracks the effective
   request scheme: set when the request is https (direct TLS or
   `X-Forwarded-Proto: https` from a trusted proxy), omitted over plain http so the
   cookie works on raw-http/dev. Zero-config, correct across https-direct,
   behind-a-TLS-proxy, and raw-http. Residual (documented, not fought): a raw-http
   deployment has no `Secure` protection — the inherent cost of running without TLS.
7. **OIDC secrets.**
   - state / nonce / PKCE verifier: the §4 32-byte helper.
   - `mpa_oidc_tx`: AEAD (AES-256-GCM or nacl secretbox), ephemeral random 32-byte
     key by default, optional `MPA_OIDC_TX_SECRET` override. Rotating/restarting at
     most drops in-flight logins (self-heal on retry).
   - unlinked-login reject log: **WARN**, one line per rejected callback, `issuer` +
     `subject` + `email`/`preferred_username`, never tokens or the raw ID token.
     Encode claim values safely (untrusted IdP strings — avoid log injection). No
     throttle (at most one slow WARN per completed IdP round-trip, nothing persisted).

---

## 13. Config / env surface

The complete auth env surface. All required-vs-optional noted. ([#76](https://github.com/nuxencs/moviepickarr/issues/76), [#77](https://github.com/nuxencs/moviepickarr/issues/77), [#79](https://github.com/nuxencs/moviepickarr/issues/79), [#83](https://github.com/nuxencs/moviepickarr/issues/83))

| Var | Required | Purpose |
|---|---|---|
| `MPA_ADMIN_NAME` | seed trio — all three or none | Break-glass admin display name (matched against `users.name`, §7) |
| `MPA_ADMIN_USERNAME` | seed trio | Break-glass admin login handle (charset-validated) |
| `MPA_ADMIN_PASSWORD` | seed trio | Break-glass admin password |
| `MPA_OIDC_ISSUER` | OIDC quartet — all four or OIDC off | Discovery + `iss` validation |
| `MPA_OIDC_CLIENT_ID` | OIDC quartet | Confidential client id |
| `MPA_OIDC_CLIENT_SECRET` | OIDC quartet | Confidential client secret |
| `MPA_OIDC_REDIRECT_URL` | OIDC quartet | Exact registered callback URL (explicit, not derived) |
| `MPA_OIDC_TX_SECRET` | optional | AEAD key for `mpa_oidc_tx`; auto-generated per boot if unset |

Not env-configurable (deliberately hardcoded): session durations (30d idle / 90d
absolute, §5), OIDC scopes (`openid profile email`, §8), password bounds (8–128),
the `Secure` cookie flag (scheme-derived, §12).

**Enablement is presence-derived**, no boolean flags: the admin seed runs iff the
trio is set; OIDC is on iff the quartet is set (else `/oidc/*` → `404` and the
frontend hides SSO controls). `Secure` is scheme-derived (no var). Sessions add no
vars.

**Env-var prefix reconciliation (build decision).** The auth vars use an `MPA_`
prefix, but the existing config surface uses bare names (`TMDB_API_KEY`, `DB_FILE`,
`LOG_LEVEL`, `DB_BACKUP_MAX`, the `TMDB_ENRICH_*` family) with no prefix. The build
should reconcile this: either keep `MPA_` for the auth vars and accept a mixed
surface, or align everything under one convention. The design tickets locked the
`MPA_`-prefixed *names*; the prefix choice itself was never a design decision, so
it's the build's call. Whatever wins, update `.env.example` and `docs/INSTALL.md`.

**Dev-experience note — OIDC + secure cookies locally.** Running OIDC end-to-end
locally means the browser hitting the app over `http://localhost:3030`. Because
`Secure` is scheme-derived (§12), the session and `mpa_oidc_tx` cookies are set
*without* `Secure` over plain http, so login and the OIDC round-trip work on
localhost with no extra config. Two things to know:

- The provider (Authelia or another IdP) must allow an `http://localhost:...`
  redirect URI registered against `MPA_OIDC_REDIRECT_URL`. Some providers refuse
  non-https redirect URIs except for `localhost` — check the provider's rules.
- If you front local dev with a TLS-terminating proxy, it must pass
  `X-Forwarded-Proto: https` for the app to mark cookies `Secure`; otherwise the
  app sees plain http and omits `Secure` (still correct — the cookie works).

Update `.env.example` (add a `── Auth ──` block mirroring the table above, all
commented, with the seed trio and OIDC quartet grouped) and `docs/INSTALL.md`.

---

## 14. Frontend surfaces

Structural directions locked via prototype on the throwaway branch
`proto/auth-login-claim` (never merged to develop). Each prototype route below is
the primary source for its surface. ([#85](https://github.com/nuxencs/moviepickarr/issues/85), [#86](https://github.com/nuxencs/moviepickarr/issues/86), [#87](https://github.com/nuxencs/moviepickarr/issues/87))

### Login & claim — split marquee (`/prototype/auth?variant=b`)

Two panes. Left: a cinematic panel (poster wall + wordmark, no tagline); collapses
to a slim header band on phones. Right: the form column, vertically centered. Same
shell for login and claim.

**Login page** (`/login`):
- Eyebrow "Welcome back" + display headline; username + password; primary "Sign in".
- "Log in with SSO" ghost button **conditionally rendered** (hidden, not disabled) —
  shown only when the presence-derived OIDC config endpoint (§13) reports a provider.
- One error banner above the fields:
  - bad credentials and silent soft-lockout → **uniform 401**, identical copy
    "That username and password don't match."
  - `?error=oidc_unlinked` → **warn**-tone banner "That account isn't linked to a
    member yet. Ask an admin for an invite."
- Success: `204` + `mpa_session` → redirect into the app.

**Claim page** (`/claim/<token>`, driven by `GET /auth/claim/{token}`) — four
distinct states, visually and textually different:
- **placeholder** (fresh): "Welcome, {name}", new + confirm password → "Create
  login"; plus a "Set up with SSO instead" ghost (`intent=claim`).
- **reset**: "Choose a new password", password fields → "Update password". No
  SSO-instead branch.
- **no longer valid** (expired / revoked / consumed): terminal, centered, error icon
  — "This invite is done" + "ask an admin for a fresh invite". Plain, not a pun.
- **already set up**: terminal, centered, ok icon — "You're already in" + a "Go to
  login" button. Distinct from *no longer valid*.

Copy register: plain over cute.

### Admin roster — roster table (`/prototype/admin?variant=a`, commit 8db2506)

A dense table, one row per member, deliberately unlike the card-heavy movie boards.
Top bar: "Roster" + member count on the left, a "New member's name…" field + "Add &
invite" primary on the right. Below the active table, a dimmed **Archived** section
("Kept for attribution. Can't log in until restored + re-invited.").

Columns: Member (avatar + display name + `@username`/"no username", with `You`/`Admin`
tags) · Role · Login (credential chips) · Added (movies authored, tabular) · Last
active · row kebab.

**Login chips** — presence-derived, one per held credential: `Password` and/or `SSO`;
placeholder with an outstanding invite → `Invite sent`; placeholder with none →
dashed `No login yet`; archived → `Archived`. Never a login boolean.

**Row actions (kebab)** — contextual to state: Send/Regenerate invite (+ Revoke when
one is outstanding) for placeholders; Set/Reset password and Unlink SSO for members
with credentials; Remove member (danger) always; **Restore** for archived rows (runs
the two-step un-archive + re-invite of §11).

Four shared moments (same across all variants):
1. **One-time invite reveal** — a modal after create-member+invite (and regenerate):
   the claim URL in a monospace field with a **Copy** button and a warn banner "Copy
   it now — this is the only time it's shown. There's no resend. If it's lost,
   regenerate a fresh link (which invalidates this one)." No resend affordance
   anywhere. This same reveal fires on Restore.
2. **Remove = one action, two outcomes** — the confirm names which happens *before*
   commit. Zero authored → destructive **Delete member** (red), "…this is a clean
   delete… the name frees up for reuse." Authored → non-destructive **Archive
   member** (accent), "…keeps the row so those stay credited… This is an archive, not
   a delete."
3. **Unlink last-credential guard** — for self when SSO is the only credential, the
   UI refuses before the round trip: "Can't unlink SSO … unlinking would lock you
   out … Set a password first." Offers a **Set a password** path. (409 is the
   server backstop.)
4. **Non-admin forbidden state** — a plain member hitting the admin URL gets a
   first-class "Admins only" screen with "Back to movie night", **not** a 404 mask.

### Account settings — settings page (`/prototype/account?variant=a`, commit 2c6727a)

A dedicated, full-page account screen, three stacked sections:
- **You** — read-only identity: avatar, display name, `@username`, role badge.
  Name/username not self-editable (username immutable once set; naming is an admin
  job).
- **Sign-in** — password row + SSO row.
  - Has a password → "Change" opens a verify-current + new + confirm dialog (min-8)
    with a visible "this signs you out on every other device" warning.
  - SSO-first, no password → a highlighted **Set a password** CTA row opens a dialog
    that also picks the immutable username (`POST /auth/local-login`).
  - SSO row is **absent, not disabled, when no provider is configured** (presence-
    derived, mirrors the login SSO button). Connected → provider name + claim email
    + Unlink; not connected → Connect. Last-credential unlink refused client-side
    with a "set a password first" recovery; server 409 backstop.
- **Sessions** — "Log out" (this device) and "Log out everywhere" (`{all:true}`), the
  latter showing the other-device count so the choice is concrete.

The ceremony dialogs (change/set password, log out everywhere, unlink guard) are
shared and carry into the build unchanged.

### Not-next-up board controls — disable, don't hide (folded-in loose end)

Behavior is decided (#80: disable not hide; the three board actions draw / reveal /
watch are next-up-or-admin, `403 not_next_up`). The residual is the disabled-control
microcopy. When the viewer is **not** next-up and **not** admin, the three controls
render **disabled** (visible, greyed, non-interactive) with an explanatory tooltip —
never hidden, so the group always sees the same board and understands whose turn it
is. Admins never see them disabled (they hold the override).

Proposed copy (plain register, matching §14; `{name}` = the current next-up member's
display name):

| Control | Disabled tooltip |
|---|---|
| Draw | "It's {name}'s turn to draw." |
| Reveal | "Only {name} can reveal this draw." |
| Watch | "Only {name} can mark this watched." |

Fallback when next-up is somehow unresolved (empty pool, single-member roster, a
null `next_up`): "Waiting for the next-up member." The controls stay disabled. On a
server `403 not_next_up` (a stale client that fired anyway — e.g. rotation advanced
between render and click), surface the same short line as a transient toast rather
than a hard error screen.

This is the one bit of copy newly authored during assembly rather than carried from
a closed ticket; treat it as a proposal open to a copy pass, not a locked string.

### Deferred to the build (from the prototype tickets)

Inline password-rule hints, submitting/loading/disabled states, exact post-auth
redirect targets, per-action toast copy, create-member validation, and roster
pagination/search are implementation details; the visual + structural directions are
locked.

---

## 15. Suggested build order

Rough dependency order; a build effort can re-slice.

1. **Migration 009 + schema** (§3) — everything else needs the tables. Includes the
   `local_accounts` lockout columns and `invites.revoked_at`.
2. **Token helper + argon2id wrapper** (§2, §4) — the shared primitive.
3. **Session store + mint helper + auth middleware + CSRF middleware** (§5, §6).
4. **Boot seed** (§7 bootstrap, §11 ordering) — migrate → seed → serve.
5. **Local auth flow** (§7) + lockout (§12) — login, `/me`, logout, password change,
   admin set/reset.
6. **Authz + route reshape** (§10) — rename `/users`→`/members`, endpoint matrix,
   rotation-on-watch (Model B), SSE handshake auth.
7. **Invite / claim flow** (§9) — issuance, claim page backend, password claim,
   `/auth/local-login`.
8. **OIDC RP flow** (§8) — provider config, tx cookie AEAD, login/link/claim intents,
   callback engine, unlink.
9. **Delete-member / archive / restore** (§11).
10. **Frontend** (§14) — login, claim, admin roster, account settings, forbidden
    state, not-next-up disabled controls. Port the locked directions off
    `proto/auth-login-claim`.
11. **Config + docs** (§13) — `.env.example`, `docs/INSTALL.md`, and the CONTEXT.md
    edits in §10.

---

## 16. Out of scope

Ruled out of *this* effort (redraw the destination to reopen):

- Personalization payoffs (per-movie ratings, personal views) — auth is their
  prerequisite; separate future efforts.
- The Radarr / integration feature itself — this designs the admin gate, not the
  integration.
- Multi-provider OIDC simultaneity.
- SMTP/email, including self-service "forgot password".
- Self-service registration / public signup.
- Persisted OIDC pending / link-request queue — an unlinked SSO login rejects
  ephemerally; an admin approve-from-queue surface is functionally self-service
  registration. Onboarding stays invite-only and admin-initiated. (Ruled out in #77.)
- RP-initiated provider logout (`end_session_endpoint`) — our logout stays
  session-only; the IdP SSO session is left alive. (Ruled out in #77.)
- Admin-manual-link (admin hand-binds an `(issuer, subject)`) — the subject is
  unknowable until the member authenticates. Admin-set-password and re-issuing an
  invite are the real fallbacks. (Ruled out in #78.)
- The "Next up" → tense-neutral rename — deferred to a dedicated vocabulary pass
  after this layer lands (§10).

---

## Ticket index

| # | Title | Type | What it settled |
|---|---|---|---|
| [#75](https://github.com/nuxencs/moviepickarr/issues/75) | Identity data model & glossary (keystone) | grilling | Five tables, role-on-member, derived link-state, hash-only tokens. Glossary via [PR #82](https://github.com/nuxencs/moviepickarr/pull/82). |
| [#72](https://github.com/nuxencs/moviepickarr/issues/72) | Research: password hashing | research | argon2id via `alexedwards/argon2id`, OWASP-min params. |
| [#73](https://github.com/nuxencs/moviepickarr/issues/73) | Research: OIDC RP library | research | `coreos/go-oidc/v3` + `x/oauth2`, no refresh tokens. |
| [#74](https://github.com/nuxencs/moviepickarr/issues/74) | Research: session store & CSRF | research | Hand-rolled SQLite sessions; `Sec-Fetch-Site`/Origin CSRF. |
| [#76](https://github.com/nuxencs/moviepickarr/issues/76) | Local auth flow | grilling | Login, `/me`, password change, admin set/reset, env seed. |
| [#77](https://github.com/nuxencs/moviepickarr/issues/77) | OIDC RP flow | grilling | Callback engine, login/link intents, tx cookie, redirect semantics. |
| [#78](https://github.com/nuxencs/moviepickarr/issues/78) | Invite / claim flow | grilling | Claim URL, consume-on-first-credential, `/auth/local-login`. |
| [#79](https://github.com/nuxencs/moviepickarr/issues/79) | Session lifecycle & CSRF | grilling | 30d/90d durations, mint helper, logout, sweep, CSRF middleware. |
| [#80](https://github.com/nuxencs/moviepickarr/issues/80) | Authorization model & API reshape | grilling | `/users`→`/members`, endpoint matrix, 401/403/404, Model B. |
| [#81](https://github.com/nuxencs/moviepickarr/issues/81) | Roster migration & schema rollout | grilling | Migration 009, delete-vs-archive, hard cutover. |
| [#83](https://github.com/nuxencs/moviepickarr/issues/83) | Security hardening & threat model | grilling | Lockout columns, max-128, scheme-derived Secure, AEAD tx. |
| [#85](https://github.com/nuxencs/moviepickarr/issues/85) | Login & claim UX | prototype | Split marquee; four claim states. |
| [#86](https://github.com/nuxencs/moviepickarr/issues/86) | Admin roster & invite UX | prototype | Roster table; chips; four shared moments. |
| [#87](https://github.com/nuxencs/moviepickarr/issues/87) | Account settings, auth gate & 403 UX | prototype | Settings page; shared ceremony dialogs. |
