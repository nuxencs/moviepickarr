/* Skeleton primitives and the Stats loading body. The body reuses the real
 * layout containers, so real content causes no layout shift. */

import type { CSSProperties } from "react";

type SkeletonRadius = "sm" | "md" | "lg" | "xl" | "full";

const skel = (extra?: string) => (extra ? `skel ${extra}` : "skel");

const RADIUS: Record<SkeletonRadius, string> = {
  sm: "var(--r-sm)",
  md: "var(--r-md)",
  lg: "var(--r-lg)",
  xl: "var(--r-xl)",
  full: "999px",
};

interface SkeletonProps {
  w?: number | string;
  h?: number | string;
  radius?: SkeletonRadius;
  className?: string;
  style?: CSSProperties;
}

export function Skeleton({ w, h, radius, className, style }: SkeletonProps) {
  return (
    <div
      className={skel(className)}
      aria-hidden="true"
      style={{ width: w, height: h, borderRadius: radius ? RADIUS[radius] : undefined, ...style }}
    />
  );
}

export function SkeletonText({ w = "100%", h = 12, ...rest }: SkeletonProps) {
  return <Skeleton w={w} h={h} {...rest} />;
}

export function SkeletonPoster({ className, style }: { className?: string; style?: CSSProperties }) {
  return <div className={skel(className ? `skel--poster ${className}` : "skel--poster")} aria-hidden="true" style={style} />;
}

const range = (n: number) => Array.from({ length: n });

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

