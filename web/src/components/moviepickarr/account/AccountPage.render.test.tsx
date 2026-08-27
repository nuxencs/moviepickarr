/* ============================================================
   Render tests for the account page's dialog wiring (#140).

   AccountOverlays.render.test.tsx renders each ceremony directly and covers
   what it does once it's on screen. What that can't reach is the page above
   it: which row button opens which ceremony, that only one is ever open (the
   page drives them off a single tag, not a pile of booleans), and that a
   server refusal comes back INTO the open dialog as inline copy rather than a
   toast the member has to catch.

   That last one is the whole reason ChangePasswordDialog takes a serverError
   prop. Rendering the dialog with the prop already set proves it paints; only
   the page can prove anything ever sets it.

   This is the one place a provider harness earns its keep: the page reads its
   OIDC-link redirect through useSearch({ from: "/_app/settings" }), which
   needs a real route tree, so a stubbed router would stub out the subject.
   ============================================================ */

import { act, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AuthKeys } from "@/api/query_keys";

import { AccountPage } from "@/components/moviepickarr/account/AccountPage";

import type { MeResponse, SessionSummary } from "@/types/Response";

import { renderWithProviders } from "@/test/providers";


const changePassword = vi.fn<(current: string, next: string) => Promise<void>>();
const setPassword = vi.fn<(username: string, password: string) => Promise<void>>();
const logout = vi.fn<(all: boolean) => Promise<void>>();
const unlinkSelf = vi.fn<() => Promise<void>>();
const listSessions = vi.fn<() => Promise<SessionSummary[]>>();
const revokeSession = vi.fn<(id: string) => Promise<void>>();

// ApiError and oidcLinkPath stay real: the page branches on the error's status,
// so a stubbed error class would let a wrong branch pass.
vi.mock("@/api/APIClient", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/APIClient")>();
  return {
    ...actual,
    APIClient: {
      auth: {
        changePassword: (c: string, n: string) => changePassword(c, n),
        setPassword: (u: string, p: string) => setPassword(u, p),
        logout: (all: boolean) => logout(all),
        sessions: () => listSessions(),
        revokeSession: (id: string) => revokeSession(id),
      },
      members: { unlinkSelf: () => unlinkSelf() },
    },
  };
});

function actor(overrides: Partial<MeResponse> = {}): MeResponse {
  return {
    id: 1,
    displayName: "Cleo",
    username: "cleo",
    role: "member",
    hasLocalLogin: true,
    hasLinkedIdentity: false,
    ...overrides,
  };
}

/** One live session. `current` marks the device the page is being read on. */
function session(overrides: Partial<SessionSummary> = {}): SessionSummary {
  return {
    id: "session-current",
    device: "Chrome on macOS",
    lastSeenAt: new Date().toISOString(),
    current: true,
    ...overrides,
  };
}

const thisDevice = session();
const phone = session({ id: "session-phone", device: "Safari on iPhone", current: false });

function renderPage({ me = actor(), oidc = false, sessions = [thisDevice] } = {}) {
  listSessions.mockResolvedValue(sessions);
  return renderWithProviders(<AccountPage />, {
    path: "/settings",
    seed: (queryClient) => {
      queryClient.setQueryData(AuthKeys.me(), me);
      queryClient.setQueryData(AuthKeys.config(), { oidc });
      queryClient.setQueryData(AuthKeys.sessions(), sessions);
    },
  });
}

function button(name: string) {
  return screen.getByRole("button", { name });
}

function dialog() {
  return screen.queryByRole("dialog");
}

/** Let a mutation's promise chain settle before asserting on the result. */
async function settle() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
}

/** Long enough to outrun exitDelayMs(), whatever the motion tokens say. */
const AFTER_EXIT = 1000;

/** A dismissed dialog stays mounted until its exit motion finishes. */
async function runExit() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(AFTER_EXIT);
  });
}

beforeEach(() => {
  vi.useFakeTimers();
  changePassword.mockResolvedValue(undefined);
  setPassword.mockResolvedValue(undefined);
  logout.mockResolvedValue(undefined);
  unlinkSelf.mockResolvedValue(undefined);
  listSessions.mockResolvedValue([thisDevice]);
  revokeSession.mockResolvedValue(undefined);
});
afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

