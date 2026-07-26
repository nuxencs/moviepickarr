/* ============================================================
   Render tests for the Members page: the status line (#230), the rail of
   members beside one pane (#231) and the pane's wall of posters (#232).

   Which words the status line uses is a pure question and poolLock.test.ts
   owns it: the whole state table is asserted there against membersStatus, and
   none of it is repeated here. What that test can't see is the split that
   makes the line bearable to listen to: the visible span carries every clause
   and is silent, while a visually-hidden role="status" carries the round and
   draw clauses alone. Merge them and every promote arriving over SSE re-reads
   the whole string at anyone using a screen reader.

   The rail's rules are split the same way. Which member the URL selects is
   pure and membersSearch.test.ts owns it; what only exists once the page
   renders is here: that a row is a link carrying an explicit id, what a row
   announces, and that a shut drawer is inert. The wall's filter and its miss
   line are pure too and stashWall.test.ts owns those.
   ============================================================ */

import { QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AuthKeys, SettingsKeys, UsersKeys } from "@/api/query_keys";

import { UsersTab } from "@/components/moviepickarr/UsersTab";

import type { MeResponse, Movie, User } from "@/types/Response";

import { renderWithProviders } from "@/test/providers";

vi.mock("@/api/APIClient", () => ({
  APIClient: {
    board: { getAll: vi.fn(), moveMovie: vi.fn(), deleteMovie: vi.fn(), updateMovie: vi.fn() },
    settings: { getLock: vi.fn() },
    auth: { me: vi.fn() },
  },
}));

function movie(movieID: number): Movie {
  return {
    movieID,
    title: `Film ${movieID}`,
    link: "",
    addedAt: "2026-07-01T00:00:00Z",
    addedByID: 1,
    addedByName: "Cleo",
  };
}

/** A member with `pooled` of their three slots filled and `stashed` in stash. */
function member(userID: number, pooled: number, stashed = 0, name = `Member ${userID}`): User {
  const currentPool: Record<string, Movie> = {};
  for (let i = 0; i < pooled; i++) currentPool[`${userID}${i}`] = movie(userID * 10 + i);
  const stash: Record<string, Movie> = {};
  for (let i = 0; i < stashed; i++) stash[`s${userID}${i}`] = movie(userID * 100 + i);
  return { userID, name, currentPool, stash, createdAt: "2026-07-01T00:00:00Z" };
}

function session(id: number): MeResponse {
  return {
    id,
    displayName: `Member ${id}`,
    username: null,
    role: "member",
    hasLocalLogin: true,
    hasLinkedIdentity: false,
    otherSessions: 0,
  };
}

/** Renders the page as the /users route, so its search params resolve. */
async function renderTab({
  users,
  locked = false,
  meID,
  href = "/users",
}: {
  users?: User[];
  locked?: boolean;
  meID?: number;
  href?: `/users` | `/users?${string}`;
}) {
  // Captured out of the seed so a test can push the roster the way SSE does.
  let client!: QueryClient;
  const { router } = await renderWithProviders(<UsersTab />, {
    path: href,
    seed: (queryClient) => {
      client = queryClient;
      // Seeded rather than fetched: the page is the subject, not the requests.
      if (users) queryClient.setQueryData(UsersKeys.list(), users);
      queryClient.setQueryData(SettingsKeys.poolLock(), locked);
      if (meID !== undefined) queryClient.setQueryData(AuthKeys.me(), session(meID));
    },
  });
  return { client, router };
}

/** The visually-hidden live region, whatever it currently says. */
const liveRegion = () => document.querySelector('[role="status"]');

/** The rail's rows, in DOM order. */
const railRows = () => within(screen.getByRole("navigation", { name: "Members" })).getAllByRole("link");

