import { type RefObject, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

import { exitDelayMs } from "@/components/moviepickarr/exitDelay";

/**
 * FLIP motion for a stats rail (films, people, member bars). Instead of fading
 * the whole list up on every filter change, each item is animated by how its box
 * actually moved between the old and new layout:
 *
 *   - persistent item, position changed  → glides (translate) to its new spot
 *   - persistent item, position unchanged → no motion (e.g. the 30d films that
 *     are a prefix of the 1y set get a zero delta and never move — the "skip the
 *     overlap" optimisation falls out for free)
 *   - newly-matched item                 → pops in (mg-fadeUp, staggered)
 *   - dropped item                       → fades out in place, then unmounts and
 *     the survivors glide to close the gap
 *
 * This owns the rendered list: `entries` is the incoming items PLUS any dropped
 * items still playing their exit, so the caller maps over `entries`, not the raw
 * array. Each entry carries its own item snapshot, so the item is never
 * undefined even across the several interleaved renders a keepPreviousData
 * refetch produces. Item DATA is refreshed live for present keys during render
 * (so each card's NumberFlow count keeps rolling on a same-set refetch); a
 * dropped item renders from its frozen snapshot.
 *
 * Translate deltas are measured from getBoundingClientRect() and applied in the
 * same CSS-pixel coordinate space. prefers-reduced-motion skips every transform,
 * entrance, and exit delay.
 *
 * The FLIP measure/replay runs in a layout effect (before paint), and the
 * exit-retention reconcile is a sibling layout effect, so the list swap and the
 * inverse-transform land in the same frame with no flash of the settled state.
 */

export interface FlipEntry<T> {
  key: string;
  item: T;
  /** True while the item is playing its fade-out before unmounting. */
  exiting: boolean;
}

const ENTER_STAGGER_MS = 40;
// Cap the entrance stagger so a long rail can't trail a multi-second tail
// (matches the previous fadeUp-replay cap).
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
  // Positions are stored CONTAINER-RELATIVE (each item's offset from the rail's
  // own top-left), not viewport-absolute — so a reflow ABOVE the rail (e.g. the
  // films rail tripling in height) slides the whole rail without making every
  // card glide that page-shift distance. Only movement WITHIN the rail animates.
  const prevRects = useRef(new Map<string, { left: number; top: number }>());
  // Keys present in the last committed render (non-exiting). This — not the
  // presence of a recorded rect — decides whether an item is genuinely NEW: a
  // recorded position can be missing for an item that WAS on screen (node not
  // yet registered, or churned during a multi-render transition), and such an
  // item must stay put, never fade in as if new.
  const prevKeys = useRef(new Set<string>());
  const exitTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // The rendered list: incoming items plus any dropped items still fading. Each
  // carries its own snapshot, so `item` is never undefined regardless of how the
  // incoming `items` interleaves with this state across renders.
  const [state, setState] = useState<FlipEntry<T>[]>(() =>
    items.map((item) => ({ key: keyOf(item), item, exiting: false })),
  );

  // Order + membership fingerprint — drives the reconcile. A pure data change
  // (e.g. a count) leaves this untouched, so the rail doesn't re-measure when
  // only a NumberFlow value needs to roll.
  const fingerprint = items.map(keyOf).join(",");

  // Reconcile: keep dropped keys around (marked exiting) long enough to fade.
  useLayoutEffect(() => {
    const reduced = prefersReducedMotion();
    const incomingKeys = items.map(keyOf);
    const incoming = new Set(incomingKeys);

    setState((prev) => {
      const next: FlipEntry<T>[] = items.map((item) => ({ key: keyOf(item), item, exiting: false }));
      if (!reduced) {
        prev.forEach((e, idx) => {
          if (incoming.has(e.key)) return; // still present (possibly reordered)
          // Newly dropped, or already fading — re-seat it at its old slot with
          // its last-known data so the fade renders the same content. Removal is
          // handled by the single batched timer below, not per-key.
          next.splice(Math.min(idx, next.length), 0, { key: e.key, item: e.item, exiting: true });
        });
      }
      return sameEntries(prev, next) ? prev : next;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fingerprint]);

  // Drop all currently-exiting items together, on ONE timer. Per-key timers fire
  // as separate tasks (React can't batch across them), which splits a gap-close
  // into several FLIP runs and a stepped, restarting glide. Batching them into a
  // single removal makes the survivors close the gap in one clean glide.
  const hasExiting = state.some((e) => e.exiting);
  useEffect(() => {
    if (!hasExiting || exitTimer.current !== null) return;
    exitTimer.current = setTimeout(() => {
      exitTimer.current = null;
      setState((cur) => cur.filter((e) => !e.exiting));
    }, exitDelayMs());
  }, [hasExiting]);

  // FLIP: measure true layout positions, invert moved items, play, pop in
  // newcomers. Runs after the reconcile commit, so the new list is in the DOM.
  const entriesFp = state.map((e) => (e.exiting ? `-${e.key}` : e.key)).join(",");
  useLayoutEffect(() => {
    const root = containerRef.current;
    if (!root) return;
    const reduced = prefersReducedMotion();
    const exiting = new Set(state.filter((e) => e.exiting).map((e) => e.key));

    // FIRST: clear any in-flight transforms so getBoundingClientRect reports the
    // settled layout position, not a mid-glide one.
    nodes.current.forEach((el) => {
      el.style.transition = "none";
      el.style.transform = "";
    });
    void root.offsetWidth; // force reflow so the cleared layout is measured

    // Measure container-relative so page reflow above the rail isn't animated.
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
        // ENTER — genuinely new to the rail; pop in, staggered among this
        // round's newcomers only.
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
      // Was on screen last render. If we have its old position, glide; if not
      // (position unrecorded), leave it exactly where it landed — never fade.
      const prev = prevRects.current.get(key);
      if (!prev) return;
      // INVERT: jump to the old position with no transition. The PLAY pass below
      // releases it under a transition.
      const dx = prev.left - cur.left;
      const dy = prev.top - cur.top;
      if (!reduced && (Math.abs(dx) > MOVE_EPSILON || Math.abs(dy) > MOVE_EPSILON)) {
        el.style.transition = "none";
        el.style.transform = `translate(${dx}px, ${dy}px)`;
        movers.push(el);
      }
    });

    // PLAY — commit the inverted frame with one synchronous reflow, then release
    // every transform under a transition so they glide home. Doing this in the
    // same effect (not a rAF) guarantees the browser registers the "from" frame
    // before the "to" frame, so the transition reliably fires.
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
    // Record the keys we just rendered (non-exiting) so the next pass knows what
    // was already on screen — built from state, so it's complete regardless of
    // which nodes happened to be measured.
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
          // NB: do NOT touch prevRects here. It's replaced wholesale at the end
          // of every FLIP run, and a transient rail unmount/remount (keepPrevious
          // briefly emptying the matched set → the empty-state <p>) would fire
          // ref-null for every tile and wipe it mid-transition, breaking the
          // glide. New-vs-existing is decided by prevKeys, not by a stale rect.
        }
      };
      refCbs.current.set(key, cb);
    }
    return { ref: cb };
  }, []);

  // Overlay live data onto present (non-exiting) keys so counts keep rolling;
  // exiting keys render from their frozen snapshot.
  const liveByKey = new Map(items.map((item) => [keyOf(item), item] as const));
  const entries: FlipEntry<T>[] = state.map((e) => ({
    key: e.key,
    exiting: e.exiting,
    item: e.exiting ? e.item : liveByKey.get(e.key) ?? e.item,
  }));

  return { containerRef, entries, itemProps };
}
