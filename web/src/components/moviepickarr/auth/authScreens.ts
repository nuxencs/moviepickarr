import type { ClaimMode } from "@/types/Response";

export type BannerTone = "error" | "warn";

export interface Banner {
  tone: BannerTone;
  text: string;
}

// One string for unknown user, wrong password and lockout, so the form is no
// account-enumeration oracle.
export const UNIFORM_401 = "That username and password don't match.";

const OIDC_GENERIC = "SSO sign-in didn't complete. Please try again.";

// Structural read, so this module needs no import from the API layer.
function statusOf(err: unknown): number | undefined {
  if (typeof err === "object" && err !== null && "status" in err) {
    const s = (err as { status: unknown }).status;
    if (typeof s === "number") return s;
  }
  return undefined;
}

// Maps the OIDC callback's ?error= bucket on /login. Only oidc_unlinked is
// actionable ("ask an admin"), so only it gets the warn tone.
export function bannerForOidcError(error: string | null | undefined): Banner | null {
  if (!error) return null;
  if (error === "oidc_unlinked") {
    return {
      tone: "warn",
      text: "That account isn't linked to a member yet. Ask an admin for an invite.",
    };
  }
  return { tone: "error", text: OIDC_GENERIC };
}

// Only a 401 means bad credentials; anything else is a try-again error.
export function bannerForLoginError(err: unknown): Banner {
  if (statusOf(err) === 401) {
    return { tone: "error", text: UNIFORM_401 };
  }
  return { tone: "error", text: "Something went wrong. Please try again." };
}

// 404: expired, revoked or unknown token. 410: already set up.
export type ClaimTerminal = "invalid" | "already" | "error";

export function claimTerminalFromError(err: unknown): ClaimTerminal {
  switch (statusOf(err)) {
    case 404:
      return "invalid";
    case 410:
      return "already";
    default:
      return "error";
  }
}

// Mirror the server's rules; the server stays the source of truth.
export const USERNAME_RE = /^[a-zA-Z0-9._-]{3,32}$/;
export const PASSWORD_MIN = 8;
export const PASSWORD_MAX = 128;

export interface ClaimFormInput {
  mode: ClaimMode;
  username: string;
  password: string;
  confirm: string;
}

// First blocking problem as copy, or null. A reset keeps its username, so only
// placeholder claims check it.
export function validateClaimForm(input: ClaimFormInput): string | null {
  if (input.mode === "placeholder" && !USERNAME_RE.test(input.username.trim())) {
    return "Pick a username 3 to 32 characters long, using letters, numbers, dots, dashes or underscores.";
  }
  if (input.password.length < PASSWORD_MIN) {
    return `Use a password of at least ${PASSWORD_MIN} characters.`;
  }
  if (input.password.length > PASSWORD_MAX) {
    return `Keep your password to ${PASSWORD_MAX} characters or fewer.`;
  }
  if (input.password !== input.confirm) {
    return "Those passwords don't match.";
  }
  return null;
}
