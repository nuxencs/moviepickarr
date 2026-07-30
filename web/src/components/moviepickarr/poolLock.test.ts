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
    expect(membersStatus({ state: "pending" }, false, false)).toEqual({ text: null, announce: "" });
  });

  it("names the failure when the roster errored", () => {
    expect(membersStatus({ state: "error" }, false, false)).toEqual({
      text: "Members failed to load",
      announce: "",
    });
  });

  it("says there are no members when the roster is empty", () => {
    expect(membersStatus({ state: "ready", filled: 0, slots: 0 }, false, false)).toEqual({
      text: "No members yet",
      announce: "",
    });
  });

  it("reports bare occupancy for an open, not-full round", () => {
    expect(membersStatus(partial, false, false)).toEqual({
      text: "9 of 12 slots filled",
      announce: "",
    });
  });

  it("adds the draw clause to an open, not-full round", () => {
    expect(membersStatus(partial, false, true)).toEqual({
      text: "9 of 12 slots filled · draw in progress",
      announce: "draw in progress",
    });
  });

  it("says ready to lock once every pool is full", () => {
    expect(membersStatus(full, false, false)).toEqual({
      text: "12 of 12 slots filled · ready to lock",
      announce: "ready to lock",
    });
  });

  it("composes ready to lock with a draw in progress", () => {
    expect(membersStatus(full, false, true)).toEqual({
      text: "12 of 12 slots filled · ready to lock · draw in progress",
      announce: "ready to lock · draw in progress",
    });
  });

  it("says the round is closed when locked", () => {
    expect(membersStatus(partial, true, false)).toEqual({
      text: `9 of 12 slots filled · ${ROUND_CLOSED}`,
      announce: ROUND_CLOSED,
    });
  });

  it("composes a closed round with a draw in progress", () => {
    expect(membersStatus(partial, true, true)).toEqual({
      text: `9 of 12 slots filled · ${ROUND_CLOSED} · draw in progress`,
      announce: `${ROUND_CLOSED} · draw in progress`,
    });
  });

  it("keeps the closed round's word when a locked pool is also full", () => {
    expect(membersStatus(full, true, false).text).toBe(`12 of 12 slots filled · ${ROUND_CLOSED}`);
  });

  it("falls out as 0 of 3 for a single member", () => {
    expect(membersStatus({ state: "ready", filled: 0, slots: 3 }, false, false).text).toBe(
      "0 of 3 slots filled",
    );
  });

  it("never says the round is open", () => {
    for (const drawInProgress of [false, true]) {
      for (const occupancy of [partial, full]) {
        expect(membersStatus(occupancy, false, drawInProgress).text).not.toContain(ROUND_OPEN);
      }
    }
  });

  it("holds the numerator whether or not a draw is in progress", () => {
    // The pool stays frozen with the winner still in it until the reveal lands,
    // so the count must read the same the whole way through. A numerator that
    // dropped at reveal would say a film had been drawn.
    for (const drawInProgress of [false, true]) {
      expect(membersStatus(partial, false, drawInProgress).text).toContain("9 of 12 slots filled");
    }
  });
});
