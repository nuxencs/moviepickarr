/* ============================================================
   Render tests for the draw reel's mount behaviour.

   The reel's scroll progress is component state and the Movies tab unmounts
   with the route, so a mount is not always the start of a scroll: switching
   tabs and coming back mounts a fresh reel onto a draw that has already moved
   on. There's no pure seam below that (it IS the mount), so it's tested here,
   through what a member sees: the label, the Skip control, and the OK confirm.
   ============================================================ */

import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { SpinDescriptor } from "@/components/moviepickarr/drawMachine";
import { DrawReel } from "@/components/moviepickarr/DrawReel";

import type { Movie } from "@/types/Response";

const NOW = 1_000_000;
const SPIN_MS = 6500;

function movie(id: number): Movie {
  return {
    movieID: id,
    title: `Movie ${id}`,
    link: "",
    addedAt: "2026-07-17T20:00:00Z",
    addedByID: 1,
    addedByName: "ana",
  } as Movie;
}

function spin(overrides: Partial<SpinDescriptor> = {}): SpinDescriptor {
  return {
    drawnAt: "2026-07-17T20:00:00Z",
    winnerId: 1,
    candidates: [movie(1), movie(2), movie(3)],
    durationMs: SPIN_MS,
    startedAtMs: NOW,
    live: true,
    mine: true,
    confirmMs: 10_000,
    ...overrides,
  };
}

function renderReel(props: Partial<Parameters<typeof DrawReel>[0]> = {}) {
  const onScrollDone = vi.fn();
  const onConfirm = vi.fn();
  const view = render(
    <DrawReel
      spin={spin()}
      phase="spinning"
      canReveal
      revealTip=""
      onScrollDone={onScrollDone}
      onConfirm={onConfirm}
      {...props}
    />,
  );
  return { ...view, onScrollDone, onConfirm };
}

/** Let the reel's timers run: the scroll's own settle timer fires inside act,
 *  so the render that follows is the one a member would be looking at. */
const advance = (ms: number) => act(() => void vi.advanceTimersByTime(ms));

const skip = () => screen.queryByRole("button", { name: "Skip" });
const ok = () => screen.queryByRole("button", { name: "OK" });

beforeEach(() => {
  vi.useFakeTimers({ now: NOW });
});

afterEach(() => {
  vi.useRealTimers();
});

describe("a fresh draw", () => {
  it("scrolls, then rests on the winner and offers the confirm", () => {
    const { onScrollDone } = renderReel();

    expect(screen.getByText("Drawing…")).toBeTruthy();
    expect(skip()).toBeTruthy();
    expect(ok()).toBeNull();

    advance(SPIN_MS + 200);

    expect(screen.getByText("Your draw")).toBeTruthy();
    expect(ok()).toBeTruthy();
    expect(skip()).toBeNull();
    expect(onScrollDone).toHaveBeenCalled();
  });
});

describe("coming back to the Movies tab", () => {
  it("keeps the confirm up when the draw already settled, instead of replaying the scroll", () => {
    const { unmount } = renderReel();
    advance(SPIN_MS + 200);
    expect(ok()).toBeTruthy();

    // Switching tabs unmounts the Hero; switching back mounts a new reel
    // against the same draw, which the machine now reports as settled.
    unmount();
    renderReel({ spin: spin(), phase: "settled" });

    expect(ok()).toBeTruthy();
    expect(screen.getByText("Your draw")).toBeTruthy();
    expect(skip()).toBeNull();
  });

  it("finishes a mid-scroll draw on its original schedule, not 6.5s later", () => {
    const { unmount } = renderReel();
    advance(5000);
    unmount();

    // 5s in: 1.5s of scroll is left, and that's all the remount may take.
    renderReel({ spin: spin(), phase: "spinning" });
    expect(skip()).toBeTruthy();

    advance(1500 + 200);
    expect(ok()).toBeTruthy();
    expect(skip()).toBeNull();
  });

  it("reports the landing when the scroll window ran out with no reel mounted", () => {
    const { unmount } = renderReel();
    unmount();

    // Away long enough that the scroll would have finished: nothing was
    // mounted to notice, so the machine is still spinning and needs telling.
    advance(SPIN_MS + 1000);
    const { onScrollDone } = renderReel({ spin: spin(), phase: "spinning" });

    expect(ok()).toBeTruthy();
    expect(skip()).toBeNull();
    expect(onScrollDone).toHaveBeenCalled();
  });
});
