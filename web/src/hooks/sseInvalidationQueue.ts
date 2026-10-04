// Coalesces SSE invalidations: a bulk operation emits one event per item, and
// each would refetch the same heavy keys. The window is fixed from the first
// push, not a resetting debounce, so latency stays bounded under a stream.

type QueryKey = readonly unknown[];

/** Runs `run` later and returns a cancel function. */
export type Scheduler = (run: () => void) => () => void;

export type InvalidationQueue = {
  push: (keys: Iterable<QueryKey>) => void;
  cancel: () => void;
};

export function createInvalidationQueue(
  flush: (keys: QueryKey[]) => void,
  schedule: Scheduler,
): InvalidationQueue {
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
      // Never restart an open window.
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

export const INVALIDATION_WINDOW_MS = 50;

export const timeoutScheduler: Scheduler = (run) => {
  const id = setTimeout(run, INVALIDATION_WINDOW_MS);
  return () => clearTimeout(id);
};
