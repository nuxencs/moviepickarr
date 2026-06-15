import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
} from "@tanstack/react-router";

import { queryClient } from "@/api/QueryClient";

import { Hero } from "@/components/movie-gang/Hero";
import { MoviesTab } from "@/components/movie-gang/MoviesTab";
import { NavBar } from "@/components/movie-gang/NavBar";
import { StatsTab } from "@/components/movie-gang/StatsTab";
import { UsersTab } from "@/components/movie-gang/UsersTab";
import { Toaster } from "@/components/ui/toast";

import type { QueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { useSSE } from "@/hooks/useSSE";

interface RouterContext {
  queryClient: QueryClient;
}

/**
 * App shell. The root route mounts once and persists across tab navigations,
 * so the SSE stream (useSSE) opens a single EventSource for the session rather
 * than tearing it down and reconnecting on every route change.
 */
function RootLayout() {
  useSSE();
  return (
    <div className="app">
      <NavBar />
      <Outlet />
      <Toaster />
    </div>
  );
}

/**
 * Mirrors the former `<main className="shell">` wrapper. Each route renders its
 * own, so navigating between tabs unmounts/remounts the content — preserving the
 * old `key={tab}` behavior (fresh state and scroll on every tab entry).
 */
function Shell({ children }: { children: ReactNode }) {
  return <main className="shell">{children}</main>;
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