describe("the Members status line", () => {
  it("puts every clause on the visible span and keeps it out of the live region", async () => {
    await renderTab({ users: [member(1, 3), member(2, 3)], locked: true });

    const visible = document.querySelector(".sec-status");
    expect(visible?.textContent).toBe("6 of 6 slots filled · round closed");
    expect(visible?.getAttribute("role")).toBeNull();
    expect(visible?.getAttribute("aria-live")).toBeNull();

    // The round clause alone reaches the region, and the region is hidden.
    expect(liveRegion()?.textContent).toBe("round closed");
    expect(liveRegion()?.className).toContain("vis-hidden");
  });

  it("announces nothing when occupancy moves", async () => {
    const { client } = await renderTab({ users: [member(1, 1), member(2, 0)] });
    expect(document.querySelector(".sec-status")?.textContent).toBe("1 of 6 slots filled");
    expect(liveRegion()?.textContent).toBe("");

    // Somebody else's promote arriving over SSE.
    client.setQueryData(UsersKeys.list(), [member(1, 1), member(2, 1)]);

    await waitFor(() =>
      expect(document.querySelector(".sec-status")?.textContent).toBe("2 of 6 slots filled"),
    );
    expect(liveRegion()?.textContent).toBe("");
  });

  it("says ready to lock, out loud, once every pool fills", async () => {
    const { client } = await renderTab({ users: [member(1, 3), member(2, 2)] });
    expect(liveRegion()?.textContent).toBe("");

    client.setQueryData(UsersKeys.list(), [member(1, 3), member(2, 3)]);

    await waitFor(() => expect(liveRegion()?.textContent).toBe("ready to lock"));
  });

  it("heads a pending roster with a bare Members and no announcement", async () => {
    await renderTab({});

    expect(screen.getByRole("heading", { name: "Members" })).toBeTruthy();
    expect(document.querySelector(".sec-count")).toBeNull();
    expect(document.querySelector(".sec-status")).toBeNull();
    expect(liveRegion()?.textContent).toBe("");
  });
});

