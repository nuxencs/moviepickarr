/* Render tests for the Members page (#230-#236). The pure rules (status
   words, member selection, wall filter and keys, refusal reasons) have their
   own tests; this file pins only what needs the rendered page. jsdom has no
   layout, so the wall is one column here. */

import { onlineManager, QueryClient } from "@tanstack/react-query";
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { APIClient } from "@/api/APIClient";
import { AuthKeys, SettingsKeys, UsersKeys } from "@/api/query_keys";

import { UsersTab } from "@/components/moviepickarr/UsersTab";
import { toast } from "@/components/ui/toast-api";

import type { MeResponse, MovieTile, User } from "@/types/Response";

import { renderWithProviders } from "@/test/providers";

vi.mock("@/api/APIClient", () => ({
  APIClient: {
    board: { getAll: vi.fn(), moveMovie: vi.fn(), deleteMovie: vi.fn(), updateMovie: vi.fn() },
    settings: { getPoolState: vi.fn() },
    auth: { me: vi.fn() },
    // Never resolves, so the modal shows the clicked tile's own lean object.
    movies: { get: vi.fn(() => new Promise<never>(() => {})) },
  },
}));

afterEach(() => {
  onlineManager.setOnline(true);
  vi.mocked(APIClient.board.moveMovie).mockReset();
  vi.restoreAllMocks();
});

function deferred<T = void>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((onResolve, onReject) => {
    resolve = onResolve;
    reject = onReject;
  });
  return { promise, resolve, reject };
}

function movie(movieID: number): MovieTile {
  return {
    movieID,
    title: `Movie ${movieID}`,
    link: "",
    addedAt: "2026-07-01T00:00:00Z",
    addedByID: 1,
    addedByName: "Cleo",
  };
}

/** A member with `pooled` of their three slots filled and `stashed` in stash. */
function member(userID: number, pooled: number, stashed = 0, name = `Member ${userID}`): User {
  const currentPool: Record<string, MovieTile> = {};
  for (let i = 0; i < pooled; i++) currentPool[`${userID}${i}`] = movie(userID * 10 + i);
  const stash: Record<string, MovieTile> = {};
  for (let i = 0; i < stashed; i++) stash[`s${userID}${i}`] = movie(userID * 100 + i);
  return { userID, name, currentPool, stash, createdAt: "2026-07-01T00:00:00Z" };
}

function session(id: number, role: MeResponse["role"] = "member"): MeResponse {
  return {
    id,
    displayName: `Member ${id}`,
    username: null,
    role,
    hasLocalLogin: true,
    hasLinkedIdentity: false,
  };
}

/** Renders the page as the /users route, so its search params resolve. */
async function renderTab({
  users,
  locked = false,
  drawInProgress = false,
  seedPoolState = true,
  meID,
  role = "member",
  href = "/users",
}: {
  users?: User[];
  locked?: boolean;
  drawInProgress?: boolean;
  seedPoolState?: boolean;
  meID?: number;
  role?: MeResponse["role"];
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
      if (seedPoolState) {
        queryClient.setQueryData(SettingsKeys.poolLock(), { poolLocked: locked, drawInProgress });
      }
      if (meID !== undefined) queryClient.setQueryData(AuthKeys.me(), session(meID, role));
    },
  });
  return { client, router };
}

const liveRegion = () => document.querySelector('[role="status"]');

/** Rail rows by class: each drawer also holds a stash link (#236), and jsdom ignores inert. */
const railRows = () =>
  within(screen.getByRole("navigation", { name: "Members" }))
    .getAllByRole("link")
    .filter((link) => link.classList.contains("mem-row__link"));

