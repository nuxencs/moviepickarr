# ADR 0006: Record integration runs without a generic job system

Status: accepted (2026-08-04)

## Context

TMDB refreshes need live progress and durable history. Future integrations will
need the same recording mechanics, but their execution, retries, pacing, and
operations will differ. Storing only the latest TMDB result would make that
later split expensive, while a generic background-job system would take on
unneeded scheduling and queue semantics.

## Decision

Use a shared integration run ledger with a small interface for starting a run,
reporting progress, finishing or interrupting it, and reading paginated history.
Each typed integration module still owns execution. The ledger records the
integration, operation, trigger, initiating admin when applicable, configuration
revision, status, timestamps, counts, a sanitized error summary, and bounded
failed-subject details.

Keep runs for 12 months, with a safety cap of 10,000 runs per integration.
Record single-subject work such as enriching a newly added movie. A scheduled or
manual scan that selects no subjects updates the integration's last-check state
but does not create a run. Do not turn the ledger into a generic scheduler,
distributed queue, automatic replay system, or store for raw remote requests and
responses.

Connection tests are transient diagnostics and do not create run records. A
library-wide run supports cooperative cancellation: stop scheduling new
subjects, let the active remote request finish, preserve completed updates, and
record the outcome as `Cancelled`. A process restart records an unfinished run
as `Interrupted` instead.

Admin exposes one shared run-history page across integrations, with filters for
integration, operation, status, and trigger. An integration page shows its
current or latest run and links to the shared history with that integration
filter applied.

## Consequences

TMDB and later integrations share Admin history and progress behavior without
sharing their workers. Non-integration background work stays outside this seam.
