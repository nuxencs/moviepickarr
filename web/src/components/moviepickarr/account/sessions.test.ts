import { describe, expect, it } from "vitest";

import { otherDeviceCount, sessionMeta } from "@/components/moviepickarr/account/sessions";

import type { SessionSummary } from "@/types/Response";

const now = Date.parse("2026-07-19T12:00:00Z");

function session(overrides: Partial<SessionSummary> = {}): SessionSummary {
  return {
    id: "session-a",
    device: "Chrome on macOS",
    lastSeenAt: "2026-07-19T10:00:00Z",
    current: false,
    ...overrides,
  };
}

describe("sessionMeta", () => {
  it("reads the session activity", () => {
    expect(sessionMeta(session(), now)).toBe("active 2 hours ago");
  });

  it("says active now for a session touched seconds ago", () => {
    expect(sessionMeta(session({ lastSeenAt: "2026-07-19T11:59:50Z" }), now)).toBe("active now");
  });

  it("returns an empty string when the timestamp is unusable", () => {
    expect(sessionMeta(session({ lastSeenAt: "" }), now)).toBe("");
  });
});

describe("otherDeviceCount", () => {
  it("counts every session but this one", () => {
    expect(otherDeviceCount([session({ current: true }), session({ id: "session-b" }), session({ id: "session-c" })])).toBe(2);
  });

  it("is zero when this device is the only one", () => {
    expect(otherDeviceCount([session({ current: true })])).toBe(0);
  });

  it("is zero for an empty list", () => {
    expect(otherDeviceCount([])).toBe(0);
  });
});