describe("the Members status line", () => {
  // Split so an SSE promote does not re-read the whole line to a screen reader.
  it("puts every clause on the visible span and keeps it out of the live region", async () => {
    await renderTab({ users: [member(1, 3), member(2, 3)], locked: true });

    const visible = document.querySelector(".sec-status");
    expect(visible?.textContent).toBe("6 of 6 slots filled · round closed");
    expect(visible?.getAttribute("role")).toBeNull();
    expect(visible?.getAttribute("aria-live")).toBeNull();

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

/* jsdom has no layout: this pins what the skeleton is made of. Pixel parity
   with the loaded page is a browser check. */
describe("the Members loading skeleton", () => {
  const skeleton = () => document.querySelector(".mem-skel");

  it("takes the page's own containers, so the shape is the layout's and not a copy of it", async () => {
    await renderTab({});

    const skel = skeleton();
    expect(skel).toBeTruthy();
    expect(skel?.classList.contains("mem__shell")).toBe(true);
    expect(skel?.classList.contains("mem__shell--with-head")).toBe(false);
    expect(skel?.querySelector(".mem-rail")).toBeTruthy();
    expect(skel?.querySelector(".mem-pane")).toBeTruthy();
    expect(skel?.querySelector(".mem-wallbox")).toBeTruthy();
    // A clip, not a scroller: the overdrawn filler must not be reachable.
    expect(skel?.querySelector(".mem-wallbox")?.classList.contains("mem-skel__wall")).toBe(true);
    // No fade, which would promise a scroller there is none of.
    expect(skel?.querySelector("[data-overflow]")).toBeNull();
  });

  it("is member-agnostic, with no name and no accent line on any row", async () => {
    await renderTab({ meID: 1 });

    const rows = skeleton()?.querySelectorAll(".mem-row") ?? [];
    expect(rows.length).toBe(6);
    // Row 0 included, though the session resolves before the route renders.
    expect(skeleton()?.querySelectorAll("a").length).toBe(0);
    expect(skeleton()?.textContent).toBe("");
    expect(skeleton()?.querySelector("[data-active]")).toBeNull();
  });

  it("opens exactly one drawer, at row 0", async () => {
    await renderTab({});

    const rows = Array.from(skeleton()?.querySelectorAll(".mem-row") ?? []);
    const open = rows.map((row) => !!row.querySelector('.mem-drop[data-open="true"]'));
    expect(open).toEqual([true, false, false, false, false, false]);
  });

  it("shimmers the pips and the pool slots rather than drawing the marks they stand in for", async () => {
    await renderTab({});

    // An empty pip or dashed cell would claim an empty pool.
    const pips = skeleton()?.querySelectorAll(".mem-pips > *") ?? [];
    expect(pips.length).toBeGreaterThan(0);
    for (const pip of pips) expect(pip.classList.contains("skel")).toBe(true);
    expect(skeleton()?.querySelector(".mem-pip")).toBeNull();
    expect(skeleton()?.querySelector(".pslot--empty")).toBeNull();

    const slots = skeleton()?.querySelectorAll(".mem-pool > *") ?? [];
    expect(slots.length).toBe(3);
    for (const slot of slots) expect(slot.classList.contains("skel")).toBe(true);
  });

  it("overdraws the wall by a fixed shape, wired to no stash", async () => {
    await renderTab({});

    expect(skeleton()?.querySelectorAll(".mem-wall > *").length).toBe(126);
  });

  it("stays out of the accessibility tree and leaves the live region in it", async () => {
    await renderTab({});

    expect(skeleton()?.getAttribute("aria-hidden")).toBe("true");
    // The push removes the head, so the region is its sibling, not its child.
    const region = liveRegion();
    expect(region).toBeTruthy();
    expect(region?.closest(".mem-skel")).toBeNull();
    expect(region?.closest(".sec-head")).toBeNull();
  });

  it("spends the flight on the screen a deep link is arriving at", async () => {
    // Below 761 CSS draws the screen off this flag, read from the URL while the roster loads.
    await renderTab({ href: "/users?member=2&stash=true" });

    expect(document.querySelector(".mem")?.getAttribute("data-pushed")).toBe("true");
    expect(skeleton()).toBeTruthy();
    // Still rendered: members.css hides the head on that screen in both states.
    expect(document.querySelector(".sec-head")).toBeTruthy();
  });

  it("draws the rail's screen when the URL names no stash", async () => {
    await renderTab({ href: "/users?member=2" });

    expect(document.querySelector(".mem")?.getAttribute("data-pushed")).toBe("false");
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

    // No authored aria-label, so visible and spoken text cannot drift. jsdom has
    // no layout, so it runs the parts together; a browser separates them.
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
    // The pool is always drawn at full size, filled or dashed.
    drawers.forEach((d) => expect(d.querySelectorAll(".pslot").length).toBe(3));
    expect(drawers[0].hasAttribute("inert")).toBe(false);
    expect(drawers[1].hasAttribute("inert")).toBe(true);
    expect(drawers[2].hasAttribute("inert")).toBe(true);
  });

  it("rechecks rail overflow only after the drawer size transition", async () => {
    await renderTab({ users: roster, meID: 2 });
    const rail = screen.getByRole("navigation", { name: "Members" });
    const heightRead = vi.spyOn(rail, "scrollHeight", "get").mockReturnValue(100);

    // Count only reads caused by the transition events below.
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    heightRead.mockClear();

    fireEvent.transitionEnd(rail.querySelector(".mem-row__link") as HTMLElement, {
      propertyName: "color",
    });
    expect(heightRead).not.toHaveBeenCalled();

    const drawer = rail.querySelector(".mem-drop") as HTMLElement;
    fireEvent.transitionEnd(drawer, { propertyName: "opacity" });
    expect(heightRead).not.toHaveBeenCalled();

    fireEvent.transitionEnd(drawer, { propertyName: "grid-template-rows" });
    expect(heightRead).toHaveBeenCalledTimes(1);
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

/* The wall (#232). Sizes and hover are CSS and belong to the browser pass;
   this pins the markup. */
describe("the stash wall", () => {
  const roster = [member(1, 1, 3, "Ada"), member(2, 0, 0, "Cleo Sands")];

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
    // First name only: "Cleo Sands' stash" reads like a record.
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
      // The poster (opens the record, #233) and the one corner action.
      const controls = within(tile as HTMLElement).getAllByRole("button");
      expect(controls.map((c) => c.getAttribute("aria-label"))).toEqual([
        `Movie ${100 + i}`,
        "Move to pool",
      ]);
    });
    // Edit, delete and the link out live in the movie modal.
    expect(within(wall()).queryByRole("link")).toBeNull();
    expect(within(wall()).queryByRole("button", { name: "More actions" })).toBeNull();

    cleanup();
    await renderTab({ users: roster, meID: 2, href: "/users?member=1" });
    const guestTiles = wall().querySelectorAll(".mem-tile");
    expect(guestTiles.length).toBe(3);
    guestTiles.forEach((tile, i) => {
      const controls = within(tile as HTMLElement).getAllByRole("button");
      expect(controls.map((c) => c.getAttribute("aria-label"))).toEqual([`Movie ${100 + i}`]);
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

    typeFilter("Movie 1");
    expect(within(wall()).queryByRole("button", { name: /^Add to / })).toBeNull();
    expect(wall().querySelectorAll(".mem-tile").length).toBe(3);

    typeFilter("zzz");
    expect(within(wall()).queryByRole("button", { name: /^Add to / })).toBeNull();
  });

  it("makes the add tile the whole of your own empty wall", async () => {
    await renderTab({ users: [member(1, 0, 0, "Ada")], meID: 1 });

    expect(within(wall()).getByRole("button", { name: "Add to Ada's stash" })).toBeTruthy();
    // No prose: the add tile is the empty state.
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

    // No sort control: the order is fixed title-ascending.
    const head = document.querySelector(".mem-stash__head") as HTMLElement;
    expect(within(head).queryAllByRole("button")).toEqual([]);
    expect(within(head).getAllByRole("textbox").length).toBe(1);
  });
});

/* Every poster opens the modal (#233). Each case runs on both boards: a guest
   board is your own minus the corner action. */
describe("opening a movie's record", () => {
  // Ada: two of three pool slots, three in stash. Cleo: one and two.
  const roster = [member(1, 2, 3, "Ada"), member(2, 1, 2, "Cleo Sands")];

  const wall = () => document.querySelector(".mem-wall") as HTMLElement;
  const openPool = () => document.querySelector(".mem-drop__inner:not([inert])") as HTMLElement;
  const dialog = () => screen.getByRole("dialog");

  it("sizes pool and stash poster requests for their rendered slots", async () => {
    const ada = member(1, 1, 1, "Ada");
    Object.values(ada.currentPool)[0]!.posterPath = "/pool.jpg";
    Object.values(ada.stash)[0]!.posterPath = "/stash.jpg";

    await renderTab({ users: [ada], meID: 1 });

    const candidates = (path: string) =>
      `https://image.tmdb.org/t/p/w154/${path} 154w, ` +
      `https://image.tmdb.org/t/p/w185/${path} 185w, ` +
      `https://image.tmdb.org/t/p/w342/${path} 342w, ` +
      `https://image.tmdb.org/t/p/w500/${path} 500w`;
    const poolPoster = screen.getByRole("img", { name: "Movie 10" });
    const stashPoster = screen.getByRole("img", { name: "Movie 100" });

    expect(poolPoster.getAttribute("src")).toBe("https://image.tmdb.org/t/p/w342/pool.jpg");
    expect(poolPoster.getAttribute("srcset")).toBe(candidates("pool.jpg"));
    expect(poolPoster.getAttribute("sizes")).toBe(
      "auto, (max-width: 700px) calc((100vw - 92px) / 3), " +
        "(min-width: 761px) and (max-width: 900px) 112px, " +
        "(min-width: 761px) 128px, calc((100vw - 120px) / 3)",
    );

    expect(stashPoster.getAttribute("src")).toBe("https://image.tmdb.org/t/p/w342/stash.jpg");
    expect(stashPoster.getAttribute("srcset")).toBe(candidates("stash.jpg"));
    expect(stashPoster.getAttribute("sizes")).toBe(
      "auto, (max-width: 700px) calc((100vw - 66px) / 4), " +
        "(min-width: 761px) and (max-width: 899px) 120px, " +
        "(min-width: 761px) and (max-width: 1199px) 112px, " +
        "(min-width: 761px) 128px, calc((100vw - 94px) / 4)",
    );
  });

  it("makes every filled poster a button, on your own board and on a guest's", async () => {
    await renderTab({ users: roster, meID: 1 });

    // Named by the movie, not a verb: the role already says button.
    expect(
      Array.from(openPool().querySelectorAll<HTMLElement>(".pslot--filled .mem-open")).map((b) =>
        b.getAttribute("aria-label"),
      ),
    ).toEqual(["Movie 10", "Movie 11"]);
    expect(
      Array.from(wall().querySelectorAll<HTMLElement>(".mem-open")).map((b) =>
        b.getAttribute("aria-label"),
      ),
    ).toEqual(["Movie 100", "Movie 101", "Movie 102"]);

    cleanup();
    await renderTab({ users: roster, meID: 1, href: "/users?member=2" });

    expect(openPool().querySelectorAll(".pslot--filled .mem-open").length).toBe(1);
    expect(wall().querySelectorAll(".mem-open").length).toBe(2);
  });

  it("opens the clicked movie from a pool slot, on a board that is not yours", async () => {
    await renderTab({ users: roster, meID: 1, href: "/users?member=2" });

    fireEvent.click(openPool().querySelector(".pslot--filled .mem-open") as HTMLElement);

    await waitFor(() => expect(within(dialog()).getByRole("heading").textContent).toBe("Movie 20"));
  });

  it("opens the clicked movie from the stash wall, on a board that is not yours", async () => {
    await renderTab({ users: roster, meID: 1, href: "/users?member=2" });

    fireEvent.click(wall().querySelectorAll(".mem-open")[1] as HTMLElement);

    await waitFor(() => expect(within(dialog()).getByRole("heading").textContent).toBe("Movie 201"));
  });

  it("closes on Back, so the modal costs one history entry per open", async () => {
    const { router } = await renderTab({ users: roster, meID: 1, href: "/users?member=2" });

    fireEvent.click(wall().querySelectorAll(".mem-open")[0] as HTMLElement);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeNull());

    router.history.back();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    // The pushed entry has the same URL and differs only by state.
    expect(router.state.location.href).toBe("/users?member=2");
  });

  /* The adder link (#238) navigates with replace, so it spends the modal's own
     history entry and reads as the modal closing onto that board. */
  it("goes from a movie to whoever added it, closing the record onto their board", async () => {
    const { router } = await renderTab({ users: roster, meID: 1 });

    fireEvent.click(wall().querySelectorAll(".mem-open")[0] as HTMLElement);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeNull());

    // The fixture names member 1 ("Cleo") as the adder of every movie.
    fireEvent.click(within(dialog()).getByRole("link", { name: "Cleo" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(router.state.location.href).toBe("/users?member=1");
    // Replaced, not stacked: Back leaves the page rather than reopening the record.
    expect(router.state.location.state.movieModal).toBeUndefined();
  });

  it("leaves the empty pool slot the only cell that answers nothing, identically on both boards", async () => {
    await renderTab({ users: roster, meID: 1 });
    const own = Array.from(openPool().querySelectorAll(".pslot--empty")).map((s) => s.outerHTML);
    expect(own.length).toBe(1);

    cleanup();
    await renderTab({ users: roster, meID: 1, href: "/users?member=2" });
    const guest = Array.from(openPool().querySelectorAll(".pslot--empty")).map((s) => s.outerHTML);

    expect(guest.length).toBe(2);
    // Same markup: an empty slot says nothing about who is looking.
    expect(new Set([...own, ...guest]).size).toBe(1);
    guest.forEach((slot) => expect(slot).toContain('aria-hidden="true"'));
    expect(openPool().querySelectorAll(".pslot--empty button").length).toBe(0);
  });
});

/* The three refusals (#234). All are temporary, so none removes a control:
   absence is for guest boards only. */
describe("a refused action", () => {
  /** Ada: two of three pool slots filled, two in stash. Her own board. */
  const roster = [member(1, 2, 2, "Ada"), member(2, 0, 0, "Bo")];
  /** Every corner action, pool band then wall. */
  const actions = () => Array.from(document.querySelectorAll<HTMLElement>(".mem-act"));
  const named = () => actions().map((a) => a.getAttribute("aria-label"));

  it("keeps the control where it is and puts the reason on it", async () => {
    await renderTab({ users: roster, meID: 1, locked: true });

    // The same four controls an open round draws: two demotes, two promotes.
    expect(named()).toEqual([
      "Move back to stash, round closed",
      "Move back to stash, round closed",
      "Move to pool, round closed",
      "Move to pool, round closed",
    ]);
    actions().forEach((a) => {
      expect(a.getAttribute("aria-disabled")).toBe("true");
      // One string for tooltip and name, so they cannot drift.
      expect(a.getAttribute("title")).toBe(a.getAttribute("aria-label"));
      // Never natively disabled: that drops it from the tab order, so a keyboard
      // user never meets the reason.
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

  it("keeps a Guest's demotes live and explains why promotion is unavailable", async () => {
    await renderTab({ users: roster, meID: 1, role: "guest" });

    expect(named()).toEqual([
      "Move back to stash",
      "Move back to stash",
      "Move to pool, guest role cannot add movies to the pool",
      "Move to pool, guest role cannot add movies to the pool",
    ]);
  });

  it("says round closed on a locked full pool, which used to report the full one", async () => {
    await renderTab({ users: [member(1, 3, 2, "Ada")], meID: 1, locked: true });

    // Full already shows as slots and pips; locked is only in the status line.
    expect(new Set(named())).toEqual(
      new Set(["Move back to stash, round closed", "Move to pool, round closed"]),
    );
  });

  it("freezes a server-held pool when no reel animation runs, and leaves the stash alone", async () => {
    vi.mocked(APIClient.settings.getPoolState).mockResolvedValueOnce({
      poolLocked: false,
      drawInProgress: true,
    });
    await renderTab({ users: roster, meID: 1, seedPoolState: false });

    await waitFor(() =>
      expect(named()).toEqual([
        "Move back to stash, a draw is in progress",
        "Move back to stash, a draw is in progress",
        // The stash is untouched: a draw never refuses a promote.
        "Move to pool",
        "Move to pool",
      ]),
    );
    expect(document.querySelector(".sec-status")?.textContent).toContain("draw in progress");
  });

  it("keeps the roster readable while the round state is still loading", async () => {
    vi.mocked(APIClient.settings.getPoolState).mockReturnValueOnce(
      new Promise<never>(() => {}),
    );
    await renderTab({ users: roster, meID: 1, seedPoolState: false });

    expect(screen.getByRole("link", { name: /Ada/ })).not.toBeNull();
    expect(new Set(named())).toEqual(
      new Set([
        "Move back to stash, round state unavailable",
        "Move to pool, round state unavailable",
      ]),
    );
  });

  it("fails pooled controls closed when the round-state request errors", async () => {
    vi.mocked(APIClient.settings.getPoolState).mockRejectedValueOnce(
      new Error("round state unavailable"),
    );
    await renderTab({ users: roster, meID: 1, seedPoolState: false });

    await waitFor(() =>
      expect(document.querySelector(".sec-status")?.textContent).toBe(
        "Round state failed to load",
      ),
    );
    expect(
      named().every((label) => label?.endsWith("round state unavailable") === true),
    ).toBe(true);
  });

  it("fails pooled controls closed during a background round-state refresh", async () => {
    const { client } = await renderTab({ users: roster, meID: 1 });
    vi.mocked(APIClient.settings.getPoolState).mockReturnValueOnce(
      new Promise<never>(() => {}),
    );

    act(() => {
      void client.invalidateQueries({ queryKey: SettingsKeys.poolLock() });
    });

    await waitFor(() =>
      expect(
        named().every((label) => label?.endsWith("round state unavailable") === true),
      ).toBe(true),
    );
  });

  it("freezes all three pool tiles identically, so none of them is the winner", async () => {
    await renderTab({
      users: [member(1, 3, 0, "Ada")],
      meID: 1,
      drawInProgress: true,
    });

    const demotes = Array.from(document.querySelectorAll<HTMLElement>(".pslot--filled .mem-act"));
    expect(demotes.length).toBe(3);
    // Any per-tile difference would give away the drawn movie before the reveal.
    expect(new Set(demotes.map((d) => d.outerHTML)).size).toBe(1);
  });

  it("says the draw ahead of the lock on a pool that is both", async () => {
    await renderTab({ users: roster, meID: 1, locked: true, drawInProgress: true });

    expect(named()).toEqual([
      "Move back to stash, a draw is in progress",
      "Move back to stash, a draw is in progress",
      // A draw does not reach the stash, so the promote falls through to the lock.
      "Move to pool, round closed",
      "Move to pool, round closed",
    ]);
  });

  it("draws nothing for a refusal, on a tile or at board level", async () => {
    /** The board with the reason strings blanked: everything but the words. */
    const boardShape = () => {
      const board = document.querySelector(".mem__shell")?.cloneNode(true) as HTMLElement;
      // The status line holds refusal words too; this compares the boards only.
      board.querySelector(".sec-head")?.remove();
      return board.innerHTML
        .replace(/ aria-disabled="true"/g, "")
        .replace(/(aria-label|title)="Move[^"]*"/g, '$1=""');
    };

    await renderTab({ users: roster, meID: 1, locked: true, drawInProgress: true });
    const refused = boardShape();

    cleanup();
    await renderTab({ users: roster, meID: 1 });

    // A refusal is page-wide, so it gets no mark: the only difference is the CSS dim.
    expect(refused).toBe(boardShape());
  });
});

describe("a pending move", () => {
  it("sends one request per movie while repeated activation stays focusable", async () => {
    vi.mocked(APIClient.board.moveMovie).mockImplementation(
      () => new Promise<never>(() => {}),
    );
    await renderTab({ users: [member(1, 1, 2, "Ada")], meID: 1 });

    const promote = document.querySelector(".mem-tile .mem-act") as HTMLElement;
    fireEvent.click(promote);
    fireEvent.click(promote);
    await waitFor(() => expect(APIClient.board.moveMovie).toHaveBeenCalled());

    expect(APIClient.board.moveMovie).toHaveBeenCalledTimes(1);
    expect(APIClient.board.moveMovie).toHaveBeenLastCalledWith(100, "pool");
    promote.focus();
    expect(document.activeElement).toBe(promote);

    const demote = document.querySelector(".pslot--filled .mem-act") as HTMLElement;
    fireEvent.click(demote);
    fireEvent.click(demote);
    await waitFor(() =>
      expect(APIClient.board.moveMovie).toHaveBeenCalledWith(10, "stash"),
    );

    expect(APIClient.board.moveMovie).toHaveBeenCalledTimes(2);
    demote.focus();
    expect(document.activeElement).toBe(demote);
  });

  it("keeps a hidden and restored stash movie inside the same pending request", async () => {
    vi.mocked(APIClient.board.moveMovie).mockImplementation(
      () => new Promise<never>(() => {}),
    );
    await renderTab({ users: [member(1, 1, 2, "Ada")], meID: 1 });

    fireEvent.click(document.querySelector(".mem-tile .mem-act") as HTMLElement);
    const field = screen.getByRole("textbox", { name: "Search Ada's stash" });
    fireEvent.change(field, { target: { value: "Movie 101" } });
    expect(screen.queryByRole("button", { name: "Movie 100" })).toBeNull();

    fireEvent.change(field, { target: { value: "" } });
    fireEvent.click(document.querySelector(".mem-tile .mem-act") as HTMLElement);

    await waitFor(() => expect(APIClient.board.moveMovie).toHaveBeenCalled());
    expect(APIClient.board.moveMovie).toHaveBeenCalledTimes(1);
    expect(APIClient.board.moveMovie).toHaveBeenCalledWith(100, "pool");
  });

  it("keeps a movie pending while its keyed stash pane is switched away and back", async () => {
    vi.mocked(APIClient.board.moveMovie).mockImplementation(
      () => new Promise<never>(() => {}),
    );
    const { router } = await renderTab({
      users: [member(1, 1, 2, "Ada"), member(2, 0, 1, "Bo")],
      meID: 1,
    });

    fireEvent.click(document.querySelector(".mem-tile .mem-act") as HTMLElement);
    await waitFor(() => expect(APIClient.board.moveMovie).toHaveBeenCalledTimes(1));
    await router.navigate({ to: "/users", search: { member: 2 } });
    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 3 }).textContent).toBe("Bo's stash"),
    );
    await router.navigate({ to: "/users", search: { member: 1 } });
    await waitFor(() =>
      expect(screen.getByRole("heading", { level: 3 }).textContent).toBe("Your stash"),
    );

    fireEvent.click(document.querySelector(".mem-tile .mem-act") as HTMLElement);
    await act(async () => {
      await Promise.resolve();
    });

    expect(APIClient.board.moveMovie).toHaveBeenCalledTimes(1);
    expect(APIClient.board.moveMovie).toHaveBeenCalledWith(100, "pool");
  });

  it("keeps an offline move pending and sends it once after reconnecting", async () => {
    onlineManager.setOnline(false);
    vi.mocked(APIClient.board.moveMovie).mockImplementation(
      () => new Promise<never>(() => {}),
    );
    await renderTab({ users: [member(1, 1, 2, "Ada")], meID: 1 });
    const promote = document.querySelector(".mem-tile .mem-act") as HTMLElement;

    fireEvent.click(promote);
    fireEvent.click(promote);
    await act(async () => {
      await Promise.resolve();
    });
    expect(APIClient.board.moveMovie).not.toHaveBeenCalled();

    onlineManager.setOnline(true);

    await waitFor(() => expect(APIClient.board.moveMovie).toHaveBeenCalledTimes(1));
    expect(APIClient.board.moveMovie).toHaveBeenCalledWith(100, "pool");
  });
});

/* Keyboard and focus on the wall (#235). Focus moves only when the element
   under it goes away. */
describe("moving around the wall with the keyboard", () => {
  /** Ada's board, holding exactly these movies. */
  function ada({ pool = [], stash = [] }: { pool?: number[]; stash?: number[] }): User {
    return {
      userID: 1,
      name: "Ada",
      createdAt: "2026-07-01T00:00:00Z",
      currentPool: Object.fromEntries(pool.map((id) => [`p${id}`, movie(id)])),
      stash: Object.fromEntries(stash.map((id) => [`s${id}`, movie(id)])),
    };
  }
  const bo = member(2, 0, 2, "Bo");
  const roster = [ada({ pool: [10], stash: [100, 101, 102, 103] }), bo];

  const wall = () => document.querySelector(".mem-wall") as HTMLElement;
  const heading = () => screen.getByRole("heading", { level: 3 });
  const openPool = () => document.querySelector(".mem-drop__inner:not([inert])") as HTMLElement;
  /** The wall's cells, in DOM order: the add tile, then the movies. */
  const cells = () => Array.from(wall().querySelectorAll<HTMLElement>("[data-cell]"));
  const tabStops = () => Array.from(wall().querySelectorAll<HTMLElement>('[tabindex="0"]'));
  const named = (el: Element | null) => el?.getAttribute("aria-label") ?? null;
  const press = (key: string) =>
    fireEvent.keyDown((document.activeElement as HTMLElement) ?? wall(), { key });
  const typeFilter = (term: string) =>
    fireEvent.change(screen.getByRole("textbox", { name: /^Search / }), {
      target: { value: term },
    });

  it("is a list, not a grid", async () => {
    await renderTab({ users: roster, meID: 1 });

    // The wall is an A-Z list; grid coordinates would announce the CSS column count.
    expect(document.querySelector('[role="grid"]')).toBeNull();
    expect(wall().getAttribute("role")).toBeNull();
    expect(wall().querySelector('[role="row"], [role="gridcell"]')).toBeNull();
  });

  it("costs two tab stops on your own board and one on a guest's", async () => {
    await renderTab({ users: roster, meID: 1 });

    // The index starts on the add tile, which carries no corner action.
    expect(tabStops().map(named)).toEqual(["Add to Ada's stash"]);

    cells()[0].focus();
    press("ArrowRight");
    // The whole wall is two stops: this poster and its corner action.
    expect(tabStops().map(named)).toEqual(["Movie 100", "Move to pool"]);
    // Nine controls: two per movie plus the add tile.
    expect(wall().querySelectorAll(".mem-open, .mem-act, .mem-addtile").length).toBe(9);

    cleanup();
    await renderTab({ users: roster, meID: 2, href: "/users?member=1" });
    expect(tabStops().map(named)).toEqual(["Movie 100"]);
  });

  it("moves the one tab stop with the arrows, and with Home and End", async () => {
    await renderTab({ users: roster, meID: 1 });
    cells()[0].focus();

    press("ArrowRight");
    expect(document.activeElement).toBe(cells()[1]);
    expect(cells()[0].getAttribute("tabindex")).toBe("-1");

    press("End");
    expect(named(document.activeElement)).toBe("Movie 103");

    press("Home");
    expect(named(document.activeElement)).toBe("Add to Ada's stash");

    // jsdom has one column, so a row is a cell (multi-column: stashWall.test.ts).
    press("ArrowDown");
    expect(named(document.activeElement)).toBe("Movie 100");
    press("ArrowUp");
    expect(named(document.activeElement)).toBe("Add to Ada's stash");
  });

  it("stays put at the ends rather than wrapping", async () => {
    await renderTab({ users: roster, meID: 1 });
    cells()[0].focus();

    press("ArrowLeft");
    expect(document.activeElement).toBe(cells()[0]);

    press("End");
    press("ArrowRight");
    expect(named(document.activeElement)).toBe("Movie 103");
  });

  it("answers the arrows from the corner action too", async () => {
    await renderTab({ users: roster, meID: 1 });
    cells()[0].focus();
    press("ArrowRight");

    // Tab from the poster lands here, so arrows must work here too.
    const action = wall().querySelector(".mem-tile .mem-act") as HTMLElement;
    action.focus();
    press("ArrowRight");
    expect(named(document.activeElement)).toBe("Movie 101");
  });

  it("takes the index to wherever focus lands, so a pointer and the arrows agree", async () => {
    await renderTab({ users: roster, meID: 1 });

    // A click leaves focus on the poster; the next arrow must start from there.
    cells()[3].focus();
    await waitFor(() => expect(tabStops().map(named)).toEqual(["Movie 102", "Move to pool"]));

    press("ArrowRight");
    expect(named(document.activeElement)).toBe("Movie 103");

    // The corner action counts as its tile's cell.
    const action = wall().querySelectorAll<HTMLElement>(".mem-tile .mem-act")[0];
    action.focus();
    await waitFor(() => expect(tabStops()).toContain(action));
    press("ArrowRight");
    expect(named(document.activeElement)).toBe("Movie 101");
  });

  it("keeps the focused movie as the tab stop when an earlier movie is removed", async () => {
    const { client } = await renderTab({ users: roster, meID: 1 });
    const focused = within(wall()).getByRole("button", { name: "Movie 102" });
    focused.focus();
    await waitFor(() =>
      expect(tabStops().map(named)).toEqual(["Movie 102", "Move to pool"]),
    );

    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [10], stash: [101, 102, 103] }),
      bo,
    ]);

    await waitFor(() => expect(focused.getAttribute("data-cell")).toBe("2"));
    expect(document.activeElement).toBe(focused);
    expect(tabStops().map(named)).toEqual(["Movie 102", "Move to pool"]);
    press("ArrowRight");
    expect(named(document.activeElement)).toBe("Movie 103");
  });

  it("keeps the focused movie as the tab stop when an earlier movie is inserted", async () => {
    const { client } = await renderTab({ users: roster, meID: 1 });
    const focused = within(wall()).getByRole("button", { name: "Movie 102" });
    focused.focus();
    await waitFor(() =>
      expect(tabStops().map(named)).toEqual(["Movie 102", "Move to pool"]),
    );

    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [10], stash: [1, 100, 101, 102, 103] }),
      bo,
    ]);

    await waitFor(() => expect(focused.getAttribute("data-cell")).toBe("4"));
    expect(document.activeElement).toBe(focused);
    expect(tabStops().map(named)).toEqual(["Movie 102", "Move to pool"]);
    press("ArrowRight");
    expect(named(document.activeElement)).toBe("Movie 103");
  });

  it("resets the index to the first cell on a filter change", async () => {
    await renderTab({ users: roster, meID: 1 });
    cells()[0].focus();
    press("End");

    typeFilter("Movie 10");
    // No add tile under a filter, so the first cell is the first match.
    expect(tabStops().map(named)).toEqual(["Movie 100", "Move to pool"]);
  });

  it("resets the index on a member switch", async () => {
    const { router } = await renderTab({ users: roster, meID: 1 });
    cells()[0].focus();
    press("End");

    await router.navigate({ to: "/users", search: { member: 2 } });
    await waitFor(() => expect(heading().textContent).toBe("Bo's stash"));
    expect(tabStops().map(named)).toEqual(["Movie 200"]);
  });

  it("puts Tab out of the field on Add on your own board and on the first match on a guest's", async () => {
    await renderTab({ users: roster, meID: 1 });
    // Inside the roving list, so it is the same stop the arrows move.
    expect(named(tabStops()[0])).toBe("Add to Ada's stash");

    cleanup();
    await renderTab({ users: roster, meID: 2, href: "/users?member=1" });
    expect(named(tabStops()[0])).toBe("Movie 100");
  });

  it("is not a tab stop at all with no matches", async () => {
    await renderTab({ users: roster, meID: 1 });

    typeFilter("zzz");
    expect(tabStops()).toEqual([]);
    // Nothing focusable either, so Tab passes the wall.
    expect(within(wall()).queryAllByRole("button")).toEqual([]);
    expect(wall().querySelector(".mem-wall__empty")?.textContent).toBe('Nothing matches "zzz"');
  });

  it("hands focus to the movie taking the vacated cell after a promote", async () => {
    const { client } = await renderTab({ users: roster, meID: 1 });

    const promote = wall().querySelectorAll<HTMLElement>(".mem-tile .mem-act")[1];
    promote.focus();
    fireEvent.click(promote);
    // The roster arriving over SSE, separate from the move request.
    client.setQueryData(UsersKeys.list(), [ada({ pool: [10, 101], stash: [100, 102, 103] }), bo]);

    // The poster, not its corner action: once the pool fills, that action is refused.
    await waitFor(() => expect(named(document.activeElement)).toBe("Movie 102"));
    expect((document.activeElement as HTMLElement).className).toBe("mem-open");
  });

  it("falls back to the previous cell when the promoted movie was the last one", async () => {
    const { client } = await renderTab({ users: roster, meID: 1 });
    const promote = wall().querySelectorAll<HTMLElement>(".mem-tile .mem-act")[3];

    promote.focus();
    fireEvent.click(promote);
    client.setQueryData(UsersKeys.list(), [ada({ pool: [10, 103], stash: [100, 101, 102] }), bo]);

    await waitFor(() => expect(named(document.activeElement)).toBe("Movie 102"));
  });

  it("falls back to the pane heading when the promote empties the wall", async () => {
    const { client } = await renderTab({ users: roster, meID: 1 });

    // Filtered, because your own unfiltered wall always keeps the add tile.
    typeFilter("Movie 103");
    const promote = wall().querySelector(".mem-tile .mem-act") as HTMLElement;
    promote.focus();
    fireEvent.click(promote);
    client.setQueryData(UsersKeys.list(), [ada({ pool: [10, 103], stash: [100, 101, 102] }), bo]);

    await waitFor(() => expect(document.activeElement).toBe(heading()));
  });

  it("leaves a landing alone when focus has moved on since the click", async () => {
    const { client } = await renderTab({ users: roster, meID: 1 });

    fireEvent.click(wall().querySelectorAll<HTMLElement>(".mem-tile .mem-act")[1]);
    // The roster lands after the person has moved on to typing.
    const field = screen.getByRole("textbox", { name: /^Search / });
    field.focus();
    client.setQueryData(UsersKeys.list(), [ada({ pool: [10, 101], stash: [100, 102, 103] }), bo]);

    await waitFor(() => expect(wall().querySelectorAll(".mem-tile").length).toBe(3));
    expect(document.activeElement).toBe(field);
  });

  it("does not steal focus from another stash movie while a promote lands", async () => {
    const { client } = await renderTab({ users: roster, meID: 1 });

    fireEvent.click(wall().querySelector(".mem-tile .mem-act") as HTMLElement);
    const destination = within(wall()).getByRole("button", { name: "Movie 103" });
    destination.focus();
    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [10, 100], stash: [101, 102, 103] }),
      bo,
    ]);

    await waitFor(() => expect(wall().querySelectorAll(".mem-tile").length).toBe(3));
    expect(document.activeElement).toBe(destination);
  });

  it("does not reclaim focus after a pending promote was deliberately blurred", async () => {
    const { client } = await renderTab({ users: roster, meID: 1 });
    const promote = wall().querySelector(".mem-tile .mem-act") as HTMLElement;

    promote.focus();
    fireEvent.click(promote);
    promote.blur();
    expect(document.activeElement).toBe(document.body);
    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [10, 100], stash: [101, 102, 103] }),
      bo,
    ]);

    await waitFor(() => expect(wall().querySelectorAll(".mem-tile")).toHaveLength(3));
    expect(document.activeElement).toBe(document.body);
  });

  it("does not treat an unfocused promote activation as a lost pane focus", async () => {
    vi.mocked(APIClient.board.moveMovie).mockImplementation(
      () => new Promise<never>(() => {}),
    );
    const { client } = await renderTab({ users: roster, meID: 1 });

    expect(document.activeElement).toBe(document.body);
    fireEvent.click(wall().querySelector(".mem-tile .mem-act") as HTMLElement);
    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [10, 100], stash: [101, 102, 103] }),
      bo,
    ]);

    await waitFor(() => expect(wall().querySelectorAll(".mem-tile")).toHaveLength(3));
    expect(document.activeElement).toBe(document.body);
  });

  it("keeps a newer promote landing when an older request fails", async () => {
    const first = deferred<void>();
    const second = deferred<void>();
    const move = vi.mocked(APIClient.board.moveMovie);
    move.mockImplementation((movieID) =>
      movieID === 100 ? first.promise : second.promise,
    );
    const errorToast = vi.spyOn(toast, "error").mockImplementation(() => 0);
    const { client } = await renderTab({ users: roster, meID: 1 });
    const promotes = wall().querySelectorAll<HTMLElement>(".mem-tile .mem-act");

    fireEvent.click(promotes[0]);
    promotes[1].focus();
    fireEvent.click(promotes[1]);
    await waitFor(() => expect(move).toHaveBeenCalledTimes(2));

    first.reject(new Error("first move failed"));
    await waitFor(() => expect(errorToast).toHaveBeenCalledTimes(1));
    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [10, 101], stash: [100, 102, 103] }),
      bo,
    ]);

    await waitFor(() => expect(named(document.activeElement)).toBe("Movie 102"));
  });

  it("rebases a newer promote landing after an older movie leaves first", async () => {
    vi.mocked(APIClient.board.moveMovie).mockImplementation(
      () => new Promise<never>(() => {}),
    );
    const { client } = await renderTab({ users: roster, meID: 1 });
    const promotes = wall().querySelectorAll<HTMLElement>(".mem-tile .mem-act");

    fireEvent.click(promotes[0]);
    promotes[1].focus();
    fireEvent.click(promotes[1]);
    await waitFor(() => expect(APIClient.board.moveMovie).toHaveBeenCalledTimes(2));

    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [10, 100], stash: [101, 102, 103] }),
      bo,
    ]);
    await waitFor(() =>
      expect(within(wall()).queryByRole("button", { name: "Movie 100" })).toBeNull(),
    );
    expect(document.activeElement).toBe(promotes[1]);

    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [10, 100, 101], stash: [102, 103] }),
      bo,
    ]);

    await waitFor(() => expect(named(document.activeElement)).toBe("Movie 102"));
  });

  it("returns focus ownership to a repeated pending promote without another request", async () => {
    vi.mocked(APIClient.board.moveMovie).mockImplementation(
      () => new Promise<never>(() => {}),
    );
    const { client } = await renderTab({ users: roster, meID: 1 });
    const promotes = wall().querySelectorAll<HTMLElement>(".mem-tile .mem-act");

    fireEvent.click(promotes[0]);
    fireEvent.click(promotes[1]);
    promotes[0].focus();
    fireEvent.click(promotes[0]);
    await waitFor(() => expect(APIClient.board.moveMovie).toHaveBeenCalledTimes(2));

    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [10, 100], stash: [101, 102, 103] }),
      bo,
    ]);

    await waitFor(() => expect(named(document.activeElement)).toBe("Movie 101"));
  });

  it("hands focus to the next filled slot after a demote", async () => {
    const { client } = await renderTab({ users: [ada({ pool: [10, 11], stash: [100] }), bo], meID: 1 });
    const demote = openPool().querySelector(".pslot--filled .mem-act") as HTMLElement;

    demote.focus();
    fireEvent.click(demote);
    client.setQueryData(UsersKeys.list(), [ada({ pool: [11], stash: [10, 100] }), bo]);

    // An empty slot is not focusable, so focus goes to the movie now in that slot.
    await waitFor(() => expect(named(document.activeElement)).toBe("Movie 11"));
  });

  it("does not steal focus from another pool movie while a demote lands", async () => {
    const { client } = await renderTab({
      users: [ada({ pool: [10, 11, 12], stash: [100] }), bo],
      meID: 1,
    });

    fireEvent.click(openPool().querySelector(".pslot--filled .mem-act") as HTMLElement);
    const destination = within(openPool()).getByRole("button", { name: "Movie 12" });
    destination.focus();
    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [11, 12], stash: [10, 100] }),
      bo,
    ]);

    await waitFor(() =>
      expect(openPool().querySelectorAll(".pslot--filled")).toHaveLength(2),
    );
    expect(document.activeElement).toBe(destination);
  });

  it("does not reclaim focus after a pending demote was deliberately blurred", async () => {
    const { client } = await renderTab({
      users: [ada({ pool: [10, 11], stash: [100] }), bo],
      meID: 1,
    });
    const demote = openPool().querySelector(".pslot--filled .mem-act") as HTMLElement;

    demote.focus();
    fireEvent.click(demote);
    demote.blur();
    expect(document.activeElement).toBe(document.body);
    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [11], stash: [10, 100] }),
      bo,
    ]);

    await waitFor(() =>
      expect(openPool().querySelectorAll(".pslot--filled")).toHaveLength(1),
    );
    expect(document.activeElement).toBe(document.body);
  });

  it("does not treat an unfocused demote activation as lost pool focus", async () => {
    vi.mocked(APIClient.board.moveMovie).mockImplementation(
      () => new Promise<never>(() => {}),
    );
    const { client } = await renderTab({
      users: [ada({ pool: [10, 11], stash: [100] }), bo],
      meID: 1,
    });

    expect(document.activeElement).toBe(document.body);
    fireEvent.click(openPool().querySelector(".pslot--filled .mem-act") as HTMLElement);
    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [11], stash: [10, 100] }),
      bo,
    ]);

    await waitFor(() =>
      expect(openPool().querySelectorAll(".pslot--filled")).toHaveLength(1),
    );
    expect(document.activeElement).toBe(document.body);
  });

  it("rebases a pending demote after a promoted movie sorts ahead of it", async () => {
    vi.mocked(APIClient.board.moveMovie).mockImplementation(
      () => new Promise<never>(() => {}),
    );
    const { client } = await renderTab({
      users: [ada({ pool: [11, 12], stash: [10, 100] }), bo],
      meID: 1,
    });
    const demote = openPool().querySelector<HTMLElement>(".pslot--filled .mem-act")!;

    demote.focus();
    fireEvent.click(demote);
    fireEvent.click(wall().querySelector(".mem-tile .mem-act") as HTMLElement);
    await waitFor(() => expect(APIClient.board.moveMovie).toHaveBeenCalledTimes(2));

    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [10, 11, 12], stash: [100] }),
      bo,
    ]);
    await waitFor(() =>
      expect(within(openPool()).getByRole("button", { name: "Movie 10" })).toBeTruthy(),
    );
    expect(document.activeElement).toBe(demote);

    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [10, 12], stash: [11, 100] }),
      bo,
    ]);

    await waitFor(() => expect(named(document.activeElement)).toBe("Movie 12"));
  });

  it("keeps a newer demote landing when an older request fails", async () => {
    const first = deferred<void>();
    const second = deferred<void>();
    const move = vi.mocked(APIClient.board.moveMovie);
    move.mockImplementation((movieID) =>
      movieID === 10 ? first.promise : second.promise,
    );
    const errorToast = vi.spyOn(toast, "error").mockImplementation(() => 0);
    const { client } = await renderTab({
      users: [ada({ pool: [10, 11, 12], stash: [100] }), bo],
      meID: 1,
    });
    const demotes = openPool().querySelectorAll<HTMLElement>(
      ".pslot--filled .mem-act",
    );

    fireEvent.click(demotes[0]);
    demotes[1].focus();
    fireEvent.click(demotes[1]);
    await waitFor(() => expect(move).toHaveBeenCalledTimes(2));

    first.reject(new Error("first move failed"));
    await waitFor(() => expect(errorToast).toHaveBeenCalledTimes(1));
    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [10, 12], stash: [11, 100] }),
      bo,
    ]);

    await waitFor(() => expect(named(document.activeElement)).toBe("Movie 12"));
  });

  it("returns focus ownership to a repeated pending demote without another request", async () => {
    vi.mocked(APIClient.board.moveMovie).mockImplementation(
      () => new Promise<never>(() => {}),
    );
    const { client } = await renderTab({
      users: [ada({ pool: [10, 11, 12], stash: [100] }), bo],
      meID: 1,
    });
    const demotes = openPool().querySelectorAll<HTMLElement>(
      ".pslot--filled .mem-act",
    );

    fireEvent.click(demotes[0]);
    fireEvent.click(demotes[1]);
    demotes[0].focus();
    fireEvent.click(demotes[0]);
    await waitFor(() => expect(APIClient.board.moveMovie).toHaveBeenCalledTimes(2));

    client.setQueryData(UsersKeys.list(), [
      ada({ pool: [11, 12], stash: [10, 100] }),
      bo,
    ]);

    await waitFor(() => expect(named(document.activeElement)).toBe("Movie 11"));
  });

  it("hands focus to the member's own row when the demote empties the pool", async () => {
    const { client } = await renderTab({ users: [ada({ pool: [10], stash: [] }), bo], meID: 1 });
    const demote = openPool().querySelector(".pslot--filled .mem-act") as HTMLElement;

    demote.focus();
    fireEvent.click(demote);
    client.setQueryData(UsersKeys.list(), [ada({ pool: [], stash: [10] }), bo]);

    await waitFor(() => expect(document.activeElement).toBe(railRows()[0]));
  });

  it("sends focus to the pane heading when the tile under it is taken away", async () => {
    const { client } = await renderTab({ users: roster, meID: 1 });
    cells()[1].focus();

    // Another person's edit, or another tab. Left alone, focus falls to the document.
    client.setQueryData(UsersKeys.list(), [ada({ pool: [10], stash: [101, 102, 103] }), bo]);

    await waitFor(() => expect(document.activeElement).toBe(heading()));
  });

  it("sends focus to the new pane's heading when a member switch unmounts the tile under it", async () => {
    const { router } = await renderTab({ users: roster, meID: 1 });
    cells()[1].focus();

    await router.navigate({ to: "/users", search: { member: 2 } });

    await waitFor(() => expect(heading().textContent).toBe("Bo's stash"));
    expect(document.activeElement).toBe(heading());
  });

  it("leaves focus where the pointer put it, rather than chasing an unmount", async () => {
    const { client } = await renderTab({ users: roster, meID: 1 });
    cells()[1].focus();

    // Focus left the wall before the tile went, so nothing is handed on.
    screen.getByRole("textbox", { name: /^Search / }).focus();
    client.setQueryData(UsersKeys.list(), [ada({ pool: [10], stash: [101, 102, 103] }), bo]);

    await waitFor(() => expect(wall().querySelectorAll(".mem-tile").length).toBe(3));
    expect(document.activeElement).toBe(screen.getByRole("textbox", { name: /^Search / }));
  });
});

