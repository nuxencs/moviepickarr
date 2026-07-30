# ADR 0002: Scoped transactions for cross-table writes

Status: accepted (2026-07-30)

## Context

ADR 0001 left two credential and invite sequences non-transactional because
their partial states are valid and recoverable. Issue #157 identified a
different class of multi-repository write: a partial commit can violate a
domain invariant.

Rotation-on-watch requires the current movie and next-up holder to change
together. The previous HTTP flow first committed the movie as watched, then
advanced `next_up`. A failed handoff was logged and suppressed, leaving a
watched movie with the outgoing member still holding the turn.

SQLite already serializes mutations through one writer connection. That
prevents competing writers from overlapping, but separate statements still
commit independently.

## Decision

Use a narrow, use-case-specific store operation when a cross-table transition
must be all-or-nothing:

- The store operation owns one transaction on `Pool.Write`.
- Every read used to derive the writes runs through that same transaction.
- The operation returns domain records, not `*sql.Tx`, and returns success only
  after commit.
- Services update process-local state and publish events only after the store
  reports a successful commit.
- Existing repositories stay unchanged unless a use case needs an atomic
  cross-table operation. There is no project-wide repository rewrite.

The first cross-table operation is `WatchCurrentAndAdvanceNextUp`. It validates
and updates the current movie, checks whether a pooled movie remains, and
conditionally reads the ordered active roster and raw next-up pointer. It
writes the new pointer when rotation applies, then commits. The movie service
depends on this operation through a consumer-side interface.

This is the scoped unit-of-work seam requested by #157. It completes the
watch-and-rotate part of that issue.

The movie-add flow from #157 does not need a transaction or the cross-table
seam. Its title, stash status, adder, and stable identity now land in one
`INSERT`. A uniqueness failure therefore leaves no partial row. The concrete
repository keeps its generic `Add` helper for fixtures, but production code
depends on the identified stash operation.

The break-glass admin path remains separate work.

ADR 0001 still governs its two named invite and claim flows. Their accepted
partial states do not become transactions under this decision.

## Consequences

- A failed next-up write rolls back the watched update. The active draw, reveal
  timer, stats cache, and client event stream stay untouched, so the request can
  be retried.
- The store operation intentionally knows the `movies`, `users`, and `next_up`
  schema. The transaction boundary follows the lifecycle invariant instead of
  a single repository table.
- Cross-table operations need integration tests at the SQLite boundary. Failure
  injection must prove rollback, not only returned errors.
- A future corrupting multi-table write can add another scoped operation under
  this pattern without exposing SQL transactions to handlers or services.
