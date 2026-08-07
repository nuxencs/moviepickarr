import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  lazyRouteComponent,
  Outlet,
  redirect,
  RouterProvider,
} from "@tanstack/react-router";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AdminLayout } from "@/components/moviepickarr/admin/AdminLayout";

import type { ComponentType } from "react";

const scrollTo = vi.fn();
window.scrollTo = scrollTo as typeof window.scrollTo;
const scrollIntoView = vi.fn();
Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
  configurable: true,
  value: scrollIntoView,
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

async function renderAdmin(
  path: string,
  options: { runsModule?: Promise<{ AdminRunsPage: ComponentType }> } = {},
) {
  const rootRoute = createRootRoute({ notFoundComponent: () => <p>Not found</p> });
  const adminRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/admin",
    component: AdminLayout,
    notFoundComponent: () => <p>Not found</p>,
  });
  const adminIndexRoute = createRoute({
    getParentRoute: () => adminRoute,
    path: "/",
    beforeLoad: () => {
      throw redirect({ to: "/admin/roster", replace: true });
    },
  });
  const rosterRoute = createRoute({
    getParentRoute: () => adminRoute,
    path: "roster",
    component: () => <p>Member roster</p>,
  });
  const integrationsRoute = createRoute({
    getParentRoute: () => adminRoute,
    path: "integrations",
    component: () => <Outlet />,
  });
  const integrationsIndexRoute = createRoute({
    getParentRoute: () => integrationsRoute,
    path: "/",
    component: () => <p>Integration catalog</p>,
  });
  const tmdbRoute = createRoute({
    getParentRoute: () => integrationsRoute,
    path: "tmdb",
    component: () => <p>TMDB settings</p>,
  });
  const runsRoute = createRoute({
    getParentRoute: () => adminRoute,
    path: "runs",
    component: options.runsModule
      ? lazyRouteComponent(() => options.runsModule!, "AdminRunsPage")
      : () => <p>Run history</p>,
  });

  const router = createRouter({
    routeTree: rootRoute.addChildren([
      adminRoute.addChildren([
        adminIndexRoute,
        rosterRoute,
        integrationsRoute.addChildren([integrationsIndexRoute, tmdbRoute]),
        runsRoute,
      ]),
    ]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });

  render(<RouterProvider router={router} />);
  await router.load();
  return router;
}

