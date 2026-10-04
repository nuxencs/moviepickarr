import { type RefObject, useCallback, useEffect, useRef, useState } from "react";

import { exitDelayMs } from "@/components/moviepickarr/exitDelay";

/**
 * Floating surfaces on screen, oldest first; a surface keeps its place through
 * its exit motion. Module-level because nested Modals portal as siblings, so
 * React and the DOM cannot see their depth.
 */
const layers: symbol[] = [];

export interface DismissOptions {
  /** Refocus the trigger. Pass false for an outside click. Default true. */
  restoreFocus?: boolean;
  /** Runs once the exit motion completes (alongside onClosed). */
  after?: () => void;
}

/**
 * Dismissal machine for every floating surface: open, then closing (mounted
 * while the exit motion plays), then closed after exitDelayMs(). dismiss() while
 * closing is a no-op; show() while closing clears the timer so it cannot slam
 * the surface shut. Focus restores synchronously so a dialog opened by the
 * dismissal still sees the trigger as its opener. Escape, outside-click and
 * focus trapping belong to the isTopmost() surface only (#220).
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
  /** The parent mounts the surface (the Modal), so mounted means on screen. */
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

  // Read at event time: surfaces above this one come and go without a re-render.
  const isTopmost = useCallback(
    () => layers.length === 0 || layers[layers.length - 1] === layerRef.current,
    [],
  );

  return { open, closing, show, dismiss, hideNow, isTopmost };
}
