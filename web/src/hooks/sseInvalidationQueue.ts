/* ============================================================
   moviepickarr: coalescing for SSE query invalidations.

   A bulk operation (adding, moving or deleting a handful of movies) emits one
   SSE event per item, and each event stales the same heavy keys: the board
   list (every member with their full pool and stash) and the pool list.
   Invalidating per event means one refetch round per event against the single
   SQLite connection.

   This queue collects keys in a window and flushes each distinct key once.
   The window is a fixed delay from the FIRST enqueue, not a resetting
   debounce: latency stays bounded at the window length even under a sustained
   stream, and a lone event pays that window and nothing more.

   Pure: the timer comes in as a scheduler, so tests drive it by hand.
   ============================================================ */

type QueryKey = readonly unknown[];

/** Runs `run` later and returns a cancel function. */
export type Scheduler = (run: () => void) => () => void;

export type InvalidationQueue = {
  /** Collect keys for the next flush, opening a window if none is open. */
  push: (keys: Iterable<QueryKey>) => void;
  /** Drop anything pending and close the window (unmount). */
  cancel: () => void;
};

export function createInvalidationQueue(
  flush: (keys: QueryKey[]) => void,
  schedule: Scheduler,
): InvalidationQueue {
  // Hashed key → the key itself, so a burst of identical keys collapses.
  const pending = new Map<string, QueryKey>();
  let cancelScheduled: (() => void) | null = null;

  const run = () => {
    cancelScheduled = null;
    if (pending.size === 0) return;
    const keys = [...pending.values()];
    pending.clear();
    flush(keys);
  };

  return {
    push(keys) {
      let added = false;
      for (const key of keys) {
        pending.set(JSON.stringify(key), key);
        added = true;
      }
      // Nothing to flush (an event with an empty row), and never restart an
      // open window: the window measures from the first key, not the last.
      if (!added || cancelScheduled !== null) return;
      cancelScheduled = schedule(run);
    },

    cancel() {
      pending.clear();
      if (cancelScheduled !== null) {
        cancelScheduled();
        cancelScheduled = null;
      }
    },
  };
}

/** The coalescing window. Short enough to feel immediate for a lone event,
 *  long enough that a per-item burst lands in one flush. */
export const INVALIDATION_WINDOW_MS = 50;

/** The production scheduler: a fixed trailing window. */
export const timeoutScheduler: Scheduler = (run) => {
  const id = setTimeout(run, INVALIDATION_WINDOW_MS);
  return () => clearTimeout(id);
};
