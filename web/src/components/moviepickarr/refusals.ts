// Why a board's own control is inert right now, and what it says about it.
//
// Three things refuse a move on your own board: the pool is full, the round is
// locked, a draw is out and unrevealed. All three are temporary, so the control
// stays where it is and goes inert — absence is reserved for the permanent
// boundary, this is not your board (see ownership.ts). And all three are
// all-or-nothing across the whole wall: a full pool refuses every promote
// exactly as a locked round refuses every control, so there is no per-tile fact
// for a per-tile mark to express and the board draws nothing for a refusal.
// What is left is a string, which is why the rule lives here as a pure function
// and the markup only spends it.
//
// The movie modal's delete (#237) is refused by two of the same three flags and
// says so in the same words, so its rule lives here too — one place where the
// round and the draw are turned into a reason, whatever surface asks.

import { ROUND_CLOSED } from "@/components/moviepickarr/poolLock";

import type { MovieStatus } from "@/types/Response";

/** The one control a tile carries: promote on a stash poster, demote on a pool one. */
export type ActionKind = "promote" | "demote";

/** Why the action is refused, or null when it isn't. */
export type Refusal = "unavailable" | "drawing" | "locked" | "full";

const VERB: Record<ActionKind, string> = {
  promote: "Move to pool",
  demote: "Move back to stash",
};

const REASON: Record<Refusal, string> = {
  unavailable: "round state unavailable",
  drawing: "a draw is in progress",
  // The same words the status line uses for the same flag, from the same
  // constant, so the line and the control cannot describe the round differently.
  locked: ROUND_CLOSED,
  full: "pool is full",
};

/**
 * Which refusal an action meets, if any.
 *
 * Precedence is drawing > locked > full: the board says the part you cannot
 * already see. A full pool is on screen twice before anyone hovers anything, as
 * three filled slots and three gold pips, while a locked round and a draw in
 * flight exist on this page only in the status line. That ordering is also the
 * fix for a locked full pool reporting "pool is full", which carried no lock
 * signal at all.
 */
export function refusalOf({
  kind,
  isLocked,
  drawInFlight,
  poolFull,
  stateKnown = true,
}: {
  kind: ActionKind;
  isLocked: boolean;
  drawInFlight: boolean;
  /** Only ever read for a promote: demoting is the way out of a full pool. */
  poolFull: boolean;
  /** False while the server-owned round gates are missing or refreshing. */
  stateKnown?: boolean;
}): Refusal | null {
  if (!stateKnown) return "unavailable";
  // A draw freezes the pool and nothing else, identically across all three
  // tiles so that no per-tile difference singles out the held winner. The stash
  // is untouched, so a promote is still live while a draw is out.
  if (kind === "demote" && drawInFlight) return "drawing";
  if (isLocked) return "locked";
  if (kind === "promote" && poolFull) return "full";
  return null;
}

/**
 * What the control is called: the action, then the reason it won't run.
 *
 * One string for both the accessible name and the tooltip, so the shown and the
 * spoken reason cannot drift. The reason stays on every control rather than
 * being lifted somewhere central, against the accessibility recommendation and
 * knowingly: browse mode walks all 120 elements whatever the tab order does, so
 * a locked wall really does say the reason about sixty times. That is the cost
 * of an all-or-nothing refusal, and the alternative is a control that goes
 * inert without saying why.
 */
export function actionLabel(kind: ActionKind, refusal: Refusal | null): string {
  return refusal ? `${VERB[kind]}, ${REASON[refusal]}` : VERB[kind];
}

/** The statuses a film can be deleted from — the same two the server accepts.
 *  A watched film is history and the held winner is mid-draw, so neither is
 *  offered the control at all: absence is the permanent boundary here, as it is
 *  on a board that isn't yours. */
export function isDeletable(status: MovieStatus | undefined): boolean {
  return status === "stash" || status === "pool";
}

/**
 * Why deleting this film is refused, or null when it isn't.
 *
 * Restates the server's own two refusals (movie.Service.Delete): a draw in
 * flight freezes the pool, and a locked round fixes the candidate set, so both
 * refuse a pool film and neither touches a stash one. Stash adds aren't
 * lock-checked, so stash deletes aren't either.
 *
 * Precedence is drawing > locked, matching refusalOf: a mid-draw locked pool
 * film reads `a draw is in progress`, which is the part that will pass on its
 * own in a minute.
 */
export function deleteRefusalOf({
  status,
  isLocked,
  drawInFlight,
  stateKnown = true,
}: {
  status: MovieStatus | undefined;
  isLocked: boolean;
  drawInFlight: boolean;
  /** False while the movie lifecycle or a required round gate is unavailable. */
  stateKnown?: boolean;
}): Refusal | null {
  if (!isDeletable(status)) return null;
  if (!stateKnown) return "unavailable";
  if (status === "stash") return null;
  if (drawInFlight) return "drawing";
  if (isLocked) return "locked";
  return null;
}

/** What the modal's delete button is called: the verb, then the reason it won't
 *  run. One string for the accessible name and the tooltip, as on a tile. Not
 *  an ActionKind, because delete is not one of the two moves a tile carries —
 *  it lives on the film's own record — but it refuses in the same words. */
export function deleteLabel(refusal: Refusal | null): string {
  return refusal ? `Delete, ${REASON[refusal]}` : "Delete";
}
