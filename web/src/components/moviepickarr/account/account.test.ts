import { describe, expect, it } from "vitest";

import { ApiError } from "@/api/APIClient";

import {
  apiMessage,
  linkResultFromSearch,
  otherDevicesLabel,
  PROVIDER,
  unlinkWouldStrand,
  validateChangePassword,
  validateSetPassword,
} from "@/components/moviepickarr/account/account";

describe("unlinkWouldStrand", () => {
  it("strands only an SSO-only member (no password)", () => {
    expect(unlinkWouldStrand(false, true)).toBe(true);
  });

  it("does not strand when a password is also set", () => {
    expect(unlinkWouldStrand(true, true)).toBe(false);
  });

  it("does not strand when there is no SSO to unlink", () => {
    expect(unlinkWouldStrand(true, false)).toBe(false);
    expect(unlinkWouldStrand(false, false)).toBe(false);
  });
});

describe("validateChangePassword", () => {
  it("requires the current password", () => {
    expect(validateChangePassword("", "a-good-password", "a-good-password")).toMatch(/current password/i);
  });

  it("enforces the shared minimum length on the new password", () => {
    expect(validateChangePassword("current", "short", "short")).toMatch(/at least 8/i);
  });

  it("rejects a mismatched confirmation", () => {
    expect(validateChangePassword("current", "a-good-password", "different")).toMatch(/don't match/i);
  });

  it("returns null for a valid change", () => {
    expect(validateChangePassword("current", "a-good-password", "a-good-password")).toBeNull();
  });
});

describe("validateSetPassword", () => {
  it("rejects an invalid username", () => {
    expect(validateSetPassword("no", "a-good-password", "a-good-password")).toMatch(/username/i);
  });

  it("enforces the password minimum", () => {
    expect(validateSetPassword("marcus", "short", "short")).toMatch(/at least 8/i);
  });

  it("rejects a mismatched confirmation", () => {
    expect(validateSetPassword("marcus", "a-good-password", "nope")).toMatch(/don't match/i);
  });

  it("returns null for a valid set", () => {
    expect(validateSetPassword("marcus", "a-good-password", "a-good-password")).toBeNull();
  });
});

describe("apiMessage", () => {
  it("prefers a server-provided reason", () => {
    expect(apiMessage(new ApiError(409, "conflict: last credential"), "fallback")).toBe(
      "conflict: last credential",
    );
  });

  it("falls back for an ApiError with no message", () => {
    expect(apiMessage(new ApiError(500, ""), "fallback")).toBe("fallback");
  });

  it("falls back for a non-ApiError", () => {
    expect(apiMessage(new Error("boom"), "fallback")).toBe("fallback");
    expect(apiMessage(undefined, "fallback")).toBe("fallback");
  });
});

describe("otherDevicesLabel", () => {
  it("singularizes one device", () => {
    expect(otherDevicesLabel(1)).toBe("1 other device");
  });

  it("pluralizes more than one", () => {
    expect(otherDevicesLabel(3)).toBe("3 other devices");
  });
});

describe("linkResultFromSearch", () => {
  it("returns null when neither param is present", () => {
    expect(linkResultFromSearch(undefined, undefined)).toBeNull();
  });

  it("reports a success toast for linked=1", () => {
    expect(linkResultFromSearch("1", undefined)).toEqual({
      tone: "success",
      text: `${PROVIDER} connected.`,
    });
  });

  it("maps the already-linked conflict to specific copy", () => {
    const result = linkResultFromSearch(undefined, "oidc_link_conflict");
    expect(result?.tone).toBe("error");
    expect(result?.text).toMatch(/already linked to another member/i);
  });

  it("maps an expired link session to a retry message", () => {
    const result = linkResultFromSearch(undefined, "oidc_session_expired");
    expect(result?.tone).toBe("error");
    expect(result?.text).toMatch(/expired/i);
  });

  it("falls back to a generic error for an unknown bucket", () => {
    const result = linkResultFromSearch(undefined, "oidc_failed");
    expect(result?.tone).toBe("error");
    expect(result?.text).toMatch(/couldn't connect/i);
  });
});
