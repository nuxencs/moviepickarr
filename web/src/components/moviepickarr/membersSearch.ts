import type { User } from "@/types/Response";

/**
 * Search params for `/users?member=<userID>&stash=true`. A search param rather
 * than a nested `/users/$id` route, so /users needs no index child. Mirrors
 * statsSearch.ts.
 */
export interface MembersSearch {
  /** The selected member's user id. Absent means "me". */
  member?: number;
  /**
   * Below 761px, pushes the member's stash over the rail (#236). Only ever
   * `true`: leaving it out keeps `/users` from serializing as `?stash=false`.
   */
  stash?: true;
}

/**
 * Total, never-throwing validator for the route's search. Invalid ids drop out
 * as undefined, not a 0 sentinel, because the router serializes the return
 * value back into the URL. Dead ids pass through untouched (see selectedMember).
 */
export function validateMembersSearch(search: Record<string, unknown>): MembersSearch {
  const stash =
    search.stash === true || search.stash === "true" ? ({ stash: true } as const) : {};

  // Number() alone reads `true` as 1 and `["4"]` as 4.
  const raw = search.member;
  if (typeof raw !== "number" && typeof raw !== "string") return { ...stash };
  const id = Number(raw);
  return Number.isSafeInteger(id) && id > 0 ? { member: id, ...stash } : { ...stash };
}

/** The roster with the session member first, the rest in API order. */
export function orderMembers(users: User[] | undefined, meID: number | undefined): User[] {
  if (!users) return [];
  if (meID === undefined) return users;
  return [...users.filter((u) => u.userID === meID), ...users.filter((u) => u.userID !== meID)];
}

/**
 * The board the URL asks for. An unknown id falls back to your own board and
 * leaves the URL alone: rewriting it would race an empty roster during load.
 * `ordered` must come from orderMembers.
 */
export function selectedMember(
  ordered: User[],
  member: number | undefined,
  meID: number | undefined,
): User | undefined {
  return (
    ordered.find((u) => u.userID === member) ??
    ordered.find((u) => u.userID === meID) ??
    ordered[0]
  );
}
