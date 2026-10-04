// Admin roster presentation, derived from RosterMember presence flags, never a
// stored status.
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

export function loginChips(m: RosterMember): LoginChip[] {
  if (m.archived) {
    return [{ kind: "archived", label: "Archived" }];
  }
  if (isPlaceholder(m)) {
    return m.invitePending
      ? [{ kind: "pending", label: "Invite link open" }]
      : [{ kind: "empty", label: "No login yet" }];
  }
  const chips: LoginChip[] = [];
  if (m.hasLocalLogin) chips.push({ kind: "password", label: "Password" });
  if (m.hasLinkedIdentity) chips.push({ kind: "sso", label: "SSO" });
  return chips;
}

/** One-line login state for the dense archived rows. */
export function credLabel(m: RosterMember): string {
  if (m.archived) return "Archived";
  if (isPlaceholder(m)) return m.invitePending ? "Invite link open" : "No login yet";
  if (m.hasLocalLogin && m.hasLinkedIdentity) return "Password + SSO";
  if (m.hasLocalLogin) return "Password";
  return "SSO";
}

/** Mirrors the backend added_by_id guard so the confirm can name the outcome. */
export function removeOutcome(m: RosterMember): "delete" | "archive" {
  return m.moviesAuthored === 0 ? "delete" : "archive";
}

/** An admin unlinking their own last credential would lock themselves out (server 409s too). */
export function unlinkWouldStrand(m: RosterMember, isSelf: boolean): boolean {
  return isSelf && m.hasLinkedIdentity && !m.hasLocalLogin;
}
