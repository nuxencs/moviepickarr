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

## Infrastructure

- `internal/repository/sqlite.go`: SQLite repository implementations.
- `internal/repository/movie_metadata.go`: `movie_metadata` repository (upsert / get / batch-get-by-ids / needs-enrichment).
- `internal/db/*`: DB open/migrations + Bolt->SQLite migration.

## API

- API served under `/api/v1/*` only.
- Resource ID operations use path params only (no query/body ID fallbacks).
- Error responses use `application/problem+json`.

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
exponential backoff + jitter on 5xx/network). Successful upserts broadcast a
`movie:enriched` SSE event. Enrichment is skipped entirely when `TMDB_API_KEY`
is unset.

Env knobs (all optional; sensible defaults): `TMDB_ENRICH_MIN_INTERVAL_MS`
(250), `TMDB_ENRICH_MAX_RETRIES` (4), `TMDB_ENRICH_BACKOFF_MS` (500),
`TMDB_ENRICH_QUEUE_SIZE` (256), `TMDB_ENRICH_BATCH_LIMIT` (200),
`TMDB_ENRICH_REFRESH_INTERVAL` (`1h`, `0` disables), `TMDB_ENRICH_TTL` (`720h`).
