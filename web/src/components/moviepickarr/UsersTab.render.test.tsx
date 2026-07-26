/* ============================================================
   Render tests for the Members page: the status line (#230), the rail of
   members beside one pane (#231), the pane's wall of posters (#232) and every
   poster opening the movie modal (#233).

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

   Same split again for the refusals (#234): which reason wins and how it reads
   is pure and refusals.test.ts owns the whole table. What is here is what only
   the rendered board can answer — that the control stays, that it is inert
   without being disabled, that a click does nothing while focus still lands on
   it, and that a refused board is the same markup as an open one.
   ============================================================ */

import { QueryClient } from "@tanstack/react-query";
import { cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { APIClient } from "@/api/APIClient";
import { AuthKeys, SettingsKeys, UsersKeys } from "@/api/query_keys";

import type { DrawPhase } from "@/components/moviepickarr/drawMachine";
import { UsersTab } from "@/components/moviepickarr/UsersTab";

import type { MeResponse, Movie, User } from "@/types/Response";

import { renderWithProviders } from "@/test/providers";

// The draw store is a module singleton the page reads directly. Faked rather
// than driven through the machine: what the board does with a draw in flight is
// a function of the phase alone, and drawMachine.test.ts owns how you get there.
const draw = vi.hoisted(() => ({ phase: "idle" as DrawPhase }));
vi.mock("@/components/moviepickarr/drawStore", () => ({
  drawStore: {
    // No notifications: every test sets the phase before it renders.
    subscribe: () => () => {},
    getState: () => ({ phase: draw.phase }),
  },
}));
afterEach(() => {
  draw.phase = "idle";
});

vi.mock("@/api/APIClient", () => ({
  APIClient: {
    board: { getAll: vi.fn(), moveMovie: vi.fn(), deleteMovie: vi.fn(), updateMovie: vi.fn() },
    settings: { getLock: vi.fn() },
    auth: { me: vi.fn() },
    // The modal lazy-loads the full record on open. It never resolves here, so
    // what the modal shows is the tile's own lean object — which is the point:
    // the poster that was clicked is the film that opens.
    movies: { get: vi.fn(() => new Promise<never>(() => {})) },
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

  it("carries exactly one corner action per tile on your own board and none on a guest's", async () => {
    await renderTab({ users: roster, meID: 1 });

    const tiles = wall().querySelectorAll(".mem-tile");
    expect(tiles.length).toBe(3);
    tiles.forEach((tile, i) => {
      // Two buttons and no more: the poster itself, which opens the record
      // (#233), and the one corner action.
      const controls = within(tile as HTMLElement).getAllByRole("button");
      expect(controls.map((c) => c.getAttribute("aria-label"))).toEqual([
        `Film ${100 + i}`,
        "Move to pool",
      ]);
    });
    // Edit, delete and the link out are gone from the tile: they belong to the
    // movie modal, which is where every poster on this page now goes.
    expect(within(wall()).queryByRole("link")).toBeNull();
    expect(within(wall()).queryByRole("button", { name: "More actions" })).toBeNull();

    cleanup();
    await renderTab({ users: roster, meID: 2, href: "/users?member=1" });
    const guestTiles = wall().querySelectorAll(".mem-tile");
    expect(guestTiles.length).toBe(3);
    guestTiles.forEach((tile, i) => {
      // The poster and nothing beside it: the corner action is the whole of
      // what a guest board is missing.
      const controls = within(tile as HTMLElement).getAllByRole("button");
      expect(controls.map((c) => c.getAttribute("aria-label"))).toEqual([`Film ${100 + i}`]);
    });
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

/* Every poster opens the modal (#233). The point of the ticket is what a board
   you cannot act on is made of: the same buttons as your own, minus the corner
   action. So each case here is asserted on both boards, and the empty pool slot
   is the one cell that answers nothing on either. */
describe("opening a film's record", () => {
  // Ada: two of three pool slots, three in stash. Cleo: one and two.
  const roster = [member(1, 2, 3, "Ada"), member(2, 1, 2, "Cleo Sands")];

  const wall = () => document.querySelector(".mem-wall") as HTMLElement;
  /** The selected member's pool, which is the only drawer that is not inert. */
  const openPool = () => document.querySelector(".mem-drop__inner:not([inert])") as HTMLElement;
  const dialog = () => screen.getByRole("dialog");

  it("makes every filled poster a button, on your own board and on a guest's", async () => {
    await renderTab({ users: roster, meID: 1 });

    // Named by the film, not by an authored verb: the poster is the film, and
    // the role already says it is a button. Both bands, one language.
    expect(
      Array.from(openPool().querySelectorAll<HTMLElement>(".pslot--filled .mem-open")).map((b) =>
        b.getAttribute("aria-label"),
      ),
    ).toEqual(["Film 10", "Film 11"]);
    expect(
      Array.from(wall().querySelectorAll<HTMLElement>(".mem-open")).map((b) =>
        b.getAttribute("aria-label"),
      ),
    ).toEqual(["Film 100", "Film 101", "Film 102"]);

    cleanup();
    await renderTab({ users: roster, meID: 1, href: "/users?member=2" });

    expect(openPool().querySelectorAll(".pslot--filled .mem-open").length).toBe(1);
    expect(wall().querySelectorAll(".mem-open").length).toBe(2);
  });

  it("opens the clicked film from a pool slot, on a board that is not yours", async () => {
    await renderTab({ users: roster, meID: 1, href: "/users?member=2" });

    fireEvent.click(openPool().querySelector(".pslot--filled .mem-open") as HTMLElement);

    await waitFor(() => expect(within(dialog()).getByRole("heading").textContent).toBe("Film 20"));
  });

  it("opens the clicked film from the stash wall, on a board that is not yours", async () => {
    await renderTab({ users: roster, meID: 1, href: "/users?member=2" });

    fireEvent.click(wall().querySelectorAll(".mem-open")[1] as HTMLElement);

    await waitFor(() => expect(within(dialog()).getByRole("heading").textContent).toBe("Film 201"));
  });

  it("closes on Back, so the modal costs one history entry per open", async () => {
    const { router } = await renderTab({ users: roster, meID: 1, href: "/users?member=2" });

    fireEvent.click(wall().querySelectorAll(".mem-open")[0] as HTMLElement);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeNull());

    router.history.back();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    // Back landed on the board it was opened from, not on the page before it:
    // the entry the open pushed carries the same URL and differs only by state.
    expect(router.state.location.href).toBe("/users?member=2");
  });

  it("leaves the empty pool slot the only cell that answers nothing, identically on both boards", async () => {
    await renderTab({ users: roster, meID: 1 });
    const own = Array.from(openPool().querySelectorAll(".pslot--empty")).map((s) => s.outerHTML);
    expect(own.length).toBe(1);

    cleanup();
    await renderTab({ users: roster, meID: 1, href: "/users?member=2" });
    const guest = Array.from(openPool().querySelectorAll(".pslot--empty")).map((s) => s.outerHTML);

    expect(guest.length).toBe(2);
    // Same markup down to the attribute: an empty slot is a statement about the
    // pool, never about who is looking at it.
    expect(new Set([...own, ...guest]).size).toBe(1);
    guest.forEach((slot) => expect(slot).toContain('aria-hidden="true"'));
    expect(openPool().querySelectorAll(".pslot--empty button").length).toBe(0);
  });
});

/* The three refusals (#234): a full pool, a locked round, a draw in flight.
   All three are temporary, so none of them takes a control away — absence is
   the permanent boundary, which the block above pins on a guest board. */
describe("a refused action", () => {
  /** Ada: two of three pool slots filled, two in stash. Her own board. */
  const roster = [member(1, 2, 2, "Ada"), member(2, 0, 0, "Bo")];
  /** Every corner action on the page, pool band then wall, in DOM order. */
  const actions = () => Array.from(document.querySelectorAll<HTMLElement>(".mem-act"));
  const named = () => actions().map((a) => a.getAttribute("aria-label"));

  it("keeps the control where it is and puts the reason on it", async () => {
    await renderTab({ users: roster, meID: 1, locked: true });

    // Four controls, the same four an open round draws: two demotes on the
    // pool band, two promotes on the wall. Locked used to delete the demotes.
    expect(named()).toEqual([
      "Move back to stash, round closed",
      "Move back to stash, round closed",
      "Move to pool, round closed",
      "Move to pool, round closed",
    ]);
    actions().forEach((a) => {
      expect(a.getAttribute("aria-disabled")).toBe("true");
      // The tooltip and the accessible name are one string, so what is hovered
      // and what is spoken cannot drift.
      expect(a.getAttribute("title")).toBe(a.getAttribute("aria-label"));
      // Never natively disabled: that would drop it out of the tab order, kill
      // the focus reveal and leave a keyboard user tabbing a locked wall
      // without ever meeting the action or the reason.
      expect(a.hasAttribute("disabled")).toBe(false);
    });
  });

  it("refuses the click but not the focus", async () => {
    await renderTab({ users: roster, meID: 1, locked: true });
    const promote = actions()[2];

    fireEvent.click(promote);
    expect(APIClient.board.moveMovie).not.toHaveBeenCalled();

    promote.focus();
    expect(document.activeElement).toBe(promote);
  });

  it("refuses every promote on a full pool and none of the demotes", async () => {
    await renderTab({ users: [member(1, 3, 2, "Ada")], meID: 1 });

    expect(named()).toEqual([
      // Demoting is the way out of a full pool, so it is never refused by one.
      "Move back to stash",
      "Move back to stash",
      "Move back to stash",
      "Move to pool, pool is full",
      "Move to pool, pool is full",
    ]);
    expect(actions().filter((a) => a.getAttribute("aria-disabled") === "true").length).toBe(2);
  });

  it("says round closed on a locked full pool, which used to report the full one", async () => {
    await renderTab({ users: [member(1, 3, 2, "Ada")], meID: 1, locked: true });

    // Full is on screen twice already, as three filled slots and three gold
    // pips. Locked is only in the status line, so the control says that.
    expect(new Set(named())).toEqual(
      new Set(["Move back to stash, round closed", "Move to pool, round closed"]),
    );
  });

  it("freezes the pool for a draw and leaves the stash alone", async () => {
    draw.phase = "spinning";
    await renderTab({ users: roster, meID: 1 });

    expect(named()).toEqual([
      "Move back to stash, a draw is in progress",
      "Move back to stash, a draw is in progress",
      // The stash is untouched: a draw never refuses a promote.
      "Move to pool",
      "Move to pool",
    ]);
  });

  it("freezes all three pool tiles identically, so none of them is the winner", async () => {
    draw.phase = "settled";
    await renderTab({ users: [member(1, 3, 0, "Ada")], meID: 1 });

    const demotes = Array.from(document.querySelectorAll<HTMLElement>(".pslot--filled .mem-act"));
    expect(demotes.length).toBe(3);
    // Down to the markup: any per-tile difference at all would say which film
    // was drawn before the reveal.
    expect(new Set(demotes.map((d) => d.outerHTML)).size).toBe(1);
  });

  it("says the draw ahead of the lock on a pool that is both", async () => {
    draw.phase = "revealing";
    await renderTab({ users: roster, meID: 1, locked: true });

    expect(named()).toEqual([
      "Move back to stash, a draw is in progress",
      "Move back to stash, a draw is in progress",
      // A draw does not reach the stash, so the promote falls through to the
      // lock rather than picking up the draw's reason.
      "Move to pool, round closed",
      "Move to pool, round closed",
    ]);
  });

  it("draws nothing for a refusal, on a tile or at board level", async () => {
    /** The board with the reason strings blanked: everything but the words. */
    const boardShape = () =>
      (document.querySelector(".mem__shell") as HTMLElement).innerHTML
        .replace(/ aria-disabled="true"/g, "")
        .replace(/(aria-label|title)="Move[^"]*"/g, '$1=""');

    draw.phase = "spinning";
    await renderTab({ users: roster, meID: 1, locked: true });
    const refused = boardShape();

    cleanup();
    draw.phase = "idle";
    await renderTab({ users: roster, meID: 1 });

    // No glyph on the tile, no chip on the pool head, no banner over the page:
    // every refusal here is true of the whole wall at once, so a mark would be
    // one page-wide fact stamped across sixty posters. Same markup, and the
    // rest is the dim on the control, which is CSS.
    expect(refused).toBe(boardShape());
  });
});
