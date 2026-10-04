/* Render tests for the movie detail modal (#178): the lean tile paints first,
   then the lazy detail fills in. Layout is CSS and checked in a browser; jsdom
   pins the markup. Only the router Link and the API are stubbed. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { APIClient, ApiError } from "@/api/APIClient";
import { AuthKeys, MoviesKeys, SettingsKeys } from "@/api/query_keys";

import { MovieModal } from "@/components/moviepickarr/MovieModal";

import type { MeResponse, MovieDetail, MovieTile } from "@/types/Response";
import type { ReactNode } from "react";

// Routing is nav.test.ts's job: the stub flattens `to` + `search` into an href
// and exposes `replace` as an attribute.
vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    search,
    replace,
    children,
    onClick,
    ...rest
  }: {
    to: string;
    search?: Record<string, string | number | boolean | undefined>;
    replace?: boolean;
    children: ReactNode;
    onClick?: () => void;
  }) => {
    const query = new URLSearchParams(
      Object.entries(search ?? {})
        .filter(([, v]) => v !== undefined)
        .map(([k, v]) => [k, String(v)]),
    ).toString();
    return (
      <a href={query ? `${to}?${query}` : to} data-replace={String(Boolean(replace))} onClick={onClick} {...rest}>
        {children}
      </a>
    );
  },
}));

// The detail read never resolves, so "still loading" is the default; tests seed
// the detail, session and pool lock into the cache.
vi.mock("@/api/APIClient", () => ({
  ApiError: class ApiError extends Error {
    readonly status: number;

    constructor(status: number, message: string) {
      super(message);
      this.status = status;
    }
  },
  APIClient: {
    movies: { get: vi.fn(() => new Promise<never>(() => {})) },
    auth: { me: vi.fn(() => new Promise<never>(() => {})) },
    settings: { getPoolState: vi.fn(() => new Promise<never>(() => {})) },
    board: {
      updateMovie: vi.fn(() => Promise.resolve()),
      deleteMovie: vi.fn(() => Promise.resolve()),
    },
  },
}));

afterEach(() => {
  vi.clearAllMocks();
});

const MOVIE_ID = 42;
const ADDER_ID = 1;

/** The lean tile object: no overview, credits, cast or backdrop. */
function lean(overrides: Partial<MovieTile> = {}): MovieTile {
  return {
    movieID: MOVIE_ID,
    title: "Apocalypse Now",
    link: "",
    addedAt: "2026-07-22T10:00:00Z",
    addedByID: 1,
    addedByName: "Cleo",
    posterPath: "/poster.jpg",
    tmdbId: 28,
    imdbId: "tt0078788",
    ...overrides,
  };
}

/** The same movie as the detail payload answers it. */
function detailed(overrides: Partial<MovieDetail> = {}): MovieDetail {
  return {
    ...lean(),
    status: "pool",
    overview: "At the height of the Vietnam war…",
    backdropPath: "/backdrop.jpg",
    crew: [
      { id: 1, name: "Francis Ford Coppola", job: "Director" },
      { id: 1, name: "Francis Ford Coppola", job: "Writer" },
      { id: 2, name: "John Milius", job: "Screenplay" },
    ],
    cast: [{ id: 9, name: "Martin Sheen", character: "Willard" }],
    ...overrides,
  };
}

function session(id: number): MeResponse {
  return {
    id,
    displayName: `Member ${id}`,
    username: null,
    role: "member",
    hasLocalLogin: true,
    hasLinkedIdentity: false,
  };
}

function renderModal({
  movie = lean(),
  detail,
  meID,
  locked,
  drawInProgress,
  useDefaultRetry = false,
}: {
  movie?: MovieTile;
  detail?: MovieDetail;
  /** The session member. Left out, /auth/me never lands and nobody owns the movie. */
  meID?: number;
  locked?: boolean;
  drawInProgress?: boolean;
  useDefaultRetry?: boolean;
} = {}) {
  const client = useDefaultRetry
    ? new QueryClient()
    : new QueryClient({
        defaultOptions: { queries: { retry: false, staleTime: Infinity } },
      });
  if (detail) client.setQueryData(MoviesKeys.detail(MOVIE_ID), detail);
  if (meID !== undefined) client.setQueryData(AuthKeys.me(), session(meID));
  if (locked !== undefined || drawInProgress !== undefined) {
    client.setQueryData(
      SettingsKeys.poolLock(),
      { poolLocked: locked ?? false, drawInProgress: drawInProgress ?? false },
    );
  }

  const onRequestClose = vi.fn();
  render(
    <QueryClientProvider client={client}>
      <MovieModal movie={movie} open onRequestClose={onRequestClose} onClose={vi.fn()} />
    </QueryClientProvider>,
  );
  // The action pair's dialogs portal as siblings, so the first dialog is the record.
  return { client, dialog: screen.getAllByRole("dialog")[0], onRequestClose };
}

