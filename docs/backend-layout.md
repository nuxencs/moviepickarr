# Backend Layout

## Entry

- `main.go`: thin bootstrap only (embed web assets + invoke server runtime).

## Runtime

- `internal/server/server.go`: app lifecycle, middleware, route registration.
- `internal/server/handler_base.go`: shared handler type + common parsing/sanitization helpers.
- `internal/server/users_handlers.go`: members (roster) bounded context handlers.
  Reads are any-authenticated; create/delete/restore are admin-only via an inline
  `requireAdmin` guard. `DELETE /members/:memberID` removes a member as one action
  with two outcomes (reported in the 200 body as `{outcome}`): a member who
  authored no movies is hard-deleted (credentials/sessions/invites cascade,
  `next_up` nulls, name freed); a member who authored movies is archived
  (`archived_at` set, login rows stripped, row and attribution kept). `POST
  /members/:memberID/restore` clears `archived_at` and re-issues a fresh claim
  link in one action (archiving stripped the credentials), returning the roster
  row plus the one-time `claimUrl`. The membership reads filter `archived_at IS
  NULL` (`UserRepo.List` backs the roster and the rotation candidate list;
  `FindByID` gates the per-member pool/stash reads; the `next_up` join drops a
  pointer left on an archived member), so archived members leave the active
  board. The all-time stats leaderboard is the opposite case on purpose: it is
  an attribution view of watch history keyed by adder name, so an archived
  member keeps their historical rows (the whole reason archive preserves the
  `users` row instead of deleting it).
- `internal/server/movies_handlers.go`: movies bounded context handlers.
- `internal/server/settings_handlers.go`: settings bounded context handlers.
- `internal/server/stats_handlers.go`: stats bounded context handlers.
- `internal/server/stats_filters.go`: the stats filter value (`statsFilters`) plus its
  parse / cache-key / matcher / echo logic (genre, actor/crew ids, release year/decade,
  addedBy): one filter representation shared by the parser, the SQL/in-memory matcher,
  and the response echo.
- `internal/server/tmdb_handlers.go`: TMDB bounded context handlers.
- `internal/server/events_handlers.go`: SSE/events bounded context handlers.
- `internal/server/errors.go`: centralized domain-to-HTTP error mapping.
- `internal/server/tmdb.go`: TMDB API client adapter (search, reverse lookup + details with rate-limit/retry, and `DiscoverPopularPosters` on `/discover/movie` for the poster wall).
- `internal/server/poster_wall.go`: the public poster-wall cache + endpoint. An in-memory `[]string` of up to 20 poster paths (popularity order, null posters dropped), warmed once in a background goroutine on boot and refreshed every 7 days, keeping the last good list on a failed warm. Nil when no `TMDB_API_KEY` (mirrors the `enrichRunner` guard); `GET /auth/poster-wall` then serves `[]`. The route sits ahead of `requireSession` beside `/auth/config`, carries no secrets, and rides the `csrfGuard` safe-method exemption.
- `internal/server/enrichment.go`: TMDB enrichment use case (`EnrichOne`: link → IMDb id → reverse lookup → details → upsert).
- `internal/server/enrich_worker.go`: background enrichment worker (queue, rate limiter, backfill/refresh drain, config).
- `internal/server/events.go`: SSE broker.
- `internal/server/auth_middleware.go`: the request-time auth chain. `csrfGuard`
  (origin check on unsafe methods, fail-closed) runs before `requireSession`
  (session cookie → live actor via `SessionManager`, 401 + cookie-clear on any
  miss), the `mpa_session` cookie set/clear helpers, the scheme-derived Secure
  flag, and the startup + hourly expired-session sweep.
