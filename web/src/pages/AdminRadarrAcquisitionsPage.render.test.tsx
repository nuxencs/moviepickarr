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

import { IntegrationProblem } from "@/api/integrations";

import { AdminRadarrAcquisitionsPage } from "@/pages/AdminRadarrAcquisitionsPage";

const api = vi.hoisted(() => ({ listRadarrAcquisitions: vi.fn() }));

vi.mock("@/api/radarr", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/radarr")>()),
  listRadarrAcquisitions: api.listRadarrAcquisitions,
}));

window.scrollTo = (() => {}) as typeof window.scrollTo;

async function renderPage() {
  const rootRoute = createRootRoute();
  const radarrRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/admin/integrations/radarr",
    component: () => <Outlet />,
  });
  const indexRoute = createRoute({
    getParentRoute: () => radarrRoute,
    path: "/",
    component: AdminRadarrAcquisitionsPage,
  });
  const detailRoute = createRoute({
    getParentRoute: () => radarrRoute,
    path: "acquisitions/$acquisitionID",
    component: () => <p>Acquisition detail</p>,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([radarrRoute.addChildren([indexRoute, detailRoute])]),
    history: createMemoryHistory({ initialEntries: ["/admin/integrations/radarr"] }),
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  await router.load();
  return router;
}

beforeEach(() => {
  api.listRadarrAcquisitions.mockReset();
});

describe("Radarr acquisition register", () => {
  it("separates action, progress, and searchable history without a duplicate page heading", async () => {
    api.listRadarrAcquisitions.mockResolvedValue([
      {
        id: 19,
        title: "Arrival",
        year: 2016,
        status: "needs_release",
        actionReason: "release_required",
        target: { presetName: "Movies 1080p" },
      },
      {
        id: 21,
        title: "Dune",
        year: 2021,
        status: "downloading",
        target: { presetName: "Movies 4K" },
      },
      {
        id: 20,
        title: "Heat",
        year: 1995,
        status: "downloaded",
        target: { presetName: "Movies 4K" },
      },
    ]);
    const router = await renderPage();

    const active = await screen.findByRole("list", { name: "Active Radarr acquisitions" });
    expect(within(active).getByRole("link", { name: /Arrival/ })).toBeTruthy();
    const actionHeading = screen.getByRole("heading", { name: "Action required" });
    expect(actionHeading.closest(".radarr-page__toolbar")).toBeTruthy();
    expect(screen.getByLabelText("1 acquisition requires action")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "In progress" })).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Acquisitions" })).toBeNull();
    expect(within(screen.getByRole("list", { name: "Radarr acquisitions in progress" })).getByText("Dune")).toBeTruthy();

    const historyTrigger = screen.getByRole("button", { name: "History" });
    expect(within(historyTrigger).getByText("1")).toBeTruthy();
    expect(historyTrigger.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(historyTrigger);
    expect(historyTrigger.getAttribute("aria-expanded")).toBe("true");
    const history = screen.getByRole("list", { name: "Radarr acquisition history" });
    expect(within(history).getByText("Heat")).toBeTruthy();
    fireEvent.change(screen.getByRole("searchbox", { name: "Search acquisition history" }), {
      target: { value: "not here" },
    });
    await waitFor(() =>
      expect(screen.getByText("No acquisition history matches this search.")).toBeTruthy(),
    );

    fireEvent.click(within(active).getByRole("link", { name: /Arrival/ }));
    expect(await screen.findByText("Acquisition detail")).toBeTruthy();
    expect(router.state.location.pathname).toBe(
      "/admin/integrations/radarr/acquisitions/19",
    );
  });

  it("shows the protected-route error without acquisition details", async () => {
    api.listRadarrAcquisitions.mockRejectedValue(
      new IntegrationProblem(403, "forbidden", "administrator access required"),
    );

    await renderPage();

    expect((await screen.findByRole("alert")).textContent).toBe(
      "Admin access is required to view Radarr acquisitions.",
    );
  });
});
