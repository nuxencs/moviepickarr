import {
  type CSSProperties,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
  type RefObject,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";

import "./movie-scrollbar.css";

type Orientation = "vertical" | "horizontal";

interface ScrollAxis {
  backwardKey: string;
  clientSize: (element: HTMLElement) => number;
  coordinate: (event: { clientX: number; clientY: number }) => number;
  forwardKey: string;
  position: (element: HTMLElement) => number;
  scrollBy: (element: HTMLElement, amount: number) => void;
  scrollSize: (element: HTMLElement) => number;
  setPosition: (element: HTMLElement, position: number) => void;
  thumbStyle: (size: number, offset: number) => CSSProperties;
  trackSize: (element: HTMLElement) => number;
  trackStart: (box: DOMRect) => number;
}

const SCROLL_AXES: Record<Orientation, ScrollAxis> = {
  vertical: {
    backwardKey: "ArrowUp",
    clientSize: (element) => element.clientHeight,
    coordinate: (event) => event.clientY,
    forwardKey: "ArrowDown",
    position: (element) => element.scrollTop,
    scrollBy: (element, amount) => element.scrollBy({ top: amount }),
    scrollSize: (element) => element.scrollHeight,
    setPosition: (element, position) => element.scrollTo({ top: position }),
    thumbStyle: (size, offset) => ({
      height: size + 14,
      transform: `translateY(${offset}px)`,
    }),
    trackSize: (element) => element.clientHeight,
    trackStart: (box) => box.top,
  },
  horizontal: {
    backwardKey: "ArrowLeft",
    clientSize: (element) => element.clientWidth,
    coordinate: (event) => event.clientX,
    forwardKey: "ArrowRight",
    position: (element) => element.scrollLeft,
    scrollBy: (element, amount) => element.scrollBy({ left: amount }),
    scrollSize: (element) => element.scrollWidth,
    setPosition: (element, position) => element.scrollTo({ left: position }),
    thumbStyle: (size, offset) => ({
      transform: `translateX(${offset}px)`,
      width: size + 16,
    }),
    trackSize: (element) => element.clientWidth,
    trackStart: (box) => box.left,
  },
};

interface ScrollMetrics {
  overflow: boolean;
  thumbOffset: number;
  thumbSize: number;
  valueNow: number;
}

const hitAreaClassName = (thumbClassName: string) => `${thumbClassName}-hit-area`;

const EMPTY_METRICS: ScrollMetrics = {
  overflow: false,
  thumbOffset: 0,
  thumbSize: 0,
  valueNow: 0,
};

function ScrollbarRail({
  label,
  minThumbSize,
  orientation,
  thumbClassName,
  trackClassName,
  viewportRef,
}: {
  label: string;
  minThumbSize: number;
  orientation: Orientation;
  thumbClassName: string;
  trackClassName: string;
  viewportRef: RefObject<HTMLDivElement | null>;
}) {
  const axis = SCROLL_AXES[orientation];
  const [metrics, setMetrics] = useState<ScrollMetrics>(EMPTY_METRICS);
  const trackRef = useRef<HTMLDivElement>(null);
  const thumbHitAreaRef = useRef<HTMLDivElement>(null);
  const dragCleanupRef = useRef<(() => void) | null>(null);
  const suppressTrackClickRef = useRef(false);
  const viewportId = `movie-${orientation}-scroll-${useId().replace(/:/g, "")}`;

  const update = useCallback(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    const scrollRange = axis.scrollSize(viewport) - axis.clientSize(viewport);
    if (scrollRange <= 1) {
      setMetrics(EMPTY_METRICS);
      return;
    }

    const trackSize = trackRef.current
      ? axis.trackSize(trackRef.current)
      : axis.clientSize(viewport);
    const thumbSize = Math.max(
      minThumbSize,
      (axis.clientSize(viewport) / axis.scrollSize(viewport)) * trackSize,
    );
    const thumbRange = Math.max(0, trackSize - thumbSize);
    const ratio = axis.position(viewport) / scrollRange;
    setMetrics({
      overflow: true,
      thumbOffset: ratio * thumbRange,
      thumbSize,
      valueNow: Math.round(ratio * 100),
    });
  }, [axis, minThumbSize, viewportRef]);

  useLayoutEffect(() => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    viewport.id = viewportId;

    const resizeObserver =
      typeof ResizeObserver === "undefined" ? null : new ResizeObserver(update);
    const mutationObserver =
      typeof MutationObserver === "undefined" ? null : new MutationObserver(update);
    resizeObserver?.observe(viewport);
    mutationObserver?.observe(viewport, { childList: true, subtree: true, attributes: true });
    viewport.addEventListener("scroll", update, { passive: true });
    const frame = requestAnimationFrame(update);

    return () => {
      cancelAnimationFrame(frame);
      viewport.removeEventListener("scroll", update);
      resizeObserver?.disconnect();
      mutationObserver?.disconnect();
      viewport.removeAttribute("id");
    };
  }, [update, viewportId, viewportRef]);

  useLayoutEffect(() => {
    if (!metrics.overflow) return;
    const frame = requestAnimationFrame(update);
    return () => cancelAnimationFrame(frame);
  }, [metrics.overflow, update]);

  useEffect(() => () => dragCleanupRef.current?.(), []);

  const startThumbDrag = (
    event: ReactPointerEvent<HTMLDivElement>,
    suppressTrackClick = false,
  ) => {
    const viewport = viewportRef.current;
    const track = trackRef.current;
    if (!viewport || !track) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    suppressTrackClickRef.current = suppressTrackClick;
    const startCoordinate = axis.coordinate(event);
    const startPosition = axis.position(viewport);
    const thumbRange = Math.max(1, axis.trackSize(track) - metrics.thumbSize);
    const scrollRange = axis.scrollSize(viewport) - axis.clientSize(viewport);

    const onMove = (move: PointerEvent) => {
      const delta = axis.coordinate(move) - startCoordinate;
      axis.setPosition(viewport, startPosition + (delta / thumbRange) * scrollRange);
    };
    const cleanup = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", cleanup);
      window.removeEventListener("pointercancel", cleanup);
      dragCleanupRef.current = null;
      if (suppressTrackClick) {
        window.setTimeout(() => {
          suppressTrackClickRef.current = false;
        }, 0);
      }
    };
    dragCleanupRef.current?.();
    dragCleanupRef.current = cleanup;
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", cleanup, { once: true });
    window.addEventListener("pointercancel", cleanup, { once: true });
  };

  const onScrollbarKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    const viewport = viewportRef.current;
    if (!viewport) return;
    const current = axis.position(viewport);
    const clientSize = axis.clientSize(viewport);
    const next =
      event.key === axis.forwardKey
        ? current + 48
        : event.key === axis.backwardKey
          ? current - 48
          : event.key === "PageDown"
            ? current + clientSize * 0.88
            : event.key === "PageUp"
              ? current - clientSize * 0.88
              : event.key === "Home"
                ? 0
                : event.key === "End"
                  ? axis.scrollSize(viewport)
                  : null;
    if (next === null) return;
    event.preventDefault();
    axis.setPosition(viewport, next);
  };

  if (!metrics.overflow) return null;

  return (
    <div
      ref={trackRef}
      className={trackClassName}
      role="scrollbar"
      aria-label={label}
      aria-controls={viewportId}
      aria-orientation={orientation}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={metrics.valueNow}
      tabIndex={0}
      onKeyDown={onScrollbarKeyDown}
      onPointerDown={(event) => {
        if (event.target !== event.currentTarget) return;
        // Firefox can retarget a press from the moving thumb to its stationary
        // rail while the modal scales in. Recover only presses beside the thumb.
        const hitArea = thumbHitAreaRef.current;
        if (!hitArea) return;
        const box = hitArea.getBoundingClientRect();
        const coordinate = axis.coordinate(event);
        const start = orientation === "vertical" ? box.top : box.left;
        const end = orientation === "vertical" ? box.bottom : box.right;
        if (coordinate < start - 4 || coordinate > end + 4) return;
        startThumbDrag(event, true);
      }}
      onClickCapture={(event) => {
        if (!suppressTrackClickRef.current) return;
        suppressTrackClickRef.current = false;
        event.preventDefault();
        event.stopPropagation();
      }}
      onClick={(event) => {
        if (event.target !== event.currentTarget) return;
        const viewport = viewportRef.current;
        const track = trackRef.current;
        if (!viewport || !track) return;
        const thumbStart = axis.trackStart(track.getBoundingClientRect()) + metrics.thumbOffset;
        const direction = axis.coordinate(event) < thumbStart ? -1 : 1;
        axis.scrollBy(viewport, direction * axis.clientSize(viewport) * 0.88);
      }}
    >
      <div
        ref={thumbHitAreaRef}
        className={hitAreaClassName(thumbClassName)}
        style={axis.thumbStyle(metrics.thumbSize, metrics.thumbOffset)}
        onPointerDown={startThumbDrag}
      >
        <div className={thumbClassName} />
      </div>
    </div>
  );
}

export function MovieScrollbar({
  viewportRef,
  children,
}: {
  viewportRef: RefObject<HTMLDivElement | null>;
  children: ReactNode;
}) {
  return (
    <div className="movie-scrollbar">
      {children}
      <ScrollbarRail
        label="Movie details position"
        minThumbSize={36}
        orientation="vertical"
        thumbClassName="movie-scrollbar__thumb"
        trackClassName="movie-scrollbar__track"
        viewportRef={viewportRef}
      />
    </div>
  );
}

export function MovieCastScrollbar({
  children,
  hiddenFromAccessibility = false,
}: {
  children: ReactNode;
  hiddenFromAccessibility?: boolean;
}) {
  const viewportRef = useRef<HTMLDivElement>(null);
  return (
    <div className="movie-cast-scrollbar" aria-hidden={hiddenFromAccessibility || undefined}>
      <div ref={viewportRef} className="castrow">
        {children}
      </div>
      {!hiddenFromAccessibility && (
        <ScrollbarRail
          label="Cast position"
          minThumbSize={44}
          orientation="horizontal"
          thumbClassName="movie-cast-scrollbar__thumb"
          trackClassName="movie-cast-scrollbar__track"
          viewportRef={viewportRef}
        />
      )}
    </div>
  );
}
