import { QueryClient, useQueryClient } from "@tanstack/react-query";
import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  redirect,
  useLocation,
  useNavigate,
} from "@tanstack/react-router";
import { ChartNoAxesColumnIcon, FilmIcon, UsersIcon } from "lucide-react";

import { AuthMeQueryOptions } from "@/api/queries";
import { queryClient } from "@/api/QueryClient";
import { AuthKeys } from "@/api/query_keys";

import { AuthGate } from "@/components/AuthGate";
import { Header } from "@/components/Header";
import { MoviePicker } from "@/components/MoviePicker";
import { NextPicker } from "@/components/NextPicker";
import { StatsTab } from "@/components/StatsTab";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { UsersGrid } from "@/components/UserManagement";

import { useSSE } from "@/hooks/useSSE";
import type { AuthUser } from "@/types/Response";

interface RouterContext {
  queryClient: QueryClient;
}

async function resolveAuthUser(client: QueryClient): Promise<AuthUser | null> {
  try {
    return await client.fetchQuery(AuthMeQueryOptions());
  } catch {
    client.removeQueries({ queryKey: AuthKeys.me(), exact: true });
    return null;
  }
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: () => <Outlet />,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  beforeLoad: async ({ context }) => {
    const authUser = await resolveAuthUser(context.queryClient);
    if (authUser) {
      throw redirect({ to: "/movies" });
    }
  },
  component: LoginRoutePage,
});

const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  beforeLoad: async ({ context }) => {
    const authUser = await resolveAuthUser(context.queryClient);
    if (!authUser) {
      throw redirect({ to: "/login" });
    }

    return { authUser };
  },
  component: AppLayout,
});

const appIndexRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/",
  beforeLoad: () => {
    throw redirect({ to: "/movies" });
  },
});

const moviesRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/movies",
  component: MoviesPage,
});

const usersRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/users",
  component: UsersPage,
});

const statsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/stats",
  component: StatsPage,
});

function AppLayout() {
  const navigate = useNavigate();
  const location = useLocation();
  const { authUser } = appRoute.useRouteContext();

  useSSE();

  const activeTab = location.pathname.startsWith("/users")
    ? "users"
    : location.pathname.startsWith("/stats")
      ? "stats"
      : "movies";

  return (
    <div className="App">
      <Header authUser={authUser} />
      <div className="p-4">
        <NextPicker authUser={authUser} />
      </div>
      <div className="px-4">
        <Tabs
          value={activeTab}
          onValueChange={(nextTab) => {
            const destination = nextTab === "movies" ? "/movies" : `/${nextTab}`;
            void navigate({ to: destination });
          }}
          className="w-full"
        >
          <TabsList className="grid w-full grid-cols-3">
            <TabsTrigger value="movies" className="flex items-center gap-2">
              <FilmIcon className="size-4" />
              Movies
            </TabsTrigger>
            <TabsTrigger value="users" className="flex items-center gap-2">
              <UsersIcon className="size-4" />
              Users
            </TabsTrigger>
            <TabsTrigger value="stats" className="flex items-center gap-2">
              <ChartNoAxesColumnIcon className="size-4" />
              Stats
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </div>
      <div className="mt-4">
        <Outlet />
      </div>
    </div>
  );
}

function LoginRoutePage() {
  const navigate = useNavigate();
  const client = useQueryClient();

  return (
    <AuthGate
      onAuthenticated={(authUser) => {
        client.setQueryData(AuthKeys.me(), authUser);
        void navigate({ to: "/movies" });
      }}
    />
  );
}

function MoviesPage() {
  const { authUser } = appRoute.useRouteContext();
  return <MoviePicker authUser={authUser} />;
}

function UsersPage() {
  const { authUser } = appRoute.useRouteContext();
  return <UsersGrid authUser={authUser} />;
}

function StatsPage() {
  return <StatsTab />;
}

const routeTree = rootRoute.addChildren([
  loginRoute,
  appRoute.addChildren([appIndexRoute, moviesRoute, usersRoute, statsRoute]),
]);

export const router = createRouter({
  routeTree,
  context: {
    queryClient,
  },
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
