import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  lazyRouteComponent,
  redirect,
  stripSearchParams,
} from "@tanstack/react-router";

import { redirectIfSignedIn, requireSession } from "@/api/authGuard";
import { clearPrincipalCache } from "@/api/principalCache";
import { MeQueryOptions } from "@/api/queries";
import { queryClient } from "@/api/QueryClient";

import { AppLayout, RootShell, Shell } from "@/components/moviepickarr/AppShell";
import { Hero } from "@/components/moviepickarr/Hero";
import { validateMembersSearch } from "@/components/moviepickarr/membersSearch";
import { MoviesTab } from "@/components/moviepickarr/MoviesTab";
import { statsSearchDefaults, validateStatsSearch } from "@/components/moviepickarr/statsSearch";

import type { QueryClient } from "@tanstack/react-query";

import { clearMovieModalHistory } from "@/hooks/useMovieModalHistory";
import { validateAdminRunsSearch } from "@/pages/adminRunsSearch";

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
  beforeLoad: ({ context }) =>
    requireSession(
      () => resolveMe(context.queryClient),
      () => clearPrincipalCache(context.queryClient),
    ),
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
  beforeLoad: ({ context }) =>
    redirectIfSignedIn(
      () => resolveMe(context.queryClient),
      () => clearPrincipalCache(context.queryClient),
    ),
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
  // Which member's board is open lives in the URL (see membersSearch). No
  // stripSearchParams here, unlike /stats: the rail's rows always carry an
  // explicit id, and an id that doesn't resolve has to stay in the URL rather
  // than be canonicalised out from under whoever pasted it.
  validateSearch: validateMembersSearch,
  component: lazyRouteComponent(() => import("@/pages/UsersPage"), "UsersPage"),
});

// Admin is one top-level tab with its own internal route seam. The shared shell
// stays mounted while each destination remains an independent lazy chunk.
// Non-admins still reach the route and get the API's first-class 403 state once
// a destination reads protected data, rather than a masked 404.
const adminRoute = createRoute({
  getParentRoute: () => appLayoutRoute,
  path: "/admin",
  component: lazyRouteComponent(
    () => import("@/components/moviepickarr/admin/AdminLayout"),
    "AdminLayout",
  ),
});

const adminIndexRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/admin/roster", replace: true });
  },
});

const adminRosterRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "roster",
  component: lazyRouteComponent(() => import("@/pages/AdminPage"), "AdminPage"),
});

// /admin/members is intentionally absent. Old bookmarks fall through to the
// normal not-found path instead of preserving a second name for this surface.

const adminIntegrationsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "integrations",
});

const adminIntegrationsIndexRoute = createRoute({
  getParentRoute: () => adminIntegrationsRoute,
  path: "/",
  component: lazyRouteComponent(
    () => import("@/pages/AdminIntegrationsPage"),
    "AdminIntegrationsPage",
  ),
});

const adminTMDBRoute = createRoute({
  getParentRoute: () => adminIntegrationsRoute,
  path: "tmdb",
  component: lazyRouteComponent(() => import("@/pages/AdminTMDBPage"), "AdminTMDBPage"),
});

const adminRadarrRoute = createRoute({
  getParentRoute: () => adminIntegrationsRoute,
  path: "radarr",
  component: lazyRouteComponent(
    () => import("@/pages/AdminRadarrLayout"),
    "AdminRadarrLayout",
  ),
});

const adminRadarrIndexRoute = createRoute({
  getParentRoute: () => adminRadarrRoute,
  path: "/",
  component: lazyRouteComponent(
    () => import("@/pages/AdminRadarrAcquisitionsPage"),
    "AdminRadarrAcquisitionsPage",
  ),
});

const adminRadarrAcquisitionRoute = createRoute({
  getParentRoute: () => adminRadarrRoute,
  path: "acquisitions/$acquisitionID",
  component: lazyRouteComponent(
    () => import("@/pages/AdminRadarrAcquisitionPage"),
    "AdminRadarrAcquisitionPage",
  ),
});

const adminRadarrSetupRoute = createRoute({
  getParentRoute: () => adminRadarrRoute,
  path: "setup",
  component: lazyRouteComponent(
    () => import("@/pages/AdminRadarrSetupPage"),
    "AdminRadarrSetupPage",
  ),
});

const adminRadarrWebhooksRoute = createRoute({
  getParentRoute: () => adminRadarrRoute,
  path: "webhooks",
  component: lazyRouteComponent(
    () => import("@/pages/AdminRadarrWebhooksPage"),
    "AdminRadarrWebhooksPage",
  ),
});

const adminRunsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "runs",
  validateSearch: validateAdminRunsSearch,
  component: lazyRouteComponent(() => import("@/pages/AdminRunsPage"), "AdminRunsPage"),
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
  appLayoutRoute.addChildren([
    moviesRoute,
    usersRoute,
    adminRoute.addChildren([
      adminIndexRoute,
      adminRosterRoute,
      adminIntegrationsRoute.addChildren([
        adminIntegrationsIndexRoute,
        adminTMDBRoute,
        adminRadarrRoute.addChildren([
          adminRadarrIndexRoute,
          adminRadarrAcquisitionRoute,
          adminRadarrSetupRoute,
          adminRadarrWebhooksRoute,
        ]),
      ]),
      adminRunsRoute,
    ]),
    settingsRoute,
    statsRoute,
  ]),
  loginRoute,
  claimRoute,
]);

export const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPreload: "intent",
});

// A refresh with the movie modal open lands on a clean page, so the entry's
// location state is stripped here, before the first render (see #196).
clearMovieModalHistory(router);

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
