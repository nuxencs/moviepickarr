/* ============================================================
   moviepickarr: the Draw machine.

   One pure state machine owns the client side of a draw: identity + dedup
   (an SSE event and the drawer's own mutation response describe the same
   draw), the reload-resume decision, the settle, and the reveal-once flip
   that used to be guarded in three different places. It is a reducer:

       reduce(state, event, env) -> [state, commands]

   No DOM, no timers, no fetches: environment reads arrive as the `env`
   snapshot and side effects leave as command data, so every rule in here is
   testable with plain values (see drawMachine.test.ts). drawStore executes
   the commands and adapts React/SSE/the API to the machine; DrawReel and the
   Hero just render its state.

   Phases of one draw:

     idle -> spinning -> settled -> revealing -> idle (commitSeq bumps)
                  \________________^
                  (a remote reveal may close a still-scrolling reel)

   - spinning: the reel scrolls across the candidates.
   - settled:  the reel rests on the winner (see CONTEXT.md "Settle"); the
               drawer sees the OK countdown, timed off the server's revealAt.
   - revealing: the reveal is decided; the winner's backdrop is decoding so
               the hero handoff paints in one frame.
   ============================================================ */

import type { Movie } from "@/types/Response";

/** Environment snapshot, resolved by the store at send() time. Tests pass a
 *  plain object; nothing in the machine reads the DOM or the clock. */
export interface DrawEnv {
  /** Reel scroll length (the --dur-spin token). */
  spinDurationMs: number;
  reducedMotion: boolean;
  /** This browser's stable client id: decides `mine` (who drew this). */
  clientId: string;
  /** Confirm-countdown length when a payload carries no revealAt. */
  confirmFallbackMs: number;
  /** Grace past the server's reveal deadline before the local self-heal
   *  confirm fires (covers a dropped movie:revealed frame). */
  fallbackGraceMs: number;
  /** Wall clock (Date.now()) at send() time: stamps when a spin's scroll
   *  started, so a reel remount can resume it instead of replaying it. */
  now: number;
}

/** The reel descriptor a spin renders. Immutable per draw. */
export interface SpinDescriptor {
  /** Server draw time (RFC3339): the identity of this draw. */
  drawnAt: string;
  /** The movie the reel must land on. */
  winnerId: number;
  /** Reel source: the draw candidates (winner included), deduped by id. */
  candidates: Movie[];
  /** How long THIS client scrolls: full duration fresh, remaining on resume. */
  durationMs: number;
  /** Wall clock when the scroll started. The reel is a component and its
   *  progress dies with it (a tab switch unmounts the Hero), so the elapsed
   *  time lives here, on the store singleton that outlives the remount. */
  startedAtMs: number;
  /** Fresh draw (true) vs reload-resume (false): gates the draw sound. */
  live: boolean;
  /** Whether THIS client initiated the draw. The reveal (OK) itself is turn-
   *  gated by the board (admin or next-up member), not by this; `mine` only
   *  drives the confirm countdown fill, which is the drawer's own cue. */
  mine: boolean;
  /** Settled confirm-countdown length, derived from the server's revealAt
   *  deadline: the OK fill and the self-heal fallback both time off it. */
  confirmMs: number;
}

export type DrawPhase = "idle" | "spinning" | "settled" | "revealing";

export interface DrawState {
  phase: DrawPhase;
  /** The in-flight spin; null only in idle. */
  spin: SpinDescriptor | null;
  /** drawnAt of every draw already handled this page session: dedups the
   *  SSE event against the mutation response, and stops a tab-switch remount
   *  from replaying a spin that already ran. */
  seen: readonly string[];
  /** Bumps once per completed reveal (after the backdrop decode), signalling
   *  the Hero to commit the winner in the same render that drops the reel. */
  commitSeq: number;
}

export const initialDrawState: DrawState = {
  phase: "idle",
  spin: null,
  seen: [],
  commitSeq: 0,
};

export type DrawEvent =
  /** A draw happened, from the movie:drawn SSE event or the drawer's own
   *  mutation response (whichever lands first; the other dedups). */
  | { type: "DRAWN"; movie: Movie }
  /** Reload with a pending draw: current movie + the (post-draw) pool. */
  | { type: "RESUME"; current: Movie; pool: Movie[] }
  /** The reel finished (or skipped) its scroll and rests on the winner. */
  | { type: "SCROLL_DONE" }
  /** The reveal is decided: the drawer's OK / the fallback timer (local), or
   *  the server's movie:revealed reaching a matching spin (remote). */
  | { type: "CONFIRM"; source: "local" | "remote" }
  /** movie:revealed from the server; closes the matching reel everywhere. */
  | { type: "REVEALED"; drawnAt: string }
  /** The winner's backdrop finished decoding (or there was none). */
  | { type: "DECODE_DONE"; drawnAt: string };

