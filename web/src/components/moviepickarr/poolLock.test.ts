import { describe, expect, it } from "vitest";

import { canLockPool } from "@/components/moviepickarr/poolLock";

describe("canLockPool", () => {
  it("lets an admin toggle the pool lock", () => {
    expect(canLockPool("admin")).toBe(true);
  });

  it("keeps a plain member out", () => {
    expect(canLockPool("member")).toBe(false);
  });

  it("errs open while the session is still loading (no role)", () => {
    expect(canLockPool(undefined)).toBe(true);
  });
});
