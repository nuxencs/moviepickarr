import { type CSSProperties, type RefObject, useLayoutEffect, useState } from "react";

/** Resolved track list, lane count and gaps of a CSS grid, plus its offset from
 *  the document top (the window virtualizer's scrollMargin). */
export interface GridMetrics {
  /** The container's resolved `grid-template-columns`, replayed verbatim onto
   *  each virtual row so the row's tracks can't drift from the container's. */
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
 * Read a grid's resolved geometry. The track list comes back from the browser
 * already resolved to pixel widths, so `repeat(auto-fill, minmax(…))` and its
 * media-query overrides stay in the stylesheet — nothing about the breakpoints
 * is duplicated here. A non-grid container ("none") reads as a single lane.
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

/** Document-top offset from the viewport rect. This avoids walking a mixed
 *  offset-parent chain when layout above the grid changes. */
function documentTop(el: HTMLElement): number {
  return Math.round(el.getBoundingClientRect().top + window.scrollY);
}

/** Absolute placement of one virtualized row inside its sizing container. */
export function virtualRowStyle(offset: number): CSSProperties {
  return { position: "absolute", top: 0, left: 0, width: "100%", transform: `translateY(${offset}px)` };
}

/**
 * Track a grid container's lane count, gaps and document offset, re-reading on
 * any resize of the container or of the page above it (the pool grid grows and
 * shrinks over SSE, which moves the watched grid down the page).
 */
export function useGridMetrics(ref: RefObject<HTMLElement | null>): GridMetrics {
  const [metrics, setMetrics] = useState<GridMetrics>(EMPTY);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;

    const read = () => {
      const next = { ...readGridMetrics(getComputedStyle(el)), offsetTop: documentTop(el) };
      setMetrics((prev) => (same(prev, next) ? prev : next));
    };

    read();
    const observer = new ResizeObserver(read);
    observer.observe(el);
    observer.observe(document.body);
    return () => observer.disconnect();
  }, [ref]);

  return metrics;
}
