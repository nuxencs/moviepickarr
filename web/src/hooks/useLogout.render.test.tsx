/* Render test for useLogout. The cached actor must be gone before the login
   route mounts, or the login page sees a stale session and bounces back into
   the app. A failed logout leaves the member in place with a toast. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@/api/APIClient";
import { AuthKeys } from "@/api/query_keys";

import type { MeResponse } from "@/types/Response";
import type { ReactNode } from "react";

import { useLogout } from "@/hooks/useLogout";

const navigate = vi.fn();
vi.mock("@tanstack/react-router", () => ({ useNavigate: () => navigate }));

const logout = vi.fn<(all: boolean) => Promise<void>>();
// ApiError stays real: apiMessage branches on it to prefer the server's own
// wording over the fallback, and a stubbed class would let that branch pass.
vi.mock("@/api/APIClient", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/APIClient")>();
  return { ...actual, APIClient: { auth: { logout: (all: boolean) => logout(all) } } };
});

const toastError = vi.fn();
vi.mock("@/components/ui/toast-api", () => ({ toast: { error: (m: string) => toastError(m) } }));

const actor: MeResponse = {
  id: 1,
  displayName: "Cleo",
  username: "cleo",
  role: "member",
  hasLocalLogin: true,
  hasLinkedIdentity: false,
};

/** Whether the actor was still cached at the moment navigate was called. */
let cachedAtNavigate: MeResponse | undefined;

function Harness({ all }: { all: boolean }) {
  const logoutMutation = useLogout();
  return (
    <button type="button" onClick={() => logoutMutation.mutate(all)} disabled={logoutMutation.isPending}>
      Log out
    </button>
  );
}

function renderHook({ all = false }: { all?: boolean } = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  client.setQueryData(AuthKeys.me(), actor);
  client.setQueryData(AuthKeys.sessions(), [{ id: "old-device", device: "Safari on iPhone" }]);
  client.setQueryData(["invites", "list"], [{ id: "old-invite" }]);
  navigate.mockImplementation(() => {
    cachedAtNavigate = client.getQueryData<MeResponse>(AuthKeys.me());
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  render(wrapper({ children: <Harness all={all} /> }));
  return { client, button: screen.getByRole("button", { name: "Log out" }) };
}

/** A mutation runs through a promise chain, so the call lands after the click returns. */
async function clickAndSettle(button: HTMLElement) {
  await act(async () => {
    button.click();
    await vi.advanceTimersByTimeAsync(0);
  });
}

beforeEach(() => {
  vi.useFakeTimers();
  cachedAtNavigate = actor;
  logout.mockResolvedValue(undefined);
});
afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

describe("useLogout", () => {
  it("carries the scope through to the request", async () => {
    const { button } = renderHook({ all: true });

    await clickAndSettle(button);

    expect(logout).toHaveBeenCalledWith(true);
  });

  it("ends this session only when the scope is single-device", async () => {
    const { button } = renderHook({ all: false });

    await clickAndSettle(button);

    expect(logout).toHaveBeenCalledWith(false);
  });

  it("drops the cached actor before entering the login route", async () => {
    const { client, button } = renderHook();

    await clickAndSettle(button);

    expect(navigate).toHaveBeenCalledWith({ to: "/login" });
    // Read from inside the navigate stub, so the order is pinned.
    expect(cachedAtNavigate).toBeUndefined();
    expect(client.getQueryData(AuthKeys.me())).toBeUndefined();
    expect(client.getQueryData(AuthKeys.sessions())).toBeUndefined();
    expect(client.getQueryData(["invites", "list"])).toBeUndefined();
  });

  it("stays put and toasts when the request fails", async () => {
    logout.mockRejectedValueOnce(new ApiError(503, "Session store is unreachable."));
    const { client, button } = renderHook();

    await clickAndSettle(button);

    expect(navigate).not.toHaveBeenCalled();
    // The session may well still be live, so the cached actor stands.
    expect(client.getQueryData(AuthKeys.me())).toEqual(actor);
    expect(client.getQueryData(AuthKeys.sessions())).toBeDefined();
    // The server's wording, not the fallback.
    expect(toastError).toHaveBeenCalledWith("Session store is unreachable.");
  });

  it("falls back to its own wording when the failure carries none", async () => {
    logout.mockRejectedValueOnce(new Error("network down"));
    const { button } = renderHook();

    await clickAndSettle(button);

    expect(toastError).toHaveBeenCalledWith("Couldn't log out.");
  });

  it("reports the request in flight, so a caller can hold its button shut", async () => {
    logout.mockImplementationOnce(() => new Promise<void>(() => {}));
    const { button } = renderHook();

    await clickAndSettle(button);

    expect(button.hasAttribute("disabled")).toBe(true);
  });
});
