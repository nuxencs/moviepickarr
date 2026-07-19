// Pure presentation logic for the admin roster: login-state chips, remove
// outcome, and the self-unlink guard, all derived from a RosterMember's
// presence flags (never a stored status). Kept side-effect free so the
// derivation is unit-tested without rendering.
import type { RosterMember } from "@/types/Response";

export type LoginChipKind = "password" | "sso" | "pending" | "empty" | "archived";

export interface LoginChip {
  kind: LoginChipKind;
  label: string;
}

/** A member holds no credentials: usable as an adder, but can't log in yet. */
export function isPlaceholder(m: RosterMember): boolean {
  return !m.hasLocalLogin && !m.hasLinkedIdentity;
}

/**
 * The login-state chips for a member, derived from credential/invite/archive
 * presence. Archived collapses to one muted chip; a placeholder shows either the
 * pending-invite or the no-login-yet state; a credentialed member shows one chip
 * per credential it actually holds. Never a single stored boolean.
 */
export function loginChips(m: RosterMember): LoginChip[] {
  if (m.archived) {
    return [{ kind: "archived", label: "Archived" }];
  }
  if (isPlaceholder(m)) {
    return m.invitePending
      ? [{ kind: "pending", label: "Invite sent" }]
      : [{ kind: "empty", label: "No login yet" }];
  }
  const chips: LoginChip[] = [];
  if (m.hasLocalLogin) chips.push({ kind: "password", label: "Password" });
  if (m.hasLinkedIdentity) chips.push({ kind: "sso", label: "SSO" });
  return chips;
}

/** A one-line summary of a member's login state, for the dense archived rows. */
export function credLabel(m: RosterMember): string {
  if (m.archived) return "Archived";
  if (isPlaceholder(m)) return m.invitePending ? "Invited" : "No login yet";
  if (m.hasLocalLogin && m.hasLinkedIdentity) return "Password + SSO";
  if (m.hasLocalLogin) return "Password";
  return "SSO";
}

/**
 * Whether removing this member hard-deletes (they authored nothing, so the row
 * goes and the name frees up) or archives (they authored movies, so the row
 * survives to keep attribution). Mirrors the backend's added_by_id guard, so the
 * confirm can name the outcome before the request.
 */
export function removeOutcome(m: RosterMember): "delete" | "archive" {
  return m.moviesAuthored === 0 ? "delete" : "archive";
}

/**
 * The self-unlink guard: an admin unlinking their own SSO when it's their only
 * credential would lock themselves out. Refused client-side before the round
 * trip (the server 409s as the backstop). Unlinking someone ELSE's last
 * credential is fine: they fall back to a placeholder.
 */
export function unlinkWouldStrand(m: RosterMember, isSelf: boolean): boolean {
  return isSelf && m.hasLinkedIdentity && !m.hasLocalLogin;
}

const UNITS: [limit: number, secs: number, name: string][] = [
  [60, 1, "second"],
  [3600, 60, "minute"],
  [86400, 3600, "hour"],
  [604800, 86400, "day"],
  [2629800, 604800, "week"],
  [31557600, 2629800, "month"],
  [Infinity, 31557600, "year"],
];

/**
 * A compact "time ago" for the last-active column. Returns "" for a missing
 * timestamp (a member who never logged in) so the caller can render a dash.
 * "now" under a minute, otherwise the largest whole unit ("3 days ago").
 */
export function timeAgo(iso: string | undefined, now: number = Date.now()): string {
  if (!iso) return "";
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "";
  const secs = Math.max(0, Math.round((now - then) / 1000));
  if (secs < 45) return "now";
  for (const [limit, per, name] of UNITS) {
    if (secs < limit) {
      const n = Math.max(1, Math.floor(secs / per));
      return `${n} ${name}${n === 1 ? "" : "s"} ago`;
    }
  }
  return "";
}