/** The record's own title, which the rail precedes. */
function title() {
  return screen.getByRole("heading", { name: "Apocalypse Now" });
}

/** The confirm that opens over the record, told apart by the heading it carries. */
function confirmDialog() {
  return screen
    .getAllByRole("dialog")
    .find((d) => within(d).queryByRole("heading", { name: "Delete movie" })) as HTMLElement;
}

/** Reading order: all jsdom can say about where a block sits. */
function comesBefore(first: HTMLElement, second: HTMLElement) {
  return (first.compareDocumentPosition(second) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0;
}

/** The credit block: the "Directed by" lines and the attribution beside them. */
function creditBlock(dialog: HTMLElement) {
  return dialog.querySelector(".moviemodal__credit") as HTMLElement;
}

function attribution(dialog: HTMLElement) {
  return dialog.querySelector(".moviemodal__by") as HTMLElement;
}

describe("MovieModal", () => {
  it("paints the lean tile object immediately, before the detail lands", () => {
    renderModal();

    expect(screen.getByRole("heading", { name: "Apocalypse Now" })).not.toBeNull();
    expect(screen.getByText("Cleo")).not.toBeNull();
    expect(screen.queryByText(/Directed by/)).toBeNull();
  });

  it("holds the credit rows while the detail loads, so the block doesn't grow under the reader", () => {
    const { dialog } = renderModal();

    // Two placeholder rows, one per credit line the landed detail will fill.
    expect(creditBlock(dialog).querySelectorAll(".moviemodal__credits__ghost")).toHaveLength(2);
  });

  it("fills the credits in beside the attribution once the detail lands", () => {
    const { dialog } = renderModal({ detail: detailed() });

    const block = creditBlock(dialog);
    expect(block.textContent).toContain("Directed by");
    // A person credited twice (Coppola wrote and directed) is named once per line.
    expect(screen.getAllByRole("link", { name: "Francis Ford Coppola" })).toHaveLength(2);
    expect(block.querySelectorAll(".moviemodal__credits__ghost")).toHaveLength(0);
    expect(block.contains(attribution(dialog))).toBe(true);
  });

  it("puts the attribution above the overview instead of after it", () => {
    const { dialog } = renderModal({ detail: detailed() });

    const overview = dialog.querySelector(".moviemodal__overview") as HTMLElement;
    expect(overview.textContent).not.toContain("Cleo");
    expect(
      attribution(dialog).compareDocumentPosition(overview) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("dates the watch beside the attribution on a watched movie", () => {
    const { dialog } = renderModal({
      movie: lean({ watchedAt: "2026-07-21T20:00:00Z" }),
      detail: detailed({ watchedAt: "2026-07-21T20:00:00Z" }),
    });

    expect(attribution(dialog).textContent).toMatch(/Watched/);
  });

  it("says nothing about a watch on a pooled movie", () => {
    const { dialog } = renderModal({ detail: detailed() });

    expect(attribution(dialog).textContent).not.toMatch(/Watched/);
  });

  // Lean object only (#238): the id is on the tile, so the link is there from the first frame.
  it("points the adder's name at their board", () => {
    const { dialog } = renderModal({ movie: lean({ addedByID: 7, addedByName: "Cleo" }) });

    const link = within(attribution(dialog)).getByRole("link", { name: "Cleo" });
    expect(link.getAttribute("href")).toBe("/users?member=7");
  });

  it("keeps an archived adder as attribution without linking to another board", () => {
    const { dialog } = renderModal({
      movie: lean({
        addedByID: 7,
        addedByName: "Cleo",
        addedByArchived: true,
      }),
    });

    expect(within(attribution(dialog)).getByText("Cleo")).not.toBeNull();
    expect(within(attribution(dialog)).queryByRole("link", { name: "Cleo" })).toBeNull();
  });

  it("navigates over the modal's own entry, the way the chips do", () => {
    const { dialog } = renderModal({ movie: lean({ addedByID: 7, addedByName: "Cleo" }) });

    // Replace, not push: consuming the modal's own entry is what closes it (see
    // useMovieModalHistory). The real navigation is pinned in UsersTab.render.test.tsx.
    expect(
      within(attribution(dialog)).getByRole("link", { name: "Cleo" }).getAttribute("data-replace"),
    ).toBe("true");
  });

  it("renders the external links as quiet links to a new tab, not buttons", () => {
    renderModal({ detail: detailed() });

    for (const label of ["IMDb", "TMDB", "Letterboxd"]) {
      const link = screen.getByRole("link", { name: label });
      expect(link.getAttribute("target")).toBe("_blank");
      expect(link.getAttribute("rel")).toContain("noopener");
      expect(link.className).not.toContain("btn");
    }
    // In the rail, not a trailing row: the rail comes before the title.
    expect(comesBefore(screen.getByRole("link", { name: "IMDb" }), title())).toBe(true);
  });

  it("caps the surface so the content scrolls inside it, not the page", () => {
    const { dialog } = renderModal();

    expect(dialog.classList.contains("modal--capped")).toBe(true);
    expect(dialog.querySelector(".modal__scroll")).not.toBeNull();
    // The close X stays outside the scrolling region so it doesn't scroll away.
    const close = screen.getByRole("button", { name: "Close" });
    expect(close.closest(".modal__scroll")).toBeNull();
  });

  it("keeps the cast strip out of the way when the movie has no cast", () => {
    const { dialog } = renderModal({ detail: detailed({ cast: [] }) });

    expect(dialog.querySelector(".castrow")).toBeNull();
  });
});

describe("MovieModal hero", () => {
  function hero(dialog: HTMLElement) {
    return dialog.querySelector(".moviemodal__hero") as HTMLElement;
  }

  function backdrop(dialog: HTMLElement) {
    return dialog.querySelector(".moviemodal__backdrop") as HTMLElement;
  }

  function heroPreload(dialog: HTMLElement) {
    return dialog.querySelector(".moviemodal__hero__preload") as HTMLImageElement | null;
  }

  it("does not stand the poster in for the backdrop while the detail loads", async () => {
    const { client, dialog } = renderModal();

    // The detail is about to bring a backdrop; painting the poster now means a visible swap.
    expect(heroPreload(dialog)?.src ?? "").not.toContain("poster.jpg");

    act(() => {
      client.setQueryData(MoviesKeys.detail(MOVIE_ID), detailed());
    });
    await vi.waitFor(() => expect(heroPreload(dialog)?.src ?? "").toContain("backdrop.jpg"));
  });

  it("stands the poster in once the detail says the movie has no backdrop", () => {
    const { dialog } = renderModal({ detail: detailed({ backdropPath: undefined }) });

    expect(heroPreload(dialog)?.src ?? "").toContain("poster.jpg");
  });

  // The rail shows the same poster sharp; a muted wash keeps the stand-in from
  // reading as a duplicate, and needs only a small source.
  it("mutes the stand-in poster and asks for a small one", () => {
    const { dialog } = renderModal({ detail: detailed({ backdropPath: undefined }) });
    const preload = heroPreload(dialog)!;

    expect(preload.className).toContain("moviemodal__hero__preload--wash");
    expect(preload.src).toContain("w185");
    fireEvent.load(preload);
    expect(backdrop(dialog).style.backgroundImage).toContain("rgba(8, 9, 14, 0.68)");
  });

  it("leaves a real backdrop sharp", async () => {
    const { dialog } = renderModal({ detail: detailed() });

    await vi.waitFor(() => expect(heroPreload(dialog)?.src ?? "").toContain("backdrop.jpg"));
    expect(heroPreload(dialog)?.className).not.toContain("--wash");
    expect(backdrop(dialog).style.backgroundImage).not.toContain("rgba(8, 9, 14, 0.68)");
  });

  it("preloads the photograph before painting it beneath the modal scrollbar", () => {
    const { dialog } = renderModal({ detail: detailed() });
    const preload = heroPreload(dialog)!;

    expect(preload.hidden).toBe(true);
    expect(backdrop(dialog).style.backgroundImage).not.toContain("backdrop.jpg");

    fireEvent.load(preload);

    expect(backdrop(dialog).style.backgroundImage).toContain("backdrop.jpg");
    expect(backdrop(dialog).style.backgroundImage).toContain("transparent 72%");
    // Keep the fade and body mask off the same device-pixel edge: Safari and
    // Firefox round them independently and expose the backdrop.
    expect(backdrop(dialog).style.backgroundSize).toContain(
      "calc(var(--moviemodal-hero-height) + 1px)",
    );
    expect(hero(dialog).style.backgroundImage).not.toContain("backdrop.jpg");
    expect(dialog.querySelector(".moviemodal__hero__img")).toBeNull();
  });
});

/* The action pair (#237): rename and delete on the record. Refusal reasons
   are refusals.test.ts's table. */
describe("MovieModal actions", () => {
  const edit = () => screen.queryByRole("button", { name: "Edit" });
  const del = () => screen.queryByRole("button", { name: /^Delete/ });

  it("offers the adder both actions once the detail lands", () => {
    renderModal({ meID: ADDER_ID, locked: false, detail: detailed({ status: "stash" }) });

    expect(edit()).not.toBeNull();
    expect(del()).not.toBeNull();
    // At the foot of the rail: after the last link, before the title.
    expect(comesBefore(screen.getByRole("link", { name: "Letterboxd" }), edit()!)).toBe(true);
    expect(comesBefore(edit()!, title())).toBe(true);
  });

  it("keeps delete inert until the round state is known", () => {
    // No lock seeded: a delete the server might refuse must not be offered yet.
    renderModal({ meID: ADDER_ID, detail: detailed({ status: "pool" }) });

    expect(edit()).not.toBeNull();
    expect(del()?.getAttribute("aria-disabled")).toBe("true");
    expect(del()?.getAttribute("aria-label")).toBe("Delete, round state unavailable");
  });

  it("fails delete closed when the round-state request errors", async () => {
    vi.mocked(APIClient.settings.getPoolState).mockRejectedValueOnce(
      new Error("round state unavailable"),
    );
    renderModal({ meID: ADDER_ID, detail: detailed({ status: "pool" }) });

    await vi.waitFor(() =>
      expect(del()?.getAttribute("aria-label")).toBe(
        "Delete, round state unavailable",
      ),
    );
    fireEvent.click(del()!);
    expect(screen.queryByRole("heading", { name: "Delete movie" })).toBeNull();
  });

  it("fails delete closed during a background round-state refresh", async () => {
    const { client } = renderModal({
      meID: ADDER_ID,
      locked: false,
      detail: detailed({ status: "pool" }),
    });
    vi.mocked(APIClient.settings.getPoolState).mockReturnValueOnce(
      new Promise<never>(() => {}),
    );

    act(() => {
      void client.invalidateQueries({ queryKey: SettingsKeys.poolLock() });
    });

    await vi.waitFor(() =>
      expect(del()?.getAttribute("aria-label")).toBe(
        "Delete, round state unavailable",
      ),
    );
  });

  it("fails delete closed while a lifecycle detail refresh is in flight", async () => {
    const { client } = renderModal({
      meID: ADDER_ID,
      locked: false,
      detail: detailed({ status: "pool" }),
    });
    vi.mocked(APIClient.movies.get).mockReturnValueOnce(
      new Promise<never>(() => {}),
    );

    act(() => {
      void client.invalidateQueries({ queryKey: MoviesKeys.detail(MOVIE_ID) });
    });

    await vi.waitFor(() =>
      expect(del()?.getAttribute("aria-label")).toBe(
        "Delete, round state unavailable",
      ),
    );
  });

  it("fails a cached stash delete closed while its lifecycle detail refreshes", async () => {
    const { client } = renderModal({
      meID: ADDER_ID,
      detail: detailed({ status: "stash" }),
    });
    vi.mocked(APIClient.movies.get).mockReturnValueOnce(
      new Promise<never>(() => {}),
    );

    act(() => {
      void client.invalidateQueries({ queryKey: MoviesKeys.detail(MOVIE_ID) });
    });

    await vi.waitFor(() => {
      expect(del()?.getAttribute("aria-disabled")).toBe("true");
      expect(del()?.getAttribute("aria-label")).toBe(
        "Delete, round state unavailable",
      );
    });
    fireEvent.click(del()!);
    expect(screen.queryByRole("heading", { name: "Delete movie" })).toBeNull();
    expect(APIClient.settings.getPoolState).not.toHaveBeenCalled();
  });

  it("offers nothing on somebody else's movie", () => {
    renderModal({ meID: ADDER_ID + 1, locked: false, detail: detailed({ status: "stash" }) });

    expect(edit()).toBeNull();
    expect(del()).toBeNull();
  });

  it("waits for the detail: the lean tile object carries no status to judge", () => {
    renderModal({ meID: ADDER_ID });

    expect(edit()).toBeNull();
    expect(del()).toBeNull();
  });

  it("deletes a stash movie whatever the round is doing", () => {
    renderModal({
      meID: ADDER_ID,
      locked: true,
      drawInProgress: true,
      detail: detailed({ status: "stash" }),
    });

    expect(del()?.getAttribute("aria-disabled")).toBeNull();
    expect(del()?.getAttribute("aria-label")).toBe("Delete");
  });

  it("does not read pool state for a stash movie", () => {
    renderModal({ meID: ADDER_ID, detail: detailed({ status: "stash" }) });

    expect(APIClient.settings.getPoolState).not.toHaveBeenCalled();
  });

  it("says nothing about deleting a watched movie, but still lets it be renamed", () => {
    renderModal({
      meID: ADDER_ID,
      detail: detailed({ status: "watched", watchedAt: "2026-07-21T20:00:00Z" }),
    });

    expect(edit()).not.toBeNull();
    expect(del()).toBeNull();
  });

  it("refuses a pooled movie in place while the round is closed", () => {
    renderModal({ meID: ADDER_ID, locked: true, detail: detailed({ status: "pool" }) });

    const button = del();
    expect(button?.getAttribute("aria-disabled")).toBe("true");
    // Never natively disabled: a keyboard user must reach the reason.
    expect(button).not.toHaveProperty("disabled", true);
    expect(button?.getAttribute("aria-label")).toBe("Delete, round closed");
    expect(button?.getAttribute("title")).toBe("Delete, round closed");
  });

  it("names the draw first when a locked round and a draw refuse together", () => {
    renderModal({
      meID: ADDER_ID,
      locked: true,
      drawInProgress: true,
      detail: detailed({ status: "pool" }),
    });

    expect(del()?.getAttribute("aria-label")).toBe("Delete, a draw is in progress");
  });

  it("refuses pooled delete from the server hold when no reel animation runs", () => {
    renderModal({
      meID: ADDER_ID,
      locked: false,
      drawInProgress: true,
      detail: detailed({ status: "pool" }),
    });

    expect(del()?.getAttribute("aria-disabled")).toBe("true");
    expect(del()?.getAttribute("aria-label")).toBe("Delete, a draw is in progress");
    fireEvent.click(del()!);
    expect(screen.queryByRole("heading", { name: "Delete movie" })).toBeNull();
  });

  it("does nothing when a refused delete is clicked", () => {
    renderModal({ meID: ADDER_ID, locked: true, detail: detailed({ status: "pool" }) });

    fireEvent.click(del()!);

    expect(screen.queryByRole("heading", { name: "Delete movie" })).toBeNull();
  });

  it.each([
    ["edit", "Edit", "Edit movie"],
    ["delete", "Delete", "Delete movie"],
  ])("exposes only the %s dialog while it covers the record", (_action, trigger, heading) => {
    const { dialog: record } = renderModal({
      meID: ADDER_ID,
      detail: detailed({ status: "stash" }),
    });

    const action = screen.getByRole("button", { name: trigger });
    action.focus();
    fireEvent.click(action);

    const [visible] = screen.getAllByRole("dialog");
    expect(screen.getAllByRole("dialog")).toHaveLength(1);
    expect(within(visible).getByRole("heading", { name: heading })).not.toBeNull();
    expect(screen.getAllByRole("dialog", { hidden: true })).toHaveLength(2);
    expect(record.getAttribute("aria-modal")).toBeNull();
    expect(record.getAttribute("aria-hidden")).toBe("true");
    expect(record.hasAttribute("inert")).toBe(true);
  });

  it("opens the delete confirm on an allowed movie, and its success closes the record", async () => {
    const { onRequestClose } = renderModal({
      meID: ADDER_ID,
      locked: false,
      detail: detailed({ status: "pool" }),
    });

    fireEvent.click(del()!);
    expect(screen.getByRole("heading", { name: "Delete movie" })).not.toBeNull();

    // Same name as the rail's control, so scope to the confirm.
    fireEvent.click(within(confirmDialog()).getByRole("button", { name: "Delete" }));

    await vi.waitFor(() => expect(APIClient.board.deleteMovie).toHaveBeenCalledWith(MOVIE_ID));
    await vi.waitFor(() => expect(onRequestClose).toHaveBeenCalled());
  });

  it("pins a pending delete and refuses a duplicate confirmation", async () => {
    let releaseDelete = () => {};
    const pendingDelete = new Promise<void>((resolve) => {
      releaseDelete = () => resolve();
    });
    vi.mocked(APIClient.board.deleteMovie).mockReturnValueOnce(pendingDelete);
    const { onRequestClose } = renderModal({
      meID: ADDER_ID,
      detail: detailed({ status: "stash" }),
    });

    fireEvent.click(del()!);
    const dialog = confirmDialog();
    const confirm = within(dialog).getByRole("button", { name: "Delete" });
    fireEvent.click(confirm);

    await vi.waitFor(() =>
      expect(
        within(dialog)
          .getByRole("button", { name: "Deleting…" })
          .hasAttribute("disabled"),
      ).toBe(true),
    );
    const pendingConfirm = within(dialog).getByRole("button", { name: "Deleting…" });
    const cancelDisabled = within(dialog)
      .getByRole("button", { name: "Cancel" })
      .hasAttribute("disabled");
    const closeDisabled = within(dialog)
      .getByRole("button", { name: "Close" })
      .hasAttribute("disabled");

    // The button stays mounted during the request; a second activation must be inert.
    fireEvent.click(pendingConfirm);
    const submissions = vi.mocked(APIClient.board.deleteMovie).mock.calls.length;

    await act(async () => {
      releaseDelete();
      await pendingDelete;
    });
    await vi.waitFor(() => expect(onRequestClose).toHaveBeenCalled());

    expect(submissions).toBe(1);
    expect(cancelDisabled).toBe(true);
    expect(closeDisabled).toBe(true);
  });

  it("keeps the link field on the edit dialog, so the re-point path survives", () => {
    renderModal({
      meID: ADDER_ID,
      detail: detailed({ status: "stash", link: "https://www.imdb.com/title/tt0078788/" }),
    });

    fireEvent.click(edit()!);

    const link = screen.getByLabelText("Movie link") as HTMLInputElement;
    expect(link.value).toBe("https://www.imdb.com/title/tt0078788/");
  });

  it("keeps the record open when an edit saves", async () => {
    const link = "https://www.imdb.com/title/tt0078788/";
    const { onRequestClose } = renderModal({
      meID: ADDER_ID,
      detail: detailed({ status: "stash", link }),
    });

    fireEvent.click(edit()!);
    fireEvent.change(screen.getByLabelText("Movie title"), { target: { value: "Apocalypse Now Redux" } });
    fireEvent.click(screen.getByRole("button", { name: "Save changes" }));

    await vi.waitFor(() =>
      expect(APIClient.board.updateMovie).toHaveBeenCalledWith(MOVIE_ID, "Apocalypse Now Redux", link),
    );
    expect(onRequestClose).not.toHaveBeenCalled();
    expect(screen.getAllByRole("heading", { name: "Apocalypse Now" }).length).toBeGreaterThan(0);
  });

  it("closes a record remotely deleted while it is open without retrying 404", async () => {
    vi.mocked(APIClient.movies.get).mockRejectedValueOnce(
      new ApiError(404, "Not Found"),
    );
    const { onRequestClose } = renderModal({
      meID: ADDER_ID,
      detail: detailed({ status: "stash" }),
      useDefaultRetry: true,
    });

    await vi.waitFor(() => expect(onRequestClose).toHaveBeenCalled(), {
      timeout: 500,
    });
    expect(APIClient.movies.get).toHaveBeenCalledTimes(1);
  });

  it("takes a child dialog with it when the record's own entry goes", () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(MoviesKeys.detail(MOVIE_ID), detailed({ status: "stash" }));
    client.setQueryData(AuthKeys.me(), session(ADDER_ID));

    const view = render(
      <QueryClientProvider client={client}>
        <MovieModal movie={lean()} open onRequestClose={vi.fn()} onClose={vi.fn()} />
      </QueryClientProvider>,
    );

    fireEvent.click(edit()!);
    expect(screen.getByRole("heading", { name: "Edit movie" })).not.toBeNull();

    // Browser Back arrives as `open` going false: the child dialog goes at once
    // while the record plays its exit.
    view.rerender(
      <QueryClientProvider client={client}>
        <MovieModal movie={lean()} open={false} onRequestClose={vi.fn()} onClose={vi.fn()} />
      </QueryClientProvider>,
    );

    expect(screen.queryByRole("heading", { name: "Edit movie" })).toBeNull();
  });
});