export type DrawCommand =
  /** Tell the server the draw is confirmed (POST reveal). Emitted exactly
   *  once per draw, only for a local confirm: a remote one IS the server. */
  | { cmd: "postReveal" }
  /** Decode the winner's backdrop; completion comes back as DECODE_DONE. */
  | { cmd: "decode"; drawnAt: string; backdropPath: string | null }
  /** Refresh the pool cache: held until the reel lands so the grid doesn't
   *  drop the winner mid-spin and spoil the result. */
  | { cmd: "invalidatePool" }
  | { cmd: "scheduleFallback"; afterMs: number }
  | { cmd: "cancelFallback" };

const NONE: DrawCommand[] = [];

/** Dedup movies by id, preserving first-seen order. */
function uniqueById(movies: Movie[]): Movie[] {
  const seenIds = new Set<number>();
  const out: Movie[] = [];
  for (const m of movies) {
    if (m && !seenIds.has(m.movieID)) {
      seenIds.add(m.movieID);
      out.push(m);
    }
  }
  return out;
}

/** Confirm-countdown length: time left to the server's revealAt deadline once
 *  the scroll (durationMs) has run, measured from `reference` (drawnAt for a
 *  fresh draw, serverNow for a resume: both server clocks, so client skew
 *  never enters). Clamped to at least a second so the OK fill stays visible
 *  even when the deadline is imminent; missing/bad revealAt falls back. */
function confirmWindowMs(reference: string, revealAt: string | undefined, durationMs: number, env: DrawEnv): number {
  if (revealAt) {
    const window = Date.parse(revealAt) - Date.parse(reference) - durationMs;
    if (Number.isFinite(window)) return Math.max(1000, window);
  }
  return env.confirmFallbackMs;
}

/** Whether a current movie still has a reel pending: drawn but not yet
 *  revealed. Lets a reload decide, from the current movie alone (before the
 *  pool loads), whether to hold the hero commit for a resume. */
export function drawAwaitingReveal(current: Movie, env: DrawEnv): boolean {
  if (env.reducedMotion) return false;
  return !!current.drawnAt && !current.revealed;
}

/** The spin for a fresh draw. Null when the spin should be skipped: reduced
 *  motion, no draw time, or fewer than two candidates (a pool of one isn't
 *  really a draw, and the graceful degradation if candidates are absent). */
function buildLiveSpin(drawn: Movie, env: DrawEnv): SpinDescriptor | null {
  if (env.reducedMotion || !drawn.drawnAt) return null;
  const candidates = uniqueById([...(drawn.candidates ?? []), drawn]);
  if (candidates.length < 2) return null;
  return {
    drawnAt: drawn.drawnAt,
    winnerId: drawn.movieID,
    candidates,
    durationMs: env.spinDurationMs,
    startedAtMs: env.now,
    live: true,
    mine: !!drawn.drawClientId && drawn.drawClientId === env.clientId,
    confirmMs: confirmWindowMs(drawn.drawnAt, drawn.revealAt, env.spinDurationMs, env),
  };
}

/** The spin for a reload mid-draw: the scroll resumes from the
 *  server-relative elapsed time (serverNow − drawnAt), or snaps straight to
 *  the settled winner when the scroll already finished (durationMs 0). Null
 *  when there's nothing to resume: reduced motion, missing timing, already
 *  revealed, or no decoys to scroll past. */
function buildResumeSpin(current: Movie, pool: Movie[], env: DrawEnv): SpinDescriptor | null {
  if (env.reducedMotion || !current.drawnAt || !current.serverNow || current.revealed) return null;
  const elapsed = Date.parse(current.serverNow) - Date.parse(current.drawnAt);
  if (!Number.isFinite(elapsed)) return null;
  const candidates = uniqueById([...(pool ?? []), current]);
  if (candidates.length < 2) return null;
  const durationMs = Math.max(0, env.spinDurationMs - elapsed);
  return {
    drawnAt: current.drawnAt,
    winnerId: current.movieID,
    candidates,
    durationMs,
    startedAtMs: env.now,
    live: false,
    mine: !!current.drawClientId && current.drawClientId === env.clientId,
    confirmMs: confirmWindowMs(current.serverNow, current.revealAt, durationMs, env),
  };
}

