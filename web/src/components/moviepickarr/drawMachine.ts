/* The Draw machine: a pure reducer for the client side of a draw (dedup, resume,
   settle, reveal-once). No DOM, timers, or fetches: env comes in as a snapshot and
   side effects leave as commands, which drawStore executes.

     idle -> spinning -> settled -> revealing -> idle (commitSeq bumps)
                  \________________^  (a remote reveal may close a scrolling reel)
*/

import type { MovieDetail, MovieDrawPayload, MovieTile } from "@/types/Response";

/** Environment snapshot, resolved by the store at send() time. */
export interface DrawEnv {
  /** Reel scroll length (the --dur-spin token). */
  spinDurationMs: number;
  reducedMotion: boolean;
  /** This browser's stable client id: decides `mine`. */
  clientId: string;
  /** Confirm-countdown length when a payload carries no revealAt. */
  confirmFallbackMs: number;
  /** Grace past the reveal deadline before the self-heal confirm (a dropped movie:revealed). */
  fallbackGraceMs: number;
  /** Wall clock at send() time. */
  now: number;
}

/** The reel descriptor a spin renders. Immutable per draw. */
export interface SpinDescriptor {
  /** Server draw time (RFC3339): the draw's identity. */
  drawnAt: string;
  winner: MovieDetail;
  /** The draw candidates, winner included, deduped by id. */
  candidates: MovieTile[];
  /** How long THIS client scrolls: full duration fresh, remaining on resume. */
  durationMs: number;
  /** Wall clock at scroll start. Kept here, not in the reel, so a remount (tab switch) can resume. */
  startedAtMs: number;
  /** False on reload-resume; gates the draw sound. */
  live: boolean;
  /** Whether this client drew. Drives only the countdown fill; the turn gate owns the reveal. */
  mine: boolean;
  /** The server's reveal deadline in this client's clock (skew-free). An instant, not a
   *  length, so a skipped scroll or a remount still lands the confirm on the reveal. */
  deadlineAtMs: number;
}

export type DrawPhase = "idle" | "spinning" | "settled" | "revealing";

export interface DrawState {
  phase: DrawPhase;
  /** Null only in idle. */
  spin: SpinDescriptor | null;
  /** drawnAt of every handled draw: dedups SSE vs the mutation response and stops remount replays. */
  seen: readonly string[];
  /** Bumps once per completed reveal, so the Hero commits in the same render that drops the reel. */
  commitSeq: number;
  /** Backdrop the reveal decode proved paintable. */
  decodedBackdrop: {
    movieID: number;
    drawnAt: string;
    backdropPath: string;
  } | null;
}

export const initialDrawState: DrawState = {
  phase: "idle",
  spin: null,
  seen: [],
  commitSeq: 0,
  decodedBackdrop: null,
};

export type DrawEvent =
  /** From movie:drawn or the drawer's mutation response; the later one dedups. */
  | { type: "DRAWN"; movie: MovieDrawPayload }
  /** Reload with a pending draw. */
  | { type: "RESUME"; current: MovieDetail; pool: MovieTile[] }
  | { type: "SCROLL_DONE" }
  /** Local: the drawer's OK or the fallback timer. Remote: a matching movie:revealed. */
  | { type: "CONFIRM"; source: "local" | "remote" }
  | { type: "REVEALED"; drawnAt: string }
  /** decodedBackdropPath is null when there was no backdrop or the decode failed. */
  | { type: "DECODE_DONE"; drawnAt: string; decodedBackdropPath: string | null };

export type DrawCommand =
  /** Once per draw, only for a local confirm: a remote one is the server. */
  | { cmd: "postReveal" }
  | { cmd: "decode"; drawnAt: string; backdropPath: string | null }
  /** Held until the reel lands, so the grid does not drop the winner mid-spin and spoil it. */
  | { cmd: "invalidatePool" }
  | { cmd: "scheduleFallback"; afterMs: number }
  | { cmd: "cancelFallback" };

const NONE: DrawCommand[] = [];

function uniqueById(movies: MovieTile[]): MovieTile[] {
  const seenIds = new Set<number>();
  const out: MovieTile[] = [];
  for (const m of movies) {
    if (m && !seenIds.has(m.movieID)) {
      seenIds.add(m.movieID);
      out.push(m);
    }
  }
  return out;
}

/** revealAt − reference (both server clocks, so no skew) added to env.now. Floored a
 *  second past the scroll so the confirm stays visible. */
function deadline(reference: string | undefined, revealAt: string | undefined, durationMs: number, env: DrawEnv): number {
  const floor = env.now + durationMs + 1000;
  if (reference && revealAt) {
    const left = Date.parse(revealAt) - Date.parse(reference);
    if (Number.isFinite(left)) return Math.max(floor, env.now + left);
  }
  return env.now + durationMs + env.confirmFallbackMs;
}

/** Whether a reel is pending, decidable before the pool loads. */
export function drawAwaitingReveal(current: MovieDetail, env: DrawEnv): boolean {
  if (env.reducedMotion) return false;
  return !!current.drawnAt && !current.revealed;
}

/** The spin for a fresh draw, or null to skip the reel. */
function buildLiveSpin(drawn: MovieDrawPayload, env: DrawEnv): SpinDescriptor | null {
  if (env.reducedMotion || !drawn.drawnAt) return null;
  const candidates = uniqueById([...(drawn.candidates ?? []), drawn]);
  if (candidates.length < 2) return null;
  return {
    drawnAt: drawn.drawnAt,
    winner: drawn,
    candidates,
    durationMs: env.spinDurationMs,
    startedAtMs: env.now,
    live: true,
    mine: !!drawn.drawClientId && drawn.drawClientId === env.clientId,
    deadlineAtMs: deadline(drawn.serverNow ?? drawn.drawnAt, drawn.revealAt, env.spinDurationMs, env),
  };
}

