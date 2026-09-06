import { redirect } from "@tanstack/react-router";

import { ApiError } from "@/api/APIClient";
import { clearPrincipalCache } from "@/api/principalCache";
import { MeQueryOptions } from "@/api/queries";

import type { QueryClient } from "@tanstack/react-query";

/**
 * First entry waits for a session. Later navigation uses the cached principal
 * while a fresh check runs in the background. QueryClient shares an in-flight
 * check across rapid navigation and cancels it when the principal cache clears.
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

    // Remove this session synchronously so only one waiter handles expiry.
    // A response from an old, removed query must not clear a new principal.
    queryClient.removeQueries({ queryKey: options.queryKey, exact: true });
    await clearPrincipalCache(queryClient);
    onExpired();
  });
}

/**
 * Gate for an uncached session. A resolved /me lets the page render; a
 * 401 (no/expired session) redirects to /login before any 401-riddled chrome is
 * painted. A non-401 failure (network, 5xx) falls through so the page surfaces
 * its own load-error state instead of masquerading as logged-out.
 */
type ClearPrincipal = () => void | Promise<void>;

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
 * Gate for the login route. A live session bounces the member into the app
 * before the login form renders, so there is no one-frame flash of the form
 * ahead of a post-render redirect. Any /me failure (the logged-out case,
 * including the OIDC ?error= landing) falls through and the form renders.
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
