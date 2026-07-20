import { describe, expect, it } from "vitest";

import { isSelf } from "@/components/moviepickarr/ownership";

describe("isSelf", () => {
  it("is true when the session member matches the target id", () => {
    expect(isSelf(7, 7)).toBe(true);
  });

  it("is false for a different member id", () => {
    expect(isSelf(7, 8)).toBe(false);
  });

  it("is false while the session is still loading (no me id)", () => {
    expect(isSelf(undefined, 7)).toBe(false);
  });

  it("is false when the target id is missing, even without a session", () => {
    expect(isSelf(undefined, undefined)).toBe(false);
  });
});