/** The spin for a reload mid-draw, resumed from serverNow − drawnAt, or null. */
function buildResumeSpin(current: MovieDetail, pool: MovieTile[], env: DrawEnv): SpinDescriptor | null {
  if (env.reducedMotion || !current.drawnAt || !current.serverNow || current.revealed) return null;
  const elapsed = Date.parse(current.serverNow) - Date.parse(current.drawnAt);
  if (!Number.isFinite(elapsed)) return null;
  // The winner may already be in the pool as a lean tile; keep that tile's position.
  const candidates = uniqueById([...(pool ?? []), current]);
  if (candidates.length < 2) return null;
  const durationMs = Math.max(0, env.spinDurationMs - elapsed);
  return {
    drawnAt: current.drawnAt,
    winner: current,
    candidates,
    durationMs,
    startedAtMs: env.now,
    live: false,
    mine: !!current.drawClientId && current.drawClientId === env.clientId,
    deadlineAtMs: deadline(current.serverNow, current.revealAt, durationMs, env),
  };
}

/** Where a reel picks up on mount: a mount can be the same draw returning after a tab switch. */
export function reelResume(
  spin: SpinDescriptor,
  phase: DrawPhase,
  now: number,
): { settled: boolean; remainingMs: number } {
  if (phase !== "spinning") return { settled: true, remainingMs: 0 };
  const remaining = spin.durationMs - (now - spin.startedAtMs);
  if (!Number.isFinite(remaining) || remaining <= 0) return { settled: true, remainingMs: 0 };
  return { settled: false, remainingMs: remaining };
}

/** Time left to the reveal deadline, read when the bar appears: Skip or a remount moves its start. */
export function confirmRemainingMs(spin: SpinDescriptor, now: number): number {
  return Math.max(0, spin.deadlineAtMs - now);
}

/** Self-heal confirm past the deadline for a dropped movie:revealed. Anchored to the draw,
 *  not the settle, so a skipped scroll cannot reveal ahead of the server. */
function scheduleFallback(spin: SpinDescriptor, env: DrawEnv): DrawCommand {
  return { cmd: "scheduleFallback", afterMs: spin.deadlineAtMs - env.now + env.fallbackGraceMs };
}

export function reduce(state: DrawState, event: DrawEvent, env: DrawEnv): [DrawState, DrawCommand[]] {
  switch (event.type) {
    case "DRAWN": {
      const movie = event.movie;
      if (!movie.drawnAt || state.seen.includes(movie.drawnAt)) return [state, NONE];
      const seen = [...state.seen, movie.drawnAt];
      const spin = buildLiveSpin(movie, env);
      if (!spin) {
        // No reel holds the pool refresh back.
        return [{ ...state, seen }, [{ cmd: "invalidatePool" }]];
      }
      return [
        { phase: "spinning", spin, seen, commitSeq: state.commitSeq, decodedBackdrop: null },
        [scheduleFallback(spin, env)],
      ];
    }

    case "RESUME": {
      const current = event.current;
      if (!current.drawnAt || state.seen.includes(current.drawnAt)) return [state, NONE];
      const seen = [...state.seen, current.drawnAt];
      const spin = buildResumeSpin(current, event.pool, env);
      // The pool is already fresh on a reload: no refresh.
      if (!spin) return [{ ...state, seen }, NONE];
      return [
        { phase: "spinning", spin, seen, commitSeq: state.commitSeq, decodedBackdrop: null },
        [scheduleFallback(spin, env)],
      ];
    }

    case "SCROLL_DONE": {
      if (state.phase !== "spinning" || !state.spin) return [state, NONE];
      // The server owns the reveal; the fallback was scheduled at spin start.
      return [{ ...state, phase: "settled" }, NONE];
    }

    case "CONFIRM": {
      // The reveal-once guard: every confirm source funnels here, later ones no-op.
      if ((state.phase !== "spinning" && state.phase !== "settled") || !state.spin) {
        return [state, NONE];
      }
      const spin = state.spin;
      const commands: DrawCommand[] = [{ cmd: "cancelFallback" }];
      if (event.source === "local") commands.push({ cmd: "postReveal" });
      commands.push({ cmd: "decode", drawnAt: spin.drawnAt, backdropPath: spin.winner.backdropPath ?? null });
      return [{ ...state, phase: "revealing" }, commands];
    }

    case "REVEALED": {
      // Only a broadcast for the spin in flight closes the reel.
      if (state.spin && state.spin.drawnAt === event.drawnAt) {
        return reduce(state, { type: "CONFIRM", source: "remote" }, env);
      }
      return [state, NONE];
    }

    case "DECODE_DONE": {
      // A decode that outlives its draw must not commit.
      if (state.phase !== "revealing" || state.spin?.drawnAt !== event.drawnAt) return [state, NONE];
      const winner = state.spin.winner;
      const decodedBackdrop =
        event.decodedBackdropPath && event.decodedBackdropPath === winner.backdropPath
          ? {
              movieID: winner.movieID,
              drawnAt: state.spin.drawnAt,
              backdropPath: event.decodedBackdropPath,
            }
          : null;
      return [
        {
          phase: "idle",
          spin: null,
          seen: state.seen,
          commitSeq: state.commitSeq + 1,
          decodedBackdrop,
        },
        [{ cmd: "invalidatePool" }],
      ];
    }
  }
}
