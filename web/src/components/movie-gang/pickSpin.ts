/* ============================================================
   Movie Gang — pick-reveal spin model.

   A pick is decided by the server (movie:picked). Every client then plays a
   slot-machine reel that scrolls the pool candidates and lands on the winner —
   a presentational layer over an already-decided result, so it stays honestly
   random. This module holds the shared descriptor + the helpers that decide
   whether/how to spin; PickReel renders it, the Hero + useSSE wire it up.
   ============================================================ */

import { PickKeys } from "@/api/query_keys";

import type { Movie } from "@/types/Response";
import type { QueryClient } from "@tanstack/react-query";

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
}

/** Fallback used when the --dur-spin token can't be read (e.g. SSR/no DOM). */
const DEFAULT_SPIN_MS = 3200;

/** The reveal-spin duration, read from the `--dur-spin` CSS token so the JS and
 *  the keyframe share one source of truth. */
export function spinDurationMs(): number {
  if (typeof window === "undefined") return DEFAULT_SPIN_MS;
  const raw = getComputedStyle(document.documentElement).getPropertyValue("--dur-spin");
  const secs = parseFloat(raw);
  return Number.isFinite(secs) && secs > 0 ? Math.round(secs * 1000) : DEFAULT_SPIN_MS;
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
 * Build the spin for a fresh pick — from the movie:picked event (or the
 * clicker's mutation response) plus the pre-pick pool snapshot (which still
 * holds the winner). Returns null when the spin should be skipped: reduced
 * motion, no pick time, or fewer than two candidates (a pool of one isn't
 * really a pick, so it goes straight to the reveal).
 */
export function buildLiveSpin(picked: Movie, poolSnapshot: Movie[]): ActiveSpin | null {
  if (prefersReducedMotion()) return null;
  if (!picked.pickedAt) return null;
  const candidates = uniqueById([...(poolSnapshot ?? []), picked]);
  if (candidates.length < 2) return null;
  return { pickedAt: picked.pickedAt, winnerId: picked.movieID, candidates, durationMs: spinDurationMs() };
}

/**
 * Build a resume spin on reload, from the current movie's pick timing and the
 * (post-pick) pool. Elapsed is server-relative (serverNow − pickedAt), so a
 * skewed client clock can't mis-time it. Returns null when there's nothing to
 * resume: reduced motion, missing timing, the spin window already elapsed, or a
 * pool of one (no decoys left to scroll past the winner).
 */
export function buildResumeSpin(current: Movie, freshPool: Movie[]): ActiveSpin | null {
  if (prefersReducedMotion()) return null;
  if (!current.pickedAt || !current.serverNow) return null;
  const elapsed = Date.parse(current.serverNow) - Date.parse(current.pickedAt);
  if (!Number.isFinite(elapsed)) return null;
  const remaining = spinDurationMs() - elapsed;
  if (remaining <= 0) return null; // window passed — show the result directly
  const candidates = uniqueById([...(freshPool ?? []), current]);
  if (candidates.length < 2) return null;
  return { pickedAt: current.pickedAt, winnerId: current.movieID, candidates, durationMs: remaining };
}

/**
 * Whether a current movie is still inside its reveal-spin window (server-relative
 * elapsed < spin duration). Lets a reload decide — using only the current movie,
 * before the pool has loaded — whether to wait for the pool and resume the reel,
 * or just show the result. Reduced motion and missing timing both read as false.
 */
export function isWithinSpinWindow(current: Movie): boolean {
  if (prefersReducedMotion()) return false;
  if (!current.pickedAt || !current.serverNow) return false;
  const elapsed = Date.parse(current.serverNow) - Date.parse(current.pickedAt);
  return Number.isFinite(elapsed) && spinDurationMs() - elapsed > 0;
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
