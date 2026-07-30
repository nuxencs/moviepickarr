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

The movie-add flow from #157 does not need the cross-table seam. Its title,
stash status, adder, and stable identity land in one `INSERT`, so a uniqueness
failure leaves no partial row. The concrete repository wraps that insert and
the transaction-bound response read in one writer transaction. A failed read
therefore rolls back every effect of the insert, and no success response or
event is emitted before commit. The generic `Add` helper remains for fixtures,
but production code depends on the identified stash operation.

The break-glass admin seed uses the same scoped seam. `SeedAdmin` first runs an
authoritative name, archive-state, role, and local-login read on a writer
transaction. When a new login needs a password hash, that call returns a
read-only probe result without changing either table. The seed hashes the
password after the transaction releases the single writer, then calls the store
again. The second transaction repeats the authoritative read, creates or
promotes the active member, inserts the local login, and commits. An existing
login is preserved. Ambiguous and archived matches remain write-free.

This completes the three corrupting partial-write paths tracked by issue #157.

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
- A create operation that returns its inserted record keeps the response read
  inside the transaction when a failed read must also undo the insert.
- A failed break-glass login insert rolls back both a fresh admin row and an
  adopted member's role promotion. Constraint errors still fail boot, but
  leave the roster and credentials unchanged for the next retry.
- Password hashing stays outside the seed transaction. Configured no-op boots
  do not hash again, and a new seed does not hold SQLite's single writer during
  the Argon2 work.
- A future corrupting multi-table write can add another scoped operation under
  this pattern without exposing SQL transactions to handlers or services.
