# ADR 0011: Rotate Next up on Reveal

Status: accepted (2026-10-04)

## Context

Next up used to rotate on watch. One member held the turn through draw,
Reveal, and watch, and the turn passed only when that member marked the draw
watched. The drawer therefore had to come back later to mark their own draw
watched before the next member could draw.

The group watches a draw on a later movie night. The member who is next on that
night is the natural person to close the previous draw and start the next one.
Rotating on the draw itself is not an option: the drawer must still confirm
their own Reveal, and the next member must not skip the reel by watching an
unrevealed draw.

## Decision

Rotate Next up on Reveal. A turn is now: mark the previous Current draw watched,
draw, and reveal. Every Reveal path commits the handoff in the same writer
transaction as the Reveal:

- `RevealDrawAndAdvanceNextUp` serves the drawer's confirm and the server-owned
  auto-reveal deadline.
- `WatchCurrentDraw` commits the handoff only when the watch is also the
  draw's Reveal (an early watch). A watch of a revealed draw does not rotate.

The handoff rotates whenever more than one Turn participant exists. It no
longer waits for a non-empty pool: the next member still owes the watch of the
last pooled movie, and then holds the turn until the pool has movies again.

The movie service passes the committed handoff to `OnRevealed` as part of a
`Reveal`. The server broadcasts `movie:revealed` and then
`settings:next-up-changed` from that one hook, so all three Reveal paths
publish the same frames.

This replaces the rotation-on-watch decision recorded in ADR 0002 and issue
#96. The scoped transaction pattern from ADR 0002 stays the same.

## Consequences

- The drawer keeps the turn through the reel and their own confirm. After the
  Reveal, only the next member (or an admin) can mark the draw watched and draw.
- The auto-reveal timer now writes `next_up`. It runs outside the HTTP command
  lock, but it is a single-shot, generation-bound flip under the movie service
  mutex, so it cannot rotate twice for one draw.
- A failed handoff rolls back the Reveal. The draw stays held, the auto-reveal
  retry rules apply, and clients receive no Reveal publication.
- A revealed draw that was waiting when this change shipped still belongs to
  its drawer. That member marks it watched without rotation, draws once more,
  and the new rule applies from that Reveal. No data migration is needed.
- If an admin moves the next-up holder to Guest during the reel, the role
  change hands the turn on and the Reveal then rotates once more. The reel is
  short, so this edge case is accepted.
