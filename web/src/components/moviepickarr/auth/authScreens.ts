// Pure decision logic behind the login and claim screens. The components stay
// thin: they render whatever these helpers return. Everything here takes its
// inputs as data (an error, a status, form fields) and reads no DOM, so it is
// unit-tested directly (authScreens.test.ts) the way drawMachine is.
import type { ClaimMode } from "@/types/Response";

export type BannerTone = "error" | "warn";

export interface Banner {
  tone: BannerTone;
  text: string;
}

// The one uniform 401 copy — identical whether the username is unknown, the
// password is wrong, or the account is soft-locked. One string, on purpose: the
// login form must not become an account-enumeration oracle (spec §security).
export const UNIFORM_401 = "That username and password don't match.";

const OIDC_GENERIC = "SSO sign-in didn't complete. Please try again.";

// Structural status read so this module needs no import from the API layer (and
// stays trivially testable). ApiError carries a numeric `status`; anything else
// (a plain Error, a network failure) reads as undefined.
function statusOf(err: unknown): number | undefined {
  if (typeof err === "object" && err !== null && "status" in err) {
    const s = (err as { status: unknown }).status;
    if (typeof s === "number") return s;
  }
  return undefined;
}

// The OIDC callback lands back on /login with a ?error= bucket (never JSON).
// Only oidc_unlinked gets the warn tone (signed in fine, but no member is
// linked yet — an actionable "ask an admin" case); every other bucket is a
// generic try-again error. No error param means no banner.
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

// After a failed login POST: a 401 is the uniform bad-credentials/lockout case;
// any other failure (5xx, network) is a try-again error, not "wrong password".
export function bannerForLoginError(err: unknown): Banner {
  if (statusOf(err) === 401) {
    return { tone: "error", text: UNIFORM_401 };
  }
  return { tone: "error", text: "Something went wrong. Please try again." };
}

// The claim-validate error buckets. A 404 is the collapsed no-longer-valid
// state (expired / revoked / unknown token); a 410 is the distinct already-set-up
// state; anything else is an unexpected failure the page reports generically.
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

// Username charset + password bounds mirror the server (charset 3-32
// [a-zA-Z0-9._-]; password min 8, max 128). Validating client-side keeps the
// obvious mistakes (mismatch, too short) off the wire; the server stays the
// source of truth and a slipped-through value still 400s.
export const USERNAME_RE = /^[a-zA-Z0-9._-]{3,32}$/;
export const PASSWORD_MIN = 8;
export const PASSWORD_MAX = 128;

export interface ClaimFormInput {
  mode: ClaimMode;
  username: string;
  password: string;
  confirm: string;
}

// Returns the first blocking problem as human copy, or null when the form is
// submittable. Username is only checked for placeholder claims (a reset keeps
// the existing username).
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
