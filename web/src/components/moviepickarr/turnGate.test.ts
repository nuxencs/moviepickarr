import { describe, expect, it } from "vitest";

import {
  drawLockedTip,
  revealLockedTip,
  turnGate,
  watchLockedTip,
  type TurnGateInputs,
} from "@/components/moviepickarr/turnGate";

function inputs(overrides: Partial<TurnGateInputs> = {}): TurnGateInputs {
  return {
    role: "member",
    meID: 2,
    nextUpID: 1,
    nextUpName: "Ada",
    ...overrides,
  };
}

describe("turnGate", () => {
  it("locks a non-next-up member", () => {
    const gate = turnGate(inputs({ meID: 2, nextUpID: 1 }));
    expect(gate.canAct).toBe(false);
    expect(gate.locked).toBe(true);
    expect(gate.resolved).toBe(true);
    expect(gate.nextUpName).toBe("Ada");
  });

  it("lets the next-up member act", () => {
    const gate = turnGate(inputs({ meID: 1, nextUpID: 1 }));
    expect(gate.canAct).toBe(true);
    expect(gate.locked).toBe(false);
    expect(gate.isSelf).toBe(true);
  });

  it("locks an admin who isn't next-up, but lets them skip the turn", () => {
    const gate = turnGate(inputs({ role: "admin", meID: 9, nextUpID: 1 }));
    expect(gate.canAct).toBe(false);
    expect(gate.locked).toBe(true);
    expect(gate.isSelf).toBe(false);
    expect(gate.canSkip).toBe(true);
  });

  it("lets an admin act on their own turn", () => {
    const gate = turnGate(inputs({ role: "admin", meID: 1, nextUpID: 1 }));
    expect(gate.canAct).toBe(true);
    expect(gate.locked).toBe(false);
  });

  it("never offers the skip to a member or guest", () => {
    expect(turnGate(inputs({ role: "member" })).canSkip).toBe(false);
    expect(turnGate(inputs({ role: "guest" })).canSkip).toBe(false);
    expect(turnGate(inputs({ role: undefined })).canSkip).toBe(false);
  });

  it("locks a guest even if stale next-up data names them", () => {
    const gate = turnGate(inputs({ role: "guest", meID: 1, nextUpID: 1 }));
    expect(gate.canAct).toBe(false);
    expect(gate.locked).toBe(true);
    expect(gate.guest).toBe(true);
  });

  it("marks a non-next-up member as not self", () => {
    const gate = turnGate(inputs({ meID: 2, nextUpID: 1 }));
    expect(gate.isSelf).toBe(false);
  });

  it("is not self while next-up is still loading", () => {
    const gate = turnGate(inputs({ meID: 1, nextUpID: undefined, nextUpName: undefined }));
    expect(gate.isSelf).toBe(false);
  });

  it("locks an admin and offers no skip when next-up is unresolved", () => {
    const gate = turnGate(inputs({ role: "admin", meID: 9, nextUpID: 0, nextUpName: "" }));
    expect(gate.canAct).toBe(false);
    expect(gate.locked).toBe(true);
    expect(gate.resolved).toBe(false);
    expect(gate.canSkip).toBe(false);
  });

  it("locks a member with the waiting fallback when next-up is unresolved", () => {
    const gate = turnGate(inputs({ meID: 2, nextUpID: 0, nextUpName: "" }));
    expect(gate.locked).toBe(true);
    expect(gate.resolved).toBe(false);
  });

  it("errs open (no lock) while the session actor is still loading", () => {
    const gate = turnGate(inputs({ role: undefined, meID: undefined }));
    expect(gate.canAct).toBe(true);
    expect(gate.locked).toBe(false);
  });

  it("errs open (no lock) while next-up is still loading", () => {
    const gate = turnGate(inputs({ nextUpID: undefined, nextUpName: undefined }));
    expect(gate.canAct).toBe(true);
    expect(gate.locked).toBe(false);
  });
});

describe("locked tooltips", () => {
  it("name the next-up member when resolved", () => {
    const gate = turnGate(inputs({ meID: 2, nextUpID: 1, nextUpName: "Ada" }));
    expect(drawLockedTip(gate)).toBe("It's Ada's turn to draw.");
    expect(revealLockedTip(gate)).toBe("Only Ada can reveal this draw.");
    expect(watchLockedTip(gate)).toBe("Only Ada can mark this watched.");
  });

  it("uses a bare apostrophe for a next-up name ending in s", () => {
    const gate = turnGate(inputs({ meID: 2, nextUpID: 1, nextUpName: "Aleks" }));
    expect(drawLockedTip(gate)).toBe("It's Aleks' turn to draw.");
  });

  it("fall back to the waiting copy when unresolved", () => {
    const gate = turnGate(inputs({ meID: 2, nextUpID: 0, nextUpName: "" }));
    expect(drawLockedTip(gate)).toBe("Waiting for the next-up member.");
    expect(revealLockedTip(gate)).toBe("Waiting for the next-up member.");
    expect(watchLockedTip(gate)).toBe("Waiting for the next-up member.");
  });

  it("explains the Guest restriction instead of naming the turn holder", () => {
    const gate = turnGate(inputs({ role: "guest", meID: 2, nextUpID: 1 }));
    expect(drawLockedTip(gate)).toBe("Guests can view the draw but cannot start one.");
    expect(revealLockedTip(gate)).toBe("Guests can view the draw but cannot reveal it.");
    expect(watchLockedTip(gate)).toBe("Guests can view the draw but cannot mark it watched.");
  });
});
