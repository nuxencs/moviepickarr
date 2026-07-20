import { isRedirect } from "@tanstack/react-router";
import { describe, expect, it } from "vitest";

import { ApiError } from "@/api/APIClient";
import { redirectIfSignedIn, requireSession } from "@/api/authGuard";

// Assert that a guard call redirected to `to`, failing loudly if it resolved
// without throwing (the "no redirect" bug) or threw something that isn't a
// redirect.
async function expectRedirect(run: Promise<unknown>, to: string) {
  await run.then(
    () => {
      throw new Error(`expected a redirect to ${to}, but the guard resolved`);
    },
    (err: unknown) => {
      expect(isRedirect(err)).toBe(true);
      expect((err as { options?: { to?: string } }).options?.to).toBe(to);
    },
  );
}

const me = { id: 1, name: "Ada" };
const resolves = () => Promise.resolve(me);
const rejectsWith = (err: unknown) => () => Promise.reject(err);

describe("requireSession (app-layout gate)", () => {
  it("redirects a 401 to /login instead of letting the page render", async () => {
    // The main-page bug: a dead session used to paint the chrome behind 401s.
    await expectRedirect(requireSession(rejectsWith(new ApiError(401, "no session"))), "/login");
  });

  it("lets a live session through without redirecting", async () => {
    await expect(requireSession(resolves)).resolves.toBeUndefined();
  });

  it("falls through on a non-401 failure so the page shows its own error", async () => {
    // A 5xx / network error is a genuine load failure, not a logged-out state.
    await expect(requireSession(rejectsWith(new ApiError(500, "boom")))).resolves.toBeUndefined();
    await expect(requireSession(rejectsWith(new Error("network")))).resolves.toBeUndefined();
  });
});

describe("redirectIfSignedIn (login gate)", () => {
  it("bounces a live session to / before the form renders", async () => {
    // The login-flash bug: the form used to paint for a frame before redirect.
    await expectRedirect(redirectIfSignedIn(resolves), "/");
  });

  it("shows the form (no redirect) when not signed in", async () => {
    await expect(redirectIfSignedIn(rejectsWith(new ApiError(401, "no session")))).resolves.toBeUndefined();
  });

  it("shows the form when /me fails for any other reason", async () => {
    await expect(redirectIfSignedIn(rejectsWith(new Error("network")))).resolves.toBeUndefined();
  });
});
