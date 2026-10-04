# ADR 0012: Replace the admin turn exception with a Turn skip

Status: accepted (2026-10-04)

## Context

The Next up gate let an admin draw, reveal, and mark watched at any time. This
was the only rule an admin could bypass. It had three problems:

- An admin draw out of turn still rotated Next up on Reveal. The member who
  held the turn lost it without doing anything.
- The reveal exception had no real use. The server-owned auto-reveal deadline
  already closes a reel that nobody confirms. The exception only let an admin
  cut another member's reel short.
- The bypass was silent. Nothing in the app showed that an admin had acted in
  place of the turn holder.

The draw and watch exceptions did one useful thing: they let the group move on
when the turn holder was away. Without them, the only way past that member was
to make them a Guest, which also changes their role.

## Decision

Remove the admin exception. `requireNextUp` applies to every Turn participant,
admins included, and the client turn gate drops its admin case.

Add an explicit admin Turn skip: `POST /settings/next-up/skip` with the holder
the admin saw (`{"memberId": n}`). `SqliteNextUpRepository.Skip` checks the
holder, checks for an unrevealed draw, and rotates with `advanceNextUpTx`, all
in one write transaction. The handler takes `drawCommandMu`, so a skip never
interleaves with a draw, Reveal, or watch authorization. It broadcasts
`settings:next-up-changed`.

The skip is refused when:

- the stored holder is not the member the admin saw (409 `next_up_changed`),
- a draw is unrevealed (409 `draw_not_revealed`), because the drawer still owns
  that turn and the Reveal rotates on its own,
- only one Turn participant exists (409 `conflict`).

The control is a ghost button in the hero's Next up chip. It renders only for
admins and asks for confirmation, because a skip cannot be undone.

## Consequences

- An admin takes part in the draw workflow only on their own turn.
- A stuck turn now takes one deliberate, visible admin step instead of a hidden
  bypass, and it never consumes a turn as a side effect of a draw.
- A skip does not touch the Current draw. The next holder still marks it
  watched before they draw.
