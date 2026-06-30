/* ============================================================
   moviepickarr — pick-reveal spin model.

   A pick is decided by the server (movie:picked). Every client then plays a
   slot-machine reel that scrolls the pool candidates and lands on the winner —
   a presentational layer over an already-decided result, so it stays honestly
   random. This module holds the shared descriptor + the helpers that decide
   whether/how to spin; PickReel renders it, the Hero + useSSE wire it up.
   ============================================================ */

import { PickKeys } from "@/api/query_keys";


import type { Movie } from "@/types/Response";
import type { QueryClient } from "@tanstack/react-query";

import { getClientId } from "@/lib/clientId";

export interface ActiveSpin {
  /** Server pick time (RFC3339) — the identity of this pick. Setting an
   *  ActiveSpin whose pickedAt matches the current one is a no-op, which dedups
   *  the SSE event against the clicker's own mutation response and stops a
   *  re-render from restarting a spin already in flight. */
  pickedAt: string;
  /** The movie the reel must land on. */
  winnerId: number;
  /** Reel source: the pick candidates (winner included), deduped by movie id. */
  candidates: Movie[];
  /** How long THIS client should spin: the full duration for a fresh pick, or
   *  the remaining time when resuming after a reload mid-spin. */
  durationMs: number;
  /** True for a fresh pick, false for a reload-resume. A fresh pick always starts
   *  the pick sound (its click train is computed from the spin); a resume only
   *  joins if audio is already running, since a cold reload's context is suspended
   *  and scheduling onto it would replay the clicks shifted out of sync. */
  live: boolean;
  /** Whether THIS client initiated the pick. Only the picker sees the reel's
   *  confirm (OK) button; their press closes the reel for everyone. Derived from
   *  the pick's pickerClientId vs this browser's stable client id. */
  mine: boolean;
}

/** Fallback used when the --dur-spin token can't be read (e.g. SSR/no DOM). */
const DEFAULT_SPIN_MS = 6500;

/** The reveal-spin duration, read from the `--dur-spin` CSS token so the JS and
 *  the keyframe share one source of truth. */
export function spinDurationMs(): number {
  if (typeof window === "undefined") return DEFAULT_SPIN_MS;
  const raw = getComputedStyle(document.documentElement).getPropertyValue("--dur-spin");
  const secs = parseFloat(raw);
  return Number.isFinite(secs) && secs > 0 ? Math.round(secs * 1000) : DEFAULT_SPIN_MS;
}

/* ---- Reel easing geometry ----
   The reel glides on `--ease-reel` (a cubic-bezier). These helpers read that token
   and evaluate the *actual* curve — not a polynomial stand-in — so two things stay
   locked to what's rendered: the resume start-position after a reload, and the
   pick-sound clicks (one per poster gap crossing the reticle). Parsing the token
   means a future `--ease-reel` change carries through for free. */

/** Cubic-bezier control points (x1, y1, x2, y2) of `--ease-reel`, parsed once.
 *  Falls back to easeOutCubic's standard approximation (SSR / non-bezier token). */
let reelEasePts: [number, number, number, number] | null = null;
function reelEasePoints(): [number, number, number, number] {
  if (reelEasePts) return reelEasePts;
  const fallback: [number, number, number, number] = [0.33, 1, 0.68, 1];
  if (typeof window === "undefined") return (reelEasePts = fallback);
  const raw = getComputedStyle(document.documentElement).getPropertyValue("--ease-reel");
  const m = raw.match(/cubic-bezier\(([^)]+)\)/);
  const n = m ? m[1].split(",").map((s) => parseFloat(s)) : [];
  reelEasePts = n.length === 4 && n.every((v) => Number.isFinite(v))
    ? (n as [number, number, number, number])
    : fallback;
  return reelEasePts;
}

/** One cubic-bezier component with control points (0, p1, p2, 1), sampled at s∈[0,1]. */
function bezierComponent(p1: number, p2: number, s: number): number {
  const c = 3 * p1;
  const b = 3 * (p2 - p1) - c;
  const a = 1 - c - b;
  return ((a * s + b) * s + c) * s;
}

/** Invert a monotonic component: the s∈[0,1] where component(p1,p2,s) == target. */
function solveBezierS(target: number, p1: number, p2: number): number {
  let lo = 0;
  let hi = 1;
  for (let i = 0; i < 28; i++) {
    const mid = (lo + hi) / 2;
    if (bezierComponent(p1, p2, mid) < target) lo = mid;
    else hi = mid;
  }
  return (lo + hi) / 2;
}

/** Distance fraction the reel has covered at elapsed-time fraction `tx` (the CSS
 *  ease output). Resumes the scroll at the right spot after a reload mid-spin. */
export function reelEaseOutput(tx: number): number {
  if (tx <= 0) return 0;
  if (tx >= 1) return 1;
  const [x1, y1, x2, y2] = reelEasePoints();
  return bezierComponent(y1, y2, solveBezierS(tx, x1, x2));
}

