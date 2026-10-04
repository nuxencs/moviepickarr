import { QueryClient } from "@tanstack/react-query";

// staleTime spares tab switches a refetch; SSE invalidation ignores it, so mutations
// still show at once. Focus refetch is off because SSE and resync own liveness.
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 60_000,
      refetchOnWindowFocus: false,
    },
  },
});
