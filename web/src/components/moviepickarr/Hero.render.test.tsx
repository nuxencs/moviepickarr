/* ============================================================
   Render tests for the hero banner's attribution (#238).

   The reveal machinery, the turn gate and the draw reel all have their own
   homes (drawMachine.test.ts, turnGate.test.ts, DrawReel.render.test.tsx), and
   none of it is repeated here. What is here is the one thing only the rendered
   banner can answer: that an active adder's name in the eyebrow is the way to
   their board, that archived attribution is not a dead link, and that following
   an active link stacks a history entry instead of spending one.

   It also covers the boundary between content and backdrop loading. A known
   draw stays usable while its artwork decodes, and artwork updates must not
   replay the content reveal or let an older request repaint a newer draw.
   ============================================================ */

import { act, configure, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { APIClient } from "@/api/APIClient";
import { AuthKeys, MoviesKeys, SettingsKeys } from "@/api/query_keys";

import { drawStore } from "@/components/moviepickarr/drawStore";
import { Hero } from "@/components/moviepickarr/Hero";

import type { MeResponse, MovieDetail, MovieTile } from "@/types/Response";
import type { QueryClient } from "@tanstack/react-query";

import { renderWithProviders } from "@/test/providers";

vi.mock("@/api/APIClient", () => ({
  APIClient: {
    board: { getAll: vi.fn() },
    movies: {
      get: vi.fn(() => new Promise<never>(() => {})),
      getCurrent: vi.fn(), getPool: vi.fn(), getWildcard: vi.fn(), draw: vi.fn(),
      markWatched: vi.fn(), watchWildcard: vi.fn(), cancelWildcard: vi.fn(),
    },
    settings: { getNextUp: vi.fn() },
    auth: { me: vi.fn() },
  },
  ApiError: class ApiError extends Error {},
}));

const drawn: MovieDetail = {
  movieID: 42,
  title: "Apocalypse Now",
  link: "",
  addedAt: "2026-07-22T10:00:00Z",
  addedByID: 7,
  addedByName: "Ada",
  status: "current",
  drawnAt: "2026-07-28T20:00:00Z",
};

const backdrop = (path: string) => `https://image.tmdb.org/t/p/w1280${path}`;

/** Hold each Image.decode call until the test chooses which request settles. */
function controlImageDecodes() {
  const pending = new Map<
    string,
    { resolve: () => void; reject: (reason: Error) => void }
  >();
  const requests: string[] = [];

  class ControlledImage {
    src = "";

    decode(): Promise<void> {
      requests.push(this.src);
      return new Promise((resolve, reject) => pending.set(this.src, { resolve, reject }));
    }
  }

  vi.stubGlobal("Image", ControlledImage);

  const requested = (url: string) => waitFor(() => expect(pending.has(url)).toBe(true));
  const resolve = async (url: string) => {
    await requested(url);
    await act(async () => pending.get(url)!.resolve());
  };
  const reject = async (url: string) => {
    await requested(url);
    await act(async () => pending.get(url)!.reject(new Error("decode failed")));
  };

  return {
    requested,
    resolve,
    reject,
    count: (url: string) => requests.filter((request) => request === url).length,
  };
}

const painted = (url: string) =>
  Array.from(document.querySelectorAll<HTMLElement>(".hero__bgimg")).some((layer) =>
    layer.style.backgroundImage.includes(url),
  );

afterEach(() => {
  configure({ reactStrictMode: false });
  vi.unstubAllGlobals();
});

function session(id: number): MeResponse {
  return {
    id,
    displayName: `Member ${id}`,
    username: null,
    role: "admin",
    hasLocalLogin: true,
    hasLinkedIdentity: false,
  };
}

/** The banner on the Movies page with a draw already up. */
async function renderHero(movie: MovieDetail = drawn, strict = false) {
  let queryClient: QueryClient | undefined;
  if (strict) configure({ reactStrictMode: true });
  const view = await renderWithProviders(<Hero />, {
    path: "/",
    seed: (client) => {
      queryClient = client;
      client.setQueryData(MoviesKeys.current(), movie);
      client.setQueryData(MoviesKeys.wildcard(), null);
      client.setQueryData(MoviesKeys.listpool(), []);
      client.setQueryData(SettingsKeys.nextUp(), { id: 1, name: "Member 1" });
      client.setQueryData(SettingsKeys.poolLock(), {
        poolLocked: false,
        drawInProgress: true,
      });
      client.setQueryData(AuthKeys.me(), session(1));
    },
  });
  if (strict) configure({ reactStrictMode: false });
  return { ...view, queryClient: queryClient! };
}

/** The adder's name, once the draw has been revealed into the banner. */
async function adder() {
  return waitFor(() => screen.getByRole("link", { name: "Ada" }));
}

describe("the hero's attribution", () => {
  it("points the adder's name at their board", async () => {
    await renderHero();

    expect((await adder()).getAttribute("href")).toBe("/users?member=7");
  });

  it("keeps an archived adder as attribution without linking to another board", async () => {
    await renderHero({
      ...drawn,
      addedByArchived: true,
    });

    await waitFor(() => expect(screen.getByText("Ada")).not.toBeNull());
    expect(screen.queryByRole("link", { name: "Ada" })).toBeNull();
  });

  it("stacks the navigation, so Back comes back to the draw", async () => {
    const { router } = await renderHero();

    fireEvent.click(await adder());

    await waitFor(() => expect(router.state.location.href).toBe("/users?member=7"));
    // A push, not the modal's replace: nothing here is holding a history entry
    // the link has to spend, so leaving the banner is an ordinary navigation.
    router.history.back();
    await waitFor(() => expect(router.state.location.href).toBe("/"));
  });

  it("releases the cached draw gate when its own watched event is lost", async () => {
    vi.mocked(APIClient.movies.markWatched).mockResolvedValueOnce({
      ...drawn,
      status: "watched",
    });
    const { queryClient } = await renderHero();

    await adder();
    fireEvent.click(screen.getByRole("button", { name: "Mark as watched" }));

    await waitFor(() =>
      expect(
        queryClient.getQueryData<{ drawInProgress: boolean }>(SettingsKeys.poolLock())
          ?.drawInProgress,
      ).toBe(false),
    );
  });
});

describe("the hero's wildcard detour", () => {
  it("holds Current actions until wildcard absence is confirmed", async () => {
    vi.mocked(APIClient.movies.getWildcard).mockReturnValue(new Promise(() => {}));
    await renderWithProviders(<Hero />, {
      path: "/",
      seed: (client) => {
        client.setQueryData(MoviesKeys.current(), { ...drawn, revealed: true });
        client.setQueryData(MoviesKeys.listpool(), []);
        client.setQueryData(SettingsKeys.nextUp(), { id: 1, name: "Member 1" });
        client.setQueryData(SettingsKeys.poolLock(), { poolLocked: false, drawInProgress: true });
        client.setQueryData(AuthKeys.me(), session(1));
      },
    });

    expect((await screen.findByRole("button", { name: "Mark as watched" })).hasAttribute("disabled")).toBe(true);
    expect(screen.queryByRole("button", { name: "Choose wildcard" })).toBeNull();
  });

  it("lets the Active wildcard take over the Hero while keeping the Current draw on hold", async () => {
    const { queryClient } = await renderHero({ ...drawn, revealed: true });

    act(() => {
      queryClient.setQueryData(MoviesKeys.wildcard(), {
        id: 9,
        hostMovieId: drawn.movieID,
        selectedAt: "2026-08-25T18:00:00Z",
        movie: {
          ...drawn,
          movieID: 77,
          title: "Before Sunrise",
          status: "wildcard",
        },
      });
    });

    expect(await screen.findByRole("heading", { name: "Before Sunrise" })).not.toBeNull();
    expect(screen.queryByRole("heading", { name: "Apocalypse Now" })).toBeNull();
    expect(screen.getByText(/Active wildcard · added by/)).not.toBeNull();
    expect(screen.getByRole("button", { name: "Apocalypse Now" })).not.toBeNull();
    expect(screen.getByRole("button", { name: "Mark as watched" }).hasAttribute("disabled")).toBe(false);
    expect(screen.getByRole("button", { name: "Cancel wildcard" })).not.toBeNull();
    expect(screen.getByText("Your turn")).not.toBeNull();
  });

  it("opens the held Current draw in the movie detail modal", async () => {
    const { queryClient } = await renderHero({ ...drawn, revealed: true });
    act(() => {
      queryClient.setQueryData(MoviesKeys.wildcard(), {
        id: 9,
        hostMovieId: drawn.movieID,
        selectedAt: "2026-08-25T18:00:00Z",
        movie: { ...drawn, movieID: 77, title: "Before Sunrise", status: "wildcard" },
      });
    });

    await screen.findByRole("heading", { name: "Before Sunrise" });
    fireEvent.click(screen.getByRole("button", { name: "Apocalypse Now" }));

    expect(await screen.findByRole("dialog", { name: "Apocalypse Now" })).not.toBeNull();
  });

  it("closes a stale picker when another client selects the Active wildcard", async () => {
    const { queryClient } = await renderHero({ ...drawn, revealed: true });
    fireEvent.click(await screen.findByRole("button", { name: "Choose wildcard" }));
    expect(await screen.findByRole("dialog", { name: "Choose a wildcard" })).not.toBeNull();

    act(() => {
      queryClient.setQueryData(MoviesKeys.wildcard(), {
        id: 9,
        hostMovieId: drawn.movieID,
        selectedAt: "2026-08-25T18:00:00Z",
        movie: { ...drawn, movieID: 77, title: "Before Sunrise", status: "wildcard" },
      });
    });

    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Choose a wildcard" })).toBeNull());
  });

  it("watches the wildcard without clearing the cached Current draw", async () => {
    vi.mocked(APIClient.movies.watchWildcard).mockResolvedValueOnce({
      id: 9,
      hostMovieId: drawn.movieID,
      selectedAt: "2026-08-25T18:00:00Z",
      movie: { ...drawn, movieID: 77, title: "Before Sunrise", status: "watched" },
    });
    const { queryClient } = await renderHero({ ...drawn, revealed: true });
    act(() => {
      queryClient.setQueryData(MoviesKeys.wildcard(), {
        id: 9,
        hostMovieId: drawn.movieID,
        selectedAt: "2026-08-25T18:00:00Z",
        movie: { ...drawn, movieID: 77, title: "Before Sunrise", status: "wildcard" },
      });
    });

    await screen.findByRole("heading", { name: "Before Sunrise" });
    fireEvent.click(screen.getByRole("button", { name: "Mark as watched" }));
    await waitFor(() => expect(APIClient.movies.watchWildcard).toHaveBeenCalledWith(9));
    expect(queryClient.getQueryData(MoviesKeys.current())).toMatchObject({ movieID: drawn.movieID });
  });
});

describe("the hero's artwork handoff", () => {
  it("hands the Hero backdrop to an Active wildcard", async () => {
    const images = controlImageDecodes();
    const url = backdrop("/wildcard.jpg");
    const { queryClient } = await renderHero({ ...drawn, backdropPath: undefined });

    act(() => {
      queryClient.setQueryData(MoviesKeys.wildcard(), {
        id: 9,
        hostMovieId: drawn.movieID,
        selectedAt: "2026-08-25T18:00:00Z",
        movie: {
          ...drawn,
          movieID: 77,
          title: "Before Sunrise",
          backdropPath: "/wildcard.jpg",
          status: "wildcard",
        },
      });
    });

    await screen.findByRole("heading", { name: "Before Sunrise" });
    await images.resolve(url);
    await waitFor(() => expect(painted(url)).toBe(true));
  });

  it("renders a known draw while its backdrop decode is still pending in Strict Mode", async () => {
    const images = controlImageDecodes();
    const url = backdrop("/slow.jpg");

    const { queryClient } = await renderHero({ ...drawn, backdropPath: "/slow.jpg" }, true);
    await images.requested(url);

    expect(screen.getByRole("heading", { name: "Apocalypse Now" })).not.toBeNull();
    expect(screen.getByRole("link", { name: "Ada" })).not.toBeNull();
    expect(screen.getByRole("button", { name: "Mark as watched" })).not.toBeNull();
    expect(painted(url)).toBe(false);

    act(() => {
      queryClient.setQueryData(MoviesKeys.current(), {
        ...drawn,
        backdropPath: "/slow.jpg",
        tagline: "Late metadata",
      });
    });

    await waitFor(() => expect(screen.getByText('"Late metadata"')).not.toBeNull());
    expect(images.count(url)).toBe(1);
    await images.resolve(url);
    await waitFor(() => expect(painted(url)).toBe(true));
  });

  it("repaints changed artwork for the same draw without replaying its content", async () => {
    const images = controlImageDecodes();
    const firstUrl = backdrop("/first.jpg");
    const secondUrl = backdrop("/second.jpg");
    const { queryClient } = await renderHero({ ...drawn, backdropPath: "/first.jpg" });

    await images.resolve(firstUrl);
    await waitFor(() => expect(painted(firstUrl)).toBe(true));
    const body = document.querySelector(".hero__body");

    act(() => {
      queryClient.setQueryData(MoviesKeys.current(), {
        ...drawn,
        backdropPath: "/second.jpg",
        tagline: "Fresh metadata",
      });
    });

    await waitFor(() => expect(screen.getByText('"Fresh metadata"')).not.toBeNull());
    expect(document.querySelector(".hero__body")).toBe(body);
    await images.resolve(secondUrl);
    await waitFor(() => expect(painted(secondUrl)).toBe(true));
    expect(document.querySelector(".hero__body")).toBe(body);
  });

  it("does not let an older decode repaint a newer draw", async () => {
    const images = controlImageDecodes();
    const oldUrl = backdrop("/old.jpg");
    const newUrl = backdrop("/new.jpg");
    const { queryClient } = await renderHero({ ...drawn, backdropPath: "/old.jpg" });

    await images.requested(oldUrl);
    act(() => {
      queryClient.setQueryData(MoviesKeys.current(), {
        ...drawn,
        movieID: 84,
        title: "The Conversation",
        drawnAt: "2026-07-29T20:00:00Z",
        backdropPath: "/new.jpg",
      });
    });

    await images.resolve(newUrl);
    await waitFor(() => expect(painted(newUrl)).toBe(true));
    await images.resolve(oldUrl);

    expect(painted(oldUrl)).toBe(false);
    expect(screen.getByRole("heading", { name: "The Conversation" })).not.toBeNull();
  });

  it("keeps the fallback artwork when a backdrop cannot decode", async () => {
    const images = controlImageDecodes();
    const url = backdrop("/broken.jpg");

    await renderHero({ ...drawn, backdropPath: "/broken.jpg" });
    await images.reject(url);

    expect(screen.getByRole("heading", { name: "Apocalypse Now" })).not.toBeNull();
    expect(painted(url)).toBe(false);
  });

  it("keeps the reel up until the full winner backdrop decodes", async () => {
    const images = controlImageDecodes();
    const url = backdrop("/winner.jpg");
    const winner: MovieDetail = {
      ...drawn,
      movieID: 142,
      title: "Paris, Texas",
      drawnAt: "2026-08-01T12:34:56Z",
      backdropPath: "/winner.jpg",
    };
    const leanWinner: MovieTile = {
      movieID: winner.movieID,
      title: winner.title,
      link: winner.link,
      addedAt: winner.addedAt,
      addedByID: winner.addedByID,
      addedByName: winner.addedByName,
    };
    const decoy: MovieTile = {
      ...leanWinner,
      movieID: 143,
      title: "The Passenger",
    };
    const reelDraw = {
      ...winner,
      candidates: [leanWinner, decoy],
    };

    drawStore.send({ type: "DRAWN", movie: reelDraw });
    expect(drawStore.getState().phase).toBe("spinning");
    await renderHero(winner);

    act(() => {
      drawStore.send({ type: "SCROLL_DONE" });
      drawStore.send({ type: "CONFIRM", source: "remote" });
    });

    await images.requested(url);
    expect(screen.getByRole("dialog", { name: "Drawing a random movie" })).not.toBeNull();
    expect(painted(url)).toBe(false);
    await images.resolve(url);
    await waitFor(() => expect(screen.getByRole("heading", { name: "Paris, Texas" })).not.toBeNull());
    await waitFor(() => expect(painted(url)).toBe(true));
    expect(images.count(url)).toBe(1);
  });

  it("decodes again when current artwork changed during the reel", async () => {
    const images = controlImageDecodes();
    const drawnUrl = backdrop("/drawn-winner.jpg");
    const currentUrl = backdrop("/refreshed-winner.jpg");
    const drawnAt = "2026-08-01T13:34:56Z";
    const winner: MovieDetail = {
      ...drawn,
      movieID: 242,
      title: "Wings of Desire",
      drawnAt,
      backdropPath: "/drawn-winner.jpg",
    };
    const currentWinner = { ...winner, backdropPath: "/refreshed-winner.jpg" };
    const leanWinner: MovieTile = {
      movieID: winner.movieID,
      title: winner.title,
      link: winner.link,
      addedAt: winner.addedAt,
      addedByID: winner.addedByID,
      addedByName: winner.addedByName,
    };

    drawStore.send({
      type: "DRAWN",
      movie: {
        ...winner,
        candidates: [leanWinner, { ...leanWinner, movieID: 243, title: "Until the End of the World" }],
      },
    });
    await renderHero(currentWinner);

    act(() => {
      drawStore.send({ type: "SCROLL_DONE" });
      drawStore.send({ type: "CONFIRM", source: "remote" });
    });

    await images.resolve(drawnUrl);
    await waitFor(() => expect(screen.getByRole("heading", { name: "Wings of Desire" })).not.toBeNull());
    expect(painted(drawnUrl)).toBe(false);
    await images.requested(currentUrl);
    expect(images.count(drawnUrl)).toBe(1);
    expect(images.count(currentUrl)).toBe(1);
    await images.resolve(currentUrl);
    await waitFor(() => expect(painted(currentUrl)).toBe(true));
  });

  it("retries through Hero when the reel backdrop decode fails", async () => {
    const images = controlImageDecodes();
    const url = backdrop("/retry-winner.jpg");
    const winner: MovieDetail = {
      ...drawn,
      movieID: 342,
      title: "A Matter of Life and Death",
      drawnAt: "2026-08-01T14:34:56Z",
      backdropPath: "/retry-winner.jpg",
    };
    const leanWinner: MovieTile = {
      movieID: winner.movieID,
      title: winner.title,
      link: winner.link,
      addedAt: winner.addedAt,
      addedByID: winner.addedByID,
      addedByName: winner.addedByName,
    };

    drawStore.send({
      type: "DRAWN",
      movie: {
        ...winner,
        candidates: [leanWinner, { ...leanWinner, movieID: 343, title: "Black Narcissus" }],
      },
    });
    await renderHero(winner);

    act(() => {
      drawStore.send({ type: "SCROLL_DONE" });
      drawStore.send({ type: "CONFIRM", source: "remote" });
    });

    await images.reject(url);
    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "A Matter of Life and Death" })).not.toBeNull(),
    );
    expect(painted(url)).toBe(false);
    await waitFor(() => expect(images.count(url)).toBe(2));
    await images.resolve(url);
    await waitFor(() => expect(painted(url)).toBe(true));
  });
});