describe("Admin navigation", () => {
  it("redirects the Admin index to the canonical roster route", async () => {
    const router = await renderAdmin("/admin");

    expect(router.state.location.pathname).toBe("/admin/roster");
    expect(await screen.findByRole("link", { name: "Roster", current: "page" })).toBeTruthy();
  });

  it("does not redirect the removed Admin members route", async () => {
    const router = await renderAdmin("/admin/members");

    expect(router.state.location.pathname).toBe("/admin/members");
    expect(await screen.findByText("Not found")).toBeTruthy();
    expect(screen.queryByText("Member roster")).toBeNull();
  });

  it("leaves the child page as the only Admin heading", async () => {
    await renderAdmin("/admin/roster");

    expect(screen.queryByRole("heading", { level: 1, name: "Admin" })).toBeNull();
    expect(
      screen.queryByText("Manage members and the services connected to moviepickarr."),
    ).toBeNull();
    expect(
      within(await screen.findByRole("region", { name: "Member roster" })).getByText(
        "Member roster",
      ),
    ).toBeTruthy();
  });

  it("keeps every Admin section available around the member roster", async () => {
    await renderAdmin("/admin/roster");

    const navigation = await screen.findByRole("navigation", { name: "Admin sections" });
    const links = within(navigation).getAllByRole("link");
    const integrations = within(navigation).getByRole("button", {
      name: "Integrations",
      expanded: false,
    });

    expect(links.map((link) => link.textContent)).toEqual(["Roster", "Runs"]);
    expect(integrations).toBeTruthy();
    expect(within(navigation).getByRole("link", { name: "Roster", current: "page" }))
      .toBeTruthy();
    expect(
      within(screen.getByRole("region", { name: "Member roster" })).getByText("Member roster"),
    ).toBeTruthy();
  });

  it("expands Integrations when its child route is selected", async () => {
    const router = await renderAdmin("/admin/roster");
    const navigation = await screen.findByRole("navigation", { name: "Admin sections" });
    expect(
      within(navigation).getByRole("button", { name: "Integrations", expanded: false }),
    ).toBeTruthy();

    await act(() => router.navigate({ to: "/admin/integrations/tmdb" }));

    expect(
      within(navigation).getByRole("button", { name: "Integrations", expanded: true }),
    ).toBeTruthy();
    expect(within(navigation).getByRole("link", { name: "TMDB", current: "page" })).toBeTruthy();
  });

  it("opens the integrations index without an eager route preload", async () => {
    const router = await renderAdmin("/admin/roster");
    const preload = vi.spyOn(router, "preloadRoute");
    const integrations = screen.getByRole("button", { name: "Integrations" });

    fireEvent.focus(integrations);
    fireEvent.pointerEnter(integrations);
    expect(preload).not.toHaveBeenCalled();

    fireEvent.click(integrations);

    expect(await screen.findByText("Integration catalog")).toBeTruthy();
    expect(router.state.location.pathname).toBe("/admin/integrations");
    expect(preload).not.toHaveBeenCalled();
  });

  it("hides integration destinations until their branch opens", async () => {
    const router = await renderAdmin("/admin/roster");
    const navigation = await screen.findByRole("navigation", { name: "Admin sections" });

    expect(within(navigation).queryByRole("link", { name: "TMDB" })).toBeNull();

    await act(() => router.navigate({ to: "/admin/integrations/tmdb" }));

    expect(within(navigation).getByRole("link", { name: "TMDB", current: "page" })).toBeTruthy();
  });

  it("labels the content region for every Admin destination", async () => {
    const router = await renderAdmin("/admin/roster");
    const roster = await screen.findByRole("region", { name: "Member roster" });

    expect(within(roster).getByText("Member roster")).toBeTruthy();
    roster.focus();
    expect(document.activeElement).toBe(roster);

    await act(() => router.navigate({ to: "/admin/runs" }));

    const runs = await screen.findByRole("region", { name: "Integration run history" });
    expect(within(runs).getByText("Run history")).toBeTruthy();
    runs.focus();
    expect(document.activeElement).toBe(runs);
  });

  it("starts each Admin destination at the top of the shared scroller", async () => {
    const router = await renderAdmin("/admin/roster");
    const body = await screen.findByRole("region", { name: "Member roster" });
    body.scrollTop = 240;
    scrollTo.mockClear();

    await act(() => router.navigate({ to: "/admin/runs" }));

    expect(body.scrollTop).toBe(0);
    expect(scrollTo).toHaveBeenCalledWith(expect.objectContaining({ left: 0, top: 0 }));
  });

  it("keeps the outgoing page still while a lazy destination resolves", async () => {
    const runsModule = deferred<{ AdminRunsPage: ComponentType }>();
    await renderAdmin("/admin/roster", { runsModule: runsModule.promise });
    const body = await screen.findByRole("region", { name: "Member roster" });
    body.scrollTop = 240;

    fireEvent.click(screen.getByRole("link", { name: "Runs" }));

    await waitFor(() =>
      expect(screen.getByRole("link", { name: "Runs", current: "page" })).toBeTruthy(),
    );
    expect(within(body).getByText("Member roster")).toBeTruthy();
    expect(body.scrollTop).toBe(240);

    await act(async () => {
      runsModule.resolve({ AdminRunsPage: () => <p>Run history</p> });
    });

    expect(await within(body).findByText("Run history")).toBeTruthy();
    expect(body.scrollTop).toBe(0);
  });

  it("announces the selected destination from the persistent layout", async () => {
    const router = await renderAdmin("/admin/roster");
    const announcer = await screen.findByRole("status");

    expect(announcer.textContent).toBe("Member roster");

    await act(() => router.navigate({ to: "/admin/runs" }));

    expect(screen.getByRole("status").textContent).toBe("Integration run history");
  });

  it("nests the selected integration inside one persistent Admin index", async () => {
    await renderAdmin("/admin/integrations/tmdb");

    const navigation = await screen.findByRole("navigation", { name: "Admin sections" });
    const integrations = within(navigation).getByRole("group", { name: "Integrations" });
    const tmdb = within(integrations).getByRole("link", { name: "TMDB" });

    expect(
      within(integrations).getByRole("button", { name: "Integrations", expanded: true }),
    ).toBeTruthy();
    expect(within(navigation).getAllByRole("link", { current: "page" })).toEqual([tmdb]);
    expect(
      within(screen.getByRole("region", { name: "Selected integration configuration" }))
        .getByText("TMDB settings"),
    ).toBeTruthy();
  });

  it("keeps Integrations active on the index route", async () => {
    await renderAdmin("/admin/integrations");

    expect(
      within(await screen.findByRole("region", { name: "Integration catalog" })).getByText(
        "Integration catalog",
      ),
    ).toBeTruthy();
    expect(
      within(screen.getByRole("group", { name: "Integrations" })).getByRole("button", {
        name: "Integrations",
        expanded: true,
        current: "page",
      }),
    ).toBeTruthy();
  });

  it("keeps Integrations active on the TMDB detail page", async () => {
    await renderAdmin("/admin/integrations/tmdb");

    expect(await screen.findByText("TMDB settings")).toBeTruthy();
    expect(screen.getByRole("link", { name: "TMDB", current: "page" })).toBeTruthy();
  });

  it("gives run history its own active Admin destination", async () => {
    scrollIntoView.mockClear();
    await renderAdmin("/admin/runs");

    expect(await screen.findByText("Run history")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Runs", current: "page" })).toBeTruthy();
    expect(scrollIntoView).toHaveBeenCalledWith({ block: "nearest", inline: "nearest" });
    expect(screen.queryByRole("region", { name: "Selected integration configuration" })).toBeNull();
  });
});
