/* ============================================================
   Render tests for the movie modal's history entry (#196).

   The modal is opened from a history entry rather than a plain boolean, so
   browser Back closes it. What that's really made of is a hook and the Modal
   shell talking to each other through a router, and none of the three can
   show the behaviour alone: the hook's entry is meaningless without a surface
   reacting to it, and the shell can't reach a router. So the subject here is
   the wiring, mounted on a memory history the way both tabs mount it.

   The tabs themselves aren't the subject. MoviesTab and StatsTab differ only
   in which list they derive the live movie from, and seeding either one's
   half-dozen queries would test the seeding. What they share is reproduced
   below, once, and run against both surfaces.

   "Browser Back" is `router.history.back()` here. That's not a shortcut: the
   app's own dismiss gestures call exactly that, and against a memory history
   it's the same code path the browser's button takes through popstate.
   ============================================================ */

import { act, fireEvent, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Modal } from "@/components/moviepickarr/Modal";

import type { Movie } from "@/types/Response";

import { clearMovieModalHistory, useMovieModal } from "@/hooks/useMovieModalHistory";
import { renderWithProviders } from "@/test/providers";

const MOVIES = "/" as const;
// The Stats view lives entirely in its search params, and the entry the modal
// pushes must not disturb them, which is the bug that started the issue.
const STATS = "/stats?win=year&genres=27" as const;

function movie(overrides: Partial<Movie> = {}): Movie {
  return { movieID: 42, title: "Possession", ...overrides } as Movie;
}

/** What both tabs do: open pushes an entry, every dismiss pops it, and the
 *  surface outlives the entry just long enough to play its exit. */
function Subject({ films = [movie()] }: { films?: Movie[] } = {}) {
  const { selected, isOpen, open, close, onClosed } = useMovieModal();
  return (
    <>
      {films.map((film) => (
        <button key={film.movieID} type="button" onClick={() => open(film)}>
          {film.title}
        </button>
      ))}
      {selected && (
        <Modal open={isOpen} onRequestClose={close} onClose={onClosed} capped>
          {(closeGesture) => (
            <>
              <h2>{selected.title}</h2>
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

function mount(path: typeof MOVIES | typeof STATS, films?: Movie[]) {
  return renderWithProviders(<Subject films={films} />, { path, seed: () => {} });
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

    expect(screen.getByRole("dialog")).not.toBeNull();
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
    // Popped, not merely hidden: a gesture that closed without going back
    // would leave its entry behind and Back would then do nothing.
    expect(router.history.canGoBack()).toBe(false);
  });
});

describe("the history stack", () => {
  it("stays flat however many films are opened and closed", async () => {
    const films = [movie(), movie({ movieID: 7, title: "Stalker" }), movie({ movieID: 9, title: "Solaris" })];
    const { router } = await mount(MOVIES, films);

    for (const film of films) {
      fireEvent.click(poster(film.title));
      act(() => router.history.back());
      await runExit();
    }

    // Each open consumed the entry it pushed, so one more Back leaves the
    // page instead of replaying three closed modals.
    expect(router.history.canGoBack()).toBe(false);
  });
});

describe("an entry left behind by navigating away", () => {
  it("doesn't hold the modal open when the same film is opened again", async () => {
    const { router } = await mount(MOVIES);

    // Leave with the modal up, which strands its entry, then come back to it.
    fireEvent.click(poster("Possession"));
    await act(async () => void (await router.navigate({ to: "/admin" })));
    act(() => router.history.back());
    await runExit();
    expect(screen.queryByRole("dialog")).toBeNull();

    // Same film again: the stranded entry underneath still describes it, so
    // an id would match and the dismissal below would land on a "still open"
    // entry with every gesture already spent.
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

    // Location state survives a reload of its entry; this is what startup
    // does before the first render, so nothing reads a stale open modal.
    act(() => clearMovieModalHistory(router));
    await runExit();

    expect(router.history.location.state.movieModal).toBeUndefined();
    expect(router.history.location.href).toBe(href);
    expect(screen.queryByRole("dialog")).toBeNull();

    // The entry is spent, not merely emptied: Back after the reload goes to
    // whatever was under the modal rather than re-opening it.
    act(() => router.history.back());
    await runExit();
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
