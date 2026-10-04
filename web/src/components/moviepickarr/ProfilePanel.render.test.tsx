/* Render tests for the avatar profile panel (#140). Escape and in-panel links
   return focus to the avatar; an outside click leaves it where the pointer put
   it (deliberately unlike the #140 brief). Routing and logout are stubbed. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AudioProvider } from "@/components/AudioProvider";
import { ProfilePanel } from "@/components/moviepickarr/ProfilePanel";
import { ThemeProvider } from "@/components/ThemeProvider";

import type { MeResponse } from "@/types/Response";
import type { ReactNode } from "react";

const navigate = vi.fn();

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, onClick }: { children: ReactNode; onClick?: () => void }) => (
    <a
      href="/settings"
      onClick={(e) => {
        // jsdom can't navigate; the panel's own close-on-follow is the subject.
        e.preventDefault();
        onClick?.();
      }}
    >
      {children}
    </a>
  ),
  useNavigate: () => navigate,
}));

const logout = vi.fn<(everywhere: boolean) => Promise<void>>(() => Promise.resolve());
vi.mock("@/api/APIClient", () => ({
  APIClient: { auth: { logout: (everywhere: boolean) => logout(everywhere) } },
}));

/** Long enough to outrun exitDelayMs(), whatever the motion tokens say. */
const AFTER_EXIT = 1000;

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

function renderPanel(me: MeResponse = actor()) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <ThemeProvider defaultTheme="dark" storageKey="test-ui-theme">
        <AudioProvider>
          <ProfilePanel me={me} />
        </AudioProvider>
      </ThemeProvider>
    </QueryClientProvider>,
  );
  return { trigger: screen.getByRole("button", { name: "Your profile" }) };
}

function panel() {
  return screen.queryByRole("dialog", { name: "Your profile" });
}

function runExit() {
  act(() => void vi.advanceTimersByTime(AFTER_EXIT));
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

describe("opening the panel", () => {
  it("stays shut until the avatar is clicked", () => {
    const { trigger } = renderPanel();

    expect(panel()).toBeNull();
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
  });

  it("opens on the avatar and says so to a screen reader", () => {
    const { trigger } = renderPanel();

    fireEvent.click(trigger);

    expect(panel()).not.toBeNull();
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    expect(trigger.getAttribute("aria-controls")).toBe(panel()?.id);
  });

  it("closes again on a second click of the avatar", () => {
    const { trigger } = renderPanel();

    fireEvent.click(trigger);
    fireEvent.click(trigger);
    runExit();

    expect(panel()).toBeNull();
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
  });

  it("names the member and their role in the panel header", () => {
    renderPanel(actor({ displayName: "Cleo", role: "admin" }));

    fireEvent.click(screen.getByRole("button", { name: "Your profile" }));

    expect(screen.getByText("Cleo")).not.toBeNull();
    expect(screen.getByText("Admin")).not.toBeNull();
    expect(screen.getByText("@cleo")).not.toBeNull();
  });
});

describe("dismissing the panel", () => {
  it("closes on Escape and hands focus back to the avatar", () => {
    const { trigger } = renderPanel();
    fireEvent.click(trigger);

    fireEvent.keyDown(document, { key: "Escape" });
    // Focus moves as part of the gesture, before the exit motion finishes.
    expect(document.activeElement).toBe(trigger);

    runExit();
    expect(panel()).toBeNull();
  });

  it("closes on a click outside, leaving focus where the pointer went", () => {
    const outside = document.createElement("button");
    document.body.append(outside);
    const { trigger } = renderPanel();
    fireEvent.click(trigger);

    // jsdom's pointerDown does not move focus; without this, <body> passes either way.
    outside.focus();
    fireEvent.pointerDown(outside);
    runExit();

    expect(panel()).toBeNull();
    expect(document.activeElement).toBe(outside);
    outside.remove();
  });

  it("reopens on a click that lands mid-close, instead of slamming shut after it", () => {
    const { trigger } = renderPanel();
    fireEvent.click(trigger);

    fireEvent.keyDown(document, { key: "Escape" });
    fireEvent.click(trigger);

    // The interrupted close must not still fire on its original timer.
    runExit();
    expect(panel()).not.toBeNull();
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
  });

  it("stays open on a click inside itself", () => {
    const { trigger } = renderPanel();
    fireEvent.click(trigger);

    fireEvent.pointerDown(screen.getByText("Preferences"));
    runExit();

    expect(panel()).not.toBeNull();
  });

  it("closes when Account settings is taken, so the panel isn't left over the new page", () => {
    const { trigger } = renderPanel();
    fireEvent.click(trigger);

    fireEvent.click(screen.getByRole("link", { name: "Account settings" }));
    runExit();

    expect(panel()).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });
});

/** A mutation runs through a promise chain, so the call lands after the click returns. */
async function clickAndSettle(button: HTMLElement) {
  await act(async () => {
    fireEvent.click(button);
    await vi.advanceTimersByTimeAsync(0);
  });
}

describe("logging out", () => {
  it("is one action away from the open panel", async () => {
    const { trigger } = renderPanel();
    fireEvent.click(trigger);

    const button = screen.getByRole("button", { name: "Log out" });
    expect(button.hasAttribute("disabled")).toBe(false);

    await clickAndSettle(button);

    // Single-device logout: "everywhere" stays on the account page.
    expect(logout).toHaveBeenCalledWith(false);
  });

  it("lands on the login route once the session is gone", async () => {
    const { trigger } = renderPanel();
    fireEvent.click(trigger);

    fireEvent.click(screen.getByRole("button", { name: "Log out" }));
    await act(async () => {
      await vi.runAllTimersAsync();
    });

    expect(navigate).toHaveBeenCalledWith({ to: "/login" });
  });

  it("goes disabled while the request is in flight, so it can't be double-fired", async () => {
    logout.mockImplementationOnce(() => new Promise<void>(() => {}));
    const { trigger } = renderPanel();
    fireEvent.click(trigger);

    const button = screen.getByRole("button", { name: "Log out" });
    await clickAndSettle(button);

    expect(button.hasAttribute("disabled")).toBe(true);
  });
});
