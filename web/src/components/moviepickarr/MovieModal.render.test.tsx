/* ============================================================
   Render tests for the movie detail modal (#178).

   The modal is a lazy read: the tile's lean object paints first, then the
   detail fills in behind it. That two-phase behaviour, and where the pieces
   land once both phases exist, is what the rebuild is about — so it's asserted
   here, through what a member sees.

   Layout itself (880px surface, rail width, the rule between the credit
   columns) is CSS, and jsdom has no layout engine — those are verified in a
   real browser. What jsdom holds: the credit rows exist while loading (so the
   block can't grow under the reader), the attribution sits in the credit block
   instead of the overview's tail, the watched line is gated on a watched film,
   and the external links are links rather than buttons.

   The router `Link` behind the genre/year chips and the detail fetch are the
   two things a unit render can't have, so both are stubbed; nothing else is.
   ============================================================ */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { MoviesKeys } from "@/api/query_keys";

import { MovieModal } from "@/components/moviepickarr/MovieModal";

import type { Movie } from "@/types/Response";
import type { ReactNode } from "react";

// The chips deep-link to /stats; outside a router there's no Link, and the modal
// isn't the place to test routing (nav.test.ts owns that).
vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, onClick }: { children: ReactNode; onClick?: () => void }) => (
    <a href="/stats" onClick={onClick}>
      {children}
    </a>
  ),
}));

// The detail read never resolves on its own: a test that wants the landed
// detail seeds the cache, so the "still loading" phase is the default.
vi.mock("@/api/APIClient", () => ({
  APIClient: { movies: { get: vi.fn(() => new Promise<never>(() => {})) } },
}));

const MOVIE_ID = 42;

/** The lean tile object: no overview, credits, cast or backdrop. */
function lean(overrides: Partial<Movie> = {}): Movie {
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
  } as Movie;
}

/** The same movie as the detail payload answers it. */
function detailed(overrides: Partial<Movie> = {}): Movie {
  return lean({
    overview: "At the height of the Vietnam war…",
    backdropPath: "/backdrop.jpg",
    crew: [
      { id: 1, name: "Francis Ford Coppola", job: "Director" },
      { id: 1, name: "Francis Ford Coppola", job: "Writer" },
      { id: 2, name: "John Milius", job: "Screenplay" },
    ],
    cast: [{ id: 9, name: "Martin Sheen", character: "Willard" }],
    ...overrides,
  });
}

function renderModal({ movie = lean(), detail }: { movie?: Movie; detail?: Movie } = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  if (detail) client.setQueryData(MoviesKeys.detail(MOVIE_ID), detail);

  render(
    <QueryClientProvider client={client}>
      <MovieModal movie={movie} open onRequestClose={vi.fn()} onClose={vi.fn()} />
    </QueryClientProvider>,
  );
  return { dialog: screen.getByRole("dialog") };
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

  it("holds only the rows still missing, so a half-filled block doesn't jump either", () => {
    const { dialog } = renderModal({
      movie: lean({ crew: [{ id: 1, name: "Francis Ford Coppola", job: "Director" }] } as Partial<Movie>),
    });

    expect(dialog.textContent).toContain("Directed by");
    expect(creditBlock(dialog).querySelectorAll(".moviemodal__credits__ghost")).toHaveLength(1);
  });

  it("fills the credits in beside the attribution once the detail lands", () => {
    const { dialog } = renderModal({ detail: detailed() });

    const block = creditBlock(dialog);
    expect(block.textContent).toContain("Directed by");
    // A person credited twice (Coppola wrote and directed) is named once per line.
    expect(screen.getAllByRole("link", { name: "Francis Ford Coppola" })).toHaveLength(2);
    expect(block.querySelectorAll(".moviemodal__credits__ghost")).toHaveLength(0);
    // The attribution belongs to the credit block, not to the tail of a text block.
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

  it("dates the watch beside the attribution on a watched film", () => {
    const { dialog } = renderModal({
      movie: lean({ watchedAt: "2026-07-21T20:00:00Z" }),
      detail: detailed({ watchedAt: "2026-07-21T20:00:00Z" }),
    });

    expect(attribution(dialog).textContent).toMatch(/Watched/);
  });

  it("says nothing about a watch on a pooled film", () => {
    const { dialog } = renderModal({ detail: detailed() });

    expect(attribution(dialog).textContent).not.toMatch(/Watched/);
  });

  it("renders the external links as quiet links to a new tab, not buttons", () => {
    renderModal({ detail: detailed() });

    for (const label of ["IMDb", "TMDB", "Letterboxd"]) {
      const link = screen.getByRole("link", { name: label });
      expect(link.getAttribute("target")).toBe("_blank");
      expect(link.getAttribute("rel")).toContain("noopener");
      expect(link.className).not.toContain("btn");
    }
    // …and they live in the rail under the poster, not in a trailing row.
    expect(screen.getByRole("link", { name: "IMDb" }).closest(".moviemodal__rail")).not.toBeNull();
  });

  it("caps the surface so the content scrolls inside it, not the page", () => {
    const { dialog } = renderModal();

    expect(dialog.classList.contains("modal--capped")).toBe(true);
    expect(dialog.querySelector(".modal__scroll")).not.toBeNull();
    // The close X stays outside the scrolling region so it doesn't scroll away.
    const close = screen.getByRole("button", { name: "Close" });
    expect(close.closest(".modal__scroll")).toBeNull();
  });

  it("keeps the cast strip out of the way when the film has no cast", () => {
    const { dialog } = renderModal({ detail: detailed({ cast: [] }) });

    expect(dialog.querySelector(".castrow")).toBeNull();
  });
});
