/* Skeleton primitives + the Stats loading body.
 *
 * Primitives (Skeleton / SkeletonText / SkeletonPoster) are the reusable atoms —
 * a surface block under the shared `mg-shimmer` sweep, speaking the MG radius
 * tokens. The composite body mirrors the real layout containers (.stat-strip,
 * .movierail) so the page's shape shows immediately while the queries load —
 * instead of a bare "Loading…" — and so swapping in real content doesn't shift
 * layout (CLS). The tab renders its header/filter row eagerly and drops the
 * body skeleton in while data is in flight. Members has its own, built out of
 * that page's containers rather than approximating them (MembersSkeleton). */

import type { CSSProperties } from "react";

type SkeletonRadius = "sm" | "md" | "lg" | "xl" | "full";

/** Join the base skel class with an optional extra — a plain string join, so the
 *  primitives stay dependency-free (no clsx/tailwind-merge). */
const skel = (extra?: string) => (extra ? `skel ${extra}` : "skel");

const RADIUS: Record<SkeletonRadius, string> = {
  sm: "var(--r-sm)",
  md: "var(--r-md)",
  lg: "var(--r-lg)",
  xl: "var(--r-xl)",
  full: "999px",
};

interface SkeletonProps {
  /** Width — a number is px, a string passes through (e.g. "85%"). */
  w?: number | string;
  /** Height — a number is px, a string passes through. */
  h?: number | string;
  /** Corner radius token; omitted keeps the `.skel` base (--r-sm). */
  radius?: SkeletonRadius;
  className?: string;
  style?: CSSProperties;
}

/** The skeleton atom: a surface block under the shared mg-shimmer sweep. Purely
 *  decorative, so it's hidden from assistive tech. */
export function Skeleton({ w, h, radius, className, style }: SkeletonProps) {
  return (
    <div
      className={skel(className)}
      aria-hidden="true"
      style={{ width: w, height: h, borderRadius: radius ? RADIUS[radius] : undefined, ...style }}
    />
  );
}

/** A text-line placeholder — `Skeleton` with a line-ish default height. */
export function SkeletonText({ w = "100%", h = 12, ...rest }: SkeletonProps) {
  return <Skeleton w={w} h={h} {...rest} />;
}

/** A 2:3 poster placeholder, matching the real `.poster` aspect ratio. */
export function SkeletonPoster({ className, style }: { className?: string; style?: CSSProperties }) {
  return <div className={skel(className ? `skel--poster ${className}` : "skel--poster")} aria-hidden="true" style={style} />;
}

const range = (n: number) => Array.from({ length: n });

/** Stats body: the KPI strip, the movies rail, and the panel grid below it. */
export function StatsBodySkeleton() {
  return (
    <>
      <div className="stat-strip">
        {range(6).map((_, i) => (
          <div className="statitem" key={i}>
            <div className="statitem__top">
              <SkeletonText w={64} h={11} />
            </div>
            <div className="statitem__val">
              <Skeleton w={92} h={28} />
            </div>
            <SkeletonText w={70} />
          </div>
        ))}
      </div>

      <div className="skel-railhead">
        <SkeletonText w={210} h={15} />
      </div>
      <div className="movierail">
        {range(12).map((_, i) => (
          <div className="movietile" key={i}>
            <SkeletonPoster />
            <SkeletonText w="85%" h={11} />
            <SkeletonText w="55%" h={10} />
          </div>
        ))}
      </div>

      <div className="skelpanels">
        {range(4).map((_, i) => (
          <Skeleton key={i} w="100%" h={168} radius="md" />
        ))}
      </div>
    </>
  );
}

