/* ============================================================
   Render tests for the roster section's dialog wiring (#140).

   RosterOverlays.render.test.tsx renders each ceremony directly. The gap this
   fills is the path a member actually takes to one: the row kebab, whose
   actions are built per member from their credential state, and which then
   hands a specific member into the dialog it opens.

   That handoff is the part worth pinning. rowActions and the dialogs are
   correct in isolation and can still be wired to the wrong row: an off-by-one
   in the member passed through would open a perfectly good remove confirm
   naming somebody else. Only a test that goes trigger → menu → dialog sees it.

   Which actions a given member gets is derived by roster.ts (isPlaceholder,
   unlinkWouldStrand) and roster.test.ts owns that matrix. What's asserted here
   is that the derivation reaches the menu and the right member rides along.
   ============================================================ */

import { act, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AuthKeys, MoviesKeys, UsersKeys } from "@/api/query_keys";

import { RosterSection } from "@/components/moviepickarr/admin/RosterSection";

import type { MeResponse, RemoveResult, RosterMember } from "@/types/Response";
import type { QueryClient } from "@tanstack/react-query";

import { renderWithProviders } from "@/test/providers";


const setLogin = vi.fn<(id: number, u: string, p: string) => Promise<void>>();
const removeMember = vi.fn<(id: number) => Promise<RemoveResult>>();

vi.mock("@/api/APIClient", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/APIClient")>();
  return {
    ...actual,
    APIClient: {
      members: {
        setLocalLogin: (id: number, u: string, p: string) => setLogin(id, u, p),
        remove: (id: number) => removeMember(id),
      },
    },
  };
});

function member(overrides: Partial<RosterMember> = {}): RosterMember {
  return {
    id: 7,
    name: "Cleo",
    username: "cleo",
    role: "member",
    archived: false,
    hasLocalLogin: true,
    hasLinkedIdentity: false,
    invitePending: false,
    moviesAuthored: 0,
    ...overrides,
  };
}

const admin: MeResponse = {
  id: 1,
  displayName: "Ada",
  username: "ada",
  role: "admin",
  hasLocalLogin: true,
  hasLinkedIdentity: false,
  otherSessions: 0,
};

async function renderSection(members: RosterMember[]) {
  let queryClient: QueryClient | undefined;
  const view = await renderWithProviders(<RosterSection />, {
    path: "/admin",
    seed: (client) => {
      queryClient = client;
      client.setQueryData(AuthKeys.me(), admin);
      client.setQueryData(UsersKeys.roster(), members);
    },
  });
  return { ...view, queryClient: queryClient! };
}

/** Open a member's row kebab and take one of its actions. The menu portals to
 *  the body, and its entries are menuitems rather than plain buttons. */
function takeAction(name: string, action: string) {
  fireEvent.click(screen.getByRole("button", { name: `Actions for ${name}` }));
  fireEvent.click(within(screen.getByRole("menu")).getByRole("menuitem", { name: action }));
}

function dialog() {
  return screen.getByRole("dialog");
}

beforeEach(() => {
  vi.useFakeTimers();
  setLogin.mockResolvedValue(undefined);
  removeMember.mockResolvedValue({ outcome: "archived" });
});
afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

describe("opening a ceremony from a member's row", () => {
  it("shows no dialog until an action is taken", async () => {
    await renderSection([member()]);

    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("opens the remove confirm on the member whose row it came from", async () => {
    await renderSection([
      member({ id: 7, name: "Cleo", moviesAuthored: 0 }),
      member({ id: 8, name: "Bea", moviesAuthored: 4 }),
    ]);

    takeAction("Bea", "Remove member");

    // Bea's row, so Bea's confirm. Which outcome her movie count earns is
    // RosterOverlays.render.test.tsx's; all that matters here is whose row won.
    expect(dialog().textContent).toContain("Remove Bea?");
    expect(dialog().textContent).not.toContain("Cleo");
  });

  it("carries the member into the set-login dialog, prefilled with their username", async () => {
    await renderSection([
      member({ id: 7, name: "Cleo", username: "cleo", hasLocalLogin: true }),
      member({ id: 8, name: "Bea", username: "bea", hasLocalLogin: true }),
    ]);

    // An existing login is a reset, not a first set.
    takeAction("Bea", "Reset password");

    expect(within(dialog()).getByRole("heading").textContent).toContain("Bea");
    expect((within(dialog()).getByLabelText("Username") as HTMLInputElement).value).toBe("bea");
  });

  it("offers a first set, unprefilled, for a member who has SSO but no password", async () => {
    await renderSection([
      member({ name: "Cleo", username: undefined, hasLocalLogin: false, hasLinkedIdentity: true }),
    ]);

    takeAction("Cleo", "Set password");

    expect(within(dialog()).getByRole("heading").textContent).toContain("Set a password for Cleo");
    expect((within(dialog()).getByLabelText("Username") as HTMLInputElement).value).toBe("");
  });

  it("guards a self-unlink that would strand the account, instead of sending it", async () => {
    // The actor themselves, SSO-only: unlinking would lock them out.
    await renderSection([
      member({ id: admin.id, name: "Ada", hasLocalLogin: false, hasLinkedIdentity: true }),
    ]);

    takeAction("Ada", "Unlink SSO");

    expect(dialog().textContent).toContain("Can't unlink SSO");
  });
});

describe("committing a ceremony", () => {
  it("sends the removal for the member the confirm named", async () => {
    await renderSection([member({ id: 7, name: "Cleo" }), member({ id: 8, name: "Bea" })]);

    takeAction("Bea", "Remove member");
    fireEvent.click(within(dialog()).getByRole("button", { name: "Delete member" }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(removeMember).toHaveBeenCalledWith(8);
  });

  it("stales cached movie attribution after its own archive event is lost", async () => {
    const { queryClient } = await renderSection([
      member({ id: 8, name: "Bea", moviesAuthored: 1 }),
    ]);
    queryClient.setQueryData(MoviesKeys.current(), {
      movieID: 42,
      addedByID: 8,
      addedByName: "Bea",
    });

    takeAction("Bea", "Remove member");
    fireEvent.click(within(dialog()).getByRole("button", { name: "Archive member" }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(queryClient.getQueryState(MoviesKeys.current())?.isInvalidated).toBe(true);
  });

  it("sends the new login for the member the dialog named", async () => {
    await renderSection([member({ id: 8, name: "Bea", username: "bea", hasLocalLogin: true })]);

    takeAction("Bea", "Reset password");
    fireEvent.change(within(dialog()).getByLabelText("New password"), {
      target: { value: "hunter2" },
    });
    fireEvent.click(within(dialog()).getByRole("button", { name: "Reset password" }));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(setLogin).toHaveBeenCalledWith(8, "bea", "hunter2");
  });
});
