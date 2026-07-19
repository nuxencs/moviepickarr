// Pure decision logic behind the account settings page. The components stay
// thin: they render whatever these helpers return and call the mutations. Every
// function takes its inputs as data and reads no DOM, so it is unit-tested
// directly (account.test.ts) the way authScreens is. Credential rules (username
// charset, password bounds) are shared with the claim flow, so the account page
// and the claim page can never drift on what a valid password is.
import { ApiError } from "@/api/APIClient";

import { PASSWORD_MAX, PASSWORD_MIN, validateClaimForm } from "@/components/moviepickarr/auth/authScreens";

// The app takes a single generic OIDC provider (presence-derived, no name over
// the API), so the surface says "SSO" rather than a brand. Matches the admin
// roster's "SSO" chips and "Unlink SSO" action.
export const PROVIDER = "SSO";

/**
 * Would unlinking SSO strand this member? True when SSO is their only way in
 * (no password), so unlinking would lock them out. The server 409s as the
 * backstop; the surface refuses before the round trip and points at the fix.
 */
export function unlinkWouldStrand(hasPassword: boolean, hasSSO: boolean): boolean {
  return hasSSO && !hasPassword;
}

/**
 * Validate the change-password form. Returns the first blocking problem as human
 * copy, or null when submittable. The current password is only checked for
 * presence (the server verifies it); the new password gets the shared bounds and
 * the confirm match.
 */
export function validateChangePassword(current: string, next: string, confirm: string): string | null {
  if (current.length === 0) {
    return "Enter your current password.";
  }
  if (next.length < PASSWORD_MIN) {
    return `Use a new password of at least ${PASSWORD_MIN} characters.`;
  }
  if (next.length > PASSWORD_MAX) {
    return `Keep your new password to ${PASSWORD_MAX} characters or fewer.`;
  }
  if (next !== confirm) {
    return "Those passwords don't match.";
  }
  return null;
}

/**
 * Validate the set-a-password form for an SSO-first member. It is a placeholder
 * claim in everything but name (pick a username + password, no current to
 * verify), so it reuses the claim validator to stay in lockstep with it.
 */
export function validateSetPassword(username: string, password: string, confirm: string): string | null {
  return validateClaimForm({ mode: "placeholder", username, password, confirm });
}

/**
 * The human-readable text for a failed action: a server-provided reason (a 409's
 * "conflict: …", a taken username) is written for humans, so surface it directly;
 * anything without one falls back to the caller's copy. Mirrors the roster page's
 * `fail` helper so the two credential surfaces report failures the same way.
 */
export function apiMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError && err.message ? err.message : fallback;
}

/** "N other device" / "N other devices" — the single source of the plural rule
 *  shared by the sessions row and the log-out-everywhere confirm. */
export function otherDevicesLabel(n: number): string {
  return `${n} other ${n === 1 ? "device" : "devices"}`;
}

export type LinkTone = "success" | "error";

export interface LinkResult {
  tone: LinkTone;
  text: string;
}

/**
 * Map the OIDC link redirect's outcome (the ?linked / ?error the callback lands
 * back on /settings with) to a toast. `linked=1` is the success; each error
 * bucket gets copy specific to the link context (unlike the login page, where
 * the same buckets mean a failed sign-in). Returns null when neither param is
 * present, so a plain visit to /settings shows nothing.
 */
export function linkResultFromSearch(linked?: string, error?: string): LinkResult | null {
  if (linked === "1") {
    return { tone: "success", text: `${PROVIDER} connected.` };
  }
  if (!error) {
    return null;
  }
  switch (error) {
    case "oidc_link_conflict":
      return { tone: "error", text: `That ${PROVIDER} account is already linked to another member.` };
    case "oidc_session_expired":
      return { tone: "error", text: "Your session expired before linking finished. Try connecting again." };
    default:
      return { tone: "error", text: `Couldn't connect ${PROVIDER}. Please try again.` };
  }
}
