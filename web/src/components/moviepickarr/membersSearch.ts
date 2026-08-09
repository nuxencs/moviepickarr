import type { User } from "@/types/Response";

/**
 * A member has an address: `/users?member=<userID>`.
 *
 * A search param on the existing leaf route rather than a nested `/users/$id`,
 * which would turn /users into a layout route whose index child has to render
 * something with no member chosen. The value is the numeric user id — already
 * on the wire and on the frontend movie type, so linking to a board costs no
 * API change; a name slug dies on two members sharing a first name.
 *
 * Mirrors statsSearch.ts: one pure module owning the codec, so the route, the
 * rail's links and anything linking in all read the same rules.
 */
export interface MembersSearch {
  /** The selected member's user id. Absent means "me". */
  member?: number;
  /**
   * Their stash as a screen of its own, which is what the layout below 760px
   * pushes to (#236). Beside the rail it changes nothing: the pane is already
   * there, so this is the second half of a mobile address rather than a second
   * view.
   *
   * Two levels, because the narrow layout has two: `member` says whose pool the
   * rail has open, and this says you have gone on to their movies. One key
   * cannot say both, and a phone that could only reach a member by leaving the
   * rail would have no way to see anybody else's pool.
   *
   * Only ever `true`. False is spelled by leaving it out, so a plain `/users`
   * does not come back as `/users?stash=false` (the router serializes whatever
   * the validator returns).
   */
  stash?: true;
}

/**
 * Total, never-throwing validator — TanStack Router runs it on every
 * navigation and its return type is the route's typed search.
 *
 * Anything that is not a positive safe integer drops out entirely, which the
 * page resolves to your own board. Undefined rather than a 0 sentinel because
 * the router serializes whatever this returns back into the URL: a sentinel
 * would turn a plain `/users` into `/users?member=0` on arrival. It needs no
 * stripSearchParams for the same reason, and an id that is well-formed but
 * dead (archived, or hand-typed) survives the trip untouched — the URL is
 * never rewritten out from under whoever pasted it (see selectedMember).
 */
export function validateMembersSearch(search: Record<string, unknown>): MembersSearch {
  // A boolean from a programmatic navigation, the string off the URL, nothing
  // else. Anything that is not a plain yes drops out, so a stray `?stash=0`
  // reads as the rail rather than as the pushed screen.
  const stash =
    search.stash === true || search.stash === "true" ? ({ stash: true } as const) : {};

  // A number from a programmatic navigation, a string off the URL, nothing
  // else: Number() alone reads `true` as 1 and `["4"]` as 4, which would make
  // a malformed navigation select somebody's board.
  const raw = search.member;
  if (typeof raw !== "number" && typeof raw !== "string") return { ...stash };
  const id = Number(raw);
  return Number.isSafeInteger(id) && id > 0 ? { member: id, ...stash } : { ...stash };
}

/**
 * The roster with the session member first, everything else in the order the
 * API sent (created_at ascending, so it is stable across refetches). The API
 * stays an unordered set; being first is a frontend rule about who is reading.
 */
export function orderMembers(users: User[] | undefined, meID: number | undefined): User[] {
  if (!users) return [];
  if (meID === undefined) return users;
  return [...users.filter((u) => u.userID === meID), ...users.filter((u) => u.userID !== meID)];
}

/**
 * Which board the URL is asking for. An id that does not resolve — archived
 * mid-session, or hand-typed — silently selects your own board: no error line,
 * and the URL is left alone. Rewriting it would race an empty roster during
 * load, and the fallback already covers the archived-member event closing the
 * board that was open.
 *
 * `ordered` is orderMembers' output, so its head is the session member and the
 * last fallback covers a session whose own row is missing.
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
