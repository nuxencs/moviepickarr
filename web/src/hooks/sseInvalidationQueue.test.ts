import { describe, expect, it, vi } from "vitest";

import {
  createInvalidationQueue,
  INVALIDATION_WINDOW_MS,
  timeoutScheduler,
} from "@/hooks/sseInvalidationQueue";

/** A scheduler under test control: `fire()` runs the pending callback. */
function manualScheduler() {
  let pending: (() => void) | null = null;
  let scheduled = 0;
  let cancelled = 0;
  return {
    scheduler: (run: () => void) => {
      pending = run;
      scheduled++;
      return () => {
        pending = null;
        cancelled++;
      };
    },
    fire() {
      const run = pending;
      pending = null;
      run?.();
    },
    get scheduled() {
      return scheduled;
    },
    get cancelled() {
      return cancelled;
    },
    get isPending() {
      return pending !== null;
    },
  };
}

describe("the invalidation queue", () => {
  it("collapses a burst of per-item events into one flush per distinct key", () => {
    const flush = vi.fn();
    const clock = manualScheduler();
    const queue = createInvalidationQueue(flush, clock.scheduler);

    // Ten movie:added events in the window: same two keys every time.
    for (let i = 0; i < 10; i++) {
      queue.push([["users", "list"], ["movies", "pool"]]);
    }
    expect(flush).not.toHaveBeenCalled();

    clock.fire();
    expect(flush).toHaveBeenCalledTimes(1);
    expect(flush.mock.calls[0][0]).toEqual([["users", "list"], ["movies", "pool"]]);
  });

  it("keeps distinct keys from different events in the same flush", () => {
    const flush = vi.fn();
    const clock = manualScheduler();
    const queue = createInvalidationQueue(flush, clock.scheduler);

    queue.push([["users", "list"]]);
    queue.push([["movies", "pool"], ["stats"]]);
    clock.fire();

    expect(flush.mock.calls[0][0]).toEqual([["users", "list"], ["movies", "pool"], ["stats"]]);
  });

  it("measures the window from the first key, so a stream still flushes", () => {
    const flush = vi.fn();
    const clock = manualScheduler();
    const queue = createInvalidationQueue(flush, clock.scheduler);

    queue.push([["users", "list"]]);
    queue.push([["users", "list"]]);
    // A resetting debounce would have re-armed here; this one has not.
    expect(clock.scheduled).toBe(1);
    expect(clock.cancelled).toBe(0);
  });

  it("opens a fresh window for the next burst", () => {
    const flush = vi.fn();
    const clock = manualScheduler();
    const queue = createInvalidationQueue(flush, clock.scheduler);

    queue.push([["users", "list"]]);
    clock.fire();
    queue.push([["users", "list"]]);
    clock.fire();

    expect(flush).toHaveBeenCalledTimes(2);
    expect(clock.scheduled).toBe(2);
  });

  it("stays idle for an event with no keys", () => {
    const flush = vi.fn();
    const clock = manualScheduler();
    const queue = createInvalidationQueue(flush, clock.scheduler);

    // movie:revealed is a pure draw-machine signal: nothing cached stales.
    queue.push([]);
    expect(clock.scheduled).toBe(0);
    clock.fire();
    expect(flush).not.toHaveBeenCalled();
  });

  it("drops pending work on cancel", () => {
    const flush = vi.fn();
    const clock = manualScheduler();
    const queue = createInvalidationQueue(flush, clock.scheduler);

    queue.push([["users", "list"]]);
    queue.cancel();
    expect(clock.cancelled).toBe(1);
    expect(clock.isPending).toBe(false);

    clock.fire();
    expect(flush).not.toHaveBeenCalled();

    // Usable again after cancel (cancel is unmount, but keep it honest).
    queue.push([["users", "list"]]);
    clock.fire();
    expect(flush).toHaveBeenCalledTimes(1);
  });
});

describe("the timeout scheduler", () => {
  it("flushes after the window and cancels cleanly", () => {
    vi.useFakeTimers();
    try {
      const run = vi.fn();
      timeoutScheduler(run);
      vi.advanceTimersByTime(INVALIDATION_WINDOW_MS - 1);
      expect(run).not.toHaveBeenCalled();
      vi.advanceTimersByTime(1);
      expect(run).toHaveBeenCalledTimes(1);

      const cancel = timeoutScheduler(run);
      cancel();
      vi.advanceTimersByTime(INVALIDATION_WINDOW_MS);
      expect(run).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });
});
