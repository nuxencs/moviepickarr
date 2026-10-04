import { redirect } from "@tanstack/react-router";

import { ApiError } from "@/api/APIClient";
import { clearPrincipalCache } from "@/api/principalCache";
import { MeQueryOptions } from "@/api/queries";

import type { QueryClient } from "@tanstack/react-query";

/**
 * First entry waits for a session; later navigation uses the cached principal
 * while a fresh check runs in the background.
 */
export function requireAppSession(queryClient: QueryClient, onExpired: () => void) {
  const options = MeQueryOptions();
  const cached = queryClient.getQueryData(options.queryKey);
  const pending = queryClient.fetchQuery({ ...options, staleTime: 0 });
  if (!cached) {
    return requireSession(() => pending, () => clearPrincipalCache(queryClient));
  }

  const query = queryClient.getQueryCache().find({ queryKey: options.queryKey, exact: true });
  void pending.catch(async (error: unknown) => {
    if (!(error instanceof ApiError) || error.status !== 401) return;
    if (queryClient.getQueryCache().find({ queryKey: options.queryKey, exact: true }) !== query) return;

    // Remove synchronously so only one waiter handles expiry. The query identity
    // check above keeps a stale response from clearing a new principal.
    queryClient.removeQueries({ queryKey: options.queryKey, exact: true });
    await clearPrincipalCache(queryClient);
    onExpired();
  });
}

type ClearPrincipal = () => void | Promise<void>;

/**
 * Redirects to /login on a 401 only. Other failures fall through so the page
 * shows its own load error instead of looking logged out.
 */
export async function requireSession(
  fetchMe: () => Promise<unknown>,
  clearPrincipal: ClearPrincipal = () => {},
): Promise<void> {
  try {
    await fetchMe();
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      await clearPrincipal();
      throw redirect({ to: "/login" });
    }
  }
}

/**
 * Login route gate: redirects before render, so the form never flashes. Any /me
 * failure (including the OIDC ?error= landing) renders the form.
 */
export async function redirectIfSignedIn(
  fetchMe: () => Promise<unknown>,
  clearPrincipal: ClearPrincipal = () => {},
): Promise<void> {
  let me: unknown;
  try {
    me = await fetchMe();
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
      await clearPrincipal();
    }
    return;
  }
  if (me) {
    throw redirect({ to: "/" });
  }
}
