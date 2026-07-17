/* ============================================================
   moviepickarr — the Draw store.

   The thin impure shell around drawMachine: it resolves the environment
   snapshot at send() time, runs the reducer, executes the returned commands
   through injected executors, and notifies subscribers. A module singleton —
   like the old handledDraws Set, deliberately outliving the Hero so a
   tab-switch remount can't replay a spin, while a full reload starts fresh.

   Components read it with useSyncExternalStore(drawStore.subscribe,
   drawStore.getState); useSSE and the draw mutation feed it events.
   ============================================================ */

import { APIClient } from "@/api/APIClient";
import { MoviesKeys } from "@/api/query_keys";
import { queryClient } from "@/api/QueryClient";

import {
  type DrawCommand,
  type DrawEnv,
  type DrawEvent,
  type DrawState,
  initialDrawState,
  reduce,
} from "@/components/moviepickarr/drawMachine";
import { prefersReducedMotion, spinDurationMs } from "@/components/moviepickarr/drawSpin";
import { backdropUrl } from "@/components/moviepickarr/lib";

import { getClientId } from "@/lib/clientId";

/** Everything impure the machine's commands need, injected so tests can run
 *  the full store against fakes (see drawStore.test.ts). */
export interface DrawStoreDeps {
  resolveEnv: () => DrawEnv;
  /** POST the reveal confirm. Failures are swallowed — the server's own
   *  auto-reveal deadline is the backstop. */
  postReveal: () => Promise<unknown>;
  /** Warm + decode the winner's backdrop; resolve when paintable. Null when
   *  the image can't be decoded (the commit then proceeds anyway). */
  decodeBackdrop: (path: string) => Promise<unknown> | null;
  invalidatePool: () => void;
}

export interface DrawStore {
  getState: () => DrawState;
  subscribe: (listener: () => void) => () => void;
  send: (event: DrawEvent) => void;
}

export function createDrawStore(deps: DrawStoreDeps): DrawStore {
  let state = initialDrawState;
  let fallbackTimer: ReturnType<typeof setTimeout> | null = null;
  const listeners = new Set<() => void>();

  function send(event: DrawEvent): void {
    const [next, commands] = reduce(state, event, deps.resolveEnv());
    if (next !== state) {
      state = next;
      listeners.forEach((l) => l());
    }
    for (const command of commands) run(command);
  }

  function run(command: DrawCommand): void {
    switch (command.cmd) {
      case "postReveal":
        void deps.postReveal().catch(() => {});
        break;
      case "decode": {
        const done = () => send({ type: "DECODE_DONE", drawnAt: command.drawnAt });
        const pending = command.backdropPath ? deps.decodeBackdrop(command.backdropPath) : null;
        if (pending) pending.then(done, done);
        else done();
        break;
      }
      case "invalidatePool":
        deps.invalidatePool();
        break;
      case "scheduleFallback":
        if (fallbackTimer !== null) clearTimeout(fallbackTimer);
        fallbackTimer = setTimeout(() => send({ type: "CONFIRM", source: "local" }), command.afterMs);
        break;
      case "cancelFallback":
        if (fallbackTimer !== null) {
          clearTimeout(fallbackTimer);
          fallbackTimer = null;
        }
        break;
    }
  }

  return {
    getState: () => state,
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    send,
  };
}

export const drawStore = createDrawStore({
  resolveEnv: () => ({
    spinDurationMs: spinDurationMs(),
    reducedMotion: prefersReducedMotion(),
    clientId: getClientId(),
    confirmFallbackMs: 10_000,
    fallbackGraceMs: 5_000,
  }),
  postReveal: () => APIClient.movies.reveal(),
  decodeBackdrop: (path) => {
    const url = backdropUrl(path);
    if (!url) return null;
    const img = new Image();
    img.src = url;
    return img.decode();
  },
  invalidatePool: () => void queryClient.invalidateQueries({ queryKey: MoviesKeys.listpool() }),
});
