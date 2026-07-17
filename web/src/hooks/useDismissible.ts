import { type RefObject, useCallback, useEffect, useRef, useState } from "react";

import { exitDelayMs } from "@/components/moviepickarr/exitDelay";

export interface DismissOptions {
  /** Refocus the configured trigger as part of the dismissal. Pass false for
   *  an outside click, where focus follows the click instead. Default true. */
  restoreFocus?: boolean;
  /** Runs once the exit motion completes (alongside onClosed). */
  after?: () => void;
}

/**
 * The one dismissal machine every floating surface rides — the Modal, the
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
 * - show() while closing interrupts the exit and re-opens — clearing the
 *   timer is load-bearing, otherwise the original close timer still fires
 *   and slams the surface shut again.
 * - Focus restores to the trigger synchronously inside dismiss(), so a
 *   dialog mounted by the dismissal's action still captures the trigger as
 *   its opener.
 * - hideNow() drops the surface without the exit motion (for when the view
 *   changes out from under it) and resets the guard so a later show() isn't
 *   blocked.
 *
 * Surfaces whose mounting is parent-controlled (the Modal) ignore `open` and
 * use only closing/dismiss with an onClosed that tells the parent to unmount.
 */
export function useDismissible({
  restoreFocusTo,
  onClosed,
}: {
  /** The trigger to refocus on a focus-restoring dismissal. */
  restoreFocusTo?: RefObject<HTMLElement | null>;
  /** Runs once per completed dismissal, after the exit motion. */
  onClosed?: () => void;
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

  return { open, closing, show, dismiss, hideNow };
}
