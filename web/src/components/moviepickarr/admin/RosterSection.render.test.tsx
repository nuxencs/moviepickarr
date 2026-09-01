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

import { ApiError } from "@/api/APIClient";
import { AuthKeys, InvitesKeys, MoviesKeys, UsersKeys } from "@/api/query_keys";

import { RosterSection } from "@/components/moviepickarr/admin/RosterSection";

import type {
  InviteResult,
  InviteSummary,
  InvitesResponse,
  MeResponse,
  MemberRole,
  RemoveResult,
  RosterMember,
} from "@/types/Response";
import type { QueryClient } from "@tanstack/react-query";

import { renderWithProviders } from "@/test/providers";

const setLogin = vi.fn<(id: number, u: string, p: string) => Promise<void>>();
const removeMember = vi.fn<(id: number) => Promise<RemoveResult>>();
const listRoster = vi.fn<() => Promise<RosterMember[]>>();
const listInvites = vi.fn<() => Promise<InvitesResponse>>();
const createInvite = vi.fn<(id: number) => Promise<InviteResult>>();
const createPasswordResetInvite = vi.fn<(id: number) => Promise<InviteResult>>();
const replaceInvite = vi.fn<(id: string) => Promise<InviteResult>>();
const revokeInvite = vi.fn<(id: string) => Promise<void>>();
const dismissInvite = vi.fn<(id: string) => Promise<void>>();
const createMember = vi.fn<(name: string, role: MemberRole) => Promise<InviteResult>>();
const setMemberRole = vi.fn<(id: number, role: MemberRole, confirmTurnHandoff: boolean) => Promise<void>>();