describe("the rail of members", () => {
  const roster = [member(1, 1, 14, "Ada"), member(2, 3, 4, "Bo"), member(3, 0, 0, "Cleo")];

  it("sorts the session member first and selects their board on arrival", async () => {
    await renderTab({ users: roster, meID: 2 });

    const rows = railRows();
    expect(rows.map((r) => r.querySelector(".mem-row__nm")?.textContent)).toEqual([
      "Bo",
      "Ada",
      "Cleo",
    ]);
    expect(rows[0].getAttribute("aria-current")).toBe("page");
    expect(rows.slice(1).every((r) => r.getAttribute("aria-current") === null)).toBe(true);
  });

  it("carries an explicit id on every row, the session member's included", async () => {
    await renderTab({ users: roster, meID: 2 });

    expect(railRows().map((r) => r.getAttribute("href"))).toEqual([
      "/users?member=2",
      "/users?member=1",
      "/users?member=3",
    ]);
  });

  it("announces a row from its contents: name, stash depth, pool occupancy", async () => {
    await renderTab({ users: roster, meID: 2 });

    // Three parts, in DOM order, none of them authored as an aria-label, so
    // the visible and the spoken strings cannot drift. The avatar's initials
    // are aria-hidden, or the row would open with "AD".
    //
    // The name is run together here because jsdom has no layout and the
    // accessible-name algorithm separates on display: a browser blockifies
    // these spans (the row is a grid, the text a flex column) and reads
    // "Ada, 14 in stash, 2 of 3 slots filled". What this pins is the parts and
    // their order.
    const ada = railRows()[1];
    expect(ada.getAttribute("aria-label")).toBeNull();
    expect(ada).toBe(screen.getByRole("link", { name: "Ada14 in stash1 of 3 slots filled" }));
    expect(within(ada).getByRole("img").getAttribute("aria-label")).toBe("1 of 3 slots filled");
  });

  it("drops the pips off the open row and keeps its stash count", async () => {
    await renderTab({ users: roster, meID: 2 });

    const [own, ada] = railRows();
    expect(within(own).queryByRole("img")).toBeNull();
    expect(own.textContent).toContain("4 in stash");
    expect(within(ada).queryByRole("img")).not.toBeNull();
  });

  it("keeps every drawer mounted and makes the shut ones inert", async () => {
    await renderTab({ users: roster, meID: 2 });

    const drawers = document.querySelectorAll(".mem-drop__inner");
    expect(drawers.length).toBe(3);
    // Three slots in every drawer, open or shut: the pool is always drawn at
    // its full size, filled or dashed.
    drawers.forEach((d) => expect(d.querySelectorAll(".pslot").length).toBe(3));
    expect(drawers[0].hasAttribute("inert")).toBe(false);
    expect(drawers[1].hasAttribute("inert")).toBe(true);
    expect(drawers[2].hasAttribute("inert")).toBe(true);
  });

  it("draws empty pool slots as dashed cells that say nothing", async () => {
    await renderTab({ users: roster, meID: 3, href: "/users?member=3" });

    const open = document.querySelectorAll(".mem-drop__inner")[0];
    const empties = open.querySelectorAll(".pslot--empty");
    expect(empties.length).toBe(3);
    empties.forEach((slot) => expect(slot.getAttribute("aria-hidden")).toBe("true"));
  });

  it("opens the board the URL names, and only that one", async () => {
    await renderTab({ users: roster, meID: 2, href: "/users?member=3" });

    const rows = railRows();
    expect(rows[2].getAttribute("aria-current")).toBe("page");
    expect(rows[0].getAttribute("aria-current")).toBeNull();
    // The pane is that member's: "Cleo's stash", not the session member's.
    expect(screen.getByRole("region", { name: "Cleo's stash" })).toBeTruthy();
  });

  it("silently falls back to your own board on an id that does not resolve, without rewriting the URL", async () => {
    const { router } = await renderTab({ users: roster, meID: 2, href: "/users?member=404" });

    expect(railRows()[0].getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("region", { name: "Your stash" })).toBeTruthy();
    expect(document.querySelector(".empty.text-destructive")).toBeNull();
    expect(router.state.location.href).toBe("/users?member=404");
  });

  it("pushes a history entry per member, so Back returns to the previous one", async () => {
    const { router } = await renderTab({ users: roster, meID: 2 });

    await router.navigate({ to: "/users", search: { member: 3 } });
    await waitFor(() => expect(railRows()[2].getAttribute("aria-current")).toBe("page"));

    router.history.back();
    await waitFor(() => expect(railRows()[0].getAttribute("aria-current")).toBe("page"));
  });
});

/* The wall (#232). What the cells look like is CSS and belongs to the browser
   pass — the column count, the reserved height and the hover reveal are all
   sizes, and jsdom has no layout. What is here is what the markup decides:
   whose stash the pane says it is, what names it, how many controls a tile
   carries, and which of the four empty states renders. */
