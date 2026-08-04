# Research: session store & CSRF approach on Fiber v2

Resolves [#74](https://github.com/nuxencs/moviepickarr/issues/74). Constraints
from the parent decision: server-side sessions, opaque HttpOnly cookie holding
a session id, backed by the app's existing SQLite database (modernc driver,
dedicated read pool + single write connection), one mechanism for both local
password and OIDC login, instant revocation on logout / role change / member
deletion / password reset. Same-origin embedded SPA plus a long-lived SSE
stream at `/api/v1/events`.

All Fiber claims below are from the **v2** source at tag v2.52.5 (the docs
site defaults to v3, so source links are the citable reference). The app pins
`gofiber/fiber/v2 v2.52.13`; the middleware in question is unchanged between
those patch releases in the ways that matter here.

## Recommendation (summary)

1. **Hand-rolled SQLite session store**, not Fiber's session middleware +
   `gofiber/storage` adapter. One `sessions` table in the app's existing DB
   with a `user_id` column and both `created_at` and `expires_at`, accessed
   through the app's existing read pool / write connection. Rationale: the
   Fiber stack cannot revoke "all sessions of user X" (opaque blob keyed only
   by session id), cannot express an absolute lifetime cap, and the sqlite3
   storage adapter uses the mattn cgo driver and always opens its own DB file,
   so it can neither share the modernc connection nor respect the
   single-writer discipline.
2. **Cookie**: `HttpOnly; Secure; SameSite=Lax; Path=/`, set explicitly.
   `Lax`, not `Strict`, because the OIDC callback is a cross-site top-level
   GET and `Strict` would drop the cookie on it.
3. **CSRF**: `SameSite=Lax` alone is not sufficient per OWASP. Add a small
   origin-check middleware on unsafe methods: allow if `Sec-Fetch-Site:
   same-origin` (or `none`), fall back to comparing `Origin` against the
   app's own origin, reject otherwise. No token plumbing in the SPA, nothing
   stored, works fine with SSE. OWASP now names Fetch Metadata the primary
   CSRF signal, with origin-header fallback mandatory. Fiber's v2 `csrf`
   middleware (a token approach) is a workable alternative but is built
   around Fiber's session manager or its own storage, which we are not using.
4. **OIDC callback CSRF** is handled by `state` + PKCE per the OAuth Security
   BCP, not by SameSite or the CSRF middleware. Keep the callback GET
   side-effect-free apart from completing the login.

The rest of this file is the evidence.

## 1. Fiber v2 session middleware

