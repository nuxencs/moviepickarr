import { describe, expect, it } from "vitest";

import { ApiError } from "@/api/APIClient";

import {
  bannerForLoginError,
  bannerForOidcError,
  claimTerminalFromError,
  UNIFORM_401,
  validateClaimForm,
} from "@/components/moviepickarr/auth/authScreens";

describe("bannerForOidcError", () => {
  it("renders nothing without an error param", () => {
    expect(bannerForOidcError(null)).toBeNull();
    expect(bannerForOidcError(undefined)).toBeNull();
    expect(bannerForOidcError("")).toBeNull();
  });

  it("gives oidc_unlinked the warn tone (actionable, not a failure)", () => {
    const banner = bannerForOidcError("oidc_unlinked");
    expect(banner?.tone).toBe("warn");
    expect(banner?.text).toMatch(/isn't linked/);
  });

  it("collapses every other bucket to a generic try-again error", () => {
    for (const bucket of ["oidc_denied", "oidc_expired", "oidc_failed", "oidc_weird"]) {
      const banner = bannerForOidcError(bucket);
      expect(banner?.tone).toBe("error");
      expect(banner?.text).toMatch(/try again/i);
    }
  });
});

describe("bannerForLoginError", () => {
  it("shows the one uniform copy for a 401 (anti-enumeration)", () => {
    const banner = bannerForLoginError(new ApiError(401, "invalid credentials"));
    expect(banner.tone).toBe("error");
    expect(banner.text).toBe(UNIFORM_401);
  });

  it("does not blame the password for a server or network failure", () => {
    for (const err of [new ApiError(500, "boom"), new Error("network down"), "weird"]) {
      const banner = bannerForLoginError(err);
      expect(banner.text).not.toBe(UNIFORM_401);
      expect(banner.tone).toBe("error");
    }
  });
});

describe("claimTerminalFromError", () => {
  it("maps 404 to the collapsed no-longer-valid state", () => {
    expect(claimTerminalFromError(new ApiError(404, "gone"))).toBe("invalid");
  });

  it("maps 410 to the distinct already-set-up state", () => {
    expect(claimTerminalFromError(new ApiError(410, "used"))).toBe("already");
  });

  it("falls back to a generic error for anything else", () => {
    expect(claimTerminalFromError(new ApiError(500, "boom"))).toBe("error");
    expect(claimTerminalFromError(new Error("network"))).toBe("error");
  });
});

describe("validateClaimForm", () => {
  const ok = { mode: "placeholder" as const, username: "jamie", password: "longenough", confirm: "longenough" };

  it("accepts a well-formed placeholder claim", () => {
    expect(validateClaimForm(ok)).toBeNull();
  });

  it("skips the username check for a reset (username already set)", () => {
    expect(
      validateClaimForm({ mode: "reset", username: "", password: "longenough", confirm: "longenough" }),
    ).toBeNull();
  });

  it("rejects a placeholder username outside the 3-32 charset", () => {
    expect(validateClaimForm({ ...ok, username: "no" })).toMatch(/username/i);
    expect(validateClaimForm({ ...ok, username: "has spaces" })).toMatch(/username/i);
    expect(validateClaimForm({ ...ok, username: "a".repeat(33) })).toMatch(/username/i);
  });

  it("enforces the password bounds", () => {
    expect(validateClaimForm({ ...ok, password: "short", confirm: "short" })).toMatch(/at least/);
    const big = "x".repeat(129);
    expect(validateClaimForm({ ...ok, password: big, confirm: big })).toMatch(/or fewer/);
  });

  it("catches a confirm mismatch", () => {
    expect(validateClaimForm({ ...ok, confirm: "different" })).toMatch(/don't match/);
  });
});