/** Where a reel should pick up when it mounts. The reel's scroll progress is
 *  component state, and the Movies tab unmounts with the route, so a mount is
 *  not always the start of a scroll: it may be the same draw coming back after
 *  a tab switch. The machine outlives that, so the answer comes from here.
 *
 *  Settled once the phase is past spinning (the scroll already finished; the
 *  reel must show the confirm, not replay the scroll) or once the scroll window
 *  has run out while nothing was mounted to notice. Otherwise the reel glides
 *  only the time that's left, so the landing stays on schedule against the
 *  server's reveal deadline. */
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

/** The self-heal fallback: a backstop confirm that fires just past the server's
 *  reveal deadline (revealAt + grace) in case a movie:revealed frame is dropped.
 *  Anchored to the DRAW (scheduled at spin start over the full scroll + confirm
 *  window), not to the settle, so skipping the scroll (which only fast-forwards
 *  the visuals) can't pull the fallback in ahead of the server's on-time
 *  broadcast and reveal early. Any confirm (OK / remote reveal) cancels it. */
function scheduleFallback(spin: SpinDescriptor, env: DrawEnv): DrawCommand {
  return { cmd: "scheduleFallback", afterMs: spin.durationMs + spin.confirmMs + env.fallbackGraceMs };
}

export function reduce(state: DrawState, event: DrawEvent, env: DrawEnv): [DrawState, DrawCommand[]] {
  switch (event.type) {
    case "DRAWN": {
      const movie = event.movie;
      if (!movie.drawnAt || state.seen.includes(movie.drawnAt)) return [state, NONE];
      const seen = [...state.seen, movie.drawnAt];
      const spin = buildLiveSpin(movie, env);
      if (!spin) {
        // No reel for this draw (reduced motion / lone candidate): nothing
        // holds the pool refresh back, so release it right away.
        return [{ ...state, seen }, [{ cmd: "invalidatePool" }]];
      }
      return [{ phase: "spinning", spin, seen, commitSeq: state.commitSeq }, [scheduleFallback(spin, env)]];
    }

    case "RESUME": {
      const current = event.current;
      if (!current.drawnAt || state.seen.includes(current.drawnAt)) return [state, NONE];
      const seen = [...state.seen, current.drawnAt];
      const spin = buildResumeSpin(current, event.pool, env);
      // No reel to resume: mark the draw handled so the hero commits the
      // result directly. The pool is already fresh on a reload: no refresh.
      if (!spin) return [{ ...state, seen }, NONE];
      return [{ phase: "spinning", spin, seen, commitSeq: state.commitSeq }, [scheduleFallback(spin, env)]];
    }

    case "SCROLL_DONE": {
      if (state.phase !== "spinning" || !state.spin) return [state, NONE];
      // The server owns the reveal: it broadcasts movie:revealed at the confirm
      // deadline, and every client closes off that one frame. The self-heal
      // fallback was already scheduled at spin start (draw-anchored, so a skip
      // can't pull it early), so settling just flips the phase.
      return [{ ...state, phase: "settled" }, NONE];
    }

    case "CONFIRM": {
      // The reveal-once flip, THE guard. Everything funnels through here:
      // the drawer's OK, the fallback timer, and the server's broadcast (as
      // source "remote", possibly while the reel is still scrolling). Any
      // later confirm finds the phase already past settled and does nothing.
      if ((state.phase !== "spinning" && state.phase !== "settled") || !state.spin) {
        return [state, NONE];
      }
      const spin = state.spin;
      const winner = spin.candidates.find((m) => m.movieID === spin.winnerId);
      const commands: DrawCommand[] = [{ cmd: "cancelFallback" }];
      if (event.source === "local") commands.push({ cmd: "postReveal" });
      commands.push({ cmd: "decode", drawnAt: spin.drawnAt, backdropPath: winner?.backdropPath ?? null });
      return [{ ...state, phase: "revealing" }, commands];
    }

    case "REVEALED": {
      // Only a broadcast for the spin in flight closes the reel; anything
      // else (a stale frame, a draw this client never saw) is ignored.
      if (state.spin && state.spin.drawnAt === event.drawnAt) {
        return reduce(state, { type: "CONFIRM", source: "remote" }, env);
      }
      return [state, NONE];
    }

    case "DECODE_DONE": {
      // Stale completions (a decode outliving its draw) must not commit.
      if (state.phase !== "revealing" || state.spin?.drawnAt !== event.drawnAt) return [state, NONE];
      return [
        { phase: "idle", spin: null, seen: state.seen, commitSeq: state.commitSeq + 1 },
        [{ cmd: "invalidatePool" }],
      ];
    }
  }
}
