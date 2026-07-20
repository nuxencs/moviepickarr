import { redirect } from "@tanstack/react-router";

import { ApiError } from "@/api/APIClient";

// Route-level auth guards. They run in a route's beforeLoad, before the page
// renders, so the redirect decision is made off the resolved /me rather than
// after the component has already painted. Both take the /me fetch as a thunk so
// the decision logic is unit-testable without a router or a live session.

/**
 * Gate for the authenticated app layout. A resolved /me lets the page render; a
 * 401 (no/expired session) redirects to /login before any 401-riddled chrome is
 * painted. A non-401 failure (network, 5xx) falls through so the page surfaces
 * its own load-error state instead of masquerading as logged-out.
 */
export async function requireSession(fetchMe: () => Promise<unknown>): Promise<void> {
  try {
    await fetchMe();
  } catch (err) {
    if (err instanceof ApiError && err.status === 401) {
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
export async function redirectIfSignedIn(fetchMe: () => Promise<unknown>): Promise<void> {
  const me = await fetchMe().catch(() => null);
  if (me) {
    throw redirect({ to: "/" });
  }
}
