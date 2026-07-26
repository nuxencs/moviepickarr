/* ============================================================
   Provider harness for the "dom" vitest project (see vitest.config.ts).

   Most render tests don't need this. A component that renders from props
   should be rendered bare, and one that wants a single context should stub it
   inline. That stays the default, and the files that do it are not wrong.

   This exists for the page-level tests, where the subject is the wiring
   BETWEEN a page and the dialogs it owns: which trigger opens which ceremony,
   and whether the page's state reaches it. Stubbing the router there would
   stub out the thing under test, because `AccountPage` reads its search
   params through `useSearch({ from: "/_app/settings" })` and that only
   resolves against a real route tree.

   So the tree here mirrors the app's shape (a pathless `_app` layout with the
   app paths hanging off it, `/login` beside it) but carries none of its
   loaders, auth guards or lazy components. The subject renders AS the route
   component for `path`, which is what makes the route-id lookups work.

   Two swaps keep a test honest: a fresh QueryClient per render, so nothing
   bleeds between cases, and a memory history in place of the browser's.
   ============================================================ */

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

// The router restores scroll position on every navigation. jsdom does define
// scrollTo, but as a stub that logs "Not implemented" on every call, so it's
// replaced outright. Kept here rather than in the shared DOM setup, since it's
// the router that wants it and only tests using this harness mount one.
window.scrollTo = (() => {}) as typeof window.scrollTo;

/** The app paths that sit under the pathless `_app` layout. Add one when a
 *  page test needs it; an absent path fails the match, it doesn't fall back. */
const APP_PATHS = ["/", "/admin", "/settings", "/stats", "/users"] as const;

/** The real validators, for the pages whose search params are the subject
 *  rather than scenery: without one, `?member=3` reaches the page as the
 *  string "3" and the test proves nothing about what the app does. */
const VALIDATORS: Partial<Record<(typeof APP_PATHS)[number], (s: Record<string, unknown>) => object>> = {
  "/users": validateMembersSearch,
};

/** An app path, optionally with a query string on it (the Stats tab keeps its
 *  whole view in the search params, and a test may need them present). */
type AppHref = (typeof APP_PATHS)[number] | `${(typeof APP_PATHS)[number]}?${string}`;

function buildRouter(ui: ReactNode, path: string) {
  const rootRoute = createRootRoute();
  const subject = () => <>{ui}</>;

  // No component: the default renders an Outlet, which is all the layout is
  // for here. The id is what matters, since route-id lookups key off "/_app/…".
  const appLayout = createRoute({ getParentRoute: () => rootRoute, id: "_app" });

  const appRoutes = APP_PATHS.map((p) =>
    createRoute({
      getParentRoute: () => appLayout,
      path: p,
      component: subject,
      validateSearch: VALIDATORS[p],
    }),
  );

  // Sits beside the layout, not under it, same as the app. Nothing asserts on
  // it, but a page that logs out navigates here and the match has to resolve.
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
  /** Href the memory history starts at, whose path decides the route the
   *  subject renders as. That path must be one of APP_PATHS. */
  path: AppHref;
  /** Seed the query cache before the first render, so a page under test reads
   *  its data instead of waiting on a request. */
  seed: (queryClient: QueryClient) => void;
}

/**
 * Render `ui` inside the app's real providers, as the component of its route.
 * Await it before querying: the router resolves its first match
 * asynchronously, so nothing is on screen when render() returns.
 */
export async function renderWithProviders(ui: ReactNode, { path, seed }: ProviderOptions) {
  const queryClient = new QueryClient({
    defaultOptions: {
      // A test asserts on one outcome, so a retry would only slow a failure
      // down, and a stale time of zero would refetch under the assertions.
      queries: { retry: false, staleTime: Infinity },
      mutations: { retry: false },
    },
  });
  seed(queryClient);

  // Handed back so a test can drive history the way the browser's own back
  // button does, and read the location the subject navigated to.
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
