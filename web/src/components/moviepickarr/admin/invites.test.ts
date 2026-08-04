import { describe, expect, it } from "vitest";

import {
  expiryLabel,
  inviteStatusAt,
  issuedLabel,
  nextInviteExpiryDelay,
  serverAlignedNow,
} from "@/components/moviepickarr/admin/invites";

import type { InviteSummary } from "@/types/Response";

const now = Date.parse("2026-08-03T12:00:00Z");

function invite(overrides: Partial<InviteSummary> = {}): InviteSummary {
  return {
    id: "invite-public-1",
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

  it("uses the supplied server-aligned instant at the exact boundary", () => {
    const row = invite({ status: "open", expiresAt: "2026-08-03T12:00:00Z" });
    expect(inviteStatusAt(row, now - 1)).toBe("open");
    expect(inviteStatusAt(row, now)).toBe("expired");
    expect(expiryLabel(row, now)).toBe("expired just now");
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

describe("server clock", () => {
  it("keeps client wall-clock skew out while preserving elapsed time", () => {
    const receivedAt = Date.parse("2040-01-01T00:00:00Z");
    expect(
      serverAlignedNow("2026-08-03T12:00:00Z", receivedAt, receivedAt + 2_500),
    ).toBe(now + 2_500);
  });

  it("schedules the nearest open expiry only", () => {
    expect(
      nextInviteExpiryDelay(
        [
          invite({ expiresAt: "2026-08-03T12:00:05Z" }),
          invite({ expiresAt: "2026-08-03T12:00:02Z" }),
          invite({ expiresAt: "2026-08-03T11:00:00Z" }),
        ],
        now,
      ),
    ).toBe(2_000);
  });
});
