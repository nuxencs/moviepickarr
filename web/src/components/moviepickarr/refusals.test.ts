import { describe, expect, it } from "vitest";

import { refusalOf, actionLabel } from "@/components/moviepickarr/refusals";

describe("refusalOf", () => {
  const open = { isLocked: false, drawInFlight: false, poolFull: false };

  it("refuses nothing on an open round with room in the pool", () => {
    expect(refusalOf({ kind: "promote", ...open })).toBeNull();
    expect(refusalOf({ kind: "demote", ...open })).toBeNull();
  });

  it("refuses every promote once the pool is full", () => {
    expect(refusalOf({ kind: "promote", ...open, poolFull: true })).toBe("full");
  });

  it("never refuses a demote for a full pool: it is the way out of one", () => {
    expect(refusalOf({ kind: "demote", ...open, poolFull: true })).toBeNull();
  });

  it("refuses both directions on a locked round", () => {
    expect(refusalOf({ kind: "promote", ...open, isLocked: true })).toBe("locked");
    expect(refusalOf({ kind: "demote", ...open, isLocked: true })).toBe("locked");
  });

  it("freezes the pool during a draw and leaves the stash alone", () => {
    expect(refusalOf({ kind: "demote", ...open, drawInFlight: true })).toBe("drawing");
    expect(refusalOf({ kind: "promote", ...open, drawInFlight: true })).toBeNull();
  });

  // Precedence is drawing > locked > full; refusals.ts says why.
  it("says drawing ahead of locked on a frozen pool", () => {
    expect(refusalOf({ kind: "demote", isLocked: true, drawInFlight: true, poolFull: true })).toBe(
      "drawing",
    );
  });

  it("says locked ahead of full, which is the bug a locked full pool used to show", () => {
    expect(
      refusalOf({ kind: "promote", isLocked: true, drawInFlight: false, poolFull: true }),
    ).toBe("locked");
  });

  it("still says locked for a promote while a draw is out, since a draw does not refuse one", () => {
    expect(refusalOf({ kind: "promote", isLocked: true, drawInFlight: true, poolFull: false })).toBe(
      "locked",
    );
  });
});

describe("actionLabel", () => {
  it("names the action alone when nothing refuses it", () => {
    expect(actionLabel("promote", null)).toBe("Move to pool");
    expect(actionLabel("demote", null)).toBe("Move back to stash");
  });

  it("puts the reason after the verb, verbatim", () => {
    expect(actionLabel("promote", "full")).toBe("Move to pool, pool is full");
    expect(actionLabel("promote", "locked")).toBe("Move to pool, round closed");
    expect(actionLabel("demote", "drawing")).toBe("Move back to stash, a draw is in progress");
  });

  it("says round closed in the words the status line uses", () => {
    expect(actionLabel("demote", "locked")).toBe("Move back to stash, round closed");
  });
});
