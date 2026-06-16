import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  stripSearchParams,
} from "@tanstack/react-router";

import { queryClient } from "@/api/QueryClient";

import { RootLayout, Shell } from "@/components/movie-gang/AppShell";
import { Hero } from "@/components/movie-gang/Hero";
import { MoviesTab } from "@/components/movie-gang/MoviesTab";
import { statsSearchDefaults, validateStatsSearch } from "@/components/movie-gang/statsSearch";
import { StatsTab } from "@/components/movie-gang/StatsTab";
import { UsersTab } from "@/components/movie-gang/UsersTab";

import type { QueryClient } from "@tanstack/react-query";

interface RouterContext {
  queryClient: QueryClient;
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: RootLayout,
});

const moviesRoute = createRoute({
  getParentRoute: () => rootRoute,
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
  getParentRoute: () => rootRoute,
  path: "/users",
  component: function UsersPage() {
    return (
      <Shell>
        <UsersTab />
      </Shell>
    );
  },
});

const statsRoute = createRoute({
  getParentRoute: () => rootRoute,
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

const routeTree = rootRoute.addChildren([moviesRoute, usersRoute, statsRoute]);

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
