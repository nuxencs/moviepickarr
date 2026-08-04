import { describe, expect, it } from "vitest";

import { timeAgo } from "@/lib/time";

describe("timeAgo", () => {
  const now = Date.parse("2026-07-19T12:00:00Z");

  it("returns an empty string for a missing or unparseable timestamp", () => {
    expect(timeAgo(undefined, now)).toBe("");
    expect(timeAgo("not-a-date", now)).toBe("");
  });

  it("says now under a minute, then the largest whole unit", () => {
    expect(timeAgo("2026-07-19T11:59:30Z", now)).toBe("now");
    expect(timeAgo("2026-07-19T11:58:00Z", now)).toBe("2 minutes ago");
    expect(timeAgo("2026-07-19T09:00:00Z", now)).toBe("3 hours ago");
    expect(timeAgo("2026-07-16T12:00:00Z", now)).toBe("3 days ago");
  });

  it("clamps a future timestamp to now rather than counting backwards", () => {
    expect(timeAgo("2026-07-19T12:05:00Z", now)).toBe("now");
  });
});
