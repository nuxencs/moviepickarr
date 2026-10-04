import { type CSSProperties, type RefObject, useLayoutEffect, useState } from "react";

import { documentOffsetTop, documentScrollOwner } from "@/lib/scrollPolicy";

/** Resolved track list, lane count and gaps of a CSS grid, plus its offset from
 *  the body document owner's content origin (the virtualizer's scrollMargin). */
export interface GridMetrics {
  /** Replayed verbatim onto each virtual row so its tracks cannot drift. */
  template: string;
  lanes: number;
  columnGap: number;
  rowGap: number;
  offsetTop: number;
}

const SINGLE_LANE = "minmax(0, 1fr)";

const EMPTY: GridMetrics = { template: SINGLE_LANE, lanes: 1, columnGap: 0, rowGap: 0, offsetTop: 0 };

const px = (value: string) => {
  const n = Number.parseFloat(value);
  return Number.isFinite(n) ? n : 0;
};

/**
 * Reads a grid's resolved geometry, so breakpoints stay in the stylesheet only.
 * A non-grid container reads as a single lane.
 */
export function readGridMetrics(style: {
  gridTemplateColumns: string;
  columnGap: string;
  rowGap: string;
}): Omit<GridMetrics, "offsetTop"> {
  const tracks = style.gridTemplateColumns.trim();
  const single = !tracks || tracks === "none";
  return {
    template: single ? SINGLE_LANE : tracks,
    lanes: single ? 1 : tracks.split(/\s+/).length,
    columnGap: px(style.columnGap),
    rowGap: px(style.rowGap),
  };
}

const same = (a: GridMetrics, b: GridMetrics) =>
  a.template === b.template &&
  a.lanes === b.lanes &&
  a.columnGap === b.columnGap &&
  a.rowGap === b.rowGap &&
  a.offsetTop === b.offsetTop;

export function virtualRowStyle(offset: number): CSSProperties {
  return { position: "absolute", top: offset, left: 0, width: "100%" };
}

/** Re-reads on resize of the container or of the page above it (#root). */
export function useGridMetrics(ref: RefObject<HTMLElement | null>): GridMetrics {
  const [metrics, setMetrics] = useState<GridMetrics>(EMPTY);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;

    const read = () => {
      const next = {
        ...readGridMetrics(getComputedStyle(el)),
        offsetTop: documentOffsetTop(el),
      };
      setMetrics((prev) => (same(prev, next) ? prev : next));
    };

    read();
    const observer = new ResizeObserver(read);
    observer.observe(el);
    observer.observe(document.getElementById("root") ?? documentScrollOwner());
    return () => observer.disconnect();
  }, [ref]);

  return metrics;
}
