// Account page decision logic. Credential rules come from authScreens so the
// account and claim pages cannot drift on what a valid password is.
import { ApiError } from "@/api/APIClient";

import { PASSWORD_MAX, PASSWORD_MIN, validateClaimForm } from "@/components/moviepickarr/auth/authScreens";

// One generic OIDC provider with no name over the API; matches the roster's "SSO".
export const PROVIDER = "SSO";

/** True when SSO is the only way in; the server 409s as the backstop. */
export function unlinkWouldStrand(hasPassword: boolean, hasSSO: boolean): boolean {
  return hasSSO && !hasPassword;
}

/** First blocking problem as copy, or null. The server verifies the current password. */
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

/** A placeholder claim in all but name, so it reuses the claim validator. */
export function validateSetPassword(username: string, password: string, confirm: string): string | null {
  return validateClaimForm({ mode: "placeholder", username, password, confirm });
}

/** The server's reason when it has one, else the fallback. Mirrors RosterSection's `fail`. */
export function apiMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError && err.message ? err.message : fallback;
}

export function otherDevicesLabel(n: number): string {
  return `${n} other ${n === 1 ? "device" : "devices"}`;
}

export type LinkTone = "success" | "error";

export interface LinkResult {
  tone: LinkTone;
  text: string;
}

/** Maps the OIDC link callback's ?linked / ?error on /settings to a toast, or null. */
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
