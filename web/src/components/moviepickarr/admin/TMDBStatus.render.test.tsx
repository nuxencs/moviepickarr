import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen, within } from "@testing-library/react";
import { expect, it } from "vitest";

import type {
  IntegrationRun,
  IntegrationSetting,
  IntegrationSource,
  TMDBIntegration,
} from "@/api/integrations";

import { TMDBStatus } from "@/components/moviepickarr/admin/TMDBStatus";

window.scrollTo = (() => {}) as typeof window.scrollTo;

function setting<T>(
  value: T,
  source: IntegrationSource,
  environment: string,
): IntegrationSetting<T> {
  return { value, default: value, source, environment, hasAdminFallback: false };
}

function tmdbConfig(overrides: Partial<TMDBIntegration> = {}): TMDBIntegration {
  return {
    revision: 7,
    state: "connected",
    settings: {
      enabled: setting(true, "admin", "TMDB_ENABLED"),
      apiKey: {
        configured: true,
        source: "admin",
        hasAdminFallback: false,
        environment: "TMDB_API_KEY",
      },
      castLimit: setting(15, "admin", "TMDB_ENRICH_CAST_LIMIT"),
      refreshIntervalMs: setting(3_600_000, "default", "TMDB_ENRICH_REFRESH_INTERVAL"),
      ttlMs: setting(2_592_000_000, "admin", "TMDB_ENRICH_TTL"),
      minIntervalMs: setting(250, "default", "TMDB_ENRICH_MIN_INTERVAL_MS"),
      maxRetries: setting(4, "default", "TMDB_ENRICH_MAX_RETRIES"),
      backoffMs: setting(500, "default", "TMDB_ENRICH_BACKOFF_MS"),
      batchLimit: setting(200, "default", "TMDB_ENRICH_BATCH_LIMIT"),
    },
    ...overrides,
  };
}

function completedRun(): IntegrationRun {
  return {
    id: 77,
    integration: "tmdb",
    operation: "refresh_stale",
    trigger: "scheduled",
    configRevision: 7,
    status: "completed",
    startedAt: "2026-08-04T15:25:00Z",
    finishedAt: "2026-08-04T15:30:00Z",
    progress: {
      total: 20,
      processed: 20,
      succeeded: 19,
      failed: 1,
      skipped: 0,
      remaining: 0,
    },
    failedSubjects: [],
  };
}

function runningRun(): IntegrationRun {
  return {
    ...completedRun(),
    status: "running",
    finishedAt: undefined,
    progress: {
      total: 20,
      processed: 5,
      succeeded: 4,
      failed: 1,
      skipped: 0,
      remaining: 15,
    },
  };
}

async function renderStatus(config: TMDBIntegration) {
  const rootRoute = createRootRoute();
  const statusRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/status",
    component: () => <TMDBStatus config={config} />,
  });
  const runsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/admin/runs",
    component: () => null,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([statusRoute, runsRoute]),
    history: createMemoryHistory({ initialEntries: ["/status"] }),
  });
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await router.load();
}

it("leads with the health state and a quiet successful-refresh summary", async () => {
  await renderStatus(
    tmdbConfig({ lastSuccessfulRunAt: "2026-08-04T15:30:00Z" }),
  );

  const status = await screen.findByRole("region", { name: "TMDB status" });
  expect(within(status).getByRole("heading", { level: 3, name: "Connected" })).toBeTruthy();
  const refreshedAt = status.querySelector('time[datetime="2026-08-04T15:30:00Z"]');
  expect(refreshedAt?.parentElement?.textContent).toContain("Last refresh succeeded");
});

it("keeps routine activity in a native closed disclosure", async () => {
  await renderStatus(
    tmdbConfig({
      lastCheckedAt: "2026-08-04T16:30:00Z",
      lastConnectionTestedAt: "2026-08-04T16:45:00Z",
      nextCheckAt: "2026-08-04T17:30:00Z",
      lastSuccessfulRunAt: "2026-08-04T15:30:00Z",
      latestRun: completedRun(),
    }),
  );

  const status = await screen.findByRole("region", { name: "TMDB status" });
  const summary = within(status).getByText("Activity details", { selector: "summary" });
  const details = summary.closest("details") as HTMLDetailsElement;
  expect(details.open).toBe(false);
  expect(within(status).getByRole("group", { name: "Activity details" })).toBe(details);
  expect(within(details).getByText("Last library scan")).toBeTruthy();
  expect(within(details).getByText("Last connection test")).toBeTruthy();
  expect(details.querySelector('time[datetime="2026-08-04T16:45:00Z"]')).toBeTruthy();
  expect(within(details).getByText("Next scheduled scan")).toBeTruthy();
  expect(within(details).getByText("Last successful refresh")).toBeTruthy();
  expect(within(details).getByText("Latest run")).toBeTruthy();
  expect(within(details).getByText("Completed · Refresh stale")).toBeTruthy();
});

it("keeps an active run and its cancellation outside routine activity", async () => {
  await renderStatus(tmdbConfig({ latestRun: runningRun() }));

  const status = await screen.findByRole("region", { name: "TMDB status" });
  const activeRun = within(status).getByRole("region", { name: "Active run" });
  const activity = within(status).getByText("Activity details").closest("details");
  expect(activity?.contains(activeRun)).toBe(false);
  expect(within(activeRun).getByText("Running · Refresh stale")).toBeTruthy();
  expect(within(activeRun).getByText("5 of 20 processed")).toBeTruthy();
  expect(within(activeRun).getByText("15 remaining")).toBeTruthy();
  expect(within(status).getByRole("button", { name: "Cancel run" })).toBeTruthy();
});

it("shows single-movie enrichment as active without bulk-run controls", async () => {
  await renderStatus(
    tmdbConfig({
      latestRun: { ...runningRun(), operation: "enrich_movie", trigger: "movie_added" },
    }),
  );

  const status = await screen.findByRole("region", { name: "TMDB status" });
  const activeRun = within(status).getByRole("region", { name: "Active run" });
  expect(within(activeRun).getByText("Running · Enrich movie")).toBeTruthy();
  expect(within(status).queryByRole("button", { name: "Cancel run" })).toBeNull();
  expect(
    (within(status).getByRole("button", { name: "Refresh stale now" }) as HTMLButtonElement)
      .disabled,
  ).toBe(false);
});

it("keeps reasons, warnings, and the latest run error outside routine activity", async () => {
  await renderStatus(
    tmdbConfig({
      state: "error",
      reason: "The configured API key was rejected.",
      warnings: [{ field: "minInterval", message: "Request interval is below 250 ms." }],
      latestRun: {
        ...completedRun(),
        status: "failed",
        errorSummary: "TMDB rate limit exhausted.",
      },
    }),
  );

  const status = await screen.findByRole("region", { name: "TMDB status" });
  const activity = within(status).getByText("Activity details").closest("details");
  const reason = within(status).getByText("The configured API key was rejected.");
  const warning = within(status).getByText("Request interval is below 250 ms.");
  const runError = within(status).getByRole("alert", { name: "Latest run error" });
  expect(activity?.contains(reason)).toBe(false);
  expect(activity?.contains(warning)).toBe(false);
  expect(activity?.contains(runError)).toBe(false);
  expect(runError.textContent).toContain("TMDB rate limit exhausted.");
});