describe("opening a ceremony from its row", () => {
  it("shows no dialog until a row button is taken", async () => {
    await renderPage();

    expect(dialog()).toBeNull();
  });

  it("opens the change-password ceremony from Change", async () => {
    await renderPage({ me: actor({ hasLocalLogin: true }) });

    fireEvent.click(button("Change"));

    expect(screen.getByRole("heading", { name: "Change password" })).not.toBeNull();
    expect(screen.getByLabelText("Current password")).not.toBeNull();
  });

  it("offers a first password instead, for a member who signs in with SSO", async () => {
    await renderPage({ me: actor({ hasLocalLogin: false, username: null }) });

    // The Change button is the other branch of the same row; it must be gone.
    expect(screen.queryByRole("button", { name: "Change" })).toBeNull();
    fireEvent.click(button("Set a password"));

    expect(screen.getByRole("heading", { name: "Set a password" })).not.toBeNull();
    expect(screen.getByLabelText("Username")).not.toBeNull();
  });

  it("opens the log-out-everywhere confirm, carrying the actor's device count", async () => {
    await renderPage({ sessions: [thisDevice, phone, session({ id: "session-tablet", current: false })] });

    fireEvent.click(screen.getByText("2 other devices"));
    fireEvent.click(button("Log out everywhere"));

    expect(screen.getByRole("heading", { name: "Log out everywhere?" })).not.toBeNull();
    expect(dialog()?.textContent).toContain("2 other devices");
  });

  it("keeps one ceremony open at a time when a dialog hands off to another", async () => {
    // SSO is the only credential, so unlinking would strand the account.
    await renderPage({ me: actor({ hasLocalLogin: false, hasLinkedIdentity: true }), oidc: true });

    fireEvent.click(button("Unlink"));
    expect(screen.getByRole("heading", { name: /Can't unlink/ })).not.toBeNull();
    // Refused client-side: the guard is shown instead of the request going out.
    expect(unlinkSelf).not.toHaveBeenCalled();

    // Scoped to the guard: the row behind it offers a Set a password of its own,
    // and the handoff under test is the dialog's.
    fireEvent.click(within(dialog() as HTMLElement).getByRole("button", { name: "Set a password" }));

    expect(screen.getByRole("heading", { name: "Set a password" })).not.toBeNull();
    expect(screen.queryByRole("heading", { name: /Can't unlink/ })).toBeNull();
    expect(screen.getAllByRole("dialog")).toHaveLength(1);
  });
});

describe("a refusal from the server", () => {
  async function submitWrongPassword() {
    const { ApiError } = await import("@/api/APIClient");
    changePassword.mockRejectedValueOnce(new ApiError(401, "unauthorized"));

    await renderPage();
    fireEvent.click(button("Change"));

    fireEvent.change(screen.getByLabelText("Current password"), { target: { value: "wrong" } });
    fireEvent.change(screen.getByLabelText("New password"), { target: { value: "longenough" } });
    fireEvent.change(screen.getByLabelText("Confirm new password"), {
      target: { value: "longenough" },
    });
    fireEvent.click(button("Update password"));
    await settle();
  }

  it("lands inside the open dialog, in words about the current password", async () => {
    await submitWrongPassword();

    expect(screen.getByRole("alert").textContent).toContain("current password is incorrect");
    // Still open: a refused save must not throw away what was typed.
    expect(dialog()).not.toBeNull();
  });

  it("is cleared when the ceremony is reopened, so a stale refusal can't linger", async () => {
    await submitWrongPassword();

    fireEvent.keyDown(document, { key: "Escape" });
    await runExit();
    expect(dialog()).toBeNull();

    fireEvent.click(button("Change"));

    expect(screen.queryByRole("alert")).toBeNull();
  });
});

describe("changing a password", () => {
  it("refreshes the device list after the server rotates the actor's sessions", async () => {
    await renderPage({ sessions: [thisDevice, phone] });
    await settle();
    const callsBeforeSubmit = listSessions.mock.calls.length;

    fireEvent.click(button("Change"));
    fireEvent.change(screen.getByLabelText("Current password"), { target: { value: "old-password" } });
    fireEvent.change(screen.getByLabelText("New password"), { target: { value: "new-password" } });
    fireEvent.change(screen.getByLabelText("Confirm new password"), {
      target: { value: "new-password" },
    });
    fireEvent.click(button("Update password"));
    await settle();

    expect(changePassword).toHaveBeenCalledWith("old-password", "new-password");
    expect(listSessions.mock.calls.length).toBeGreaterThan(callsBeforeSubmit);
  });
});

describe("logging out from the sessions row", () => {
  it("ends this session only, without a confirm", async () => {
    await renderPage();

    fireEvent.click(button("Log out this device"));
    await settle();

    expect(logout).toHaveBeenCalledWith(false);
  });

  it("announces that this device is logging out while the request is pending", async () => {
    logout.mockImplementationOnce(() => new Promise<void>(() => {}));
    await renderPage();

    fireEvent.click(button("Log out this device"));
    await settle();

    expect(button("Logging out of this device").hasAttribute("disabled")).toBe(true);
  });

  it("ends every session once the confirm is taken", async () => {
    await renderPage({ sessions: [thisDevice, phone] });

    fireEvent.click(screen.getByText("1 other device"));
    fireEvent.click(button("Log out everywhere"));
    fireEvent.click(within(dialog() as HTMLElement).getByRole("button", { name: "Log out everywhere" }));
    await settle();

    expect(logout).toHaveBeenCalledWith(true);
  });
});

describe("the device list", () => {
  it("keeps the current device visible and reveals others on demand", async () => {
    await renderPage({ sessions: [thisDevice, phone] });

    expect(screen.getByText("Chrome on macOS")).not.toBeNull();
    expect(screen.getByText("This device")).not.toBeNull();
    expect(button("Change").querySelector("svg")).not.toBeNull();
    expect(button("Log out this device").querySelector("svg")).not.toBeNull();

    const disclosure = screen.getByText("1 other device").closest("details") as HTMLDetailsElement;
    expect(disclosure.open).toBe(false);

    fireEvent.click(screen.getByText("1 other device"));

    expect(disclosure.open).toBe(true);
    expect(screen.getByText("Safari on iPhone")).not.toBeNull();
    expect(button("Sign out of Safari on iPhone").querySelector("svg")).not.toBeNull();
  });

  it("signs out the device whose row was taken, not another", async () => {
    await renderPage({ sessions: [thisDevice, phone] });

    fireEvent.click(screen.getByText("1 other device"));
    fireEvent.click(button("Sign out of Safari on iPhone"));
    await settle();

    expect(revokeSession).toHaveBeenCalledWith(phone.id);
  });

  it("holds every session action while one device is being signed out", async () => {
    revokeSession.mockImplementationOnce(() => new Promise<void>(() => {}));
    await renderPage({ sessions: [thisDevice, phone] });

    fireEvent.click(screen.getByText("1 other device"));
    fireEvent.click(button("Sign out of Safari on iPhone"));
    await settle();

    expect(button("Signing out of Safari on iPhone").hasAttribute("disabled")).toBe(true);
    expect(button("Log out this device").hasAttribute("disabled")).toBe(true);
    expect(button("Log out everywhere").hasAttribute("disabled")).toBe(true);
    expect(revokeSession).toHaveBeenCalledOnce();
  });

  it("carries the count of other devices into the log-out-everywhere confirm", async () => {
    await renderPage({ sessions: [thisDevice, phone] });

    fireEvent.click(screen.getByText("1 other device"));
    fireEvent.click(button("Log out everywhere"));

    // One other device, so the confirm must not read "2 other devices" off a
    // list that includes the session doing the asking.
    expect(dialog()?.textContent).toContain("1 other device");
  });
});

describe("credits", () => {
  it("shows TMDB's approved attribution in the account section", async () => {
    await renderPage();

    expect(screen.getByRole("heading", { name: "Credits" })).not.toBeNull();
    const tmdb = screen.getByRole("link", { name: "TMDB" });
    expect(tmdb.getAttribute("href")).toBe("https://www.themoviedb.org/");
    expect(within(tmdb).getByRole("img", { name: "TMDB" }).getAttribute("src")).toBe(
      "/tmdb-logo.svg",
    );
    expect(
      screen.getByText("This product uses the TMDB API but is not endorsed or certified by TMDB."),
    ).not.toBeNull();
  });
});
