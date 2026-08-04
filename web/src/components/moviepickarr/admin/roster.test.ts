import { describe, expect, it } from "vitest";

import {
  credLabel,
  loginChips,
  removeOutcome,
  unlinkWouldStrand,
} from "@/components/moviepickarr/admin/roster";

import type { RosterMember } from "@/types/Response";


function member(overrides: Partial<RosterMember> = {}): RosterMember {
  return {
    id: 1,
    name: "Test",
    role: "member",
    archived: false,
    hasLocalLogin: false,
    hasLinkedIdentity: false,
    invitePending: false,
    moviesAuthored: 0,
    ...overrides,
  };
}

describe("loginChips", () => {
  it("collapses an archived member to one muted chip, ignoring credentials", () => {
    const chips = loginChips(member({ archived: true, hasLocalLogin: true, invitePending: true }));
    expect(chips).toEqual([{ kind: "archived", label: "Archived" }]);
  });

  it("shows invite-sent for a placeholder with a pending invite", () => {
    expect(loginChips(member({ invitePending: true }))).toEqual([
      { kind: "pending", label: "Invite sent" },
    ]);
  });

  it("shows no-login-yet for a placeholder with no invite", () => {
    expect(loginChips(member())).toEqual([{ kind: "empty", label: "No login yet" }]);
  });

  it("shows one chip per credential actually held", () => {
    expect(loginChips(member({ hasLocalLogin: true })).map((c) => c.kind)).toEqual(["password"]);
    expect(loginChips(member({ hasLinkedIdentity: true })).map((c) => c.kind)).toEqual(["sso"]);
    expect(
      loginChips(member({ hasLocalLogin: true, hasLinkedIdentity: true })).map((c) => c.kind),
    ).toEqual(["password", "sso"]);
  });
});

describe("credLabel", () => {
  it("summarizes each state in one line", () => {
    expect(credLabel(member({ archived: true }))).toBe("Archived");
    expect(credLabel(member({ invitePending: true }))).toBe("Invited");
    expect(credLabel(member())).toBe("No login yet");
    expect(credLabel(member({ hasLocalLogin: true, hasLinkedIdentity: true }))).toBe("Password + SSO");
  });
});

describe("removeOutcome", () => {
  it("hard-deletes when the member authored nothing, archives otherwise", () => {
    expect(removeOutcome(member({ moviesAuthored: 0 }))).toBe("delete");
    expect(removeOutcome(member({ moviesAuthored: 3 }))).toBe("archive");
  });
});

describe("unlinkWouldStrand", () => {
  it("blocks only when it's your own SSO-only account", () => {
    const ssoOnly = member({ hasLinkedIdentity: true });
    expect(unlinkWouldStrand(ssoOnly, true)).toBe(true);
    // Someone else's last credential is fine (they fall back to a placeholder).
    expect(unlinkWouldStrand(ssoOnly, false)).toBe(false);
    // A password fallback means unlinking your SSO doesn't strand you.
    expect(unlinkWouldStrand(member({ hasLinkedIdentity: true, hasLocalLogin: true }), true)).toBe(false);
  });
});
