import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { IntegrationProblem } from "@/api/integrations";

import { AdminIntegrationsPage } from "@/pages/AdminIntegrationsPage";

const api = vi.hoisted(() => ({ listIntegrations: vi.fn() }));

vi.mock("@/api/integrations", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/integrations")>()),
  listIntegrations: api.listIntegrations,
}));

window.scrollTo = (() => {}) as typeof window.scrollTo;

async function renderPage() {
  const rootRoute = createRootRoute();
  const adminRoute = createRoute({ getParentRoute: () => rootRoute, path: "/admin" });
  const integrationsRoute = createRoute({ getParentRoute: () => adminRoute, path: "integrations" });
  const indexRoute = createRoute({
    getParentRoute: () => integrationsRoute,
    path: "/",
    component: AdminIntegrationsPage,
  });
  const tmdbRoute = createRoute({
    getParentRoute: () => integrationsRoute,
    path: "tmdb",
    component: () => <p>TMDB settings</p>,
  });
  const router = createRouter({
    routeTree: rootRoute.addChildren([
      adminRoute.addChildren([integrationsRoute.addChildren([indexRoute, tmdbRoute])]),
    ]),
    history: createMemoryHistory({ initialEntries: ["/admin/integrations"] }),
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
  api.listIntegrations.mockReset();
});

describe("Admin integrations index", () => {
  it("lists integration state and latest activity, then opens its configuration", async () => {
    api.listIntegrations.mockResolvedValue([
      {
        id: "tmdb",
        name: "TMDB",
        state: "could_not_verify",
        reason: "TMDB connection has not been verified.",
        latestActivity: "2026-08-05T18:30:00Z",
      },
    ]);
    const router = await renderPage();

    const list = await screen.findByRole("list", { name: "Integrations" });
    const row = within(list).getByRole("link", { name: /TMDB/ });
    expect(within(row).getByText("Could not verify")).toBeTruthy();
    expect(within(row).getByText("TMDB connection has not been verified.")).toBeTruthy();
    expect(within(row).getByText("Latest activity")).toBeTruthy();
    expect(within(row).getByText(/Aug 5, 2026/)).toBeTruthy();

    fireEvent.click(row);

    expect(await screen.findByText("TMDB settings")).toBeTruthy();
    expect(router.state.location.pathname).toBe("/admin/integrations/tmdb");
  });

  it("shows the protected-route error", async () => {
    api.listIntegrations.mockRejectedValue(
      new IntegrationProblem(403, "forbidden", "administrator access required"),
    );

    await renderPage();

    expect((await screen.findByRole("alert")).textContent).toBe(
      "Admin access is required to view integrations.",
    );
  });
});
