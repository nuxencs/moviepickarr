import { describe, expect, it } from "vitest";

import { timeAgo, timeUntil } from "@/lib/time";

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

describe("timeUntil", () => {
  const now = Date.parse("2026-07-19T12:00:00Z");

  it("returns an empty string for a missing or unparseable timestamp", () => {
    expect(timeUntil(undefined, now)).toBe("");
    expect(timeUntil("not-a-date", now)).toBe("");
  });

  it("gives the largest whole unit remaining, with no trailing 'ago'", () => {
    expect(timeUntil("2026-07-19T12:02:00Z", now)).toBe("2 minutes");
    expect(timeUntil("2026-07-19T15:00:00Z", now)).toBe("3 hours");
    expect(timeUntil("2026-07-22T12:00:00Z", now)).toBe("3 days");
  });

  it("says nothing for a span too short to name, so the caller can say 'shortly'", () => {
    expect(timeUntil("2026-07-19T12:00:30Z", now)).toBe("");
  });

  it("returns an empty string once the timestamp has passed", () => {
    expect(timeUntil("2026-07-19T11:59:00Z", now)).toBe("");
    expect(timeUntil("2026-07-19T12:00:00Z", now)).toBe("");
  });
});
