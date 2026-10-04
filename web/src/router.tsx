import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  lazyRouteComponent,
  redirect,
  stripSearchParams,
} from "@tanstack/react-router";

import { redirectIfSignedIn, requireAppSession } from "@/api/authGuard";
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

// Login always checks the server; app navigation revalidates in the background.
const resolveMe = (queryClient: QueryClient) =>
  queryClient.fetchQuery({ ...MeQueryOptions(), staleTime: 0 });

// Pathless layout with the app chrome (NavBar + SSE). Auth routes sit under the
// root so they render without it.
const appLayoutRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "_app",
  beforeLoad: ({ context }) =>
    requireAppSession(context.queryClient, () => {
      void router.navigate({ to: "/login", replace: true });
    }),
  component: AppLayout,
});

// Routes load lazily except Movies, the landing route. lazyRouteComponent (not
// React.lazy) lets defaultPreload: "intent" fetch the chunk on nav-link hover.
const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  // OIDC failures land here with ?error=<bucket>; the page maps it to copy.
  validateSearch: (search: Record<string, unknown>): { error?: string } => ({
    error: typeof search.error === "string" ? search.error : undefined,
  }),
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
  // No stripSearchParams, unlike /stats: an unresolved id must stay in the URL.
  validateSearch: validateMembersSearch,
  component: lazyRouteComponent(() => import("@/pages/UsersPage"), "UsersPage"),
});

// Non-admins reach the route and get the API's 403 state, not a masked 404.
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

// /admin/members is intentionally absent: old bookmarks fall through to not-found.

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

// Path must stay /settings: the OIDC link flow redirects to /settings?linked=1
// (or ?error=<bucket>).
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

// Before first render, so a refresh with the modal open lands clean (#196).
clearMovieModalHistory(router);

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
