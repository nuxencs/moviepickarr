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