/* The mobile push (#236). Below 761px the rail and the pushed board are two
   screens: `member` selects whose pool the rail opens, `stash` pushes to their
   movies. Layout is CSS and belongs to the browser pass; this pins the flag,
   the live region, the back bar and focus. */
describe("the mobile push", () => {
  const pushQuery = "not all and (min-width: 761px)";
  const levelFourPushQuery = "not (min-width: 761px)";
  const roster = [member(1, 1, 3, "Ada"), member(2, 2, 2, "Bo")];
  const heading = () => screen.getByRole("heading", { level: 3 });
  const pushedFlag = () => document.querySelector(".mem")?.getAttribute("data-pushed");
  /** The open drawer's link to that member's stash. */
  const toStash = () =>
    document.querySelector(".mem-drop__inner:not([inert]) .mem-tostash") as HTMLAnchorElement;
  const openPool = () => document.querySelector(".mem-drop__inner:not([inert])") as HTMLElement;

  // jsdom answers every media query "no" (see setupDom); a phone flips only the push queries.
  const realMatchMedia = window.matchMedia;
  const mediaListeners = new Map<string, Set<EventListenerOrEventListenerObject>>();
  let mediaWidth = Number.POSITIVE_INFINITY;
  let mediaSupportsLevelFourBoolean = true;
  const matchesAtWidth = (
    query: string,
    width: number,
    supportsLevelFourBoolean: boolean,
  ) => {
    if (query === pushQuery) return width < 761;
    if (query === levelFourPushQuery) return supportsLevelFourBoolean && width < 761;
    if (query === "(max-width: 760px)") return width <= 760;
    return realMatchMedia(query).matches;
  };
  const atWidth = (width: number, supportsLevelFourBoolean = true) => {
    mediaWidth = width;
    mediaSupportsLevelFourBoolean = supportsLevelFourBoolean;
    window.matchMedia = ((query: string) => ({
      get matches() {
        return matchesAtWidth(query, mediaWidth, mediaSupportsLevelFourBoolean);
      },
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: (
        type: string,
        listener: EventListenerOrEventListenerObject,
      ) => {
        if (type !== "change") return;
        const listeners = mediaListeners.get(query) ?? new Set();
        listeners.add(listener);
        mediaListeners.set(query, listeners);
      },
      removeEventListener: (
        type: string,
        listener: EventListenerOrEventListenerObject,
      ) => {
        if (type === "change") mediaListeners.get(query)?.delete(listener);
      },
      dispatchEvent: () => false,
    })) as typeof window.matchMedia;
  };
  const resizeTo = (width: number, supportsLevelFourBoolean = true) => {
    const previousWidth = mediaWidth;
    const previousSupport = mediaSupportsLevelFourBoolean;
    mediaWidth = width;
    mediaSupportsLevelFourBoolean = supportsLevelFourBoolean;

    act(() => {
      for (const [query, listeners] of mediaListeners) {
        const before = matchesAtWidth(query, previousWidth, previousSupport);
        const after = matchesAtWidth(query, width, supportsLevelFourBoolean);
        if (before === after) continue;
        const event = { matches: after, media: query } as MediaQueryListEvent;
        for (const listener of [...listeners]) {
          if (typeof listener === "function") listener(event);
          else listener.handleEvent(event);
        }
      }
    });
  };
  const mediaListenerCount = (query: string) => mediaListeners.get(query)?.size ?? 0;
  const onAPhone = () => atWidth(375);
  // Zoom or a fractional DPR can give 760.5px: pushed in CSS, but above `max-width: 760px`.
  // Level 4 syntax is off because Vite 7's browser baseline predates it.
  const atFractionalPushWidth = () => atWidth(760.5, false);
  afterEach(() => {
    window.matchMedia = realMatchMedia;
    mediaListeners.clear();
  });

  it("reads the pushed state off the URL rather than holding a flag", async () => {
    const { router } = await renderTab({ users: roster, meID: 1 });
    expect(pushedFlag()).toBe("false");

    await router.navigate({ to: "/users", search: { member: 2, stash: true } });
    await waitFor(() => expect(pushedFlag()).toBe("true"));

    router.history.back();
    await waitFor(() => expect(pushedFlag()).toBe("false"));
  });

  it("selects a member without leaving the rail, so every pool stays reachable", async () => {
    const { router } = await renderTab({ users: roster, meID: 1 });

    // A rail row carries only the member: on a phone it opens the pool in place.
    expect(railRows().map((r) => r.getAttribute("href"))).toEqual([
      "/users?member=1",
      "/users?member=2",
    ]);

    await router.navigate({ to: "/users", search: { member: 2 } });
    await waitFor(() => expect(railRows()[1].getAttribute("aria-current")).toBe("page"));
    expect(pushedFlag()).toBe("false");
    expect(openPool().querySelectorAll(".pslot--filled").length).toBe(2);
  });

  it("pushes from the open drawer, and only from the open one", async () => {
    const { router } = await renderTab({ users: roster, meID: 1, href: "/users?member=2" });

    expect(toStash().getAttribute("href")).toBe("/users?member=2&stash=true");
    // Every drawer holds one so the rail height stays put; shut drawers are inert.
    expect(document.querySelectorAll(".mem-tostash").length).toBe(2);
    expect(toStash().textContent).toBe("Stash2");

    fireEvent.click(toStash());
    await waitFor(() => expect(pushedFlag()).toBe("true"));
    expect(router.state.location.href).toBe("/users?member=2&stash=true");
  });

  it("keeps the live region out of the head the pushed screen removes", async () => {
    await renderTab({ users: roster, meID: 1, locked: true, href: "/users?member=2&stash=true" });

    // The head is display: none here, and a hidden live region announces nothing.
    expect(liveRegion()?.closest(".sec-head")).toBeNull();
    expect(liveRegion()?.textContent).toBe("round closed");
  });

  it("keeps the Members head on the rail screen", async () => {
    await renderTab({ users: roster, meID: 1 });

    const railScreen = document.querySelector(".mem-rail-screen");
    expect(railScreen?.parentElement?.classList.contains("mem__shell--with-head")).toBe(true);
    expect(document.querySelector(".sec-head")?.parentElement).toBe(railScreen);
    expect(document.querySelector(".mem-rail")?.parentElement).toBe(railScreen);
  });

  it("puts the way back and the occupancy pips in the back bar", async () => {
    const { router } = await renderTab({
      users: roster,
      meID: 1,
      href: "/users?member=2&stash=true",
    });

    // The pane is keyed on the member, so the bar is re-queried after a switch.
    const bar = () => document.querySelector(".mem-backbar") as HTMLElement;
    // The rail is a screen away, so the pips are the only occupancy signal here.
    expect(within(bar()).getByRole("button").textContent).toBe("All members");
    expect(within(bar()).getByRole("img").getAttribute("aria-label")).toBe("2 of 3 slots filled");

    // History back, not a link to the rail: on a cold deep link that leaves the app.
    await router.navigate({ to: "/users", search: { member: 1, stash: true } });
    await waitFor(() => expect(heading().textContent).toBe("Your stash"));
    fireEvent.click(within(bar()).getByRole("button"));
    await waitFor(() => expect(heading().textContent).toBe("Bo's stash"));
  });

  it("carries the stash count in the pane heading", async () => {
    await renderTab({ users: roster, meID: 1, href: "/users?member=2&stash=true" });

    // Beside the rail CSS hides this count; on the pushed screen the heading carries it.
    const id = heading().closest(".mem-stash__id") as HTMLElement;
    expect(id.querySelector(".sec-count")?.textContent).toBe("2");
  });

  it("moves focus to the pane heading on the push", async () => {
    onAPhone();
    const { router } = await renderTab({ users: roster, meID: 1 });

    await router.navigate({ to: "/users", search: { member: 2, stash: true } });

    // The rail is gone, and the heading is where a screen reader meets the self-mark.
    await waitFor(() => expect(document.activeElement).toBe(heading()));
    expect(heading().textContent).toBe("Bo's stash");
  });

  it("keeps the focus handoff aligned with CSS at fractional widths", async () => {
    atFractionalPushWidth();
    const { router } = await renderTab({ users: roster, meID: 1, href: "/users?member=2" });
    const left = toStash();
    left.focus();

    await router.navigate({ to: "/users", search: { member: 2, stash: true } });
    await waitFor(() => expect(document.activeElement).toBe(heading()));

    router.history.back();
    await waitFor(() => expect(document.activeElement).toBe(left));
  });

  it("treats a pop between two boards as an entry", async () => {
    onAPhone();
    const { router } = await renderTab({
      users: roster,
      meID: 1,
      href: "/users?member=1&stash=true",
    });

    await router.navigate({ to: "/users", search: { member: 2, stash: true } });
    await waitFor(() => expect(heading().textContent).toBe("Bo's stash"));

    router.history.back();
    // Back onto a board is an arrival, so the heading takes focus as on a push.
    await waitFor(() => expect(heading().textContent).toBe("Your stash"));
    expect(document.activeElement).toBe(heading());
  });

  it("restores focus to the stash link of the board you were on", async () => {
    onAPhone();
    const { router } = await renderTab({ users: roster, meID: 1, href: "/users?member=2" });
    const left = toStash();
    await router.navigate({ to: "/users", search: { member: 2, stash: true } });
    await waitFor(() => expect(heading().textContent).toBe("Bo's stash"));

    router.history.back();

    await waitFor(() => expect(document.activeElement).toBe(left));
    // jsdom does not model `inert`, so check the drawer is the open one.
    expect(left.closest(".mem-drop__inner")?.hasAttribute("inert")).toBe(false);
  });

  it("restores nothing when the board you left is no longer the open one", async () => {
    onAPhone();
    const { router } = await renderTab({ users: roster, meID: 1, href: "/users?member=2" });
    const bosLink = toStash();
    await router.navigate({ to: "/users", search: { member: 2, stash: true } });
    await waitFor(() => expect(heading().textContent).toBe("Bo's stash"));

    // Only reachable by resizing mid-stack. Bo's drawer is shut and inert, so
    // focusing its link would fake a restore; where focus lands is #235's rule.
    await router.navigate({ to: "/users", search: { member: 1 } });

    await waitFor(() => expect(pushedFlag()).toBe("false"));
    expect(bosLink.closest(".mem-drop__inner")?.hasAttribute("inert")).toBe(true);
    expect(document.activeElement).not.toBe(bosLink);
  });

  it("moves nothing when the rail itself changes member", async () => {
    onAPhone();
    const { router } = await renderTab({ users: roster, meID: 1 });
    const rail = railRows()[1];
    rail.focus();

    // Nothing was taken away, so nothing is handed on.
    await router.navigate({ to: "/users", search: { member: 2 } });
    await waitFor(() => expect(railRows()[1].getAttribute("aria-current")).toBe("page"));
    expect(document.activeElement).toBe(rail);
  });

  it("leaves focus alone beside the rail", async () => {
    const { router } = await renderTab({ users: roster, meID: 1 });
    const rail = railRows()[1];
    rail.focus();

    await router.navigate({ to: "/users", search: { member: 2, stash: true } });
    await waitFor(() => expect(heading().textContent).toBe("Bo's stash"));

    // Both columns are on screen, so a switch moves no focus.
    expect(document.activeElement).toBe(rail);
  });

  it("does not move focus for a cold deep link", async () => {
    onAPhone();
    await renderTab({ users: roster, meID: 1, href: "/users?member=2&stash=true" });

    // Arriving on a board is not a push: focus starts where a loaded page starts.
    expect(heading().textContent).toBe("Bo's stash");
    expect(document.activeElement).toBe(document.body);
  });

  it("keeps the screen you are not on out of reach while it is still drawn", async () => {
    onAPhone();
    const { router } = await renderTab({ users: roster, meID: 1, href: "/users?member=2" });
    const rail = () => document.querySelector(".mem-rail-screen") as HTMLElement;
    const pane = () => document.querySelector(".mem-pane") as HTMLElement;

    // The exit transition (#266) keeps the leaving screen's box, so `inert`
    // takes it out of reach from the first frame.
    expect(pane().hasAttribute("inert")).toBe(true);
    expect(rail().hasAttribute("inert")).toBe(false);

    fireEvent.click(toStash());
    await waitFor(() => expect(pushedFlag()).toBe("true"));
    expect(rail().hasAttribute("inert")).toBe(true);
    expect(pane().hasAttribute("inert")).toBe(false);

    router.history.back();
    await waitFor(() => expect(pushedFlag()).toBe("false"));
    expect(rail().hasAttribute("inert")).toBe(false);
    expect(pane().hasAttribute("inert")).toBe(true);
  });

  it("updates reachability when the viewport crosses 761 in either URL state", async () => {
    atWidth(900);
    const { router } = await renderTab({
      users: roster,
      meID: 1,
      href: "/users?member=2&stash=true",
    });
    const rail = () => document.querySelector(".mem-rail-screen") as HTMLElement;
    const pane = () => document.querySelector(".mem-pane") as HTMLElement;

    expect(mediaListenerCount(pushQuery)).toBe(1);
    expect(rail().hasAttribute("inert")).toBe(false);
    expect(pane().hasAttribute("inert")).toBe(false);

    resizeTo(375);
    await waitFor(() => expect(rail().hasAttribute("inert")).toBe(true));
    expect(pane().hasAttribute("inert")).toBe(false);

    resizeTo(900);
    await waitFor(() => expect(rail().hasAttribute("inert")).toBe(false));
    expect(pane().hasAttribute("inert")).toBe(false);

    await router.navigate({ to: "/users", search: { member: 2 } });
    await waitFor(() => expect(pushedFlag()).toBe("false"));
    resizeTo(375);
    await waitFor(() => expect(pane().hasAttribute("inert")).toBe(true));
    expect(rail().hasAttribute("inert")).toBe(false);

    cleanup();
    expect(mediaListenerCount(pushQuery)).toBe(0);
  });

  it("leaves both screens reachable above 761, where they are one screen", async () => {
    await renderTab({ users: roster, meID: 1, href: "/users?member=2&stash=true" });

    // Above 761 the pane is beside the rail, so `stash` changes nothing.
    expect(document.querySelector(".mem-rail-screen")?.hasAttribute("inert")).toBe(false);
    expect(document.querySelector(".mem-pane")?.hasAttribute("inert")).toBe(false);
  });

});
