import { QueryClient } from "@tanstack/react-query";
import { isRedirect } from "@tanstack/react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

import { APIClient, ApiError } from "@/api/APIClient";
import { redirectIfSignedIn, requireAppSession, requireSession } from "@/api/authGuard";
import { clearPrincipalCache } from "@/api/principalCache";
import { AuthKeys } from "@/api/query_keys";

import type { MeResponse } from "@/types/Response";

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

afterEach(() => vi.restoreAllMocks());

describe("requireAppSession", () => {
  const actor: MeResponse = {
    id: 1, displayName: "Ada", username: "ada", role: "admin",
    hasLocalLogin: true, hasLinkedIdentity: false,
  };

  function setup(cached = true) {
    const client = new QueryClient({ defaultOptions: { queries: { gcTime: Infinity } } });
    if (cached) client.setQueryData(AuthKeys.me(), actor);
    let resolve!: (value: MeResponse) => void;
    let reject!: (error: unknown) => void;
    const pending = new Promise<MeResponse>((yes, no) => { resolve = yes; reject = no; });
    const fetch = vi.spyOn(APIClient.auth, "me").mockReturnValue(pending);
    const expired = vi.fn();
    return { client, resolve, reject, fetch, expired };
  }

  it("allows repeated cached navigation while one session check waits", async () => {
    const { client, resolve, fetch, expired } = setup();
    await requireAppSession(client, expired);
    await requireAppSession(client, expired);
    expect(fetch).toHaveBeenCalledOnce();
    expect(expired).not.toHaveBeenCalled();
    resolve({ ...actor, role: "member" });
    await vi.waitFor(() => expect(client.getQueryData<MeResponse>(AuthKeys.me())?.role).toBe("member"));
    client.clear();
  });

  it("waits for the first session check without a cached principal", async () => {
    const { client, resolve, expired } = setup(false);
    let entered = false;
    const guard = Promise.resolve(requireAppSession(client, expired)).then(() => { entered = true; });
    await Promise.resolve();
    expect(entered).toBe(false);
    resolve(actor);
    await guard;
    expect(entered).toBe(true);
    client.clear();
  });

  it("clears private query and mutation data before one background expiry redirect", async () => {
    const { client, reject, expired } = setup();
    client.setQueryData(["movies", "private"], ["old movie"]);
    await client.getMutationCache().build(client, { mutationFn: async () => "private invite" }).execute(undefined);
    expired.mockImplementation(() => {
      expect(client.getQueryCache().getAll()).toHaveLength(0);
      expect(client.getMutationCache().getAll()).toHaveLength(0);
    });
    await requireAppSession(client, expired);
    await requireAppSession(client, expired);
    reject(new ApiError(401, "expired"));
    await vi.waitFor(() => expect(expired).toHaveBeenCalledOnce());
    client.clear();
  });

  it.each([new ApiError(500, "unavailable"), new Error("offline")])(
    "keeps the cached principal on a background failure: %s", async (error) => {
      const { client, reject, expired } = setup();
      await requireAppSession(client, expired);
      reject(error);
      await vi.waitFor(() => expect(client.getQueryState(AuthKeys.me())?.status).toBe("error"));
      expect(client.getQueryData(AuthKeys.me())).toEqual(actor);
      expect(expired).not.toHaveBeenCalled();
      client.clear();
    },
  );

  it("ignores a late expiry response after the principal cache was replaced", async () => {
    const { client, reject, expired } = setup();
    await requireAppSession(client, expired);
    await clearPrincipalCache(client);
    const next = { ...actor, id: 2, displayName: "Ben" };
    client.setQueryData(AuthKeys.me(), next);
    reject(new ApiError(401, "old session"));
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(expired).not.toHaveBeenCalled();
    expect(client.getQueryData(AuthKeys.me())).toEqual(next);
    client.clear();
  });
});

describe("requireSession (app-layout gate)", () => {
  it("redirects a 401 to /login instead of letting the page render", async () => {
    const clearPrincipal = vi.fn();
    // The main-page bug: a dead session used to paint the chrome behind 401s.
    await expectRedirect(
      requireSession(rejectsWith(new ApiError(401, "no session")), clearPrincipal),
      "/login",
    );
    expect(clearPrincipal).toHaveBeenCalledOnce();
  });

  it("lets a live session through without redirecting", async () => {
    await expect(requireSession(resolves)).resolves.toBeUndefined();
  });

  it("falls through on a non-401 failure so the page shows its own error", async () => {
    const clearPrincipal = vi.fn();
    // A 5xx / network error is a genuine load failure, not a logged-out state.
    await expect(
      requireSession(rejectsWith(new ApiError(500, "boom")), clearPrincipal),
    ).resolves.toBeUndefined();
    await expect(
      requireSession(rejectsWith(new Error("network")), clearPrincipal),
    ).resolves.toBeUndefined();
    expect(clearPrincipal).not.toHaveBeenCalled();
  });
});

describe("redirectIfSignedIn (login gate)", () => {
  it("bounces a live session to / before the form renders", async () => {
    // The login-flash bug: the form used to paint for a frame before redirect.
    await expectRedirect(redirectIfSignedIn(resolves), "/");
  });

  it("shows the form (no redirect) when not signed in", async () => {
    const clearPrincipal = vi.fn();
    await expect(
      redirectIfSignedIn(rejectsWith(new ApiError(401, "no session")), clearPrincipal),
    ).resolves.toBeUndefined();
    expect(clearPrincipal).toHaveBeenCalledOnce();
  });

  it("shows the form when /me fails for any other reason", async () => {
    const clearPrincipal = vi.fn();
    await expect(
      redirectIfSignedIn(rejectsWith(new Error("network")), clearPrincipal),
    ).resolves.toBeUndefined();
    expect(clearPrincipal).not.toHaveBeenCalled();
  });
});
