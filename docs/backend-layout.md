# Backend Layout

## Entry

- `main.go`: thin bootstrap only (embed web assets + invoke server runtime).

## Runtime

- `internal/server/server.go`: app lifecycle, middleware, route registration.
- `internal/server/handler_base.go`: shared handler type + common parsing/sanitization helpers.
- `internal/server/users_handlers.go`: users bounded context handlers.
- `internal/server/movies_handlers.go`: movies bounded context handlers.
- `internal/server/settings_handlers.go`: settings bounded context handlers.
- `internal/server/stats_handlers.go`: stats bounded context handlers.
- `internal/server/tmdb_handlers.go`: TMDB bounded context handlers.
- `internal/server/events_handlers.go`: SSE/events bounded context handlers.
- `internal/server/errors.go`: centralized domain-to-HTTP error mapping.
- `internal/server/tmdb.go`: TMDB API client adapter (search, reverse lookup + details with rate-limit/retry).
- `internal/server/enrichment.go`: TMDB enrichment use case (`EnrichOne`: link → IMDb id → reverse lookup → details → upsert).
- `internal/server/enrich_worker.go`: background enrichment worker (queue, rate limiter, backfill/refresh drain, config).
- `internal/server/events.go`: SSE broker.
- `internal/server/models.go`: API DTO mapping.

## Domain + Use Cases

- `internal/domain/*`: entities, repository ports, typed domain errors.
- `internal/{user,movie,nextpicker,settings}`: service-layer use cases.

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
  `added_by_id ON DELETE RESTRICT` (deleting a member with movies is a 409;
  watch history is never cascaded away). In-SQL stamps use `unixepoch()`
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

## API

- API served under `/api/v1/*` only.
- Resource ID operations use path params only (no query/body ID fallbacks).
- Error responses use `application/problem+json`.
- Responses (and the embedded SPA assets) are gzip-compressed via the `compress`
  middleware. The SSE stream (`/api/v1/events`) is excluded — compression buffers
  the body, which would break its per-event flush.

## Movie identity & enrichment (TMDB)

A movie's stable identity is its `tmdb_id` / `imdb_id` (columns on `movies`,
added in migration `004`). There is **no stored link** — migration `005`
backfilled `imdb_id` from legacy links and dropped the `movies.link` column. The
link is **derived** on read (`models.go::movieLink`: IMDb URL → TMDB URL → `""`),
and the API still exposes a `link` field. `movie_metadata` (1:1, FK cascade)
holds only enriched **display** fields (overview, poster/backdrop, runtime,
genres-as-JSON-names, rating, tagline, `enriched_at`).

Movie responses **fold in this display metadata**: every movie-returning GET
(pool / watched / current / user pool / stash / `GET /users`) batch-loads
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
`addedByIds` (comma-separated user ids of the movie's adder/picker, any-of),
and either `releaseYear` (exact year, 1870–2100) or `decade` (its floor — a
multiple of 10 in the same range, so `1990` ⇒ 1990–1999; mutually exclusive with
`releaseYear`). People lists are **any-of within a list and
AND-ed across filters**. Filters narrow **every** aggregate in the response —
including the `watchedByUser` counts and `countsByWindow` — to the matching
subset; movies that were never enriched fail any active filter (their
genre/year/people are unknown). One exception by design: `watchedByUser`
always carries a **row per roster member**, zeroed when nothing of theirs
matches — members never vanish from the leaderboard under any window or
filter. (Pickers no longer on the roster keep their historical rows only
while their picks pass the active filters.) The roster comes from
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
