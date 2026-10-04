import { type RefObject, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

import { exitDelayMs } from "@/components/moviepickarr/exitDelay";

/**
 * FLIP motion for a stats rail: moved items glide, new items pop in, dropped
 * items fade in place and then the survivors close the gap. Map over `entries`,
 * not the raw items: it also holds dropped items still playing their exit.
 * Both passes are layout effects so the list swap and the inverse transform land
 * in the same frame.
 */

export interface FlipEntry<T> {
  key: string;
  item: T;
  /** True while the item is playing its fade-out before unmounting. */
  exiting: boolean;
}

const ENTER_STAGGER_MS = 40;
// Caps the stagger so a long rail cannot trail a multi-second tail.
const ENTER_STAGGER_CAP = 12;
const MOVE_EPSILON = 0.5; // px below which a move reads as "didn't move"

function prefersReducedMotion(): boolean {
  return typeof window !== "undefined" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

function sameEntries(
  a: { key: string; exiting: boolean }[],
  b: { key: string; exiting: boolean }[],
): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (a[i].key !== b[i].key || a[i].exiting !== b[i].exiting) return false;
  }
  return true;
}

export function useFlipRail<T, E extends HTMLElement = HTMLDivElement>(
  items: T[],
  keyOf: (item: T) => string,
): {
  containerRef: RefObject<E | null>;
  entries: FlipEntry<T>[];
  itemProps: (key: string) => { ref: (el: HTMLElement | null) => void };
} {
  const containerRef = useRef<E | null>(null);
  const nodes = useRef(new Map<string, HTMLElement>());
  const refCbs = useRef(new Map<string, (el: HTMLElement | null) => void>());
  // Container-relative, so a reflow above the rail does not make every card glide.
  const prevRects = useRef(new Map<string, { left: number; top: number }>());
  // Decides "new", not prevRects: a rect can be missing for an item that was on
  // screen, and that item must stay put rather than fade in.
  const prevKeys = useRef(new Set<string>());
  const exitTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Each entry keeps its own snapshot, so `item` is never undefined across the
  // interleaved renders of a keepPreviousData refetch.
  const [state, setState] = useState<FlipEntry<T>[]>(() =>
    items.map((item) => ({ key: keyOf(item), item, exiting: false })),
  );

  // Order and membership only, so a count change does not re-measure the rail.
  const fingerprint = items.map(keyOf).join(",");

  useLayoutEffect(() => {
    const reduced = prefersReducedMotion();
    const incomingKeys = items.map(keyOf);
    const incoming = new Set(incomingKeys);

    setState((prev) => {
      const next: FlipEntry<T>[] = items.map((item) => ({ key: keyOf(item), item, exiting: false }));
      if (!reduced) {
        prev.forEach((e, idx) => {
          if (incoming.has(e.key)) return;
          // Re-seat at its old slot; the batched timer below removes it.
          next.splice(Math.min(idx, next.length), 0, { key: e.key, item: e.item, exiting: true });
        });
      }
      return sameEntries(prev, next) ? prev : next;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fingerprint]);

  // One timer for all exits: per-key timers are separate tasks React cannot
  // batch, so the gap would close in several stepped glides.
  const hasExiting = state.some((e) => e.exiting);
  useEffect(() => {
    if (!hasExiting || exitTimer.current !== null) return;
    exitTimer.current = setTimeout(() => {
      exitTimer.current = null;
      setState((cur) => cur.filter((e) => !e.exiting));
    }, exitDelayMs());
  }, [hasExiting]);

  const entriesFp = state.map((e) => (e.exiting ? `-${e.key}` : e.key)).join(",");
  useLayoutEffect(() => {
    const root = containerRef.current;
    if (!root) return;
    const reduced = prefersReducedMotion();
    const exiting = new Set(state.filter((e) => e.exiting).map((e) => e.key));

    // Clear in-flight transforms so the measure reads the settled layout.
    nodes.current.forEach((el) => {
      el.style.transition = "none";
      el.style.transform = "";
    });
    void root.offsetWidth; // force reflow so the cleared layout is measured

    const rootRect = root.getBoundingClientRect();
    const rects = new Map<string, { left: number; top: number }>();
    nodes.current.forEach((el, key) => {
      if (exiting.has(key)) return;
      const r = el.getBoundingClientRect();
      rects.set(key, { left: r.left - rootRect.left, top: r.top - rootRect.top });
    });

    let enterIdx = 0;
    const movers: HTMLElement[] = [];
    nodes.current.forEach((el, key) => {
      if (exiting.has(key)) return; // fades in place via the data-flip-exit CSS
      const cur = rects.get(key);
      if (!cur) return;
      if (!prevKeys.current.has(key)) {
        if (!reduced) {
          el.style.animationDelay = `${Math.min(enterIdx, ENTER_STAGGER_CAP) * ENTER_STAGGER_MS}ms`;
          enterIdx += 1;
          el.setAttribute("data-flip-enter", "");
          const clear = () => {
            el.removeAttribute("data-flip-enter");
            el.style.animationDelay = "";
            el.removeEventListener("animationend", clear);
          };
          el.addEventListener("animationend", clear);
        }
        return;
      }
      // Was on screen: glide if its old position is known, else stay put.
      const prev = prevRects.current.get(key);
      if (!prev) return;
      const dx = prev.left - cur.left;
      const dy = prev.top - cur.top;
      if (!reduced && (Math.abs(dx) > MOVE_EPSILON || Math.abs(dy) > MOVE_EPSILON)) {
        el.style.transition = "none";
        el.style.transform = `translate(${dx}px, ${dy}px)`;
        movers.push(el);
      }
    });

    // Reflow in the same effect (not a rAF) so the browser registers the
    // inverted frame before the release and the transition fires.
    if (movers.length > 0) {
      void root.offsetWidth;
      for (const el of movers) {
        el.style.transition = "transform var(--dur-base) var(--ease)";
        el.style.transform = "";
        const clear = () => {
          el.style.transition = "";
          el.removeEventListener("transitionend", clear);
        };
        el.addEventListener("transitionend", clear);
      }
    }

    prevRects.current = rects;
    // From state, not measured nodes, so it is complete.
    prevKeys.current = new Set(state.filter((e) => !e.exiting).map((e) => e.key));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [entriesFp]);

  useEffect(
    () => () => {
      if (exitTimer.current !== null) clearTimeout(exitTimer.current);
    },
    [],
  );

  const itemProps = useCallback((key: string) => {
    let cb = refCbs.current.get(key);
    if (!cb) {
      cb = (el: HTMLElement | null) => {
        if (el) {
          nodes.current.set(key, el);
        } else {
          nodes.current.delete(key);
          refCbs.current.delete(key);
          // Do not touch prevRects: a transient rail remount fires ref-null for
          // every tile and would wipe it mid-transition.
        }
      };
      refCbs.current.set(key, cb);
    }
    return { ref: cb };
  }, []);

  // Live data for present keys so counts keep rolling; exiting keys stay frozen.
  const liveByKey = new Map(items.map((item) => [keyOf(item), item] as const));
  const entries: FlipEntry<T>[] = state.map((e) => ({
    key: e.key,
    exiting: e.exiting,
    item: e.exiting ? e.item : liveByKey.get(e.key) ?? e.item,
  }));

  return { containerRef, entries, itemProps };
}
