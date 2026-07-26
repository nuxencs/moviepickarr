// The pool-lock gate: who may lock or unlock the shared pool. Locking is an
// admin-only action (the backend's handleSetPoolLock calls requireAdmin), so
// the Movies board disables the toggle for everyone else instead of hiding it,
// mirroring the turn gate's disable-not-hide treatment on the draw controls.
// The rule is a pure function of the session actor's role, unit-tested without
// rendering.
//
// The module also owns the words for the round's state and the Members page
// status line that composes them, so Movies and Members can't drift into
// describing the same flag differently.

import type { DrawPhase } from "@/components/moviepickarr/drawMachine";

/**
 * Whether the session actor may toggle the pool lock. Errs open while /auth/me
 * is still loading (role undefined) so an admin never flashes a disabled toggle
 * on first paint; the backend requireAdmin is the backstop for a non-admin who
 * clicks during that window.
 */
export function canLockPool(role: "member" | "admin" | undefined): boolean {
  return role === undefined || role === "admin";
}

/** The round is taking pool changes. Movies says it beside the lock toggle;
 *  Members never renders it (an open round there reads `ready to lock` when
 *  every pool is full and says nothing when they aren't). */
export const ROUND_OPEN = "round open";
/** The round is locked: no promotes, no demotes. Said by both pages. */
export const ROUND_CLOSED = "round closed";

/** Members-local: every pool is full and an admin can close the round. */
const READY_TO_LOCK = "ready to lock";
/** Members-local: a draw is out and unrevealed, so the pool is frozen. */
const DRAW_IN_PROGRESS = "draw in progress";

const ROSTER_FAILED = "Members failed to load";
const NO_MEMBERS = "No members yet";

/** How full the group's pools are, or why we can't say yet. `slots` is members
 *  times the pool size, so a zero-member roster arrives as `slots: 0`. */
export type RosterOccupancy =
  | { state: "pending" }
  | { state: "error" }
  | { state: "ready"; filled: number; slots: number };

export interface MembersStatus {
  /** All clauses, for the visible span. `null` while the roster is pending, so
   *  the caller draws a skeleton bar in the slot instead. */
  text: string | null;
  /** The round and draw clauses alone, for the visually-hidden live region.
   *  Empty when neither applies, so nothing is announced. */
  announce: string;
}

/**
 * The Members page status line: up to three clauses joined by ` · `. Occupancy
 * first, then the round clause, then the draw clause; the last two are
 * independent and compose.
 *
 * `announce` deliberately drops the occupancy clause. Occupancy ticks on every
 * other member's promote arriving over SSE (an event you did not cause and
 * cannot act on), and one region holding all three clauses would re-read the
 * whole string each time. Round and draw state is the opposite: rare, not
 * self-inflicted, and it changes what every control on the page will do.
 *
 * The numerator is whatever the caller passes and nothing here adjusts it for a
 * draw. The server keeps the pool frozen with the winner still in it until the
 * reveal, so a moving numerator would say a film had been drawn.
 */
export function membersStatus(
  occupancy: RosterOccupancy,
  locked: boolean,
  drawPhase: DrawPhase,
): MembersStatus {
  // Pending and errored rosters say nothing about the round: the occupancy the
  // round clause qualifies isn't known yet, and an announcement for a line the
  // page hasn't drawn is noise.
  if (occupancy.state === "pending") return { text: null, announce: "" };
  if (occupancy.state === "error") return { text: ROSTER_FAILED, announce: "" };
  if (occupancy.slots === 0) return { text: NO_MEMBERS, announce: "" };

  const round = locked
    ? ROUND_CLOSED
    : occupancy.filled >= occupancy.slots
      ? READY_TO_LOCK
      : null;
  const draw = drawPhase === "idle" ? null : DRAW_IN_PROGRESS;
  const announce = [round, draw].filter((clause): clause is string => clause !== null);

  return {
    text: [`${occupancy.filled} of ${occupancy.slots} slots filled`, ...announce].join(" · "),
    announce: announce.join(" · "),
  };
}
