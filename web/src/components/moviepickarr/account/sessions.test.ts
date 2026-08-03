import { describe, expect, it } from "vitest";

import { otherDeviceCount, sessionMeta } from "@/components/moviepickarr/account/sessions";

import type { SessionSummary } from "@/types/Response";

const now = Date.parse("2026-07-19T12:00:00Z");

function session(overrides: Partial<SessionSummary> = {}): SessionSummary {
  return {
    id: 1,
    device: "Chrome on macOS",
    ip: "192.168.1.40",
    lastSeenAt: "2026-07-19T10:00:00Z",
    current: false,
    ...overrides,
  };
}

describe("sessionMeta", () => {
  it("reads activity then address", () => {
    expect(sessionMeta(session(), now)).toBe("active 2 hours ago · 192.168.1.40");
  });

  it("says active now for a session touched seconds ago", () => {
    expect(sessionMeta(session({ lastSeenAt: "2026-07-19T11:59:50Z" }), now)).toBe("active now · 192.168.1.40");
  });

  it("drops an address the server never recorded", () => {
    expect(sessionMeta(session({ ip: undefined }), now)).toBe("active 2 hours ago");
  });

  it("falls back to the address alone when the timestamp is unusable", () => {
    expect(sessionMeta(session({ lastSeenAt: "" }), now)).toBe("192.168.1.40");
  });

  it("returns an empty string when it knows nothing about the device", () => {
    expect(sessionMeta(session({ ip: undefined, lastSeenAt: "" }), now)).toBe("");
  });
});

describe("otherDeviceCount", () => {
  it("counts every session but this one", () => {
    expect(otherDeviceCount([session({ current: true }), session({ id: 2 }), session({ id: 3 })])).toBe(2);
  });

  it("is zero when this device is the only one", () => {
    expect(otherDeviceCount([session({ current: true })])).toBe(0);
  });

  it("is zero for an empty list", () => {
    expect(otherDeviceCount([])).toBe(0);
  });
});
