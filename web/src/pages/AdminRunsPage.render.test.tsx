import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { IntegrationProblem, type IntegrationRun } from "@/api/integrations";

import { AdminRunsPage } from "@/pages/AdminRunsPage";
import { validateAdminRunsSearch } from "@/pages/adminRunsSearch";

const api = vi.hoisted(() => ({
  listIntegrationRuns: vi.fn(),
  listIntegrations: vi.fn(),
}));

vi.mock("@/api/integrations", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/integrations")>()),
  listIntegrationRuns: api.listIntegrationRuns,
  listIntegrations: api.listIntegrations,
}));

window.scrollTo = (() => {}) as typeof window.scrollTo;

const completedRun = {
  id: 43,
  integration: "tmdb",
  operation: "refresh_stale" as const,
  trigger: "manual" as const,
  initiatedBy: 7,
  configRevision: 12,
  status: "completed_with_errors" as const,
  startedAt: "2026-08-04T13:00:00Z",
  finishedAt: "2026-08-04T13:02:00Z",
  progress: {
    total: 9,
    processed: 9,
    succeeded: 7,
    failed: 1,
    skipped: 1,
    remaining: 0,
  },
  errorSummary: "1 subject failed",
  failedSubjects: [{ subject: "movie:603", error: "TMDB enrichment failed" }],
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

async function renderPage(initialEntry = "/admin/runs?integration=tmdb") {
  const rootRoute = createRootRoute();
  const appRoute = createRoute({ getParentRoute: () => rootRoute, id: "_app" });
  const adminRoute = createRoute({ getParentRoute: () => appRoute, path: "/admin" });
  const runsRoute = createRoute({
    getParentRoute: () => adminRoute,
    path: "runs",
    validateSearch: validateAdminRunsSearch,
    component: AdminRunsPage,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([appRoute.addChildren([adminRoute.addChildren([runsRoute])])]),
    history: createMemoryHistory({ initialEntries: [initialEntry] }),
  });
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
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
  api.listIntegrationRuns.mockReset();
  api.listIntegrations.mockReset();
  api.listIntegrations.mockResolvedValue([
    {
      id: "tmdb",
      name: "TMDB",
      state: "connected",
      operations: [
        { id: "refresh_stale", name: "Refresh stale" },
        { id: "re_enrich_all", name: "Re-enrich all" },
        { id: "enrich_movie", name: "Enrich movie" },
      ],
    },
    {
      id: "fanart",
      name: "Fanart",
      state: "disabled",
      operations: [{ id: "sync_collections", name: "Sync collections" }],
    },
  ]);
});

