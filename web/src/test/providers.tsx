/* Provider harness for page-level tests only; render plain components bare.
   Pages read search params by route id (`useSearch({ from: "/_app/settings" })`),
   so the subject renders as a route in a tree that mirrors the app's shape,
   without loaders, auth guards or lazy components. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render } from "@testing-library/react";

import { AudioProvider } from "@/components/AudioProvider";
import { validateMembersSearch } from "@/components/moviepickarr/membersSearch";
import { ThemeProvider } from "@/components/ThemeProvider";

import type { ReactNode } from "react";

// jsdom's scrollTo logs "Not implemented" on each router scroll restore.
window.scrollTo = (() => {}) as typeof window.scrollTo;

/** Paths under the `_app` layout. An absent path fails the match. */
const APP_PATHS = ["/", "/admin", "/settings", "/stats", "/users"] as const;

/** Real validators, else `?member=3` reaches the page as the string "3". */
const VALIDATORS: Partial<Record<(typeof APP_PATHS)[number], (s: Record<string, unknown>) => object>> = {
  "/users": validateMembersSearch,
};

type AppHref = (typeof APP_PATHS)[number] | `${(typeof APP_PATHS)[number]}?${string}`;

function buildRouter(ui: ReactNode, path: string) {
  const rootRoute = createRootRoute();
  const subject = () => <>{ui}</>;

  // Route-id lookups key off "/_app/…"; the default component is an Outlet.
  const appLayout = createRoute({ getParentRoute: () => rootRoute, id: "_app" });

  const appRoutes = APP_PATHS.map((p) =>
    createRoute({
      getParentRoute: () => appLayout,
      path: p,
      component: subject,
      validateSearch: VALIDATORS[p],
    }),
  );

  // Logout navigates here, so the match must resolve.
  const loginRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/login",
    component: () => <div>login</div>,
  });

  return createRouter({
    routeTree: rootRoute.addChildren([appLayout.addChildren(appRoutes), loginRoute]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });
}

export interface ProviderOptions {
  /** Start href; its path picks the route the subject renders as. */
  path: AppHref;
  /** Seeds the query cache before the first render. */
  seed: (queryClient: QueryClient) => void;
}

/**
 * Renders `ui` in the app's providers as its route's component. Await it: the
 * router resolves its first match asynchronously.
 */
export async function renderWithProviders(ui: ReactNode, { path, seed }: ProviderOptions) {
  const queryClient = new QueryClient({
    defaultOptions: {
      // A stale time of zero would refetch under the assertions.
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  });
  seed(queryClient);

  // Returned so a test can drive history and read the location.
  const router = buildRouter(ui, path);

  render(
    <QueryClientProvider client={queryClient}>
      <ThemeProvider defaultTheme="dark" storageKey="test-ui-theme">
        <AudioProvider>
          <RouterProvider router={router} />
        </AudioProvider>
      </ThemeProvider>
    </QueryClientProvider>,
  );

  await router.load();
  return { router };
}