vi.mock("@/api/APIClient", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/APIClient")>();
  return {
    ...actual,
    APIClient: {
      ...actual.APIClient,
      members: {
        ...actual.APIClient.members,
        roster: () => listRoster(),
        create: (name: string, role: MemberRole) => createMember(name, role),
        setRole: (id: number, role: MemberRole, confirmTurnHandoff = false) =>
          setMemberRole(id, role, confirmTurnHandoff),
        setLocalLogin: (id: number, u: string, p: string) => setLogin(id, u, p),
        remove: (id: number) => removeMember(id),
        createInvite: (id: number) => createInvite(id),
        createPasswordResetInvite: (id: number) => createPasswordResetInvite(id),
      },
      invites: {
        ...actual.APIClient.invites,
        list: () => listInvites(),
        replace: (id: string) => replaceInvite(id),
        revoke: (id: string) => revokeInvite(id),
        dismiss: (id: string) => dismissInvite(id),
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

function invite(overrides: Partial<InviteSummary> = {}): InviteSummary {
  return {
    id: "invite-public-handle-1",
    memberId: 7,
    memberName: "Cleo",
    status: "open",
    expiresAt: "2026-08-04T12:00:00Z",
    issuedAt: "2026-08-03T10:00:00Z",
    issuedBy: "Ada",
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
};

async function renderSection(
  members: RosterMember[],
  items: InviteSummary[] = [],
) {
  let queryClient: QueryClient | undefined;
  const overview = { serverNow: "2026-08-03T12:00:00Z", items };
  listRoster.mockResolvedValue(members);
  listInvites.mockResolvedValue(overview);
  const view = await renderWithProviders(<RosterSection />, {
    path: "/admin",
    seed: (client) => {
      queryClient = client;
      client.setQueryData(AuthKeys.me(), admin);
      client.setQueryData(UsersKeys.roster(), members);
      client.setQueryData(InvitesKeys.list(), overview);
    },
  });
  return { ...view, queryClient: queryClient! };
}

async function renderWithoutInviteData(members: RosterMember[]) {
  let queryClient: QueryClient | undefined;
  listRoster.mockResolvedValue(members);
  const view = await renderWithProviders(<RosterSection />, {
    path: "/admin",
    seed: (client) => {
      queryClient = client;
      client.setQueryData(AuthKeys.me(), admin);
      client.setQueryData(UsersKeys.roster(), members);
    },
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
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
  vi.setSystemTime(new Date("2026-08-03T12:00:00Z"));
  setLogin.mockResolvedValue(undefined);
  removeMember.mockResolvedValue({ outcome: "archived" });
  createInvite.mockResolvedValue({ claimUrl: "/claim/created" });
  createPasswordResetInvite.mockResolvedValue({ claimUrl: "/claim/reset" });
  replaceInvite.mockResolvedValue({ claimUrl: "/claim/replaced" });
  revokeInvite.mockResolvedValue(undefined);
  dismissInvite.mockResolvedValue(undefined);
  createMember.mockResolvedValue({ claimUrl: "/claim/new-member" });
  setMemberRole.mockResolvedValue(undefined);
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

  it("does not offer removing the last active admin", async () => {
    await renderSection([
      member({ id: admin.id, name: "Ada", role: "admin" }),
      member({ id: 8, name: "Bea", role: "member" }),
    ]);

    fireEvent.click(screen.getByRole("button", { name: "Actions for Ada" }));

    expect(
      (
        within(screen.getByRole("menu")).getByRole("menuitem", {
          name: "Remove member",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
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

describe("member invitation", () => {
  it("sends the selected starting role with the new member", async () => {
    await renderSection([member()]);

    fireEvent.change(screen.getByLabelText("New member name"), {
      target: { value: "Visiting friend" },
    });
    fireEvent.change(screen.getByLabelText("Starting role"), {
      target: { value: "guest" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add & create link" }));

    await act(async () => {});
    expect(createMember).toHaveBeenCalledWith("Visiting friend", "guest");
    expect((screen.getByLabelText("Starting role") as HTMLSelectElement).value).toBe("member");
  });
});

describe("role changes", () => {
  it("confirms before the current Next up member becomes a guest", async () => {
    setMemberRole.mockRejectedValueOnce(new ApiError(
      409,
      "making this member a Guest will hand Next up to the next eligible member",
      "turn_handoff_confirmation_required",
    ));
    await renderSection([member()]);

    takeAction("Cleo", "Make guest");
    await act(async () => {});

    expect(setMemberRole).toHaveBeenCalledWith(7, "guest", false);
    expect(within(dialog()).getByRole("heading").textContent).toBe("Make Cleo a guest?");
    expect(dialog().textContent).toContain("will not restore this turn");

    fireEvent.click(within(dialog()).getByRole("button", { name: "Make guest" }));
    await act(async () => {});

    expect(setMemberRole).toHaveBeenLastCalledWith(7, "guest", true);
  });

  it("changes a member who is not Next up without a confirmation", async () => {
    await renderSection([member()]);

    takeAction("Cleo", "Make guest");
    await act(async () => {});

    expect(setMemberRole).toHaveBeenCalledWith(7, "guest", false);
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("invite state inside the roster", () => {
  const placeholder = member({
    username: undefined,
    hasLocalLogin: false,
    hasLinkedIdentity: false,
    invitePending: true,
  });

  it("keeps invite status in the Login cell without a second page section", async () => {
    await renderSection([placeholder], [invite()]);

    expect(screen.getByText("Invite link open")).not.toBeNull();
    expect(screen.getByText(/expires in 1 day/)).not.toBeNull();
    expect(screen.queryByRole("heading", { name: "Invites" })).toBeNull();
  });

  it("routes replacement through the immutable invite handle", async () => {
    await renderSection([placeholder], [invite({ id: "invite-public-exact-1" })]);

    takeAction("Cleo", "Create replacement link");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(replaceInvite).toHaveBeenCalledWith("invite-public-exact-1");
    expect(within(dialog()).getByRole("heading").textContent).toContain("Invite link ready for Cleo");
  });

  it("keeps a password reset generation visible and manageable beside the login", async () => {
    await renderSection(
      [member({ invitePending: true })],
      [invite({ id: "reset-public-exact-1" })],
    );

    expect(screen.getByText(/Password reset link open/)).not.toBeNull();
    expect(screen.getByText("Password")).not.toBeNull();
    takeAction("Cleo", "Create replacement reset link");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(replaceInvite).toHaveBeenCalledWith("reset-public-exact-1");
  });

  it("issues an explicit password reset link for a credentialed member", async () => {
    await renderSection([member()], []);

    takeAction("Cleo", "Create password reset link");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(createPasswordResetInvite).toHaveBeenCalledWith(7);
    expect(within(dialog()).getByRole("heading").textContent).toContain(
      "Password reset link ready for Cleo",
    );
    expect(within(dialog()).getByRole("textbox", { name: "Password reset link for Cleo" }))
      .not.toBeNull();
  });

  it("offers only expired-generation actions after the server boundary", async () => {
    await renderSection(
      [{ ...placeholder, invitePending: false }],
      [invite({ id: "invite-expired-exact-1", status: "expired", expiresAt: "2026-08-03T11:00:00Z" })],
    );

    expect(screen.getByText("Invite expired")).not.toBeNull();
    takeAction("Cleo", "Dismiss expired invite");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(dismissInvite).toHaveBeenCalledWith("invite-expired-exact-1");
  });

  it("creates by member only when no current generation exists", async () => {
    await renderSection([{ ...placeholder, invitePending: false }], []);

    takeAction("Cleo", "Create invite link");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(createInvite).toHaveBeenCalledWith(7);
  });

  it("reconciles an unseen current link without offering a duplicate", async () => {
    await renderSection([placeholder], []);

    expect(screen.getByText("Invite status updating…")).not.toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Cleo" }));

    expect(
      within(screen.getByRole("menu")).queryByRole("menuitem", { name: "Create invite link" }),
    ).toBeNull();
  });

  it("shows a neutral reset fallback when a credentialed roster row is ahead", async () => {
    await renderSection([member({ invitePending: true })], []);

    expect(screen.getByText("Password")).not.toBeNull();
    expect(screen.getByText("Reset link status updating…")).not.toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Cleo" }));
    expect(
      within(screen.getByRole("menu")).queryByRole("menuitem", {
        name: "Create password reset link",
      }),
    ).toBeNull();
  });

  it("bounds projection reconciliation to one refresh per mismatch", async () => {
    const matched = member({ invitePending: false });
    const { queryClient } = await renderSection([matched], []);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    listInvites.mockClear();
    listRoster.mockClear();
    listInvites.mockImplementationOnce(() => new Promise<InvitesResponse>(() => {}));
    listRoster.mockImplementationOnce(() => new Promise<RosterMember[]>(() => {}));

    act(() => {
      queryClient.setQueryData(UsersKeys.roster(), [{ ...matched, invitePending: true }]);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(screen.getByText("Reset link status updating…")).not.toBeNull();
    expect(listInvites).toHaveBeenCalledTimes(1);
    expect(listRoster).toHaveBeenCalledTimes(1);

    act(() => {
      queryClient.setQueryData(UsersKeys.roster(), [{ ...matched, invitePending: true }]);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(listInvites).toHaveBeenCalledTimes(1);
    expect(listRoster).toHaveBeenCalledTimes(1);
  });

  it("reconciles again when the same member's mismatch changes generation and direction", async () => {
    const matched = member({ invitePending: false });
    const { queryClient } = await renderSection([matched], []);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    listInvites.mockImplementation(() => new Promise<InvitesResponse>(() => {}));
    listRoster.mockImplementation(() => new Promise<RosterMember[]>(() => {}));
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    act(() => {
      queryClient.setQueryData(UsersKeys.roster(), [{ ...matched, invitePending: true }]);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(invalidate).toHaveBeenCalledTimes(2);

    act(() => {
      queryClient.setQueryData(InvitesKeys.list(), {
        serverNow: "2026-08-03T12:00:00Z",
        items: [invite({ id: "replacement-generation" })],
      });
      queryClient.setQueryData(UsersKeys.roster(), [{ ...matched, invitePending: false }]);
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(screen.getByText("Reset link status updating…")).not.toBeNull();
    expect(invalidate).toHaveBeenCalledTimes(4);
  });

  it("shows the target's busy state while an invite command is unresolved", async () => {
    let resolve!: (result: InviteResult) => void;
    replaceInvite.mockImplementationOnce(
      () => new Promise<InviteResult>((done) => { resolve = done; }),
    );
    await renderSection([placeholder], [invite()]);

    takeAction("Cleo", "Create replacement link");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(screen.getByRole("status").textContent).toContain("Replacing link…");

    const trigger = screen.getByRole("button", { name: "Actions for Cleo" }) as HTMLButtonElement;
    expect(trigger.disabled).toBe(false);
    expect(trigger.getAttribute("aria-disabled")).toBe("true");
    expect(document.activeElement).toBe(trigger);
    fireEvent.click(trigger);
    expect(screen.queryByRole("menu")).toBeNull();
    expect(screen.queryByRole("dialog")).toBeNull();

    await act(async () => {
      resolve({ claimUrl: "/claim/replaced" });
      await vi.advanceTimersByTimeAsync(0);
    });
  });

  it("ticks at expiry, reconciles both views, and keeps menu focus inside", async () => {
    const expiring = invite({ expiresAt: "2026-08-03T12:00:02Z" });
    await renderSection([placeholder], [expiring]);
    listInvites.mockClear();
    listRoster.mockClear();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Cleo" }));
    const revoke = within(screen.getByRole("menu")).getByRole("menuitem", {
      name: "Revoke invite link",
    });
    revoke.focus();

    listInvites.mockResolvedValueOnce({
      serverNow: "2026-08-03T12:00:02Z",
      items: [{ ...expiring, status: "expired" }],
    });
    listRoster.mockResolvedValueOnce([{ ...placeholder, invitePending: false }]);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_025);
    });

    expect(screen.getByText("Invite expired")).not.toBeNull();
    expect(listInvites).toHaveBeenCalledTimes(1);
    expect(listRoster).toHaveBeenCalledTimes(1);
    expect(document.activeElement).toBe(
      within(screen.getByRole("menu")).getByRole("menuitem", { name: "Create new invite link" }),
    );
  });

  it("reconciles stale roster and invite caches after an exact-handle conflict", async () => {
    replaceInvite.mockRejectedValueOnce(new ApiError(409, "invite generation is stale"));
    await renderSection([placeholder], [invite()]);
    listInvites.mockClear();
    listRoster.mockClear();

    takeAction("Cleo", "Create replacement link");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(listInvites).toHaveBeenCalledTimes(1);
    expect(listRoster).toHaveBeenCalledTimes(1);
  });
});

describe("invite overview failure", () => {
  it("shows one retry row only when the roster has invite-dependent members", async () => {
    listInvites.mockRejectedValue(new ApiError(503, "unavailable"));
    await renderWithoutInviteData([
      member({
        username: undefined,
        hasLocalLogin: false,
        hasLinkedIdentity: false,
        invitePending: true,
      }),
    ]);

    expect(screen.getByRole("status").textContent).toContain("Couldn't load invite status.");
    expect(screen.queryByText("Invite details unavailable")).toBeNull();
    expect(screen.getByText("Invite link open")).not.toBeNull();
  });

  it("shows the retry row for a credentialed member who can hold reset links", async () => {
    listInvites.mockRejectedValueOnce(new ApiError(503, "unavailable"));
    await renderWithoutInviteData([member()]);

    expect(screen.getByText("Couldn't load invite status.")).not.toBeNull();
    expect(screen.getByText("Password")).not.toBeNull();
  });

  it("does not add an invite error above an SSO-only roster", async () => {
    listInvites.mockRejectedValueOnce(new ApiError(503, "unavailable"));
    await renderWithoutInviteData([
      member({ username: undefined, hasLocalLogin: false, hasLinkedIdentity: true }),
    ]);

    expect(screen.queryByText("Couldn't load invite status.")).toBeNull();
    expect(screen.getByText("SSO")).not.toBeNull();
  });

  it("warns on a failed background refresh and hides stale invite commands", async () => {
    const reset = invite({ id: "reset-stale-handle" });
    const { queryClient } = await renderSection([member({ invitePending: true })], [reset]);
    await act(async () => {
      await queryClient.cancelQueries({ queryKey: InvitesKeys.list() });
    });
    listInvites.mockRejectedValue(new ApiError(503, "unavailable"));

    await act(async () => {
      await queryClient.refetchQueries({ queryKey: InvitesKeys.list() });
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(queryClient.getQueryState(InvitesKeys.list())?.status).toBe("error");
    expect(screen.getByText("Invite status may be out of date.")).not.toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Actions for Cleo" }));
    expect(
      within(screen.getByRole("menu")).queryByRole("menuitem", {
        name: "Create replacement reset link",
      }),
    ).toBeNull();
    expect(within(screen.getByRole("menu")).getByRole("menuitem", { name: "Reset password" })).not.toBeNull();
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
