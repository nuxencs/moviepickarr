// Pool-lock gate and round-state wording, shared by Movies and Members so the
// two pages describe the lock flag the same way. Locking is admin-only (the
// backend's handleSetPoolLock calls requireAdmin); the toggle is disabled, not hidden.

/**
 * Whether the session actor may toggle the pool lock. Errs open while /auth/me
 * loads so an admin never sees a disabled flash; requireAdmin is the backstop.
 */
export function canLockPool(role: "member" | "guest" | "admin" | undefined): boolean {
  return role === undefined || role === "admin";
}

/** Movies only: Members says `ready to lock` or nothing for an open round. */
export const ROUND_OPEN = "round open";
export const ROUND_CLOSED = "round closed";

const READY_TO_LOCK = "ready to lock";
const DRAW_IN_PROGRESS = "draw in progress";

const ROSTER_FAILED = "Members failed to load";
const NO_MEMBERS = "No members yet";

/** Slots in one member's pool. Here because the loading skeleton draws them with no roster. */
export const POOL_SIZE = 3;

/** How full the group's pools are; `slots` is members times POOL_SIZE. */
export type RosterOccupancy =
  | { state: "pending" }
  | { state: "error" }
  | { state: "ready"; filled: number; slots: number };

export interface MembersStatus {
  /** `null` while the roster is pending: the caller draws a skeleton bar. */
  text: string | null;
  /** Round and draw clauses only, for the live region; empty announces nothing. */
  announce: string;
}

/**
 * The Members status line: occupancy, round, and draw clauses joined by ` · `.
 * `announce` drops occupancy because it ticks on every other member's promote
 * over SSE. The numerator is not adjusted for a draw: the server keeps the
 * winner in the frozen pool until the reveal.
 */
export function membersStatus(
  occupancy: RosterOccupancy,
  locked: boolean,
  drawInProgress: boolean,
): MembersStatus {
  if (occupancy.state === "pending") return { text: null, announce: "" };
  if (occupancy.state === "error") return { text: ROSTER_FAILED, announce: "" };
  if (occupancy.slots === 0) return { text: NO_MEMBERS, announce: "" };

  const round = locked
    ? ROUND_CLOSED
    : occupancy.filled >= occupancy.slots
      ? READY_TO_LOCK
      : null;
  const draw = drawInProgress ? DRAW_IN_PROGRESS : null;
  const announce = [round, draw].filter((clause): clause is string => clause !== null);

  return {
    text: [`${occupancy.filled} of ${occupancy.slots} slots filled`, ...announce].join(" · "),
    announce: announce.join(" · "),
  };
}