/** Inverse: elapsed-time fraction at which the reel has covered `frac` of its
 *  distance. Times a click to the instant a poster gap crosses the reticle. */
export function reelEaseTimeAt(frac: number): number {
  if (frac <= 0) return 0;
  if (frac >= 1) return 1;
  const [x1, y1, x2, y2] = reelEasePoints();
  return bezierComponent(x1, x2, solveBezierS(frac, y1, y2));
}

export function prefersReducedMotion(): boolean {
  return typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

/** Dedup movies by id, preserving first-seen order. */
function uniqueById(movies: Movie[]): Movie[] {
  const seen = new Set<number>();
  const out: Movie[] = [];
  for (const m of movies) {
    if (m && !seen.has(m.movieID)) {
      seen.add(m.movieID);
      out.push(m);
    }
  }
  return out;
}

/**
 * Build the spin for a fresh pick — from the movie:picked event (or the clicker's
 * mutation response), which now carries its own reel candidates (the pre-pick
 * pool, winner included, with posters). The reel is therefore self-contained: a
 * client spins even with no pool cached. The winner is re-appended as a fallback,
 * but `uniqueById` keeps the candidate copy (which carries the poster the bare
 * payload lacks). Returns null when the spin should be skipped: reduced motion, no
 * pick time, or fewer than two candidates (a pool of one isn't really a pick, so
 * it goes straight to the reveal — also the graceful degradation if candidates are
 * absent, e.g. the server couldn't load the pool).
 */
export function buildLiveSpin(picked: Movie): ActiveSpin | null {
  if (prefersReducedMotion()) return null;
  if (!picked.pickedAt) return null;
  const candidates = uniqueById([...(picked.candidates ?? []), picked]);
  if (candidates.length < 2) return null;
  return {
    pickedAt: picked.pickedAt,
    winnerId: picked.movieID,
    candidates,
    durationMs: spinDurationMs(),
    live: true,
    mine: !!picked.pickerClientId && picked.pickerClientId === getClientId(),
  };
}

/**
 * Build a resume spin on reload, from the current movie's pick timing and the
 * (post-pick) pool. The reel now holds until the pick is *revealed* (the picker's
 * confirm / countdown), not for a fixed window, so a reload mid-flight re-opens
 * it: the scroll resumes from the server-relative elapsed time (serverNow −
 * pickedAt, immune to client clock skew) — or snaps straight to the settled
 * winner if the scroll already finished — and the confirm countdown restarts.
 * Returns null when there's nothing to resume: reduced motion, missing timing,
 * the pick was already revealed, or a pool of one (no decoys to scroll past).
 */
export function buildResumeSpin(current: Movie, freshPool: Movie[]): ActiveSpin | null {
  if (prefersReducedMotion()) return null;
  if (!current.pickedAt || !current.serverNow) return null;
  if (current.revealed) return null; // already confirmed — show the result directly
  const elapsed = Date.parse(current.serverNow) - Date.parse(current.pickedAt);
  if (!Number.isFinite(elapsed)) return null;
  const candidates = uniqueById([...(freshPool ?? []), current]);
  if (candidates.length < 2) return null;
  return {
    pickedAt: current.pickedAt,
    winnerId: current.movieID,
    candidates,
    // Remaining scroll time; 0 once the scroll has elapsed (the reel then snaps to
    // the settled winner and waits for the confirm/countdown).
    durationMs: Math.max(0, spinDurationMs() - elapsed),
    live: false,
    mine: !!current.pickerClientId && current.pickerClientId === getClientId(),
  };
}

/**
 * Whether a current movie's reel is still pending — picked but not yet revealed.
 * Lets a reload decide, using only the current movie (before the pool loads),
 * whether to wait for the pool and re-open the reel or just show the result.
 * Reduced motion and a missing pick time both read as false (no reel).
 */
export function pickAwaitingReveal(current: Movie): boolean {
  if (prefersReducedMotion()) return false;
  return !!current.pickedAt && !current.revealed;
}

/** Set the active spin, deduped by pickedAt (see ActiveSpin.pickedAt). */
export function setActiveSpin(qc: QueryClient, spin: ActiveSpin | null): void {
  if (spin) {
    const existing = qc.getQueryData<ActiveSpin | null>(PickKeys.active());
    if (existing && existing.pickedAt === spin.pickedAt) return;
  }
  qc.setQueryData(PickKeys.active(), spin);
}

export function clearActiveSpin(qc: QueryClient): void {
  qc.setQueryData(PickKeys.active(), null);
}

/** Record that a pick was revealed (the picker confirmed, or the reel's countdown
 *  filled), keyed by pickedAt. Set by the useSSE movie:revealed handler; the Hero
 *  watches it and closes the matching reel on every client at once. */
export function signalRevealed(qc: QueryClient, pickedAt: string): void {
  qc.setQueryData(PickKeys.revealed(), pickedAt);
}