- `internal/server/auth_handlers.go`: the local username/password endpoints.
  `POST /auth/login` (uniform `401 {"error":"invalid credentials"}` on every
  credential miss, `204` + minted cookie on success), `GET /auth/me` (identity +
  presence-derived `hasLocalLogin`/`hasLinkedIdentity` + `otherSessions`, the
  count of the actor's other live devices for the account page), `POST
  /auth/password` (verify current, then revoke-all + fresh mint to revoke other
  devices and rotate the current token), `POST /auth/logout` (empty/`{}` ends
  this device, `{"all":true}` ends every session; always clears the cookie and
  `204`), and the admin `PUT`/`DELETE
  /members/:memberID/local-login` upsert/remove (gated by an inline admin check).
  It also holds the shared authz guards used across handlers: `requireAdmin`
  (403 `admin_required`) and `requireNextUpOrAdmin` (403 `not_next_up`).
  `/auth/login` is registered ahead of
  `requireSession` so it stays reachable without a session; it is still behind
  `csrfGuard`.
- `internal/server/invite_handlers.go`: the invite/claim onboarding endpoints.
  Admin issuance `POST /members/:memberID/invite` (re-issue/regenerate, revokes
  the old link) and `DELETE /members/:memberID/invite` (revoke, `404` when
  nothing valid); `POST /members` (in `users_handlers.go`) issues the first
  invite and returns the one-time `claimUrl` in the response only, never over
  SSE. The unauthenticated claim pair `GET /auth/claim/:token` (returns
  displayName + placeholder-vs-reset mode + options, or the two distinct
  `invite_invalid` 404 / `invite_used` 410 states) and `POST
  /auth/claim/:token/password` (placeholder sets username+password, reset sets
  password-only + revokes all sessions; both consume the invite once and mint a
  session → `204` + cookie) sit ahead of `requireSession`. `POST
  /auth/local-login` is the authed self-serve completeness path (first
  username+password for a member with no local login, `409` if one exists).
- `internal/server/models.go`: API DTO mapping via two compiler-enforced wire classes:
  `leanMovieTile` (list/tile payload: identity + tile-level enriched fields) and
  `fullMovie` (detail payload: embeds `leanMovieTile` plus the film's client-visible
  `status`, the
  draw/reveal coordination fields `drawnAt`/`revealAt`/`serverNow`/`drawClientId`/`revealed`,
  modal metadata, and `cast`/`crew`). A handler returning `leanMovieTile` cannot accidentally ship credits or
  prose, which keeps the list payloads small; the mappers (`toLeanTile`/`toFullMovie` and
  their slice forms) are the single projection from `domain.Movie` to the wire.
  Both shapes preserve the adder's id and name. `addedByArchived: true` appears only
  when that attribution belongs to an archived member, so clients keep the credit
  without linking to an active board that no longer exists. Active rows omit the flag.

## Domain + Use Cases

- `internal/domain/*`: entities, repository ports, typed domain errors.
- `internal/{user,movie,nextup,settings}`: service-layer use cases. Services
  are concrete structs (no service interfaces, nothing consumes them
  polymorphically); the repository ports in `internal/domain` remain the
  substitution seam for tests.
- `internal/nextup`: owns the whole next-up rotation. `Get` self-seeds a
  fresh install with the first roster member; `Advance` passes the turn (only
  while the pool still has movies and more than one member exists) and reports
  whether it moved, so the handler only broadcasts. The handler calls `Advance`
  on **watch**, not draw (rotation-on-watch, Model B): next up stays the runner
  across the whole draw → reveal → watch cycle and passes only once the movie is
  actually watched.
- `internal/auth`: shared auth primitives. `token.go` is the opaque-token
  generator + SHA-256 storage hash; `password.go` is the argon2id wrapper;
  `session.go` is the `SessionManager` deep module over the session store:
  `Mint` (fresh token, 90-day absolute cap, fixation-safe), `Authenticate`
  (validate both windows, slide `last_seen_at` only when >1h stale, read role
  live), `RevokeCurrent`/`RevokeAll`/`RevokeOthers`, `CountOtherSessions` (live
  sessions besides the current, for the account page's other-device count), and
  `Sweep`. Lifetimes are
  hardcoded (30-day idle, 90-day absolute); an injectable clock makes expiry
  testable without sleeps. `local.go` is the `LocalAuth` deep module over the
  local-login flow: `Login` (timing-equalized via `DummyVerify`, self-healing
  10-strike/15-min lockout, rehash-on-login, `last_login_at` bump),
  `ChangePassword`, the admin `SetLocalLogin`/`DeleteLocalLogin` (username 3–32
  `[a-zA-Z0-9._-]`, NOCASE collision → `ErrConflict`, self-last-credential
  guard), and the `/me` `Identity` projection. Password bounds (min 8, max 128,
  the max a DoS guard) and the uniform `ErrInvalidCredentials`/`ErrNoLocalLogin`
  sentinels live here; it shares the injectable clock with `SessionManager`.
  `SetFirstLocalLogin` (self-serve first credential, `ErrConflict` if one
  already exists) also lives on `LocalAuth`. `invite.go` is the `InviteManager`
  deep module over the invite/claim flow: `Issue` (revoke-then-insert, enforcing
  one valid invite per member, 7-day `InviteTTL`), `Revoke`, `Validate`
  (time-derived state machine → `ClaimContext` or the `ErrInviteInvalid` /
  `ErrInviteUsed` sentinels), and `ClaimPassword` (reuses `LocalAuth.SetLocalLogin`
  for the placeholder/reset upsert, then consumes the invite). It shares the same
  injectable clock so invite expiry is testable without sleeps; session mint and
  cookie handling stay in the HTTP layer. `oidc.go` is the `RelyingParty` deep
  module over the go-oidc/oauth2 protocol: `OIDCConfigFromEnv` (presence-derived
  enablement from the `MPA_OIDC_*` quartet), one-time discovery in
  `NewRelyingParty`, `AuthCodeURL` (state + nonce + S256 PKCE), and `Exchange`
  (code exchange, ID-token verify, nonce compare → `OIDCClaims`). `oidc_tx.go` is
  the `OIDCTxCodec` that seals/opens the `mpa_oidc_tx` cookie with AES-256-GCM
  (ephemeral key by default, `MPA_OIDC_TX_SECRET` override, ~10-min TTL, uniform
  `ErrTxInvalid` on tamper/expiry) plus the S256 PKCE helper. `oidc_link.go` is
  the `IdentityLinker` over `oidc_identities`: `Login` (match `(issuer, subject)`,
  refresh snapshots), `Link` (the collision matrix on both UNIQUEs, shared by the
  link and claim intents, idempotent for the same member), and `Unlink` (self
  last-credential guard → `ErrConflict`). All three OIDC modules take
  already-verified inputs and never touch an `http.Request` or a session; the RP
  runs off no clock, the tx codec and linker share the injectable one.
- `internal/movie`: owns the whole draw/reveal lifecycle, including the
  server-authoritative auto-reveal. `DrawRandom` picks a pooled movie, records an
  in-memory `ActiveDraw` (`DrawnAt`/`RevealAt`/`DrawClientID`/`Revealed`), and arms a
  timer at `RevealAt = DrawnAt + AutoRevealDelay` (`DefaultAutoRevealDelay` 16.5s,
  overridable via `DrawConfig`). `RevealCurrentDraw` is the once-per-draw flip fired by
  the drawer's confirm or the timer. An early watch performs the same flip before it
  clears the active draw. All paths call the `OnRevealed` hook exactly once. The HTTP
  handler's `movieNightMu` serializes draw, reveal, and watch from next-up
  authorization through synchronous lifecycle event publication. Watch keeps that
  command lock through next-up rotation, so an outgoing holder cannot start the next
  draw with stale authorization. The server serializes `RevealAt`/`ServerNow` into
  every draw payload so clients
  time their confirm countdown off `revealAt − serverNow` (skew-immune) and broadcasts
  `movie:drawn` / `movie:revealed`. It also owns the pool *view*: `Pooled`,
  `PooledByUserID` and the detail read `GetForDisplay` do not expose the held
  winner's persisted `current` status. The list reads use `withHeldDraw`; the
  detail read shares its held display projection. The row is already `current`,
  only the display copy reads as pooled. `DrawRandom` holds the draw mutex across
  the repository transition and in-memory publication, so display and gate reads
  cannot pass through the half-published state. Without it a pool read
  during the reel — a reload mid-spin, or a client opening the board — would drop
  the tile and give the winner away before the reveal. Two consequences ride
  along: while a draw is unrevealed the pool is **frozen** (demote and delete
  answer `ErrDrawInProgress` for every pool tile, the held winner included, so no
  answer singles it out; stashes stay editable), and the held tile still **counts
  against the per-user cap** (`poolLimit`), so a draw doesn't quietly buy its
  adder a fourth slot. The **pool lock** is the other refusal on the same rows:
  with it set, `Delete` answers `ErrPoolLocked` for a pooled movie, the way the
  move handler already does for promote and demote, so a locked pool can't be
  shrunk out from under the draw it was locked in for. The caller reads the lock
  and passes it in (`settingsService.GetPoolLock`); the service orders the two
  refusals so an unrevealed draw still answers first. Stashes stay deletable:
  adds aren't lock-checked either, and the lock is the pool's, not a member's
  list's.

## Logging

- `internal/logger/logger.go`: builds the root [zerolog](https://github.com/rs/zerolog)
  logger from `LOG_LEVEL`/`LOG_FORMAT` (JSON for prod, colourised `console` for
  dev). `server.Run` builds it once, mirrors it to the zerolog global, and injects
  `component`-tagged sub-loggers into the handler (`http`) and enrichment worker
  (`enrich`). HTTP access logs use the `fiberzerolog` middleware, ordered after
  `requestid` so each line carries the request id; the SSE stream is skipped.
  Full reference: [`LOGGING.md`](LOGGING.md).

## Infrastructure

- `internal/repository/sqlite.go`: SQLite repository implementations.
- `internal/repository/movie_metadata.go`: `movie_metadata` repository (upsert / get / batch-get-by-ids / needs-enrichment).
- `internal/repository/movie_credits.go`: `people` + `movie_credits` repository (transactional replace / batch-get-by-ids).
- `internal/repository/session.go`: `sessions` repository (create / find-by-token-hash joined to the member's live role / touch-last-seen / per-token, per-member, and revoke-others deletes / expiry sweep).
- `internal/repository/local_account.go`: `local_accounts` repository (find by NOCASE username / by user id, create with unique→`ErrConflict` + FK→`ErrNotFound` translation, password-hash and admin-reset updates, failed-attempt/successful-login lockout writes, delete) plus the `oidc_identities` presence read and the `/me` member-identity join.
- `internal/repository/invite.go`: `invites` repository (create with FK→`ErrNotFound`, revoke-valid-by-user returning the affected count for one-valid-invite enforcement, find-context-by-token-hash joining the member's display name + local-login presence for the claim page, mark-used). Validity is time-derived in SQL (`used_at IS NULL AND revoked_at IS NULL AND expires_at > now`).
- `internal/db/*`: DB open/migrations + Bolt->SQLite migration.

### SQLite connections & timestamps (migration `007`)

- `db.OpenSQLite` returns a `db.Pool` with **two handles over one WAL file**: a
  single-connection `Write` (serializes all mutations) and a small `Read` pool
  (WAL readers don't block behind the writer). Repos route reads to `Read`,
  mutations to `Write`; `Pool.Close` runs `PRAGMA optimize` first.
- Timestamps are stored as **INTEGER unix epoch seconds** (UTC by definition)
  and must be bound via `db.ToUnix`/`db.ToUnixPtr` — never as a raw
  `time.Time` (the driver would store TEXT, which the STRICT tables reject
  outright). Scanning goes through `db.FromUnix` (repos use `unixTimePtr`).
  Migration `007` converted the three historical text formats and rebuilt
  `movies`, `users`, and `movie_metadata` as STRICT with: `status <->
  watched_at` coupling, a partial UNIQUE on `tmdb_id`, a partial UNIQUE on
  `status = 'current'` (at most one current movie, race-proof), and
  `added_by_id ON DELETE RESTRICT` (a member who authored movies is archived,
  not deleted, so watch history is never cascaded away; see the member
  delete/archive path below). In-SQL stamps use `unixepoch()`
  (defaults and the enrichment upsert/credits stamp), not CURRENT_TIMESTAMP.
  The rebuilds preserve the AUTOINCREMENT high-water mark so deleted ids are
  never reused. Deploy check: `go run scripts/verify_migration_007.go
  <db-copy>` validates the migration against a copy of a production DB.
- Constraint errors are matched by driver code, not message text
  (`db.IsUniqueViolation` / `db.IsForeignKeyViolation`) and surface as
  `domain.ErrConflict` → HTTP 409: duplicate `tmdbId` adds delete their
  just-inserted stash row and 409; the enrichment worker treats a tmdb-id
  conflict as non-fatal (metadata/credits still persist so the row leaves the
  backlog).
- Migration files: `-- migrate:fk_off` on the first line makes the runner wrap
  the migration in the SQLite table-rebuild procedure (FKs off around the tx +
  `foreign_key_check` before commit). Version numbering has a permanent gap at
  `002` (the reverted auth schema; `003` dropped its remnants).
- Pre-migration backups: when migrations are pending against a previously
  migrated DB, startup runs `PRAGMA integrity_check` and snapshots the file via
  `VACUUM INTO` (`<DB_FILE>.vNNN-<utc>.backup`, retention `DB_BACKUP_MAX`,
  default 3, `0` disables) before applying anything. Fresh installs are never
  backed up. See `internal/db/backup.go`.

## API

- API served under `/api/v1/*` only.
- Resource ID operations use path params only (no query/body ID fallbacks).
- Error responses use `application/problem+json`.
- Responses (and the embedded SPA assets) are gzip-compressed via the `compress`
  middleware. The SSE stream (`/api/v1/events`) is excluded — compression buffers
  the body, which would break its per-event flush.

### Actor & authorization

The whole `/api/v1` group runs behind `csrfGuard` → `requireSession`, so every
handler below has a live actor (`memberID` + `role`) in `c.Locals`. The actor is
always the session member, never a path id: member-scoped routes carry no
`:userID`, so no one can act as someone else by editing a URL.

- Surface: `/members` (`:memberID`) replaces the old `/users`/`:userID`. Movie
  mutations carry no actor id — `POST /movies` (adder = session), `PUT`/`DELETE
  /movies/:movieID`, `POST /movies/:movieID/move`. Member pool/stash reads are
  `GET /members/:memberID/pool` and `/stash`.
- Status matrix: `401` not authenticated (from `requireSession`); `403` with a
  machine `code` in the problem `title` (`admin_required`, `not_next_up`,
  `not_adder`) for authenticated-but-forbidden; `404` only for genuinely-missing
  resources, never a permission mask.
- Rules: reads are any-authenticated (`GET /members`, pool/stash, movies / pool /
  stats / settings reads, `/tmdb/search`). `PUT`/`DELETE /movies/:movieID` and
  `/move` are **adder-only** (403 `not_adder`, no admin override). Draw / reveal /
  watch are **next-up-or-admin** (403 `not_next_up`). Member
  create/delete/restore, pool-lock, local-login admin actions, and invite
  issuance/revocation (`POST`/`DELETE /members/:memberID/invite`) are
  **admin-only** (403 `admin_required`). The claim endpoints (`GET`/`POST /auth/claim/:token...`) and
  `POST /auth/login` are unauthenticated (no session yet); `POST
  /auth/local-login` is any-authenticated self-serve.
- SSE (`GET /events`): authed at the handshake (401 before the stream opens) and
  revalidated on every heartbeat, so a session revoked mid-stream is dropped on
  the next tick.
- Pool state (`GET /settings/pool-lock`) returns `{poolLocked, drawInProgress}`.
  The second field is the server-owned unrevealed-draw gate, not a client reel
  phase. Members and pooled movie actions use the one response for both pool
  mutation refusals.

## Movie identity & enrichment (TMDB)

A movie's stable identity is its `tmdb_id` / `imdb_id` (columns on `movies`,
added in migration `004`). There is **no stored link** — migration `005`
backfilled `imdb_id` from legacy links and dropped the `movies.link` column. The
link is **derived** on read (`models.go::movieLink`: IMDb URL → TMDB URL → `""`),
and the API still exposes a `link` field. `movie_metadata` (1:1, FK cascade)
holds only enriched **display** fields (overview, poster/backdrop, runtime,
genres-as-JSON-names, rating, tagline, `enriched_at`).

Movie responses **fold in this display metadata**: every movie-returning GET
(pool / watched / current / member pool / stash / `GET /members`) batch-loads
`movie_metadata` via `MovieMetadataRepo.GetMetadataByMovieIDs` and emits the
extra `omitempty` fields (`posterPath`, `backdropPath`, `releaseDate`, `runtime`,
`genres`, `voteAverage`, `tagline`, `overview`) on `movieResponse`
(`models.go::toAPIMovieMeta`). Paths are raw TMDB paths (e.g. `/abc.jpg`); the
frontend builds the image URL. Fields are absent for not-yet-enriched movies, so
the UI degrades to a procedural placeholder poster. SSE broadcast payloads stay
metadata-free — the client refetches the enriched GETs on each event.

## Credits (people on movies)

Credits are persisted **normalized + trimmed** (migration `006`): a `people`
table keyed by TMDB person id (each person stored once, ever; name +
`profile_path` refreshed on re-enrich) and a `movie_credits` join table with a
`(movie_id, person_id, kind, job)` PK — so a person can be cast *and* crew on
the same movie, or crew with several jobs. Both FKs cascade on movie/person
delete.

Ingest happens inside the same enrichment call: `MovieDetails` appends
`?append_to_response=credits` (still a single TMDB request), and
`enrichment.go::mapCredits` trims the payload before persisting:

- **Cast**: sorted by billing order, deduped by person (dual roles merge their
  characters with `" / "`), then trimmed to `CastLimit` (env
  `TMDB_ENRICH_CAST_LIMIT`, default 15, `0` = full cast). Deepening the cast
  later is just a config change + re-enrich — no schema/UI impact.
- **Crew**: filtered to the job whitelist (Director, Writer, Screenplay,
  Original Music Composer, Director of Photography), deduped per (person, job).

`MovieCreditsRepo.ReplaceCredits` runs in one transaction: upsert people,
delete the movie's credit rows, insert the new ones, and stamp
`movie_metadata.credits_refreshed_at = CURRENT_TIMESTAMP`. It runs **after**
`UpsertMetadata` (the stamp lives on the metadata row) and also when the
mapped credits are empty, so credit-less titles get stamped instead of looping
the drain. A NULL `credits_refreshed_at` makes a movie a `NeedsEnrichment`
candidate — existing libraries **backfill credits automatically** on the first
drain after the migration.

Movie responses fold credits in alongside the metadata: the same GET endpoints
batch-load credits via `GetCreditsByMovieIDs` and emit `cast` / `crew` arrays
of `{id, name, profilePath?, character?|job?}` (`omitempty`; absent until
ingested). `cast` is in billing order; `crew` carries whitelisted jobs only.
SSE payloads stay credits-free, like metadata.

Add: a search add sends `tmdbId` (the `external_ids` endpoint is gone). A
manual/edit add still accepts a `link` in the request body, but only to extract
the IMDb id from it — nothing is stored as a link.

Enrichment (`EnrichOne`): if the movie already has a `tmdb_id` (search add /
prior enrichment) it goes straight to `GET /3/movie/{tmdb_id}`; otherwise it
reverse-looks-up `GET /3/find/{imdb_id}?external_source=imdb_id` first. Either
way it persists `tmdb_id`/`imdb_id` back onto the movie and upserts the display
metadata — so the costly `/find` runs at most once per movie, never on refresh.

A single background worker (`enrichRunner`) drives it: a startup backfill of
un-enriched rows, fire-and-forget enqueue when a movie is added or its link is
edited, and a periodic "drain" that re-enriches rows older than the TTL. TMDB
requests are paced by a min-interval rate limiter and retried (429 `Retry-After`,
exponential backoff + jitter on 5xx/network). Successful upserts are coalesced
into a single `movies:enriched-batch` SSE event per burst (debounced, with a hard
ceiling) rather than one event per movie — a backfill of many rows used to trigger
one invalidate-refetch wave per movie. Enrichment is skipped entirely when
`TMDB_API_KEY` is unset.

Env knobs (all optional; sensible defaults): `TMDB_ENRICH_MIN_INTERVAL_MS`
(250), `TMDB_ENRICH_MAX_RETRIES` (4), `TMDB_ENRICH_BACKOFF_MS` (500),
`TMDB_ENRICH_QUEUE_SIZE` (256), `TMDB_ENRICH_BATCH_LIMIT` (200),
`TMDB_ENRICH_BATCH_DEBOUNCE_MS` (500), `TMDB_ENRICH_BATCH_MAX_WAIT_MS` (2000),
`TMDB_ENRICH_CAST_LIMIT` (15, `0` = full cast),
`TMDB_ENRICH_REFRESH_INTERVAL` (`1h`, `0` disables), `TMDB_ENRICH_TTL` (`720h`).

## Stats

`GET /api/v1/stats` aggregates the watched history. Query params: `window`
(`24h`/`7d`/`30d`/`90d`/`1y`/`all-time`/`custom` + `start`/`end`), `tz`, and
the optional, combinable filters `genre` (case-insensitive name), `actorIds` /
`crewIds` (comma-separated positive TMDB person ids, ≤25 each, deduped +
sorted server-side; `actorIds` match cast credits, `crewIds` match crew
credits — any whitelisted job, since crew rows are job-whitelisted at ingest),
`addedByIds` (comma-separated user ids of the movie's adder, any-of),
and either `releaseYear` (exact year, 1870–2100) or `decade` (its floor — a
multiple of 10 in the same range, so `1990` ⇒ 1990–1999; mutually exclusive with
`releaseYear`). People lists are **any-of within a list and
AND-ed across filters**. Filters narrow **every** aggregate in the response —
including the `watchedByUser` counts and `countsByWindow` — to the matching
subset; movies that were never enriched fail any active filter (their
genre/year/people are unknown). One exception by design: `watchedByUser`
always carries a **row per roster member**, zeroed when nothing of theirs
matches — members never vanish from the leaderboard under any window or
filter. (Adders no longer on the roster keep their historical rows only
while their added movies pass the active filters.) The roster comes from
`userService.List`, so user create/delete both invalidate the stats cache.

The response also returns `matchedMovieIDs` — the ids of the films behind
`selectedWindowCount`, in watch-recency order — so the client can render the
exact matched set (the films-in-window rail) by joining against its cached
watched list, with no second fetch and no risk of the rail count drifting from
the KPI.

Besides the activity breakdowns (`countsByWindow`, `watchedByUser`,
`weekdayActivity`, `hourActivity`), the response carries enrichment-derived
aggregates for the selected window: `topGenres` (full list, count desc),
`topDirectors` / `topActors` (`{personId, name, profilePath?, count}`, capped
at 12, count desc then name), `releaseYears` (per-year histogram, ascending;
decade bucketing is the frontend's concern), `runtime`
(total/average/longest + title; zero runtimes are skipped), `averageRating`
(zero votes skipped; `0` when nothing qualifies) and `filters` (echo of the
active filters with `actors` / `crew` as `{personId, name}` arrays, names
resolved from the credit rows).

Responses are cached per `window|tz|start|end|genre|actorIds|crewIds|releaseYear|decade|addedByIds`
key (genre lowercased; id lists in canonical sorted form, so request order
can't split the cache) with a short TTL. The cache is invalidated when a movie is
watched/edited/user-deleted **and** once per enrichment burst
(`enrichRunner.onEnriched` → `invalidateStatsCache`, fired on each batch flush so
the TTL stays useful during a backfill), since stats now depend on
metadata/credits; the frontend hears the matching `movies:enriched-batch` SSE.
