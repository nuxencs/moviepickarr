/* ============================================================
   Render tests for the admin invites section.

   invites.test.ts owns the wording and the split; what's left to pin here is
   the part only a render sees. Three things:

   - The section is ABSENT when nobody is waiting, not an empty panel. That is
     the normal state of a settled instance, so a regression to a permanent
     "No pending invites" block would ship to everyone.
   - Which actions a row offers depends on its status, and a row's action has
     to reach the member it came from. An open row must never offer Dismiss,
     and Re-send on Ben's row must not re-invite Cleo.
   - Regenerate reveals the fresh claim link once. Only the hash is stored, so
     if the reveal doesn't open, that link is gone and the admin has to
     regenerate again.
   ============================================================ */

import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { InvitesKeys } from "@/api/query_keys";

import { InvitesSection } from "@/components/moviepickarr/admin/InvitesSection";

import type { InviteResult, InviteSummary } from "@/types/Response";

import { renderWithProviders } from "@/test/providers";

const listInvites = vi.fn<() => Promise<InviteSummary[]>>();
const reissueInvite = vi.fn<(id: number) => Promise<InviteResult>>();
const revokeInvite = vi.fn<(id: number) => Promise<void>>();
const dismissInvite = vi.fn<(id: number) => Promise<void>>();

vi.mock("@/api/APIClient", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api/APIClient")>();
  return {
    ...actual,
    APIClient: {
      members: {
        reissueInvite: (id: number) => reissueInvite(id),
        revokeInvite: (id: number) => revokeInvite(id),
      },
      invites: {
        list: () => listInvites(),
        dismiss: (id: number) => dismissInvite(id),
      },
    },
  };
});

function invite(overrides: Partial<InviteSummary> = {}): InviteSummary {
  return {
    id: 1,
    memberId: 7,
    memberName: "Ben",
    status: "open",
    expiresAt: "2026-08-06T12:00:00Z",
    issuedAt: "2026-08-01T12:00:00Z",
    issuedBy: "Ada",
    ...overrides,
  };
}

async function renderSection(invites: InviteSummary[]) {
  // Seeded AND stubbed: every mutation invalidates this query, so the refetch
  // it triggers has to answer with something. Same rows back, since what
  // happens after the action is the roster's business, not this test's.
  listInvites.mockResolvedValue(invites);
  return renderWithProviders(<InvitesSection />, {
    path: "/admin",
    seed: (client) => client.setQueryData(InvitesKeys.list(), invites),
  });
}

/** Open a row's kebab and take one of its actions. The menu portals to the
 *  body and its entries are menuitems rather than plain buttons. It is looked
 *  up by its own label, not just its role: a previously opened menu is still in
 *  the DOM while it plays its close animation, so a bare role query would find
 *  two. */
function openMenu(name: string) {
  const label = `Actions for ${name}'s invite`;
  fireEvent.click(screen.getByRole("button", { name: label }));
  return within(screen.getByRole("menu", { name: label }));
}

function takeAction(name: string, action: string) {
  fireEvent.click(openMenu(name).getByRole("menuitem", { name: action }));
}

beforeEach(() => {
  // Date only: the relative copy ("expires in 3 days") is read against a fixed
  // now, while waitFor below still needs a real timer to poll on.
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(Date.parse("2026-08-03T12:00:00Z"));
  reissueInvite.mockResolvedValue({ claimUrl: "/claim/fresh-token" });
  revokeInvite.mockResolvedValue(undefined);
  dismissInvite.mockResolvedValue(undefined);
});
afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

describe("when nobody is waiting to set up a login", () => {
  it("renders nothing at all rather than an empty section", async () => {
    await renderSection([]);

    expect(screen.queryByRole("heading", { name: "Invites" })).toBeNull();
  });
});

describe("the two groups", () => {
  it("lists each member once, under the group its invite is in", async () => {
    await renderSection([
      invite({ id: 1, memberId: 7, memberName: "Ben" }),
      invite({ id: 2, memberId: 8, memberName: "Cleo", status: "expired", expiresAt: "2026-08-01T12:00:00Z" }),
    ]);

    expect(screen.getByRole("heading", { name: "Invites" })).toBeTruthy();
    const open = screen.getByRole("heading", { name: /Open/ }).parentElement!;
    const expired = screen.getByRole("heading", { name: /Expired/ }).parentElement!;

    expect(within(open).getByText("Ben")).toBeTruthy();
    expect(within(open).queryByText("Cleo")).toBeNull();
    expect(within(expired).getByText("Cleo")).toBeTruthy();
    expect(within(open).getByText("expires in 3 days")).toBeTruthy();
    expect(within(expired).getByText("expired 2 days ago")).toBeTruthy();
  });

  it("omits the issued line when the invite records no issuer", async () => {
    await renderSection([invite({ issuedBy: undefined })]);

    expect(screen.getByText("expires in 3 days")).toBeTruthy();
    expect(screen.queryByText(/issued by/)).toBeNull();
    // Never "issued by System": nothing recorded an issuer, so nothing is named.
    expect(screen.queryByText(/System/)).toBeNull();
  });
});

describe("row actions", () => {
  it("offers regenerate and revoke on an open row, and never a way to see the old link", async () => {
    await renderSection([invite()]);

    const menu = openMenu("Ben");
    expect(menu.getByRole("menuitem", { name: "Regenerate invite" })).toBeTruthy();
    expect(menu.getByRole("menuitem", { name: "Revoke invite" })).toBeTruthy();
    expect(menu.queryByRole("menuitem", { name: "Dismiss invite" })).toBeNull();
    // Only the token's hash is stored, so there is no link left to copy. An
    // action implying otherwise would be a promise the server cannot keep.
    expect(menu.queryByRole("menuitem", { name: /copy/i })).toBeNull();
  });

  it("offers re-send and dismiss on an expired row", async () => {
    await renderSection([invite({ status: "expired", expiresAt: "2026-08-01T12:00:00Z" })]);

    const menu = openMenu("Ben");
    expect(menu.getByRole("menuitem", { name: "Re-send invite" })).toBeTruthy();
    expect(menu.getByRole("menuitem", { name: "Dismiss invite" })).toBeTruthy();
    expect(menu.queryByRole("menuitem", { name: "Revoke invite" })).toBeNull();
  });

  it("acts on the member whose row it came from", async () => {
    await renderSection([
      invite({ id: 1, memberId: 7, memberName: "Ben" }),
      invite({ id: 2, memberId: 8, memberName: "Cleo", status: "expired", expiresAt: "2026-08-01T12:00:00Z" }),
    ]);

    takeAction("Cleo", "Dismiss invite");
    // Dismiss is addressed by INVITE id, not member id: the member-scoped
    // revoke reaches valid invites only, which an expired one isn't.
    await waitFor(() => expect(dismissInvite).toHaveBeenCalledWith(2));

    takeAction("Ben", "Revoke invite");
    await waitFor(() => expect(revokeInvite).toHaveBeenCalledWith(7));
  });

  it("reveals the fresh claim link once after a regenerate", async () => {
    await renderSection([invite()]);

    takeAction("Ben", "Regenerate invite");
    await waitFor(() => expect(reissueInvite).toHaveBeenCalledWith(7));

    const dialog = await screen.findByRole("dialog");
    expect(dialog.textContent).toContain("Ben");
    expect(dialog.textContent).toContain(`${window.location.origin}/claim/fresh-token`);
  });
});
