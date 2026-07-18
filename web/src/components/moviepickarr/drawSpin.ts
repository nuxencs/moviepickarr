/* ============================================================
   moviepickarr: reel timing + easing geometry.

   The presentational constants of the draw reel: the scroll duration and the
   `--ease-reel` cubic-bezier, both read from CSS tokens so the JS and the
   keyframes share one source of truth. DrawReel uses these to glide the
   track, resume the scroll at the right spot after a reload, and time the
   draw-sound clicks; drawStore snapshots them into the machine's DrawEnv.
   The draw lifecycle itself lives in drawMachine.ts.
   ============================================================ */

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
   draw-sound clicks (one per poster gap crossing the reticle). Parsing the token
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
