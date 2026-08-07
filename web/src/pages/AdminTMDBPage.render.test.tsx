import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type {
  IntegrationSetting,
  IntegrationSource,
  TMDBIntegration,
} from "@/api/integrations";
import { IntegrationKeys, IntegrationProblem } from "@/api/integrations";

import { AdminTMDBPage } from "@/pages/AdminTMDBPage";

const api = vi.hoisted(() => ({
  cancelIntegrationRun: vi.fn(),
  getTMDBIntegration: vi.fn(),
  saveTMDBIntegration: vi.fn(),
  startTMDBRun: vi.fn(),
  testTMDBConnection: vi.fn(),
}));

vi.mock("@/api/integrations", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/integrations")>()),
  cancelIntegrationRun: api.cancelIntegrationRun,
  getTMDBIntegration: api.getTMDBIntegration,
  saveTMDBIntegration: api.saveTMDBIntegration,
  startTMDBRun: api.startTMDBRun,
  testTMDBConnection: api.testTMDBConnection,
}));

window.scrollTo = (() => {}) as typeof window.scrollTo;

function setting<T>(
  value: T,
  defaultValue: T,
  source: IntegrationSource,
  environment: string,
  hasAdminFallback = false,
): IntegrationSetting<T> {
  return { value, default: defaultValue, source, environment, hasAdminFallback };
}

function tmdbConfig(overrides: Partial<TMDBIntegration> = {}): TMDBIntegration {
  return {
    revision: 7,
    state: "connected",
    settings: {
      enabled: setting(true, false, "admin", "TMDB_ENABLED"),
      apiKey: {
        configured: true,
        source: "admin",
        hasAdminFallback: false,
        environment: "TMDB_API_KEY",
      },
      castLimit: setting(15, 15, "admin", "TMDB_ENRICH_CAST_LIMIT"),
      refreshIntervalMs: setting(
        3_600_000,
        3_600_000,
        "default",
        "TMDB_ENRICH_REFRESH_INTERVAL",
      ),
      ttlMs: setting(2_592_000_000, 2_592_000_000, "admin", "TMDB_ENRICH_TTL"),
      minIntervalMs: setting(
        100,
        250,
        "environment",
        "TMDB_ENRICH_MIN_INTERVAL_MS",
        true,
      ),
      maxRetries: setting(4, 4, "default", "TMDB_ENRICH_MAX_RETRIES"),
      backoffMs: setting(500, 500, "default", "TMDB_ENRICH_BACKOFF_MS"),
      batchLimit: setting(200, 200, "default", "TMDB_ENRICH_BATCH_LIMIT"),
    },
    ...overrides,
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

async function renderPage() {
  const rootRoute = createRootRoute();
  const integrationsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/admin/integrations",
    component: () => <Outlet />,
  });
  const tmdbRoute = createRoute({
    getParentRoute: () => integrationsRoute,
    path: "tmdb",
    component: AdminTMDBPage,
  });
  const runsRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/admin/runs",
    component: () => <p>Run history destination</p>,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      integrationsRoute.addChildren([tmdbRoute]),
      runsRoute,
    ]),
    history: createMemoryHistory({ initialEntries: ["/admin/integrations/tmdb"] }),
  });
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  });

  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await router.load();
  return { queryClient, router };
}

beforeEach(() => {
  api.getTMDBIntegration.mockReset().mockResolvedValue(tmdbConfig());
  api.cancelIntegrationRun.mockReset();
  api.saveTMDBIntegration.mockReset();
  api.startTMDBRun.mockReset();
  api.testTMDBConnection.mockReset();
});

