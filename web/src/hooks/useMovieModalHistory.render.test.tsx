/* Render tests for the movie modal's history entry (#196): the hook, the Modal
   shell and a memory-history router wired the way both tabs wire them.
   `router.history.back()` is the same path the browser's Back button takes. */

import { act, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Modal } from "@/components/moviepickarr/Modal";

import type { MovieTile } from "@/types/Response";

import { clearMovieModalHistory, useMovieModal } from "@/hooks/useMovieModalHistory";
import { renderWithProviders } from "@/test/providers";

const MOVIES = "/" as const;
// The modal's entry must not disturb Stats' search params (the bug behind #196).
const STATS = "/stats?win=year&genres=27" as const;

function movie(overrides: Partial<MovieTile> = {}): MovieTile {
  return {
    movieID: 42,
    title: "Possession",
    link: "",
    addedAt: "2026-07-01T00:00:00Z",
    addedByID: 1,
    addedByName: "Ada",
    ...overrides,
  };
}

/** What both tabs do: open pushes an entry, every dismiss pops it. */
function Subject({
  movies = [movie()],
  deleteResult,
}: {
  movies?: MovieTile[];
  deleteResult?: Promise<void>;
} = {}) {
  const { selected, isOpen, open, close, onClosed } = useMovieModal();
  return (
    <>
      {movies.map((movie) => (
        <button key={movie.movieID} type="button" onClick={() => open(movie)}>
          {movie.title}
        </button>
      ))}
      {selected && (
        <Modal label={selected.title} open={isOpen} onRequestClose={close} onClose={onClosed} capped>
          {(closeGesture) => (
            <>
              <h2>{selected.title}</h2>
              {deleteResult && (
                <button type="button" onClick={() => void deleteResult.then(close)}>
                  Delete movie
                </button>
              )}
              <button type="button" onClick={closeGesture}>
                Close
              </button>
            </>
          )}
        </Modal>
      )}
    </>
  );
}

function mount(
  path: typeof MOVIES | typeof STATS,
  movies?: MovieTile[],
  deleteResult?: Promise<void>,
) {
  return renderWithProviders(<Subject movies={movies} deleteResult={deleteResult} />, {
    path,
    seed: () => {},
  });
}

/** Long enough to outrun exitDelayMs(), whatever the motion tokens say. */
const AFTER_EXIT = 1000;

async function runExit() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(AFTER_EXIT);
  });
}

