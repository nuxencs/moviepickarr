import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  lazyRouteComponent,
  stripSearchParams,
} from "@tanstack/react-router";

import { redirectIfSignedIn, requireSession } from "@/api/authGuard";
import { MeQueryOptions } from "@/api/queries";
import { queryClient } from "@/api/QueryClient";

import { AppLayout, RootShell, Shell } from "@/components/moviepickarr/AppShell";
import { Hero } from "@/components/moviepickarr/Hero";
import { MoviesTab } from "@/components/moviepickarr/MoviesTab";
import { statsSearchDefaults, validateStatsSearch } from "@/components/moviepickarr/statsSearch";

import type { QueryClient } from "@tanstack/react-query";

interface RouterContext {
  queryClient: QueryClient;
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: RootShell,
});

// The auth gates below run on every route entry, so they must read a *fresh*
// session, not the 60s-stale /me the global staleTime hands back on tab nav (see
// QueryClient). staleTime: 0 forces a revalidation each time, so a session that
// died server-side redirects on the next navigation instead of trusting a cached
// success and painting the chrome behind a wall of 401s. /me is a cheap session
// lookup, not one of the heavy SQLite reads that staleTime exists to spare.
const resolveMe = (queryClient: QueryClient) =>
  queryClient.fetchQuery({ ...MeQueryOptions(), staleTime: 0 });

// Pathless layout route carrying the app chrome (NavBar + SSE). The
// authenticated app pages hang off it; the standalone auth routes below sit
// directly under the root so they render without that chrome.
const appLayoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "_app",
  // Auth gate for every app page: a dead/absent session bounces to /login before
  // the chrome paints behind a wall of 401s (see requireSession).
  beforeLoad: ({ context }) => requireSession(() => resolveMe(context.queryClient)),
  component: AppLayout,
});

// Every route below except the movies landing page loads its component on
// demand, so the entry bundle carries the shell plus the one route the visitor
// actually asked for. lazyRouteComponent (not React.lazy) is what makes that
// free at navigation time: the router owns the import, so defaultPreload:
// "intent" fetches the chunk on nav-link hover and it's warm by the time the
// click lands. Movies stays eager because it's the landing route, where a
// deferred chunk would only delay the first paint.
const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  // The OIDC callback redirects back here with a ?error= bucket on failure; keep
  // it as a plain optional string and let the page map it to banner copy.
  validateSearch: (search: Record<string, unknown>): { error?: string } => ({
    error: typeof search.error === "string" ? search.error : undefined,
  }),
  // A member with a live session never sees the login form: /me is resolved
  // before render so there is no one-frame flash of the form (see
  // redirectIfSignedIn).
  beforeLoad: ({ context }) => redirectIfSignedIn(() => resolveMe(context.queryClient)),
  component: lazyRouteComponent(
    () => import("@/components/moviepickarr/auth/LoginPage"),
    "LoginPage",
  ),
});

const claimRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/claim/$token",
  component: lazyRouteComponent(
    () => import("@/components/moviepickarr/auth/ClaimPage"),
    "ClaimPage",
  ),
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
  component: lazyRouteComponent(() => import("@/pages/UsersPage"), "UsersPage"),
});

// The admin roster surface. A non-admin who navigates here still gets the page
// (and its first-class "Admins only" state from the 403), never a 404, so the
// route is mounted for everyone and the gating lives in the page.
const adminRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/admin",
  component: lazyRouteComponent(() => import("@/pages/AdminPage"), "AdminPage"),
});

// The account settings surface. Path is /settings, not /account: the merged
// OIDC link flow redirects the browser back to /settings?linked=1 (or
// ?error=<bucket>) after connecting a provider, so the route has to match that
// contract. The page reads those params to toast the link outcome.
const settingsRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/settings",
  validateSearch: (search: Record<string, unknown>): { linked?: string; error?: string } => ({
    linked: typeof search.linked === "string" ? search.linked : undefined,
    error: typeof search.error === "string" ? search.error : undefined,
  }),
  component: lazyRouteComponent(() => import("@/pages/SettingsPage"), "SettingsPage"),
});

const statsRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/stats",
  // All Stats filter state lives in the URL search params (see statsSearch).
  validateSearch: validateStatsSearch,
  search: { middlewares: [stripSearchParams(statsSearchDefaults)] },
  component: lazyRouteComponent(() => import("@/pages/StatsPage"), "StatsPage"),
});

const routeTree = rootRoute.addChildren([
  appLayoutRoute.addChildren([moviesRoute, usersRoute, adminRoute, settingsRoute, statsRoute]),
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
