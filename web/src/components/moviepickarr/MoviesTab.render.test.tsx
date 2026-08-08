import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AuthKeys, MoviesKeys, SettingsKeys } from "@/api/query_keys";

import { MoviesTab } from "@/components/moviepickarr/MoviesTab";

import type { MeResponse, MovieTile } from "@/types/Response";

import { renderWithProviders } from "@/test/providers";

const virtualizerOptions = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: (options: { count: number; getScrollElement: () => HTMLElement }) => {
    virtualizerOptions(options);
    return {
      getTotalSize: () => options.count * 300,
      getVirtualItems: () =>
        Array.from({ length: options.count }, (_, index) => ({
          key: index,
          index,
          start: index * 300,
        })),
      measureElement: () => {},
    };
  },
}));

const poolMovie: MovieTile = {
  movieID: 1,
  title: "Pool Film",
  link: "",
  addedAt: "2026-07-22T10:00:00Z",
  addedByID: 7,
  addedByName: "Ada",
  posterPath: "/pool.jpg",
  voteAverage: 8.1,
};

const watchedMovie: MovieTile = {
  ...poolMovie,
  movieID: 2,
  title: "Watched Film",
  posterPath: "/watched.jpg",
  watchedAt: "2026-07-28T20:00:00Z",
};

const me: MeResponse = {
  id: 7,
  displayName: "Ada",
  username: null,
  role: "admin",
  hasLocalLogin: true,
  hasLinkedIdentity: false,
};

async function renderTab() {
  return renderWithProviders(<MoviesTab />, {
    path: "/",
    seed: (client) => {
      client.setQueryData(MoviesKeys.listpool(), [poolMovie]);
      client.setQueryData(MoviesKeys.listwatched(), [watchedMovie]);
      client.setQueryData(SettingsKeys.poolLock(), {
        poolLocked: false,
        drawInProgress: false,
      });
      client.setQueryData(AuthKeys.me(), me);
    },
  });
}

const candidates = (path: string) =>
  `https://image.tmdb.org/t/p/w154/${path} 154w, ` +
  `https://image.tmdb.org/t/p/w185/${path} 185w, ` +
  `https://image.tmdb.org/t/p/w342/${path} 342w, ` +
  `https://image.tmdb.org/t/p/w500/${path} 500w`;

describe("Movies poster sources", () => {
  it("virtualizes the watched grid against the body document owner", async () => {
    await renderTab();

    const options = virtualizerOptions.mock.lastCall?.[0] as {
      getScrollElement: () => HTMLElement;
    };
    expect(options.getScrollElement()).toBe(document.body);
  });

  it("provides responsive candidates for fluid pool and watched grids", async () => {
    await renderTab();

    const pool = screen.getByRole("img", { name: "Pool Film" });
    const watched = screen.getByRole("img", { name: "Watched Film" });

    expect(pool.getAttribute("sizes")).toBe("auto, 342px");
    expect(pool.getAttribute("srcset")).toBe(candidates("pool.jpg"));
    expect(watched.getAttribute("sizes")).toBe("auto, 342px");
    expect(watched.getAttribute("srcset")).toBe(candidates("watched.jpg"));
    expect(watched.closest(".tile")?.parentElement?.style.justifyContent).toBe("");
    expect(document.querySelector(".poster__badge")).toBeNull();
  });

  it("sizes the fixed poster in watched-list rows at 44px", async () => {
    await renderTab();

    fireEvent.click(screen.getByRole("button", { name: "List view" }));

    const watched = await waitFor(() =>
      screen.getByRole("img", { name: "Watched Film" }),
    );
    expect(watched.getAttribute("sizes")).toBe("auto, 44px");
    expect(watched.getAttribute("srcset")).toBe(candidates("watched.jpg"));
    expect(watched.closest(".wrow")?.querySelector(".rating")?.textContent).toBe("8.1");
  });
});
