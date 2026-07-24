import { QueryClient } from "@tanstack/react-query";

// staleTime keeps cached data fresh across the ~minute a user navigates between
// the Movies/Stats/Users tabs, so a route switch reads the cache instead of
// refetching (each refetch otherwise re-pulls /movies/watched through the single
// SQLite connection). This is safe with the app's invalidate-refetch model:
// explicit invalidateQueries (the SSE path) refetches active queries regardless
// of staleTime, so real mutations still update immediately — staleTime only
// gates the automatic mount/focus refetch. refetchOnWindowFocus stays off (SSE +
// reconnect-resync own liveness) and is set here only — queries inherit it, so
// none of them repeat it.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 60_000,
      refetchOnWindowFocus: false,
    },
  },
});
