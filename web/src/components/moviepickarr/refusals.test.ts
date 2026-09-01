import { describe, expect, it } from "vitest";

import {
  actionLabel,
  deleteLabel,
  deleteRefusalOf,
  isDeletable,
  refusalOf,
} from "@/components/moviepickarr/refusals";

describe("refusalOf", () => {
  const open = { isLocked: false, drawInFlight: false, poolFull: false };

  it("refuses nothing on an open round with room in the pool", () => {
    expect(refusalOf({ kind: "promote", ...open })).toBeNull();
    expect(refusalOf({ kind: "demote", ...open })).toBeNull();
  });

  it("refuses every promote once the pool is full", () => {
    expect(refusalOf({ kind: "promote", ...open, poolFull: true })).toBe("full");
  });

  it("refuses a Guest promotion but still permits demotion", () => {
    expect(refusalOf({ kind: "promote", ...open, guest: true })).toBe("guest");
    expect(refusalOf({ kind: "demote", ...open, guest: true })).toBeNull();
    expect(actionLabel("promote", "guest")).toBe(
      "Move to pool, guest role cannot add movies to the pool",
    );
  });

  it("never refuses a demote for a full pool: it is the way out of one", () => {
    expect(refusalOf({ kind: "demote", ...open, poolFull: true })).toBeNull();
  });

  it("refuses both directions on a locked round", () => {
    expect(refusalOf({ kind: "promote", ...open, isLocked: true })).toBe("locked");
    expect(refusalOf({ kind: "demote", ...open, isLocked: true })).toBe("locked");
  });

  it("refuses both directions while the round state is unavailable", () => {
    expect(refusalOf({ kind: "promote", ...open, stateKnown: false })).toBe("unavailable");
    expect(refusalOf({ kind: "demote", ...open, stateKnown: false })).toBe("unavailable");
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
    expect(actionLabel("demote", "unavailable")).toBe(
      "Move back to stash, round state unavailable",
    );
  });

  it("says round closed in the words the status line uses", () => {
    expect(actionLabel("demote", "locked")).toBe("Move back to stash, round closed");
  });
});

describe("isDeletable", () => {
  it("takes the two statuses the server deletes from", () => {
    expect(isDeletable("stash")).toBe(true);
    expect(isDeletable("pool")).toBe(true);
  });

  it("leaves history and the held winner alone", () => {
    expect(isDeletable("watched")).toBe(false);
    expect(isDeletable("current")).toBe(false);
  });

  it("says no while the status is still on its way with the detail", () => {
    expect(isDeletable(undefined)).toBe(false);
  });
});

describe("deleteRefusalOf", () => {
  const open = { isLocked: false, drawInFlight: false };

  it("refuses nothing on an open round", () => {
    expect(deleteRefusalOf({ status: "pool", ...open })).toBeNull();
    expect(deleteRefusalOf({ status: "stash", ...open })).toBeNull();
  });

  it("refuses a pooled movie while the round is closed", () => {
    expect(deleteRefusalOf({ status: "pool", ...open, isLocked: true })).toBe("locked");
  });

  it("refuses a pooled movie while a draw is out", () => {
    expect(deleteRefusalOf({ status: "pool", ...open, drawInFlight: true })).toBe("drawing");
  });

  it("leaves the stash alone throughout: stash adds are not lock-checked either", () => {
    expect(deleteRefusalOf({ status: "stash", isLocked: true, drawInFlight: true })).toBeNull();
  });

  it("refuses a pooled delete while the round state is unavailable", () => {
    expect(
      deleteRefusalOf({ status: "pool", ...open, stateKnown: false }),
    ).toBe("unavailable");
  });

  it("refuses a cached stash delete while its record state is unavailable", () => {
    expect(
      deleteRefusalOf({ status: "stash", ...open, stateKnown: false }),
    ).toBe("unavailable");
  });

  it("says drawing ahead of locked, as a move does", () => {
    expect(deleteRefusalOf({ status: "pool", isLocked: true, drawInFlight: true })).toBe("drawing");
  });
});

describe("deleteLabel", () => {
  it("names the action alone when nothing refuses it", () => {
    expect(deleteLabel(null)).toBe("Delete");
  });

  it("puts the reason after the verb, in the same words a tile uses", () => {
    expect(deleteLabel("drawing")).toBe("Delete, a draw is in progress");
    expect(deleteLabel("locked")).toBe("Delete, round closed");
    expect(deleteLabel("unavailable")).toBe("Delete, round state unavailable");
  });
});
