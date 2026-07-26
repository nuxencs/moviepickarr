import { type RefObject, useCallback, useEffect, useRef, useState } from "react";

import { exitDelayMs } from "@/components/moviepickarr/exitDelay";

/**
 * Every floating surface currently on screen, oldest first. A surface joins
 * when it appears and leaves when it is fully gone (it holds its place through
 * the exit motion, so a second Escape mid-close can't fall through to the
 * surface underneath).
 *
 * Module-level on purpose: a Modal opened from inside another Modal portals
 * into `document.body` as a sibling, so neither one can see the other through
 * React or the DOM. The stack is the only place the depth is knowable.
 */
const layers: symbol[] = [];

export interface DismissOptions {
  /** Refocus the configured trigger as part of the dismissal. Pass false for
   *  an outside click, where focus follows the click instead. Default true. */
  restoreFocus?: boolean;
  /** Runs once the exit motion completes (alongside onClosed). */
  after?: () => void;
}

/**
 * The one dismissal machine every floating surface rides: the Modal, the
 * Menu, the filter chip dropdowns, and the Stats range popover all used to
 * hand-roll the same closing-flag + re-entry-guard + exit-timer + focus-
 * restore choreography; a close-motion fix now lands here once.
 *
 * Phases: closed → show() → open → dismiss() → closing (the surface stays
 * mounted so its CSS exit motion plays) → after exitDelayMs() → closed
 * (onClosed fires). The machine owns the sharp edges:
 *
 * - dismiss() while already closing is a no-op, so racing triggers (Esc +
 *   outside click) can't double-fire.
 * - show() while closing interrupts the exit and re-opens: clearing the
 *   timer is load-bearing, otherwise the original close timer still fires
 *   and slams the surface shut again.
 * - Focus restores to the trigger synchronously inside dismiss(), so a
 *   dialog mounted by the dismissal's action still captures the trigger as
 *   its opener.
 * - hideNow() drops the surface without the exit motion (for when the view
 *   changes out from under it) and resets the guard so a later show() isn't
 *   blocked.
 * - The surface takes a place on the layer stack while it is on screen, and
 *   isTopmost() answers whether it is the one on top. Escape, outside-click
 *   and focus-trapping belong to the topmost surface only, so a dialog opened
 *   from inside another dialog takes those gestures alone (#220).
 *
 * Surfaces whose mounting is parent-controlled (the Modal) ignore `open` and
 * use only closing/dismiss with an onClosed that tells the parent to unmount.
 */
export function useDismissible({
  restoreFocusTo,
  onClosed,
  parentMounted = false,
}: {
  /** The trigger to refocus on a focus-restoring dismissal. */
  restoreFocusTo?: RefObject<HTMLElement | null>;
  /** Runs once per completed dismissal, after the exit motion. */
  onClosed?: () => void;
  /**
   * The parent mounts and unmounts the surface (the Modal), so `open` is never
   * used and being mounted *is* being on screen. Layer membership then runs
   * from mount to unmount rather than from show() to onClosed.
   */
  parentMounted?: boolean;
} = {}) {
  const [open, setOpen] = useState(false);
  const [closing, setClosing] = useState(false);
  const closingRef = useRef(false);
  const timerRef = useRef<number | null>(null);
  const onClosedRef = useRef(onClosed);
  onClosedRef.current = onClosed;

  const clearTimer = useCallback(() => {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const show = useCallback(() => {
    clearTimer();
    closingRef.current = false;
    setClosing(false);
    setOpen(true);
  }, [clearTimer]);

  const dismiss = useCallback(
    (options?: DismissOptions) => {
      if (closingRef.current) return;
      closingRef.current = true;
      setClosing(true);
      if (options?.restoreFocus !== false) restoreFocusTo?.current?.focus();
      clearTimer();
      timerRef.current = window.setTimeout(() => {
        closingRef.current = false;
        timerRef.current = null;
        setClosing(false);
        setOpen(false);
        onClosedRef.current?.();
        options?.after?.();
      }, exitDelayMs());
    },
    [clearTimer, restoreFocusTo],
  );

  const hideNow = useCallback(() => {
    clearTimer();
    closingRef.current = false;
    setClosing(false);
    setOpen(false);
  }, [clearTimer]);

  useEffect(() => clearTimer, [clearTimer]);

  // A self-mounting surface keeps `open` true through its exit motion, so
  // `open` alone covers both ends of its time on screen.
  const onScreen = parentMounted || open;
  const layerRef = useRef<symbol | null>(null);
  layerRef.current ??= Symbol("dismissible-layer");

  useEffect(() => {
    if (!onScreen) return;
    const layer = layerRef.current as symbol;
    layers.push(layer);
    return () => {
      const at = layers.lastIndexOf(layer);
      if (at !== -1) layers.splice(at, 1);
    };
  }, [onScreen]);

  // Read at event time, not at render time: surfaces above this one come and
  // go without re-rendering it.
  const isTopmost = useCallback(
    () => layers.length === 0 || layers[layers.length - 1] === layerRef.current,
    [],
  );

  return { open, closing, show, dismiss, hideNow, isTopmost };
}
