import type { QueryClient } from "@tanstack/react-query";

/**
 * A successful login, claim, logout, or confirmed session expiry changes who
 * owns every authenticated cache entry. Cancel private work first, then clear
 * both query and mutation data before routing to another principal.
 */
export async function clearPrincipalCache(queryClient: QueryClient) {
  await queryClient.cancelQueries();
  queryClient.clear();
}
