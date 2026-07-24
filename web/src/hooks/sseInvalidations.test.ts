import { describe, expect, it } from "vitest";

import { MoviesKeys, SettingsKeys, StatsKeys, UsersKeys } from "@/api/query_keys";

import { invalidationsFor, resyncKeys, SSE_INVALIDATIONS } from "@/hooks/sseInvalidations";

const has = (keys: readonly (readonly unknown[])[], key: readonly unknown[]) =>
  keys.some((k) => JSON.stringify(k) === JSON.stringify(key));

describe("the invalidation table", () => {
  // Exhaustiveness over SSEEventType is enforced at compile time by the
  // Record type; this pins the runtime lookup path.
  it("answers every known event and rejects unknown ones", () => {
    for (const type of Object.keys(SSE_INVALIDATIONS)) {
      expect(invalidationsFor(type)).not.toBeNull();
    }
    expect(invalidationsFor("movie:exploded")).toBeNull();
    expect(invalidationsFor("hasOwnProperty")).toBeNull(); // prototype trap
  });

  it("movie:drawn holds the pool for the draw machine to release", () => {
    expect(has(SSE_INVALIDATIONS["movie:drawn"], MoviesKeys.listpool())).toBe(false);
    expect(has(SSE_INVALIDATIONS["movie:drawn"], MoviesKeys.current())).toBe(true);
    expect(has(SSE_INVALIDATIONS["movie:drawn"], SettingsKeys.nextUp())).toBe(true);
  });

  it("a reveal releases the pool the server held for the reel", () => {
    // The server hands the drawn movie back in every pool read until the reveal,
    // so the refresh has to happen when the reveal lands — including on clients
    // that never ran a reel and have no land of their own to hook.
    expect(has(SSE_INVALIDATIONS["movie:revealed"], MoviesKeys.listpool())).toBe(true);
  });

  it("a watched movie stales stats and the watched-derived filter options", () => {
    const row = SSE_INVALIDATIONS["movie:watched"];
    for (const key of [MoviesKeys.current(), MoviesKeys.listwatched(), MoviesKeys.filterOptions(), StatsKeys.all]) {
      expect(has(row, key)).toBe(true);
    }
  });

  it("an enrichment burst refreshes every cache embedding enriched fields", () => {
    const row = SSE_INVALIDATIONS["movies:enriched-batch"];
    for (const key of [
      UsersKeys.list(),
      MoviesKeys.listpool(),
      MoviesKeys.current(),
      MoviesKeys.listwatched(),
      MoviesKeys.details(),
      MoviesKeys.filterOptions(),
      StatsKeys.all,
    ]) {
      expect(has(row, key)).toBe(true);
    }
  });
});

describe("resyncKeys", () => {
  it("is the union of the table (deduped), pool included", () => {
    const keys = resyncKeys();
    // Everything a missed event could have staled must be re-pulled.
    for (const key of [
      UsersKeys.list(),
      MoviesKeys.listpool(),
      MoviesKeys.current(),
      MoviesKeys.listwatched(),
      MoviesKeys.details(),
      MoviesKeys.filterOptions(),
      StatsKeys.all,
      SettingsKeys.poolLock(),
      SettingsKeys.nextUp(),
    ]) {
      expect(has(keys, key)).toBe(true);
    }
    // Deduped: no key twice.
    const ids = keys.map((k) => JSON.stringify(k));
    expect(new Set(ids).size).toBe(ids.length);
  });
});
