import { describe, expect, it } from "vitest";

import { validateAdminRunsSearch } from "@/pages/adminRunsSearch";

describe("validateAdminRunsSearch", () => {
  it("keeps a complete integration-run history address", () => {
    expect(
      validateAdminRunsSearch({
        integration: "tmdb",
        operation: "refresh_stale",
        status: "completed_with_errors",
        cursor: "2026-08-04T12:30:00Z,41",
      }),
    ).toEqual({
      integration: "tmdb",
      operation: "refresh_stale",
      status: "completed_with_errors",
      cursor: "2026-08-04T12:30:00Z,41",
    });
  });

  it("drops malformed and unsupported values without throwing", () => {
    expect(
      validateAdminRunsSearch({
        integration: "   ",
        operation: "Delete everything",
        status: ["failed"],
        trigger: "scheduled",
        cursor: "not-a-cursor",
      }),
    ).toEqual({});
  });

  it("keeps a bounded operation from a future integration", () => {
    expect(validateAdminRunsSearch({ operation: "sync_collections" })).toEqual({
      operation: "sync_collections",
    });
  });

  it("drops active-run and trigger filters from result history", () => {
    expect(validateAdminRunsSearch({ status: "running", trigger: "movie_updated" })).toEqual({});
  });

  it("drops cursors the API cannot parse", () => {
    expect(
      validateAdminRunsSearch({ cursor: "2026-02-31T12:30:00Z,41" }),
    ).toEqual({});
    expect(
      validateAdminRunsSearch({ cursor: "2026-08-04T12:30:00Z,9999999999999999999" }),
    ).toEqual({});
  });
});