describe("Admin run history", () => {
  it("keeps result rows lean and opens complete run details in a modal", async () => {
    api.listIntegrationRuns.mockResolvedValue({ runs: [completedRun] });

    await renderPage();

    const register = await screen.findByRole("list", { name: "Integration run history" });
    const row = within(register).getByRole("button", {
      name: /TMDB.*Refresh stale.*Completed with errors.*7 succeeded.*1 failed.*1 skipped.*View run details/,
    });
    expect(row.getAttribute("aria-haspopup")).toBe("dialog");
    expect(within(register).queryByText("#43")).toBeNull();
    expect(within(register).queryByText("Config 12")).toBeNull();
    expect(within(register).queryByText("Manual")).toBeNull();
    expect(within(register).queryByText("movie:603")).toBeNull();

    fireEvent.click(row);

    const dialog = await screen.findByRole("dialog", { name: "TMDB · Refresh stale" });
    expect(within(dialog).getByRole("heading", { name: "TMDB · Refresh stale" })).toBeTruthy();
    expect(within(dialog).getByText("Manual")).toBeTruthy();
    expect(within(dialog).getByText("2 min")).toBeTruthy();
    expect(within(dialog).getByText("1 subject failed")).toBeTruthy();
    expect(within(dialog).getByText("movie:603")).toBeTruthy();
    expect(within(dialog).getByText("TMDB enrichment failed")).toBeTruthy();
    expect(within(dialog).queryByText("Config 12")).toBeNull();
    expect(within(dialog).queryByText("#43")).toBeNull();
  });

  it("omits the failure section for a clean result", async () => {
    api.listIntegrationRuns.mockResolvedValue({
      runs: [{
        ...completedRun,
        status: "completed",
        errorSummary: undefined,
        failedSubjects: [],
        progress: { ...completedRun.progress, succeeded: 9, failed: 0, skipped: 0 },
      }],
    });

    await renderPage();
    fireEvent.click(await screen.findByRole("button", { name: /9 succeeded.*View run details/ }));

    const dialog = await screen.findByRole("dialog", { name: "TMDB · Refresh stale" });
    expect(within(dialog).queryByRole("heading", { name: "Failures" })).toBeNull();
  });

  it("caps defensive failure rendering at 25 subjects inside the modal", async () => {
    const failedSubjects = Array.from({ length: 26 }, (_, index) => ({
      subject: `movie:${index}`,
      error: "TMDB enrichment failed",
    }));
    api.listIntegrationRuns.mockResolvedValue({
      runs: [{ ...completedRun, errorSummary: "26 subjects failed", failedSubjects }],
    });

    await renderPage();
    fireEvent.click(await screen.findByRole("button", { name: /View run details/ }));

    const dialog = await screen.findByRole("dialog", { name: "TMDB · Refresh stale" });
    expect(within(dialog).getByText("movie:24")).toBeTruthy();
    expect(within(dialog).queryByText("movie:25")).toBeNull();
    expect(within(dialog).getByText("Showing the first 25 of 26 failures.")).toBeTruthy();
  });

  it("sources integration filters from the catalog and keeps lean filters in the URL", async () => {
    api.listIntegrationRuns.mockResolvedValue({ runs: [] });

    const { router } = await renderPage();

    const integration = await screen.findByRole("combobox", { name: "Integration" });
    expect((integration as HTMLSelectElement).value).toBe("tmdb");
    expect(within(integration).getByRole("option", { name: "Fanart" })).toBeTruthy();
    fireEvent.change(integration, { target: { value: "fanart" } });
    const type = screen.getByRole("combobox", { name: "Type" });
    await waitFor(() => {
      expect(within(type).getByRole("option", { name: "Sync collections" })).toBeTruthy();
    });
    fireEvent.change(type, {
      target: { value: "sync_collections" },
    });
    await waitFor(() => {
      expect(router.state.location.search.operation).toBe("sync_collections");
    });
    fireEvent.change(screen.getByRole("combobox", { name: "Result" }), {
      target: { value: "failed" },
    });

    expect(screen.queryByRole("combobox", { name: "Trigger" })).toBeNull();
    await waitFor(() => {
      expect(router.state.location.search).toMatchObject({
        integration: "fanart",
        operation: "sync_collections",
        status: "failed",
      });
      expect(router.state.location.search).not.toHaveProperty("trigger");
    });
  });

  it("keeps integration filters useful when the catalog request fails", async () => {
    api.listIntegrations.mockRejectedValue(new Error("catalog unavailable"));
    api.listIntegrationRuns.mockResolvedValue({
      runs: [{ ...completedRun, integration: "fanart", operation: "sync_collections" }],
    });

    await renderPage("/admin/runs");

    const integration = await screen.findByRole("combobox", { name: "Integration" });
    expect(within(integration).getByRole("option", { name: "FANART" })).toBeTruthy();
    expect(
      within(screen.getByRole("combobox", { name: "Type" })).getByRole("option", {
        name: "Sync Collections",
      }),
    ).toBeTruthy();
  });

  it("labels operations and triggers introduced by another integration", async () => {
    api.listIntegrationRuns.mockResolvedValue({
      runs: [{
        ...completedRun,
        integration: "fanart",
        operation: "sync_collections",
        trigger: "webhook",
      }],
    });

    await renderPage("/admin/runs?integration=fanart");

    const register = await screen.findByRole("list", { name: "Integration run history" });
    expect(within(register).getByText("Sync collections")).toBeTruthy();
    expect(
      within(screen.getByRole("combobox", { name: "Type" })).getByRole("option", {
        name: "Sync collections",
      }),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /Fanart.*Sync collections.*View run details/ }));
    const dialog = await screen.findByRole("dialog", { name: "Fanart · Sync collections" });
    expect(within(dialog).getByText("Webhook")).toBeTruthy();
  });

  it("keeps the current 50-row page visible while moving through keyset pages", async () => {
    const pageTwo = deferred<{ runs: IntegrationRun[] }>();
    api.listIntegrationRuns.mockImplementation(
      (query: { cursor?: string }) =>
        query.cursor
          ? pageTwo.promise
          : Promise.resolve({ runs: [completedRun], nextCursor: "2026-08-04T13:00:00Z,43" }),
    );

    const { router } = await renderPage();
    const register = await screen.findByRole("list", { name: "Integration run history" });
    expect(within(register).getByText("Refresh stale")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    await waitFor(() => {
      expect(router.state.location.search).toMatchObject({
        integration: "tmdb",
        cursor: "2026-08-04T13:00:00Z,43",
      });
    });
    expect(within(register).getByText("Refresh stale")).toBeTruthy();
    expect(api.listIntegrationRuns).toHaveBeenLastCalledWith(
      expect.objectContaining({ cursor: "2026-08-04T13:00:00Z,43", limit: 50 }),
      expect.any(AbortSignal),
    );

    pageTwo.resolve({
      runs: [{ ...completedRun, id: 42, operation: "re_enrich_all", failedSubjects: [] }],
    });
    expect(await within(register).findByText("Re-enrich all")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Previous page" }));
    await waitFor(() => expect(router.state.location.search.cursor).toBeUndefined());
    expect(await within(register).findByText("Refresh stale")).toBeTruthy();
  });

  it("restores pagination controls across browser Back and Forward", async () => {
    api.listIntegrationRuns.mockImplementation((query: { cursor?: string }) =>
      Promise.resolve(
        query.cursor
          ? { runs: [{ ...completedRun, id: 42, operation: "re_enrich_all" }] }
          : { runs: [completedRun], nextCursor: "2026-08-04T13:00:00Z,43" },
      ),
    );

    const { router } = await renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Next page" }));
    await waitFor(() => expect(router.state.location.search.cursor).toBeDefined());
    expect(screen.getByRole("button", { name: "Previous page" }).hasAttribute("disabled")).toBe(false);

    act(() => router.history.back());
    await waitFor(() => expect(router.state.location.search.cursor).toBeUndefined());
    expect(screen.getByRole("button", { name: "Previous page" }).hasAttribute("disabled")).toBe(true);

    act(() => router.history.forward());
    await waitFor(() => expect(router.state.location.search.cursor).toBeDefined());
    expect(screen.getByRole("button", { name: "Previous page" }).hasAttribute("disabled")).toBe(false);
  });

  it("offers a first-page recovery for a direct cursor URL", async () => {
    api.listIntegrationRuns.mockResolvedValue({ runs: [] });
    const { router } = await renderPage(
      "/admin/runs?cursor=2026-08-04T13%3A00%3A00Z%2C43",
    );

    const firstPage = await screen.findByRole("button", { name: "First page" });
    expect(firstPage.hasAttribute("disabled")).toBe(false);
    fireEvent.click(firstPage);

    await waitFor(() => expect(router.state.location.search.cursor).toBeUndefined());
  });

  it("does not show results from the previous filter while a filter loads", async () => {
    const filtered = deferred<{ runs: IntegrationRun[] }>();
    api.listIntegrationRuns.mockImplementation((query: { status?: string }) =>
      query.status ? filtered.promise : Promise.resolve({ runs: [completedRun] }),
    );

    await renderPage("/admin/runs");
    expect(await screen.findByText("Refresh stale")).toBeTruthy();

    fireEvent.change(screen.getByRole("combobox", { name: "Result" }), {
      target: { value: "failed" },
    });

    expect(await screen.findByText("Loading run history…")).toBeTruthy();
    expect(screen.queryByRole("list", { name: "Integration run history" })).toBeNull();
    filtered.resolve({ runs: [] });
    expect(await screen.findByText("No results match the selected filters.")).toBeTruthy();
  });

  it("explains when the selected filters have no matching results", async () => {
    api.listIntegrationRuns.mockResolvedValue({ runs: [] });

    await renderPage();

    expect(await screen.findByText("No results match the selected filters.")).toBeTruthy();
  });

  it("explains when no integration run results have been recorded", async () => {
    api.listIntegrationRuns.mockResolvedValue({ runs: [] });

    await renderPage("/admin/runs");

    expect(await screen.findByText("No integration run results have been recorded.")).toBeTruthy();
  });

  it("defensively omits active rows from result history", async () => {
    api.listIntegrationRuns.mockResolvedValue({
      runs: [{ ...completedRun, status: "running", finishedAt: undefined }],
    });

    await renderPage("/admin/runs");

    expect(await screen.findByText("No integration run results have been recorded.")).toBeTruthy();
    expect(screen.queryByText("Running")).toBeNull();
  });

  it("announces the initial loading state", async () => {
    api.listIntegrationRuns.mockReturnValue(new Promise(() => {}));

    await renderPage();

    expect((await screen.findByRole("status")).textContent).toBe("Loading run history…");
  });

  it("gives forbidden history its own recovery message", async () => {
    api.listIntegrationRuns.mockRejectedValue(
      new IntegrationProblem(403, "admin_required", "admin access required"),
    );

    await renderPage();

    expect((await screen.findByRole("alert")).textContent).toBe(
      "Admin access is required to view run history.",
    );
  });

  it("reports an ordinary history request failure without exposing internals", async () => {
    api.listIntegrationRuns.mockRejectedValue(new Error("sqlite secret detail"));

    await renderPage();

    expect((await screen.findByRole("alert")).textContent).toBe(
      "Failed to load run history.",
    );
  });
});
