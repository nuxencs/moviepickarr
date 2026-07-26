import { describe, expect, it } from "vitest";

import {
  canLockPool,
  membersStatus,
  ROUND_CLOSED,
  ROUND_OPEN,
  type RosterOccupancy,
} from "@/components/moviepickarr/poolLock";

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

describe("membersStatus", () => {
  // Four members at a pool size of three: 12 slots, 9 of them filled.
  const partial: RosterOccupancy = { state: "ready", filled: 9, slots: 12 };
  const full: RosterOccupancy = { state: "ready", filled: 12, slots: 12 };

  it("draws a skeleton bar (no text) while the roster is pending", () => {
    expect(membersStatus({ state: "pending" }, false, "idle")).toEqual({ text: null, announce: "" });
  });

  it("names the failure when the roster errored", () => {
    expect(membersStatus({ state: "error" }, false, "idle")).toEqual({
      text: "Members failed to load",
      announce: "",
    });
  });

  it("says there are no members when the roster is empty", () => {
    expect(membersStatus({ state: "ready", filled: 0, slots: 0 }, false, "idle")).toEqual({
      text: "No members yet",
      announce: "",
    });
  });

  it("reports bare occupancy for an open, not-full round", () => {
    expect(membersStatus(partial, false, "idle")).toEqual({
      text: "9 of 12 slots filled",
      announce: "",
    });
  });

  it("adds the draw clause to an open, not-full round", () => {
    expect(membersStatus(partial, false, "spinning")).toEqual({
      text: "9 of 12 slots filled · draw in progress",
      announce: "draw in progress",
    });
  });

  it("says ready to lock once every pool is full", () => {
    expect(membersStatus(full, false, "idle")).toEqual({
      text: "12 of 12 slots filled · ready to lock",
      announce: "ready to lock",
    });
  });

  it("composes ready to lock with a draw in progress", () => {
    expect(membersStatus(full, false, "revealing")).toEqual({
      text: "12 of 12 slots filled · ready to lock · draw in progress",
      announce: "ready to lock · draw in progress",
    });
  });

  it("says the round is closed when locked", () => {
    expect(membersStatus(partial, true, "idle")).toEqual({
      text: `9 of 12 slots filled · ${ROUND_CLOSED}`,
      announce: ROUND_CLOSED,
    });
  });

  it("composes a closed round with a draw in progress", () => {
    expect(membersStatus(partial, true, "settled")).toEqual({
      text: `9 of 12 slots filled · ${ROUND_CLOSED} · draw in progress`,
      announce: `${ROUND_CLOSED} · draw in progress`,
    });
  });

  it("keeps the closed round's word when a locked pool is also full", () => {
    expect(membersStatus(full, true, "idle").text).toBe(`12 of 12 slots filled · ${ROUND_CLOSED}`);
  });

  it("falls out as 0 of 3 for a single member", () => {
    expect(membersStatus({ state: "ready", filled: 0, slots: 3 }, false, "idle").text).toBe(
      "0 of 3 slots filled",
    );
  });

  it("never says the round is open", () => {
    const phases = ["idle", "spinning", "settled", "revealing"] as const;
    for (const phase of phases) {
      for (const occupancy of [partial, full]) {
        expect(membersStatus(occupancy, false, phase).text).not.toContain(ROUND_OPEN);
      }
    }
  });

  it("holds the numerator across a reveal, whatever the draw phase", () => {
    // The pool stays frozen with the winner still in it until the reveal lands,
    // so the count must read the same the whole way through. A numerator that
    // dropped at reveal would say a film had been drawn.
    const phases = ["idle", "spinning", "settled", "revealing"] as const;
    for (const phase of phases) {
      expect(membersStatus(partial, false, phase).text).toContain("9 of 12 slots filled");
    }
  });
});