describe("the stash wall", () => {
  const roster = [member(1, 1, 3, "Ada"), member(2, 0, 0, "Cleo Sands")];

  /** The pane's wall, whatever it currently holds. */
  const wall = () => document.querySelector(".mem-wall") as HTMLElement;
  const typeFilter = (term: string) =>
    fireEvent.change(screen.getByRole("textbox", { name: /^Search / }), {
      target: { value: term },
    });

  it("heads your own board with Your stash and someone else's with their first name", async () => {
    await renderTab({ users: roster, meID: 1 });
    expect(screen.getByRole("heading", { level: 3 }).textContent).toBe("Your stash");

    cleanup();
    await renderTab({ users: roster, meID: 1, href: "/users?member=2" });
    // The first name, not the full one the rail carries: "Cleo's stash" reads
    // like speech where "Cleo Sands' stash" reads like a record.
    const heading = screen.getByRole("heading", { level: 3 });
    expect(heading.textContent).toBe("Cleo's stash");
    expect(heading.getAttribute("title")).toBe("Cleo's stash");
    // Symmetric emphasis: the possessive token is the marked one on both boards.
    expect(heading.querySelector(".mem-stash__who")?.textContent).toBe("Cleo's");
  });

  it("names the pane by that heading rather than by an authored label", async () => {
    await renderTab({ users: roster, meID: 1 });

    const pane = screen.getByRole("region", { name: "Your stash" });
    expect(pane.getAttribute("aria-label")).toBeNull();
    expect(pane.getAttribute("aria-labelledby")).toBe(
      screen.getByRole("heading", { level: 3 }).id,
    );
  });

  it("carries exactly one control per tile on your own board and none on a guest's", async () => {
    await renderTab({ users: roster, meID: 1 });

    const tiles = wall().querySelectorAll(".mem-tile");
    expect(tiles.length).toBe(3);
    tiles.forEach((tile) => {
      const controls = within(tile as HTMLElement).getAllByRole("button");
      expect(controls.length).toBe(1);
      expect(controls[0].getAttribute("aria-label")).toBe("Promote to pool");
    });
    // Edit, delete and the link out are gone from the tile: they belong to the
    // movie modal, which is where every poster on this page is headed.
    expect(within(wall()).queryByRole("link")).toBeNull();
    expect(within(wall()).queryByRole("button", { name: "More actions" })).toBeNull();

    cleanup();
    await renderTab({ users: roster, meID: 2, href: "/users?member=1" });
    expect(wall().querySelectorAll(".mem-tile").length).toBe(3);
    expect(within(wall()).queryAllByRole("button")).toEqual([]);
  });

  it("puts an unlabelled add tile at cell 0 of your own wall, with a name for it", async () => {
    await renderTab({ users: roster, meID: 1 });

    const add = within(wall()).getByRole("button", { name: "Add to Ada's stash" });
    expect(add.textContent).toBe("");
    expect(wall().firstElementChild).toBe(add);

    // Not on someone else's board: the stash is self-service.
    cleanup();
    await renderTab({ users: roster, meID: 2, href: "/users?member=1" });
    expect(within(wall()).queryByRole("button", { name: /^Add to / })).toBeNull();
  });

  it("suppresses the add tile under any filter, hit or miss", async () => {
    await renderTab({ users: roster, meID: 1 });

    typeFilter("Film 1");
    expect(within(wall()).queryByRole("button", { name: /^Add to / })).toBeNull();
    expect(wall().querySelectorAll(".mem-tile").length).toBe(3);

    typeFilter("zzz");
    expect(within(wall()).queryByRole("button", { name: /^Add to / })).toBeNull();
  });

  it("makes the add tile the whole of your own empty wall", async () => {
    await renderTab({ users: [member(1, 0, 0, "Ada")], meID: 1 });

    expect(within(wall()).getByRole("button", { name: "Add to Ada's stash" })).toBeTruthy();
    // No prose beside it: the add tile is the empty state, not a route to one.
    expect(wall().querySelector(".mem-wall__empty")).toBeNull();
  });

  it("says a guest's empty stash is empty, in one line and without their name", async () => {
    await renderTab({ users: roster, meID: 1, href: "/users?member=2" });

    expect(wall().querySelector(".mem-wall__empty")?.textContent).toBe("This stash is empty");
  });

  it("reads a filter miss the same way on both boards", async () => {
    await renderTab({ users: roster, meID: 1 });
    typeFilter("dune");
    expect(wall().querySelector(".mem-wall__empty")?.textContent).toBe('Nothing matches "dune"');

    cleanup();
    await renderTab({ users: roster, meID: 2, href: "/users?member=1" });
    typeFilter("dune");
    expect(wall().querySelector(".mem-wall__empty")?.textContent).toBe('Nothing matches "dune"');
  });

  it("leaves the pane head one control: the search field", async () => {
    await renderTab({ users: roster, meID: 1 });

    // No sort control, in either direction and under no key. The order is fixed
    // title-ascending and the field is the only way to act on the wall's shape.
    const head = document.querySelector(".mem-stash__head") as HTMLElement;
    expect(within(head).queryAllByRole("button")).toEqual([]);
    expect(within(head).getAllByRole("textbox").length).toBe(1);
  });
});
