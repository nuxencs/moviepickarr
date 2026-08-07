import { describe, expect, it } from "vitest";

import type { TMDBIntegration } from "@/api/integrations";

import {
  buildTMDBRequest,
  createTMDBDraft,
  draftIsDirty,
  type TMDBFormDraft,
} from "@/pages/tmdbDraft";

const config: TMDBIntegration = {
  revision: 7,
  state: "connected",
  settings: {
    enabled: {
      value: true,
      source: "admin",
      default: false,
      hasAdminFallback: false,
      environment: "TMDB_ENABLED",
    },
    apiKey: {
      configured: true,
      source: "admin",
      hasAdminFallback: false,
      environment: "TMDB_API_KEY",
    },
    castLimit: {
      value: 15,
      source: "default",
      default: 15,
      hasAdminFallback: false,
      environment: "TMDB_ENRICH_CAST_LIMIT",
    },
    refreshIntervalMs: {
      value: 900_000,
      source: "environment",
      default: 3_600_000,
      hasAdminFallback: true,
      environment: "TMDB_ENRICH_REFRESH_INTERVAL",
    },
    ttlMs: {
      value: 86_400_000,
      source: "admin",
      default: 2_592_000_000,
      hasAdminFallback: false,
      environment: "TMDB_ENRICH_TTL",
    },
    minIntervalMs: {
      value: 250,
      source: "default",
      default: 250,
      hasAdminFallback: false,
      environment: "TMDB_ENRICH_MIN_INTERVAL_MS",
    },
    maxRetries: {
      value: 4,
      source: "default",
      default: 4,
      hasAdminFallback: false,
      environment: "TMDB_ENRICH_MAX_RETRIES",
    },
    backoffMs: {
      value: 500,
      source: "default",
      default: 500,
      hasAdminFallback: false,
      environment: "TMDB_ENRICH_BACKOFF_MS",
    },
    batchLimit: {
      value: 200,
      source: "default",
      default: 200,
      hasAdminFallback: false,
      environment: "TMDB_ENRICH_BATCH_LIMIT",
    },
  },
};

describe("TMDB form draft", () => {
  it("round-trips active Admin values without turning defaults into overrides", () => {
    const draft = createTMDBDraft(config);

    expect(buildTMDBRequest(config, draft, false)).toEqual({
      revision: 7,
      settings: { enabled: true, ttlMs: 86_400_000 },
      removeFallbacks: [],
      apiKey: "",
      clearApiKey: false,
      confirmWarnings: false,
    });
    expect(draftIsDirty(config, draft)).toBe(false);
  });

  it("converts typed durations and stages fallback removal in one request", () => {
    const draft: TMDBFormDraft = {
      ...createTMDBDraft(config),
      castLimit: "22",
      ttl: { amount: "2", unit: "days" },
      removals: ["refreshInterval"],
    };

    expect(buildTMDBRequest(config, draft, true)).toMatchObject({
      revision: 7,
      settings: { enabled: true, castLimit: 22, ttlMs: 172_800_000 },
      removeFallbacks: ["refreshInterval"],
      confirmWarnings: true,
    });
    expect(draftIsDirty(config, draft)).toBe(true);
  });

  it("reports invalid numbers before a draft reaches the API", () => {
    const editableConfig: TMDBIntegration = {
      ...config,
      settings: {
        ...config.settings,
        refreshIntervalMs: {
          ...config.settings.refreshIntervalMs,
          value: 3_600_000,
          source: "default",
          hasAdminFallback: false,
        },
      },
    };
    const draft: TMDBFormDraft = {
      ...createTMDBDraft(editableConfig),
      refreshEnabled: true,
      refreshInterval: { amount: "", unit: "hours" },
      batchLimit: "0",
    };

    expect(() => buildTMDBRequest(editableConfig, draft, false)).toThrowError(
      expect.objectContaining({
        issues: expect.arrayContaining([
          { field: "refreshInterval", message: "Enter a duration." },
          { field: "batchLimit", message: "Must be at least 1." },
        ]),
      }),
    );
  });

  it("reserves zero for the explicit all-cast and disabled-refresh choices", () => {
    const editableConfig: TMDBIntegration = {
      ...config,
      settings: {
        ...config.settings,
        refreshIntervalMs: {
          ...config.settings.refreshIntervalMs,
          value: 3_600_000,
          source: "default",
          hasAdminFallback: false,
        },
      },
    };
    const draft: TMDBFormDraft = {
      ...createTMDBDraft(editableConfig),
      castLimit: "0",
      allCast: false,
      refreshEnabled: true,
      refreshInterval: { amount: "0", unit: "hours" },
    };

    expect(() => buildTMDBRequest(editableConfig, draft, false)).toThrowError(
      expect.objectContaining({
        issues: expect.arrayContaining([
          { field: "castLimit", message: "Must be at least 1." },
          { field: "refreshInterval", message: "Must be greater than zero." },
        ]),
      }),
    );

    expect(
      buildTMDBRequest(
        editableConfig,
        { ...draft, allCast: true, refreshEnabled: false },
        false,
      ),
    ).toMatchObject({
      settings: expect.objectContaining({ castLimit: 0, refreshIntervalMs: 0 }),
    });
  });
});