describe("TMDB Admin settings", () => {
  it("exposes the selected integration without a redundant back link", async () => {
    await renderPage();

    expect(await screen.findByRole("heading", { name: "Configuration" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "TMDB" })).toBeTruthy();
    expect(screen.queryByRole("link", { name: "All integrations" })).toBeNull();
  });

  it("keeps setting help, environment keys, and defaults behind local info controls", async () => {
    await renderPage();

    const help = await screen.findByRole("button", { name: "About Enabled" });
    expect(screen.queryByRole("tooltip")).toBeNull();

    fireEvent.focus(help);
    expect(screen.getByRole("button", {
      name: "About Enabled",
      description: /Allow moviepickarr to search TMDB and fetch metadata.*TMDB_ENABLED.*Default: Disabled/,
    })).toBeTruthy();
    const tooltip = screen.getByRole("tooltip");
    expect(within(tooltip).getByText("Allow moviepickarr to search TMDB and fetch metadata."))
      .toBeTruthy();
    expect(within(tooltip).getByText("TMDB_ENABLED")).toBeTruthy();
    expect(within(tooltip).getByText("Default: Disabled")).toBeTruthy();

    fireEvent.keyDown(help, { key: "Escape" });
    expect(screen.queryByRole("tooltip")).toBeNull();

    fireEvent.click(help);
    expect(screen.getByRole("tooltip")).toBeTruthy();
    fireEvent.click(help);
    expect(screen.queryByRole("tooltip")).toBeNull();
  });

  it("pluralizes duration defaults in setting help", async () => {
    await renderPage();

    const scheduled = await screen.findByRole("button", { name: "About Scheduled refresh" });
    fireEvent.focus(scheduled);
    expect(within(screen.getByRole("tooltip")).getByText("Default: 1 hour")).toBeTruthy();
    fireEvent.blur(scheduled);

    const freshness = screen.getByRole("button", { name: "About Metadata freshness" });
    fireEvent.focus(freshness);
    expect(within(screen.getByRole("tooltip")).getByText("Default: 30 days")).toBeTruthy();
  });

  it("renders typed effective values, sources, and truthful status gaps", async () => {
    await renderPage();

    expect(await screen.findByRole("heading", { name: "Configuration" })).toBeTruthy();
    const status = screen.getByRole("region", { name: "TMDB status" });
    expect(within(status).getByText("Connected")).toBeTruthy();
    expect(within(status).getAllByText("Not available")).toHaveLength(5);

    const enabled = screen.getByRole("checkbox", { name: "Enabled" }) as HTMLInputElement;
    expect(enabled.checked).toBe(true);
    expect(screen.getAllByText("Source: Admin").length).toBeGreaterThan(0);

    const apiKey = screen.getByLabelText("New API key") as HTMLInputElement;
    expect(apiKey.type).toBe("password");
    expect(apiKey.value).toBe("");
    expect(screen.getByText("Configured")).toBeTruthy();

    const castLimit = screen.getByRole("spinbutton", { name: "Cast limit" }) as HTMLInputElement;
    expect(castLimit.value).toBe("15");
    expect(castLimit.min).toBe("1");
    expect((screen.getByRole("checkbox", { name: "All cast members" }) as HTMLInputElement).checked)
      .toBe(false);
    expect((screen.getByRole("checkbox", { name: "Scheduled refresh" }) as HTMLInputElement).checked)
      .toBe(true);
    const refreshEvery = screen.getByRole("spinbutton", { name: "Refresh every" }) as HTMLInputElement;
    expect(refreshEvery.value).toBe("1");
    expect(refreshEvery.min).toBe("1");
    expect((screen.getByRole("combobox", { name: "Refresh unit" }) as HTMLSelectElement).value)
      .toBe("hours");

    fireEvent.click(screen.getByText("Advanced"));
    expect(screen.getByText("Controlled by environment")).toBeTruthy();
    expect(screen.getByText("Below the recommended 250 ms minimum.")).toBeTruthy();
  });

  it("tests the current write-only draft without saving or clearing it", async () => {
    api.testTMDBConnection.mockResolvedValue({
      state: "connected",
      checkedAt: "2026-08-04T16:30:00Z",
    });
    await renderPage();

    const castLimit = await screen.findByRole("spinbutton", { name: "Cast limit" });
    const apiKey = screen.getByLabelText("New API key");
    fireEvent.change(castLimit, { target: { value: "22" } });
    fireEvent.change(apiKey, { target: { value: "new-secret" } });
    fireEvent.click(screen.getByRole("button", { name: "Test connection" }));

    await waitFor(() => expect(api.testTMDBConnection).toHaveBeenCalledTimes(1));
    expect(api.testTMDBConnection).toHaveBeenCalledWith(
      expect.objectContaining({
        revision: 7,
        settings: expect.objectContaining({
          enabled: true,
          castLimit: 22,
          ttlMs: 2_592_000_000,
        }),
        apiKey: "new-secret",
        clearApiKey: false,
      }),
    );
    const result = await screen.findByRole("status", { name: "Connection test result" });
    expect(within(result).getByText("Connected")).toBeTruthy();
    expect(within(result).getByText("Checked Aug 4, 2026 at 4:30 PM UTC")).toBeTruthy();
    expect((apiKey as HTMLInputElement).value).toBe("new-secret");
    expect(api.saveTMDBIntegration).not.toHaveBeenCalled();
    await waitFor(() => expect(api.getTMDBIntegration).toHaveBeenCalledTimes(2));
  });

  it("confirms aggressive settings, then saves the same whole draft", async () => {
    const saved = tmdbConfig({ revision: 8 });
    api.saveTMDBIntegration
      .mockRejectedValueOnce(
        new IntegrationProblem(
          409,
          "confirmation_required",
          "Unusually aggressive settings need confirmation.",
          [],
          [{ field: "ttl", message: "Metadata freshness is below 1 hour." }],
        ),
      )
      .mockResolvedValueOnce(saved);
    const { queryClient } = await renderPage();
    const historyKey = IntegrationKeys.runs({ integration: "tmdb", limit: 50 });
    queryClient.setQueryData(historyKey, { runs: [] });

    fireEvent.change(await screen.findByRole("spinbutton", { name: "Metadata freshness" }), {
      target: { value: "0.5" },
    });
    fireEvent.change(screen.getByRole("combobox", { name: "Metadata freshness unit" }), {
      target: { value: "hours" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    const dialog = await screen.findByRole("dialog", { name: "Confirm unusual settings" });
    expect(within(dialog).getByText("Metadata freshness is below 1 hour.")).toBeTruthy();
    expect(
      (screen.getByRole("spinbutton", { name: "Metadata freshness" }) as HTMLInputElement).value,
    ).toBe("0.5");
    fireEvent.click(within(dialog).getByRole("button", { name: "Save anyway" }));

    await waitFor(() => expect(api.saveTMDBIntegration).toHaveBeenCalledTimes(2));
    const first = api.saveTMDBIntegration.mock.calls[0]?.[0];
    const second = api.saveTMDBIntegration.mock.calls[1]?.[0];
    expect(first).toMatchObject({
      revision: 7,
      settings: expect.objectContaining({ ttlMs: 1_800_000 }),
      confirmWarnings: false,
    });
    expect(second).toEqual({ ...first, confirmWarnings: true });
    expect(await screen.findByText("Revision 8")).toBeTruthy();
    expect(screen.getByText("Changes saved.")).toBeTruthy();
    expect(queryClient.getQueryState(historyKey)?.isInvalidated).toBe(true);
  });

  it("maps structured server validation back to the typed control", async () => {
    api.saveTMDBIntegration.mockRejectedValue(
      new IntegrationProblem(
        422,
        "validation_failed",
        "TMDB settings are invalid.",
        [{ field: "batchLimit", message: "Must be at least 25 for this library." }],
      ),
    );
    await renderPage();

    expect(await screen.findByText("Advanced")).toBeTruthy();
    fireEvent.change(screen.getByRole("spinbutton", { name: "Cast limit" }), {
      target: { value: "22" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(await screen.findByText("Must be at least 25 for this library.")).toBeTruthy();
    const batchSize = screen.getByRole("spinbutton", {
      name: "Batch size",
      description: "Must be at least 25 for this library.",
    });
    expect(screen.getByText("TMDB settings are invalid.")).toBeTruthy();
    expect(batchSize.getAttribute("aria-invalid")).toBe("true");
  });

  it("links local zero-value errors to every related control", async () => {
    await renderPage();

    const castLimit = await screen.findByRole("spinbutton", { name: "Cast limit" });
    const allCast = screen.getByRole("checkbox", { name: "All cast members" });
    const refreshEvery = screen.getByRole("spinbutton", { name: "Refresh every" });
    const refreshUnit = screen.getByRole("combobox", { name: "Refresh unit" });
    const refreshEnabled = screen.getByRole("checkbox", { name: "Scheduled refresh" });
    fireEvent.change(castLimit, { target: { value: "0" } });
    fireEvent.change(refreshEvery, { target: { value: "0" } });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(await screen.findByText("Must be at least 1.")).toBeTruthy();
    expect(screen.getByText("Must be greater than zero.")).toBeTruthy();
    expect(screen.getByRole("spinbutton", {
      name: "Cast limit",
      description: "Must be at least 1.",
    })).toBe(castLimit);
    expect(screen.getByRole("checkbox", {
      name: "All cast members",
      description: "Must be at least 1.",
    })).toBe(allCast);
    expect(screen.getByRole("spinbutton", {
      name: "Refresh every",
      description: "Must be greater than zero.",
    })).toBe(refreshEvery);
    expect(screen.getByRole("combobox", {
      name: "Refresh unit",
      description: "Must be greater than zero.",
    })).toBe(refreshUnit);
    expect(screen.getByRole("checkbox", {
      name: "Scheduled refresh",
      description: "Must be greater than zero.",
    })).toBe(refreshEnabled);
    expect(api.saveTMDBIntegration).not.toHaveBeenCalled();
  });

  it("saves default, fallback, and write-only secret actions atomically", async () => {
    api.saveTMDBIntegration.mockResolvedValue(tmdbConfig({ revision: 8 }));
    await renderPage();

    const cast = await screen.findByRole("group", { name: "Cast limit" });
    fireEvent.click(within(cast).getByRole("button", { name: "Use default" }));
    const apiKey = screen.getByRole("group", { name: "API key" });
    fireEvent.click(within(apiKey).getByRole("button", { name: "Clear API key" }));
    fireEvent.click(await screen.findByText("Advanced"));
    const pacing = screen.getByRole("group", { name: "Request interval" });
    fireEvent.click(within(pacing).getByRole("button", { name: "Remove saved fallback" }));
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await waitFor(() => expect(api.saveTMDBIntegration).toHaveBeenCalledTimes(1));
    const request = api.saveTMDBIntegration.mock.calls[0]?.[0];
    expect(request).toMatchObject({
      revision: 7,
      settings: { enabled: true, ttlMs: 2_592_000_000 },
      removeFallbacks: ["castLimit", "minInterval"],
      apiKey: "",
      clearApiKey: true,
      confirmWarnings: false,
    });
    expect(request.settings).not.toHaveProperty("castLimit");
    expect(request.settings).not.toHaveProperty("minIntervalMs");
  });

  it("keeps an active environment value visible while its saved fallback is staged for removal", async () => {
    await renderPage();

    fireEvent.click(await screen.findByText("Advanced"));
    const pacing = screen.getByRole("group", { name: "Request interval" });
    const amount = within(pacing).getByRole("spinbutton", {
      name: "Request interval",
    }) as HTMLInputElement;
    expect(amount.value).toBe("100");

    fireEvent.click(within(pacing).getByRole("button", { name: "Remove saved fallback" }));

    expect(amount.value).toBe("100");
    expect(within(pacing).getByText("Saved fallback will be removed on save.")).toBeTruthy();
  });

  it("restores a field on undo and unstages default removal when it is edited", async () => {
    api.getTMDBIntegration.mockResolvedValue(
      tmdbConfig({
        settings: {
          ...tmdbConfig().settings,
          castLimit: setting(22, 15, "admin", "TMDB_ENRICH_CAST_LIMIT"),
        },
      }),
    );
    api.saveTMDBIntegration.mockResolvedValue(tmdbConfig({ revision: 8 }));
    await renderPage();

    const cast = await screen.findByRole("group", { name: "Cast limit" });
    const input = within(cast).getByRole("spinbutton", { name: "Cast limit" });
    fireEvent.click(within(cast).getByRole("button", { name: "Use default" }));
    expect((input as HTMLInputElement).value).toBe("15");
    fireEvent.click(within(cast).getByRole("button", { name: "Undo" }));
    expect((input as HTMLInputElement).value).toBe("22");

    fireEvent.click(within(cast).getByRole("button", { name: "Use default" }));
    fireEvent.change(input, { target: { value: "18" } });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(api.saveTMDBIntegration).toHaveBeenCalledTimes(1));
    expect(api.saveTMDBIntegration.mock.calls[0]?.[0]).toMatchObject({
      settings: expect.objectContaining({ castLimit: 18 }),
      removeFallbacks: [],
    });
  });

  it("keeps a dirty draft when another admin advances the revision", async () => {
    const fresh = tmdbConfig({
      revision: 8,
      settings: {
        ...tmdbConfig().settings,
        castLimit: setting(18, 15, "admin", "TMDB_ENRICH_CAST_LIMIT"),
      },
    });
    api.getTMDBIntegration
      .mockReset()
      .mockResolvedValueOnce(tmdbConfig())
      .mockResolvedValue(fresh);
    api.saveTMDBIntegration.mockRejectedValue(
      new IntegrationProblem(
        409,
        "stale_revision",
        "another admin changed these settings",
      ),
    );
    await renderPage();

    const castLimit = await screen.findByRole("spinbutton", { name: "Cast limit" });
    fireEvent.change(castLimit, { target: { value: "22" } });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    expect(
      await screen.findByText(
        "Another admin changed these settings. Your unsaved draft is still here.",
      ),
    ).toBeTruthy();
    await waitFor(() => expect(api.getTMDBIntegration).toHaveBeenCalledTimes(2));
    expect((castLimit as HTMLInputElement).value).toBe("22");
    expect(screen.getByText("Revision 7")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Load revision 8" })).toBeTruthy();
  });

  it("guards browser and router exits while the local draft is dirty", async () => {
    const { router } = await renderPage();

    fireEvent.change(await screen.findByRole("spinbutton", { name: "Cast limit" }), {
      target: { value: "22" },
    });
    const beforeUnload = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(beforeUnload);
    expect(beforeUnload.defaultPrevented).toBe(true);

    fireEvent.click(screen.getByRole("link", { name: "View run history" }));
    const dialog = await screen.findByRole("dialog", { name: "Discard unsaved changes?" });
    expect(router.state.location.pathname).toBe("/admin/integrations/tmdb");
    fireEvent.click(within(dialog).getByRole("button", { name: "Leave page" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/admin/runs"));
    expect(await screen.findByText("Run history destination")).toBeTruthy();
  });

  it("shows live timestamps, effective warnings, and current-run progress", async () => {
    api.getTMDBIntegration.mockResolvedValue(
      tmdbConfig({
        warnings: [{ field: "minInterval", message: "Request interval is below 250 ms." }],
        lastCheckedAt: "2026-08-04T16:30:00Z",
        lastConnectionTestedAt: "2026-08-04T16:45:00Z",
        nextCheckAt: "2026-08-04T17:30:00Z",
        lastSuccessfulRunAt: "2026-08-04T15:30:00Z",
        latestRun: {
          id: 77,
          integration: "tmdb",
          operation: "refresh_stale",
          trigger: "manual",
          configRevision: 7,
          status: "running",
          startedAt: "2026-08-04T16:35:00Z",
          progress: {
            total: 20,
            processed: 5,
            succeeded: 4,
            failed: 1,
            skipped: 0,
            remaining: 15,
          },
          failedSubjects: [],
        },
      }),
    );
    await renderPage();

    const status = await screen.findByRole("region", { name: "TMDB status" });
    expect(within(status).getByText("Aug 4, 2026 at 4:30 PM UTC")).toBeTruthy();
    expect(within(status).getByText("Aug 4, 2026 at 4:45 PM UTC")).toBeTruthy();
    expect(within(status).getByText("Aug 4, 2026 at 5:30 PM UTC")).toBeTruthy();
    expect(within(status).getAllByText("Aug 4, 2026 at 3:30 PM UTC")).toHaveLength(2);
    expect(within(status).getByText("5 of 20 processed")).toBeTruthy();
    expect(within(status).getByText("15 remaining")).toBeTruthy();
    expect(screen.getByText("Request interval is below 250 ms.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Cancel run" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "View run history" }).getAttribute("href")).toBe(
      "/admin/runs?integration=tmdb",
    );
    expect((screen.getByRole("button", { name: "Refresh stale now" }) as HTMLButtonElement).disabled)
      .toBe(true);
  });

  it("starts no-work refreshes, confirms full re-enrichment, and cancels active runs", async () => {
    api.startTMDBRun
      .mockResolvedValueOnce({ noWork: true })
      .mockResolvedValueOnce({
        id: 88,
        integration: "tmdb",
        operation: "re_enrich_all",
        trigger: "manual",
        configRevision: 7,
        status: "running",
        startedAt: "2026-08-04T16:45:00Z",
        progress: {
          total: 40,
          processed: 0,
          succeeded: 0,
          failed: 0,
          skipped: 0,
          remaining: 40,
        },
        failedSubjects: [],
      });
    api.cancelIntegrationRun.mockResolvedValue(undefined);
    const { queryClient } = await renderPage();
    const historyKey = IntegrationKeys.runs({ integration: "tmdb", limit: 50 });
    queryClient.setQueryData(historyKey, { runs: [] });

    fireEvent.click(await screen.findByRole("button", { name: "Refresh stale now" }));
    expect(await screen.findByText("No missing or stale movies were found.")).toBeTruthy();
    expect(api.startTMDBRun).toHaveBeenNthCalledWith(1, "refresh_stale", false);
    expect(queryClient.getQueryState(historyKey)?.isInvalidated).toBe(true);

    queryClient.setQueryData(historyKey, { runs: [] });

    fireEvent.click(screen.getByRole("button", { name: "Re-enrich all" }));
    const confirm = await screen.findByRole("dialog", { name: "Re-enrich every movie?" });
    expect(api.startTMDBRun).toHaveBeenCalledTimes(1);
    fireEvent.click(within(confirm).getByRole("button", { name: "Re-enrich all" }));
    await waitFor(() => expect(api.startTMDBRun).toHaveBeenCalledTimes(2));
    expect(api.startTMDBRun).toHaveBeenNthCalledWith(2, "re_enrich_all", true);

    const cancel = await screen.findByRole("button", { name: "Cancel run" });
    fireEvent.click(cancel);
    await waitFor(() => expect(api.cancelIntegrationRun).toHaveBeenCalledWith(88));
    expect(queryClient.getQueryState(historyKey)?.isInvalidated).toBe(true);
  });

  it("keeps confirmation dialogs open while their mutations are pending", async () => {
    const saved = tmdbConfig({ revision: 8 });
    const pendingSave = deferred<TMDBIntegration>();
    api.saveTMDBIntegration
      .mockRejectedValueOnce(
        new IntegrationProblem(
          409,
          "confirmation_required",
          "Unusually aggressive settings need confirmation.",
          [],
          [{ field: "ttl", message: "Metadata freshness is below 1 hour." }],
        ),
      )
      .mockReturnValueOnce(pendingSave.promise);
    await renderPage();

    fireEvent.change(await screen.findByRole("spinbutton", { name: "Metadata freshness" }), {
      target: { value: "0.5" },
    });
    fireEvent.change(screen.getByRole("combobox", { name: "Metadata freshness unit" }), {
      target: { value: "hours" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));
    const warning = await screen.findByRole("dialog", { name: "Confirm unusual settings" });
    fireEvent.click(within(warning).getByRole("button", { name: "Save anyway" }));
    const keepEditing = within(warning).getByRole("button", { name: "Keep editing" });
    await waitFor(() => expect((keepEditing as HTMLButtonElement).disabled).toBe(true));
    fireEvent.click(keepEditing);
    expect(screen.getByRole("dialog", { name: "Confirm unusual settings" })).toBeTruthy();
    pendingSave.resolve(saved);
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Confirm unusual settings" })).toBeNull());

    const pendingStart = deferred<unknown>();
    api.startTMDBRun.mockReturnValueOnce(pendingStart.promise);
    fireEvent.click(screen.getByRole("button", { name: "Re-enrich all" }));
    const confirm = await screen.findByRole("dialog", { name: "Re-enrich every movie?" });
    fireEvent.click(within(confirm).getByRole("button", { name: "Re-enrich all" }));
    const cancel = within(confirm).getByRole("button", { name: "Cancel" });
    await waitFor(() => expect((cancel as HTMLButtonElement).disabled).toBe(true));
    fireEvent.click(cancel);
    expect(screen.getByRole("dialog", { name: "Re-enrich every movie?" })).toBeTruthy();
    pendingStart.resolve({ noWork: true });
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Re-enrich every movie?" })).toBeNull());
  });

  it("pauses run polling while hidden, then performs a visible poll and final refetch", async () => {
    const running = tmdbConfig({
      latestRun: {
        id: 77,
        integration: "tmdb",
        operation: "refresh_stale",
        trigger: "manual",
        configRevision: 7,
        status: "running",
        startedAt: "2026-08-04T16:35:00Z",
        progress: {
          total: 2,
          processed: 1,
          succeeded: 1,
          failed: 0,
          skipped: 0,
          remaining: 1,
        },
        failedSubjects: [],
      },
    });
    const completed = tmdbConfig({
      latestRun: {
        ...running.latestRun!,
        status: "completed",
        finishedAt: "2026-08-04T16:36:00Z",
        progress: {
          total: 2,
          processed: 2,
          succeeded: 2,
          failed: 0,
          skipped: 0,
          remaining: 0,
        },
      },
    });
    api.getTMDBIntegration
      .mockReset()
      .mockResolvedValueOnce(running)
      .mockResolvedValueOnce(completed)
      .mockResolvedValue(completed);
    const originalVisibility = Object.getOwnPropertyDescriptor(document, "visibilityState");
    let visibility: DocumentVisibilityState = "hidden";
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => visibility,
    });

    try {
      const { queryClient } = await renderPage();
      const historyKey = IntegrationKeys.runHistory();
      queryClient.setQueryData(historyKey, []);
      expect(await screen.findByText("1 of 2 processed")).toBeTruthy();
      expect(api.getTMDBIntegration).toHaveBeenCalledTimes(1);
      const castLimit = screen.getByRole("spinbutton", { name: "Cast limit" });
      fireEvent.change(castLimit, { target: { value: "22" } });

      visibility = "visible";
      document.dispatchEvent(new Event("visibilitychange"));
      await waitFor(() => expect(api.getTMDBIntegration).toHaveBeenCalledTimes(3));
      expect(await screen.findByText("2 of 2 processed")).toBeTruthy();
      expect((castLimit as HTMLInputElement).value).toBe("22");
      expect(queryClient.getQueryState(historyKey)?.isInvalidated).toBe(true);
    } finally {
      if (originalVisibility) {
        Object.defineProperty(document, "visibilityState", originalVisibility);
      } else {
        Reflect.deleteProperty(document, "visibilityState");
      }
    }
  });

  it("discovers a scheduled run at its due time without idle polling while hidden", async () => {
	const nextCheckAt = new Date(Date.now() + 30).toISOString();
	const idle = tmdbConfig({ nextCheckAt });
	const running = tmdbConfig({
	  nextCheckAt,
	  latestRun: {
		id: 91,
		integration: "tmdb",
		operation: "refresh_stale",
		trigger: "scheduled",
		configRevision: 7,
		status: "running",
		startedAt: nextCheckAt,
		progress: {
		  total: 3,
		  processed: 0,
		  succeeded: 0,
		  failed: 0,
		  skipped: 0,
		  remaining: 3,
		},
		failedSubjects: [],
	  },
	});
	api.getTMDBIntegration.mockReset().mockResolvedValueOnce(idle).mockResolvedValue(running);
	const originalVisibility = Object.getOwnPropertyDescriptor(document, "visibilityState");
	let visibility: DocumentVisibilityState = "hidden";
	Object.defineProperty(document, "visibilityState", {
	  configurable: true,
	  get: () => visibility,
	});

	try {
	  const { queryClient } = await renderPage();
	  const historyKey = IntegrationKeys.runHistory();
	  queryClient.setQueryData(historyKey, []);
	  expect(await screen.findByRole("region", { name: "TMDB status" })).toBeTruthy();
	  await new Promise((resolve) => window.setTimeout(resolve, 70));
	  expect(api.getTMDBIntegration).toHaveBeenCalledTimes(1);

	  visibility = "visible";
	  document.dispatchEvent(new Event("visibilitychange"));
	  await waitFor(() => expect(api.getTMDBIntegration).toHaveBeenCalledTimes(2));
	  expect(await screen.findByRole("button", { name: "Cancel run" })).toBeTruthy();
	  expect(queryClient.getQueryState(historyKey)?.isInvalidated).toBe(true);
	} finally {
	  if (originalVisibility) {
		Object.defineProperty(document, "visibilityState", originalVisibility);
	  } else {
		Reflect.deleteProperty(document, "visibilityState");
	  }
	}
  });

  it("retries scheduled run discovery when the first due check precedes ledger creation", async () => {
    const nextCheckAt = new Date(Date.now() - 1_000).toISOString();
    const idle = tmdbConfig({ nextCheckAt });
    const running = tmdbConfig({
      nextCheckAt,
      latestRun: {
        id: 92,
        integration: "tmdb",
        operation: "refresh_stale",
        trigger: "scheduled",
        configRevision: 7,
        status: "running",
        startedAt: nextCheckAt,
        progress: {
          total: 1,
          processed: 0,
          succeeded: 0,
          failed: 0,
          skipped: 0,
          remaining: 1,
        },
        failedSubjects: [],
      },
    });
    api.getTMDBIntegration
      .mockReset()
      .mockResolvedValueOnce(idle)
      .mockResolvedValueOnce(idle)
      .mockResolvedValue(running);

    await renderPage();

    await waitFor(() => expect(api.getTMDBIntegration).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(api.getTMDBIntegration).toHaveBeenCalledTimes(3), { timeout: 3_000 });
    expect(await screen.findByRole("button", { name: "Cancel run" })).toBeTruthy();
  });
});