Source: `middleware/session/{config.go,session.go,store.go}` at
[v2.52.5](https://github.com/gofiber/fiber/blob/v2.52.5/middleware/session/config.go).

### Config and cookie attributes

Full cookie attribute control exists: `CookieHTTPOnly`, `CookieSecure`,
`CookieSameSite` (default `"Lax"`), `CookieDomain`, `CookiePath`,
`KeyLookup` (default `"cookie:session_id"`), `KeyGenerator` (default
`utils.UUIDv4`), `Expiration` (default 24h). Attributes are applied in
`setSession()` (session.go:240-270). So cookie control is not a
differentiator; both approaches can set the right attributes.

### Expiry is sliding-only

`Expiration` is just the TTL handed to storage on each `Save()`:

```go
if s.exp <= 0 {
    s.exp = s.config.Expiration
}
...
if err := s.config.Storage.Set(s.id, encodedBytes, s.exp); err != nil {
```

Each request acquires a fresh `Session` from a `sync.Pool` with `exp` reset
to 0, so every `Save()` re-stamps the full TTL on both the storage row and
the cookie. Net behavior: an idle (sliding) timeout if you save per request.
There is **no absolute-lifetime cap and no idle+absolute combo**;
`SetExpiry()` only affects the current request's save. OWASP recommends both
timeouts (see section 4), so with this middleware you would end up storing
your own `created_at` inside the session payload and checking it by hand
anyway.

### Revocation is per-id only; no "all sessions of user X"

`Session.Destroy()` deletes the storage row and expires the cookie. Outside
a request, `Store` exposes exactly two methods: `Reset()` (deletes **all**
sessions) and `Delete(id)` (one session by id). The `fiber.Storage`
interface is five methods, `Get / Set / Delete / Reset / Close`
([app.go:41-63](https://github.com/gofiber/fiber/blob/v2.52.5/app.go)), with
**no enumeration**. Session data is a gob blob keyed by session id, so
"revoke every session belonging to this member" (role change, member
deletion, password reset) is impossible without maintaining a separate
userID-to-sessionIDs index yourself. At that point you are already
hand-rolling the part of the store that matters most to this app.

### Concurrency and the SSE stream

`Save()` releases the session back to the pool; the source warns "It's not
safe to use the session after calling Save()" (session.go:178-220). Two
concurrent requests saving the same session id are last-write-wins over the
whole blob. A long-lived SSE handler must not hold a `*Session` across the
stream loop, and since only `Save()` refreshes the TTL, a stream that idles
past the sliding window can outlive its own session. A custom store that
checks the session row per request (or at connect time) and updates
`last_seen_at` with a single UPDATE has none of these sharp edges.

## 2. gofiber/storage SQLite adapters

Source: [`sqlite3/sqlite3.go`, `config.go`, `go.mod`](https://github.com/gofiber/storage/blob/main/sqlite3/sqlite3.go).

- **Driver**: `github.com/mattn/go-sqlite3` (cgo). The app uses
  `modernc.org/sqlite` and builds without cgo; this alone disqualifies the
  `sqlite3` adapter. (The same repo has a modernc-based `sqlite` module, but
  the remaining problems apply to it equally.)
- **Cannot share a connection**: `New()` calls `sql.Open` itself; `Config`
  only takes a `Database` path/DSN plus pool tuning. There is no way to
  inject the app's existing `*sql.DB`. `Conn()` reads the adapter's own
  handle out, it does not accept one in. A second independent writer on the
  same file works in WAL mode but breaks the app's deliberate
  single-write-connection design (see `docs/findings/database-review-2026-07.md`);
  a separate session DB file splinters backup/VACUUM handling.
- **Schema is opaque**: `k VARCHAR(64) PRIMARY KEY, v BLOB, e BIGINT` plus
  an index on `e`. `v` is the gob-encoded session payload. No user column,
  lookups by primary key only, so no query path for per-user revocation.
- Runs its own GC goroutine (`DELETE ... WHERE e <= ? AND e != 0` every
  `GCInterval`, default 10s).

## 3. What the hand-rolled store looks like

Small surface, fits the existing repository pattern. This updated sketch keeps
the cookie credential hash separate from the non-secret handle used by the
device-management API:

```sql
CREATE TABLE sessions (
    id            INTEGER PRIMARY KEY, -- internal only
    public_id     TEXT NOT NULL UNIQUE, -- immutable revoke handle
    token_hash    TEXT NOT NULL UNIQUE, -- hash of the opaque cookie token
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at    INTEGER NOT NULL,   -- unix epoch, absolute-cap anchor
    last_seen_at  INTEGER NOT NULL,   -- idle-timeout anchor
    expires_at    INTEGER NOT NULL    -- min(created+absolute, last_seen+idle)
) STRICT;
CREATE INDEX idx_sessions_user ON sessions(user_id);
```

- Token: 32 random bytes (`crypto/rand`), base64url in the cookie. OWASP
  requires at least 64 bits of entropy; 256 is cheap. Store only a SHA-256
  of the token so a leaked DB or backup does not yield live sessions.
- Middleware: read cookie, hash, single read-pool SELECT joined to the user
  row, reject if missing or `expires_at < now`, stash the user in
  `c.Locals`. Bump `last_seen_at` lazily (e.g. only when older than a few
  minutes) to keep writes off the hot path.
- Revocation: `DELETE WHERE token_hash = ?` (logout), owner-scoped `DELETE
  WHERE public_id = ? AND user_id = ?` (one device), `DELETE WHERE user_id = ?`
  (all devices), and member deletion cascades. All instant, all one statement,
  all through the existing write connection.
- Expiry: enforce both idle and absolute at read time; a periodic sweep
  (`DELETE WHERE expires_at < now`) can piggyback on any existing
  housekeeping. OWASP suggests 15-30 min idle for low-risk apps; for a
  friend-group app a longer idle (days) with an absolute cap (e.g. 30 days)
  is a reasonable, honest trade, since revocation is the real control here.
- Regenerate the id on login and on privilege change, per OWASP: "The
  session ID must be renewed or regenerated by the web application after any
  privilege level change within the associated user session."
  ([Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html))

Both login paths (password and OIDC) end in the same "create session row,
set cookie" call, satisfying the one-mechanism constraint.

## 4. CSRF

### SameSite=Lax alone is not enough

OWASP [CSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html):

> "SameSite is useful as a defense-in-depth control but it does not replace
> a proper CSRF defense in most deployments."

Mechanics, per [MDN Set-Cookie](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Set-Cookie):
Lax sends the cookie cross-site only on top-level navigations with safe
methods. That blocks classic cross-site form POSTs but still sends the
cookie on cross-site top-level GETs, so any state-changing GET would be
exposed (the API has none today; keep it that way). Browsers that default to
Lax also apply a permissive variant that includes cookies in POSTs "as long
as they were set no more than two minutes before the request was made";
setting `SameSite=Lax` explicitly avoids that window. All current mainstream
browsers enforce an explicit SameSite attribute; the residual risk is legacy
browsers and future GET endpoints, which is exactly why OWASP wants a second
layer.

### The layer to add: Fetch Metadata + Origin fallback

OWASP (same cheat sheet) on `Sec-Fetch-Site`:

> "the primary signal for CSRF protection. It indicates the relationship
> between the request initiator's origin and its target's origin"

supported "in all major browsers since March 2023", with "a fallback to
standard origin verification headers is a mandatory requirement for any
Fetch Metadata implementation."

Concretely: a middleware on unsafe methods (POST/PUT/PATCH/DELETE) under
`/api/v1` that accepts `Sec-Fetch-Site: same-origin` (or `none`, e.g. from
non-browser clients that also lack `Origin`), otherwise requires `Origin` to
match the app's origin, otherwise 403. For a same-origin SPA this is
invisible to the frontend: no token fetch, no header to attach, nothing to
thread through the SSE reconnect logic, and it stays correct when the
wide-open CORS middleware is tightened (a strict CORS policy is what makes
the related custom-header pattern sound, but the origin check does not even
need that).

### Why not Fiber's csrf middleware

Fiber v2's [`csrf`](https://github.com/gofiber/fiber/blob/v2.52.5/middleware/csrf/csrf.go)
middleware post-CVE-2023-45128 (fixed in v2.50.0,
[GHSA-94w9-97p3-p368](https://github.com/gofiber/fiber/security/advisories/GHSA-94w9-97p3-p368))
is a sound storage-validated double-submit design with an Origin/Referer
check, default header `X-Csrf-Token`. It is not naive double-submit. But its
token binding wants either Fiber's session manager (which we are not using)
or its own `Storage` (default in-memory, so tokens die on restart, or the
same sqlite3 adapter problem again), and it forces the SPA to fetch and
replay a token on every mutation and after every token expiry (default 1h).
That is real frontend plumbing for no security gain over the origin check in
this same-origin deployment. If a token layer is ever wanted (e.g. the app
grows cross-origin API consumers), OWASP's pick is the signed double-submit
cookie or synchronizer token; that can sit on top of the custom session
store later.

### OIDC callback

The provider redirects back via a cross-site top-level GET. Under
`SameSite=Strict` the pre-auth cookie (holding `state`/nonce/PKCE verifier)
would not be sent on that navigation and the callback could not correlate
the response; under Lax it is sent (top-level navigation + safe method), so
**Lax is required, Strict breaks OIDC**. The callback's own CSRF protection
comes from the protocol, per the
[OAuth 2.0 Security BCP](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-security-topics):
"Clients MUST prevent Cross-Site Request Forgery" via the `state` parameter,
and "PKCE provides robust protection against CSRF attacks even in presence
of an attacker that can read the authorization response." The BCP never
leans on SameSite for this. The origin-check middleware must not apply to
the callback (it is a GET, so it naturally will not).

### SSE

`EventSource` is a subresource GET, not a top-level navigation, so a Lax
cookie is sent on the same-origin stream and never cross-site. GETs are
read-only, so the CSRF middleware ignores the stream entirely. No
interaction to handle.

## Sources

- Fiber v2.52.5 source: [session/config.go](https://github.com/gofiber/fiber/blob/v2.52.5/middleware/session/config.go),
  [session/session.go](https://github.com/gofiber/fiber/blob/v2.52.5/middleware/session/session.go),
  [session/store.go](https://github.com/gofiber/fiber/blob/v2.52.5/middleware/session/store.go),
  [csrf/csrf.go](https://github.com/gofiber/fiber/blob/v2.52.5/middleware/csrf/csrf.go),
  [csrf/config.go](https://github.com/gofiber/fiber/blob/v2.52.5/middleware/csrf/config.go),
  [app.go (Storage interface)](https://github.com/gofiber/fiber/blob/v2.52.5/app.go)
- [gofiber/storage sqlite3 adapter](https://github.com/gofiber/storage/blob/main/sqlite3/sqlite3.go)
- [CVE-2023-45128 / GHSA-94w9-97p3-p368](https://github.com/gofiber/fiber/security/advisories/GHSA-94w9-97p3-p368)
- [OWASP CSRF Prevention Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html)
- [OWASP Session Management Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html)
- [MDN Set-Cookie / SameSite](https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Set-Cookie)
- [MDN Cookies guide](https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Cookies)
- [OAuth 2.0 Security BCP (draft-ietf-oauth-security-topics)](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-security-topics)
