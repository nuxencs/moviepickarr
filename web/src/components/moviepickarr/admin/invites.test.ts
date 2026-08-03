import { describe, expect, it } from "vitest";

import { expiryLabel, groupInvites, issuedLabel } from "@/components/moviepickarr/admin/invites";

import type { InviteSummary } from "@/types/Response";

const now = Date.parse("2026-08-03T12:00:00Z");

function invite(overrides: Partial<InviteSummary> = {}): InviteSummary {
  return {
    id: 1,
    memberId: 7,
    memberName: "Ben",
    status: "open",
    expiresAt: "2026-08-06T12:00:00Z",
    issuedAt: "2026-08-01T12:00:00Z",
    issuedBy: "Ada",
    ...overrides,
  };
}

describe("expiryLabel", () => {
  it("counts down for an open invite", () => {
    expect(expiryLabel(invite(), now)).toBe("expires in 3 days");
  });

  it("counts up for an expired one", () => {
    expect(expiryLabel(invite({ status: "expired", expiresAt: "2026-08-01T12:00:00Z" }), now)).toBe(
      "expired 2 days ago",
    );
  });

  it("words the last minute rather than counting to zero", () => {
    expect(expiryLabel(invite({ expiresAt: "2026-08-03T12:00:20Z" }), now)).toBe("expires shortly");
    expect(expiryLabel(invite({ status: "expired", expiresAt: "2026-08-03T11:59:40Z" }), now)).toBe(
      "expired just now",
    );
  });

  it("trusts the server's word over its own clock", () => {
    // The status is the server's, derived against its clock. A client whose
    // clock is a few minutes behind must not narrate a row it was told is
    // expired as if it were still live.
    expect(expiryLabel(invite({ status: "expired", expiresAt: "2026-08-03T12:05:00Z" }), now)).toBe(
      "expired just now",
    );
  });
});

describe("issuedLabel", () => {
  it("names the issuer and when", () => {
    expect(issuedLabel(invite(), now)).toBe("issued by Ada · 2 days ago");
  });

  it("drops the line entirely when no issuer was recorded", () => {
    expect(issuedLabel(invite({ issuedBy: undefined }), now)).toBeNull();
  });
});

describe("groupInvites", () => {
  it("splits the two groups and keeps the server's order inside each", () => {
    const rows = [
      invite({ id: 1, memberName: "Ben" }),
      invite({ id: 2, memberName: "Cleo", status: "expired" }),
      invite({ id: 3, memberName: "Dev" }),
    ];

    const { open, expired } = groupInvites(rows);

    expect(open.map((i) => i.memberName)).toEqual(["Ben", "Dev"]);
    expect(expired.map((i) => i.memberName)).toEqual(["Cleo"]);
  });
});
