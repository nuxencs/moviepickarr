# ADR 0004: Typed integration modules behind shared configuration

Status: accepted (2026-08-04)

## Context

TMDB is the first integration with settings managed inside the app. Future
integrations may need richer setup flows. Radarr, for example, needs connection
details followed by server-provided choices such as root folders and quality
profiles. A generic settings form would make simple fields cheap, but would
fight those dependent and integration-specific workflows.

## Decision

Use typed integration modules behind a shared integration framework. The shared
layer owns persisted values, environment-over-admin precedence, defaults,
write-only secret handling, connection status, test actions, runtime updates,
and common Admin UI components. Each module owns its typed configuration,
validation, connection test, runtime behavior, form, and specific actions.

This is an internal application seam, not a runtime plugin system. Adding an
integration still requires application code. Do not build a generic schema that
turns arbitrary setting rows into forms, and do not pass string maps into
integration code.

## Consequences

TMDB and later integrations share configuration mechanics without forcing
their setup flows into the same shape. A future Radarr form can fetch and show
root folders or quality profiles while reusing the same source indicators,
secret controls, save behavior, and connection status as TMDB.
