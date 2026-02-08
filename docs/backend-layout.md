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
- `internal/server/tmdb.go`: external API client adapter.
- `internal/server/events.go`: SSE broker.
- `internal/server/models.go`: API DTO mapping.

## Domain + Use Cases

- `internal/domain/*`: entities, repository ports, typed domain errors.
- `internal/{user,movie,nextpicker,settings}`: service-layer use cases.

## Infrastructure

- `internal/repository/sqlite.go`: SQLite repository implementations.
- `internal/db/*`: DB open/migrations + Bolt->SQLite migration.

## API

- API served under `/api/v1/*` only.
- Resource ID operations use path params only (no query/body ID fallbacks).
- Error responses use `application/problem+json`.