function poster(name: string) {
  return screen.getByRole("button", { name });
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe.each([
  ["the movies tab", MOVIES],
  ["the stats tab", STATS],
])("on %s", (_name, path) => {
  it("opens the modal without touching the URL", async () => {
    const { router } = await mount(path);
    const before = router.history.location.href;

    fireEvent.click(poster("Possession"));

    expect(screen.getByRole("dialog", { name: "Possession" })).not.toBeNull();
    expect(router.history.location.href).toBe(before);
  });

  it("closes on browser Back, leaving the URL where it was", async () => {
    const { router } = await mount(path);
    const before = router.history.location.href;

    fireEvent.click(poster("Possession"));
    act(() => router.history.back());

    // The entry is already gone, but the surface stays for its exit motion.
    expect(screen.queryByRole("dialog")).not.toBeNull();

    await runExit();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(router.history.location.href).toBe(before);
  });

  it.each([
    ["Escape", () => fireEvent.keyDown(document, { key: "Escape" })],
    ["the close button", () => fireEvent.click(screen.getByRole("button", { name: "Close" }))],
  ])("closes on %s by popping the same entry", async (_gestureName, gesture) => {
    const { router } = await mount(path);

    fireEvent.click(poster("Possession"));
    expect(router.history.canGoBack()).toBe(true);

    act(gesture);
    await runExit();

    expect(screen.queryByRole("dialog")).toBeNull();
    // Popped, not merely hidden, or Back would then do nothing.
    expect(router.history.canGoBack()).toBe(false);
  });
});

describe("the history stack", () => {
  it("cancels its exit when Forward restores the entry before the motion finishes", async () => {
    const { router } = await mount(MOVIES);
    const opener = poster("Possession");
    opener.focus();
    fireEvent.click(opener);
    const dialog = screen.getByRole("dialog");
    const closeButton = screen.getByRole("button", { name: "Close" });
    closeButton.focus();

    act(() => router.history.back());
    expect(dialog.classList.contains("modal--closing")).toBe(true);

    act(() => router.history.forward());
    expect(screen.getByRole("dialog")).toBe(dialog);
    expect(dialog.classList.contains("modal--closing")).toBe(false);

    await runExit();
    expect(screen.getByRole("dialog")).toBe(dialog);
    expect(document.activeElement).toBe(closeButton);

    fireEvent.keyDown(document, { key: "Escape" });
    await runExit();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(opener);
  });

  it("stays flat however many movies are opened and closed", async () => {
    const movies = [movie(), movie({ movieID: 7, title: "Stalker" }), movie({ movieID: 9, title: "Solaris" })];
    const { router } = await mount(MOVIES, movies);

    for (const movie of movies) {
      fireEvent.click(poster(movie.title));
      act(() => router.history.back());
      await runExit();
    }

    // Each open consumed its entry, so Back does not replay closed modals.
    expect(router.history.canGoBack()).toBe(false);
  });

  it("does not let a finished delete pop a newer modal entry", async () => {
    let finishDelete!: () => void;
    const deleteResult = new Promise<void>((resolve) => {
      finishDelete = resolve;
    });
    const movies = [movie(), movie({ movieID: 7, title: "Stalker" })];
    const { router } = await mount(MOVIES, movies, deleteResult);

    fireEvent.click(poster("Possession"));
    fireEvent.click(screen.getByRole("button", { name: "Delete movie" }));

    // The old completion must own neither the new modal nor its entry.
    act(() => router.history.back());
    await runExit();
    fireEvent.click(poster("Stalker"));
    const stalkerEntry = router.history.location.state.movieModal;

    await act(async () => {
      finishDelete();
      await deleteResult;
    });

    expect(screen.getByRole("heading", { name: "Stalker" })).not.toBeNull();
    expect(router.history.location.state.movieModal).toBe(stalkerEntry);
  });
});

describe("an entry left behind by navigating away", () => {
  it("doesn't hold the modal open when the same movie is opened again", async () => {
    const { router } = await mount(MOVIES);

    // Strand the entry, and let the exit finish: returning mid-motion is the
    // interrupted-exit case above.
    fireEvent.click(poster("Possession"));
    await act(async () => void (await router.navigate({ to: "/admin" })));
    await runExit();
    expect(screen.queryByRole("dialog")).toBeNull();

    act(() => router.history.back());
    await runExit();
    expect(screen.queryByRole("dialog")).toBeNull();

    // Same movie: the stranded entry below still matches its id.
    fireEvent.click(poster("Possession"));
    expect(screen.queryByRole("dialog")).not.toBeNull();

    act(() => fireEvent.keyDown(document, { key: "Escape" }));
    await runExit();
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("a reload with the modal open", () => {
  it("drops the entry's modal so the page comes back clean", async () => {
    const { router } = await mount(MOVIES);

    fireEvent.click(poster("Possession"));
    const href = router.history.location.href;
    expect(router.history.location.state.movieModal).toBeDefined();

    // Location state survives a reload; startup clears it before the first render.
    act(() => clearMovieModalHistory(router));
    await runExit();

    expect(router.history.location.state.movieModal).toBeUndefined();
    expect(router.history.location.href).toBe(href);
    expect(screen.queryByRole("dialog")).toBeNull();

    // Spent, not emptied: Back goes past the modal rather than reopening it.
    act(() => router.history.back());
    await runExit();
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
