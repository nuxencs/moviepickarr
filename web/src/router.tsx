import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  stripSearchParams,
} from "@tanstack/react-router";

import { queryClient } from "@/api/QueryClient";

import { RosterPage } from "@/components/moviepickarr/admin/RosterPage";
import { AppLayout, RootShell, Shell } from "@/components/moviepickarr/AppShell";
import { ClaimPage } from "@/components/moviepickarr/auth/ClaimPage";
import { LoginPage } from "@/components/moviepickarr/auth/LoginPage";
import { Hero } from "@/components/moviepickarr/Hero";
import { MoviesTab } from "@/components/moviepickarr/MoviesTab";
import { statsSearchDefaults, validateStatsSearch } from "@/components/moviepickarr/statsSearch";
import { StatsTab } from "@/components/moviepickarr/StatsTab";
import { UsersTab } from "@/components/moviepickarr/UsersTab";

import type { QueryClient } from "@tanstack/react-query";

interface RouterContext {
  queryClient: QueryClient;
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: RootShell,
});

// Pathless layout route carrying the app chrome (NavBar + SSE). The
// authenticated app pages hang off it; the standalone auth routes below sit
// directly under the root so they render without that chrome.
const appLayoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "_app",
  component: AppLayout,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  // The OIDC callback redirects back here with a ?error= bucket on failure; keep
  // it as a plain optional string and let the page map it to banner copy.
  validateSearch: (search: Record<string, unknown>): { error?: string } => ({
    error: typeof search.error === "string" ? search.error : undefined,
  }),
  component: LoginPage,
});

const claimRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/claim/$token",
  component: ClaimPage,
});

const moviesRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/",
  component: function MoviesPage() {
    return (
      <>
        <Hero />
        <Shell>
          <MoviesTab />
        </Shell>
      </>
    );
  },
});

const usersRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/users",
  component: function UsersPage() {
    return (
      <Shell>
        <UsersTab />
      </Shell>
    );
  },
});

// The admin roster surface. A non-admin who navigates here still gets the page
// (and its first-class "Admins only" state from the 403), never a 404, so the
// route is mounted for everyone and the gating lives in the page.
const adminRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/admin",
  component: function AdminPage() {
    return (
      <Shell>
        <RosterPage />
      </Shell>
    );
  },
});

const statsRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/stats",
  // All Stats filter state lives in the URL search params (see statsSearch).
  validateSearch: validateStatsSearch,
  search: { middlewares: [stripSearchParams(statsSearchDefaults)] },
  component: function StatsPage() {
    return (
      <Shell>
        <StatsTab />
      </Shell>
    );
  },
});

const routeTree = rootRoute.addChildren([
  appLayoutRoute.addChildren([moviesRoute, usersRoute, adminRoute, statsRoute]),
  loginRoute,
  claimRoute,
]);

export const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPreload: "intent",
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
