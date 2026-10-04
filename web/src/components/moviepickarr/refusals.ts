// Why a board's own control is inert right now, as a string. Temporary refusals
// go inert in place; absence is for "not your board" (ownership.ts). See
// docs/DESIGN.md.

import { ROUND_CLOSED } from "@/components/moviepickarr/poolLock";

import type { MovieStatus } from "@/types/Response";

export type ActionKind = "promote" | "demote";

export type Refusal = "unavailable" | "guest" | "drawing" | "locked" | "full";

const VERB: Record<ActionKind, string> = {
  promote: "Move to pool",
  demote: "Move back to stash",
};

const REASON: Record<Refusal, string> = {
  unavailable: "round state unavailable",
  guest: "guest role cannot add movies to the pool",
  drawing: "a draw is in progress",
  // Shared with the status line so both describe the round in the same words.
  locked: ROUND_CLOSED,
  full: "pool is full",
};

/**
 * Which refusal an action meets, if any. Precedence drawing > locked > full
 * names the part you cannot already see: a full pool is visible on the board.
 */
export function refusalOf({
  kind,
  isLocked,
  drawInFlight,
  poolFull,
  guest = false,
  stateKnown = true,
}: {
  kind: ActionKind;
  isLocked: boolean;
  drawInFlight: boolean;
  /** Only ever read for a promote: demoting is the way out of a full pool. */
  poolFull: boolean;
  /** Guests can curate a Stash and demote, but cannot promote. */
  guest?: boolean;
  /** False while the server-owned round gates are missing or refreshing. */
  stateKnown?: boolean;
}): Refusal | null {
  if (kind === "promote" && guest) return "guest";
  if (!stateKnown) return "unavailable";
  // A draw freezes only the pool, the same on all three tiles so none singles
  // out the held winner.
  if (kind === "demote" && drawInFlight) return "drawing";
  if (isLocked) return "locked";
  if (kind === "promote" && poolFull) return "full";
  return null;
}

/**
 * The accessible name and tooltip: the action, then the reason it won't run.
 * The reason repeats on every control on purpose, so no control goes inert
 * without saying why.
 */
export function actionLabel(kind: ActionKind, refusal: Refusal | null): string {
  return refusal ? `${VERB[kind]}, ${REASON[refusal]}` : VERB[kind];
}

/** The statuses the server accepts a delete from; others get no control. */
export function isDeletable(status: MovieStatus | undefined): boolean {
  return status === "stash" || status === "pool";
}

/**
 * Why deleting this movie is refused, or null (#237). Mirrors
 * movie.Service.Delete: a draw or a lock refuses a pool movie, never a stash
 * one. Precedence matches refusalOf.
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

/** The modal delete button's name and tooltip, worded like actionLabel. */
export function deleteLabel(refusal: Refusal | null): string {
  return refusal ? `Delete, ${REASON[refusal]}` : "Delete";
}
