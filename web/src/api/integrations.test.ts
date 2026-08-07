import { afterEach, describe, expect, it, vi } from "vitest";

import {
  IntegrationKeys,
  IntegrationProblem,
  cancelIntegrationRun,
  getTMDBIntegration,
  listIntegrationRuns,
  listIntegrations,
  saveTMDBIntegration,
  startTMDBRun,
  testTMDBConnection,
  type TMDBDraftRequest,
} from "@/api/integrations";

const draft: TMDBDraftRequest = {
  revision: 7,
  settings: {
    enabled: true,
    castLimit: 15,
    refreshIntervalMs: 3_600_000,
    ttlMs: 2_592_000_000,
    minIntervalMs: 250,
    maxRetries: 4,
    backoffMs: 500,
    batchLimit: 200,
  },
  removeFallbacks: [],
  apiKey: "",
  clearApiKey: false,
  confirmWarnings: false,
};

function jsonResponse(body: unknown, status = 200, contentType = "application/json") {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": contentType },
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("integration API", () => {
  it("keys run history pages by their complete query", () => {
    const query = { integration: "tmdb", cursor: "2026-08-04T13:00:00Z,43", limit: 50 };

    expect(IntegrationKeys.runs(query)).toEqual(["integrations", "runs", query]);
  });

  it("loads the Admin integration register with the session cookie", async () => {
    const rows = [{
      id: "tmdb",
      name: "TMDB",
      state: "connected",
      operations: [{ id: "refresh_stale", name: "Refresh stale" }],
    }];
    const fetch = vi.fn().mockResolvedValue(jsonResponse(rows));
    vi.stubGlobal("fetch", fetch);

    await expect(listIntegrations()).resolves.toEqual(rows);
    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/integrations",
      expect.objectContaining({
        method: "GET",
        credentials: "include",
        headers: { Accept: "application/json" },
      }),
    );
  });

  it("threads query cancellation into the TMDB settings read", async () => {
    const controller = new AbortController();
    const fetch = vi.fn().mockResolvedValue(jsonResponse({ revision: 7 }));
    vi.stubGlobal("fetch", fetch);

    await getTMDBIntegration(controller.signal);

    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/integrations/tmdb",
      expect.objectContaining({ signal: controller.signal }),
    );
  });

  it("sends the complete draft for saves and connection tests", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse({ revision: 8 }))
      .mockResolvedValueOnce(
        jsonResponse({ state: "connected", checkedAt: "2026-08-04T10:00:00Z" }),
      );
    vi.stubGlobal("fetch", fetch);

    await saveTMDBIntegration(draft);
    await testTMDBConnection(draft);

    expect(fetch).toHaveBeenNthCalledWith(
      1,
      "/api/v1/integrations/tmdb",
      expect.objectContaining({ method: "PUT", body: JSON.stringify(draft) }),
    );
    expect(fetch).toHaveBeenNthCalledWith(
      2,
      "/api/v1/integrations/tmdb/test",
      expect.objectContaining({ method: "POST", body: JSON.stringify(draft) }),
    );
  });

  it("preserves structured validation and warning details", async () => {
    const fetch = vi.fn().mockResolvedValue(
      jsonResponse(
        {
          title: "confirmation_required",
          status: 409,
          detail: "Unusually aggressive settings need confirmation",
          warnings: [{ field: "ttl", message: "is below 1 hour" }],
        },
        409,
        "application/problem+json",
      ),
    );
    vi.stubGlobal("fetch", fetch);

    const error = await saveTMDBIntegration(draft).catch((reason: unknown) => reason);

    expect(error).toBeInstanceOf(IntegrationProblem);
    expect(error).toMatchObject({
      status: 409,
      title: "confirmation_required",
      message: "Unusually aggressive settings need confirmation",
      warnings: [{ field: "ttl", message: "is below 1 hour" }],
    });
  });

  it("starts and cancels manual TMDB runs through the action API", async () => {
    const run = {
      id: 77,
      integration: "tmdb",
      operation: "refresh_stale",
      trigger: "manual",
      configRevision: 7,
      status: "running",
      startedAt: "2026-08-04T16:40:00Z",
      progress: {
        total: 20,
        processed: 0,
        succeeded: 0,
        failed: 0,
        skipped: 0,
        remaining: 20,
      },
      failedSubjects: [],
    };
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(jsonResponse(run, 202))
      .mockResolvedValueOnce(new Response("", { status: 202 }));
    vi.stubGlobal("fetch", fetch);

    await expect(startTMDBRun("refresh_stale", false)).resolves.toEqual(run);
    await cancelIntegrationRun(77);

    expect(fetch).toHaveBeenNthCalledWith(
      1,
      "/api/v1/integrations/tmdb/runs",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ operation: "refresh_stale", confirm: false }),
      }),
    );
    expect(fetch).toHaveBeenNthCalledWith(
      2,
      "/api/v1/integration-runs/77",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("loads a filtered keyset page of integration runs", async () => {
    const page = { runs: [], nextCursor: "2026-08-04T12:30:00Z,41" };
    const fetch = vi.fn().mockResolvedValue(jsonResponse(page));
    vi.stubGlobal("fetch", fetch);
    const controller = new AbortController();

    await expect(
      listIntegrationRuns(
        {
          integration: "tmdb",
          operation: "refresh_stale",
          status: "completed_with_errors",
          trigger: "manual",
          cursor: "2026-08-04T13:00:00Z,43",
          limit: 50,
        },
        controller.signal,
      ),
    ).resolves.toEqual(page);

    expect(fetch).toHaveBeenCalledWith(
      "/api/v1/integration-runs?integration=tmdb&operation=refresh_stale&status=completed_with_errors&trigger=manual&cursor=2026-08-04T13%3A00%3A00Z%2C43&limit=50",
      expect.objectContaining({ method: "GET", signal: controller.signal }),
    );
  });
});
