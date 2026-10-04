/* The impure shell around drawMachine: resolves env, runs the reducer, executes
   commands. A module singleton so it outlives the Hero: a tab-switch remount cannot
   replay a spin, while a full reload starts fresh. */

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

/** The machine's side effects, injected so tests can use fakes. */
export interface DrawStoreDeps {
  resolveEnv: () => DrawEnv;
  /** Failures are swallowed: the server's auto-reveal deadline is the backstop. */
  postReveal: () => Promise<unknown>;
  /** Resolves when paintable; null when there is nothing to decode. */
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
        const done = (decodedBackdropPath: string | null) =>
          send({ type: "DECODE_DONE", drawnAt: command.drawnAt, decodedBackdropPath });
        const pending = command.backdropPath ? deps.decodeBackdrop(command.backdropPath) : null;
        if (pending) pending.then(() => done(command.backdropPath), () => done(null));
        else done(null);
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

/** The live environment snapshot, also for machine helpers outside a dispatch. */
export function resolveDrawEnv(): DrawEnv {
  return {
    spinDurationMs: spinDurationMs(),
    reducedMotion: prefersReducedMotion(),
    clientId: getClientId(),
    confirmFallbackMs: 10_000,
    fallbackGraceMs: 5_000,
    now: Date.now(),
  };
}

export const drawStore = createDrawStore({
  resolveEnv: resolveDrawEnv,
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
