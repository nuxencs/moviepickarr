import { describe, expect, it } from "vitest";

import { scheduledRunDiscoveryDelay } from "@/pages/tmdbRunDiscovery";

const MAX_BROWSER_TIMEOUT_MS = 2_147_483_647;

describe("scheduled TMDB run discovery", () => {
  it("chunks waits that exceed the browser timer range", () => {
    const now = Date.parse("2026-08-05T12:00:00Z");
    const dueAt = now + MAX_BROWSER_TIMEOUT_MS + 10_000;

    expect(scheduledRunDiscoveryDelay(dueAt, now)).toBe(MAX_BROWSER_TIMEOUT_MS);
    expect(scheduledRunDiscoveryDelay(dueAt, now + MAX_BROWSER_TIMEOUT_MS)).toBe(10_100);
    expect(scheduledRunDiscoveryDelay(dueAt, dueAt)).toBeNull();
    expect(scheduledRunDiscoveryDelay(now + 10_000, now)).toBe(10_100);
    expect(scheduledRunDiscoveryDelay(now, now)).toBeNull();
  });
});
