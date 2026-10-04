import type { QueryClient } from "@tanstack/react-query";

/** Call on any principal change (login, claim, logout, expiry) before routing. */
export async function clearPrincipalCache(queryClient: QueryClient) {
  await queryClient.cancelQueries();
  queryClient.clear();
}
