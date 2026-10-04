import {
  type MouseEvent as ReactMouseEvent,
  ReactNode,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
} from "react";
import { createPortal } from "react-dom";

import { useDismissible } from "@/hooks/useDismissible";
import { lockPageScroll } from "@/lib/scrollPolicy";

interface ModalProps {
  onClose: () => void;
  label: string;
  /**
   * False starts the exit motion; `onClose` follows when it ends. Only for a
   * modal whose open state lives outside React (the movie modal's history entry).
   */
  open?: boolean;
  /**
   * Replaces direct dismissal for Esc, veil-click and `close`, so they take the
   * same path as browser Back. Fires at most once per open interval: a second
   * request would pop the history entry behind the modal.
   */
  onRequestClose?: () => void;
  className?: string;
  /** False pins the dialog open mid-save: dismiss must not race the success-close. */
  dismissible?: boolean;
  /**
   * Cap the surface at the window height and scroll inside a `.modal__scroll`
   * child instead of the veil. Children lay out as a flex column.
   */
  capped?: boolean;
  children: (close: () => void) => ReactNode;
}

const FOCUSABLE =
  'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';

interface ModalLayer {
  surface: HTMLDivElement;
  opener: HTMLElement | null;
  openerAncestors: HTMLElement[];
}

/** Modal surfaces, oldest first. Modal-only: a menu does not hide its dialog. */
const modalLayers: ModalLayer[] = [];

function focusTarget(target: HTMLElement | null | undefined): boolean {
  if (!target?.isConnected) return false;
  target.focus({ preventScroll: true });
  return document.activeElement === target;
}

function focusFromOpenerRegion(layer: ModalLayer): boolean {
  for (const ancestor of layer.openerAncestors) {
    if (!ancestor.isConnected || ancestor.hasAttribute("inert")) continue;
    const candidates = ancestor.querySelectorAll<HTMLElement>(FOCUSABLE);
    for (const candidate of candidates) {
      if (candidate.tabIndex < 0 || candidate.closest('[inert],[aria-hidden="true"]')) continue;
      if (focusTarget(candidate)) return true;
    }
  }
  return false;
}

function syncModalLayers() {
  const top = modalLayers.length - 1;
  modalLayers.forEach(({ surface }, index) => {
    const covered = index !== top;
    surface.toggleAttribute("inert", covered);
    if (covered) {
      surface.removeAttribute("aria-modal");
      surface.setAttribute("aria-hidden", "true");
    } else {
      surface.setAttribute("aria-modal", "true");
      surface.removeAttribute("aria-hidden");
    }
  });
}

/**
 * Portalled modal shell with exit motion, scroll lock and a focus trap. Nested
 * Modals portal in as siblings, so Esc, veil and Tab gate on the topmost (#220).
 */
export function Modal({
  onClose,
  label,
  open = true,
  onRequestClose,
  className,
  dismissible = true,
  capped = false,
  children,
}: ModalProps) {
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;
  const onRequestCloseRef = useRef(onRequestClose);
  onRequestCloseRef.current = onRequestClose;
  const dismissibleRef = useRef(dismissible);
  dismissibleRef.current = dismissible;
  const requestedRef = useRef(false);
  const previousOpenRef = useRef(open);
  const surfaceRef = useRef<HTMLDivElement>(null);

  // Only the closing phase is used. Focus returns in the unmount cleanup below.
  const { closing, show, dismiss, isTopmost } = useDismissible({
    parentMounted: true,
    onClosed: () => onCloseRef.current(),
  });

  const requestClose = useCallback(() => {
    if (!dismissibleRef.current) return;
    // A parent-owned close comes back as `open: false`.
    if (onRequestCloseRef.current) {
      if (requestedRef.current) return;
      requestedRef.current = true;
      onRequestCloseRef.current();
      return;
    }
    dismiss();
  }, [dismiss]);

  // Not for the render-prop `close`: a nested confirm must close its parent.
  const requestCloseFromGesture = useCallback(() => {
    if (!isTopmost()) return;
    requestClose();
  }, [isTopmost, requestClose]);

  // Dismiss on release, so the click does not land on what the veil covered.
  // Press and release must both hit the veil: a selection drag is no dismissal.
  const veilPressRef = useRef(false);

  const onVeilMouseDown = useCallback((e: ReactMouseEvent<HTMLDivElement>) => {
    // The veil's scrollbar reports the veil as target; dragging it is a scroll.
    const veil = e.currentTarget;
    const onScrollbar =
      e.nativeEvent.offsetX > veil.clientWidth || e.nativeEvent.offsetY > veil.clientHeight;
    veilPressRef.current = e.target === veil && !onScrollbar;
    // Stops text selection behind the veil and focus leaving the dialog.
    if (veilPressRef.current) e.preventDefault();
  }, []);

  const onVeilMouseUp = useCallback(
    (e: ReactMouseEvent<HTMLDivElement>) => {
      const pressed = veilPressRef.current;
      veilPressRef.current = false;
      if (!pressed || e.target !== e.currentTarget) return;
      requestCloseFromGesture();
    },
    [requestCloseFromGesture],
  );

  // Reopening during the exit keeps this surface and its opener. Cancel before
  // paint so neither the closing frame nor its old timer wins.
  useLayoutEffect(() => {
    const wasOpen = previousOpenRef.current;
    previousOpenRef.current = open;
    if (!open || wasOpen) return;
    requestedRef.current = false;
    show();
  }, [open, show]);

  // Browser Back drops the state before popstate lands, so exit runs after it.
  useEffect(() => {
    if (!open) dismiss();
  }, [open, dismiss]);

  useLayoutEffect(() => {
    const surface = surfaceRef.current;
    if (!surface) return;
    const opener = document.activeElement as HTMLElement | null;
    const openerAncestors: HTMLElement[] = [];
    for (
      let ancestor = opener?.parentElement;
      ancestor && ancestor !== document.body;
      ancestor = ancestor.parentElement
    ) {
      openerAncestors.push(ancestor);
    }

    // A form dialog lands on its input, not the close X.
    (surface.querySelector<HTMLElement>("input,textarea,select") ?? surface).focus({
      preventScroll: true,
    });

    // Capture and leave the opener before hiding anything below this surface.
    // On the way out, reverse that order so focus never targets an inert node.
    const layer: ModalLayer = { surface, opener, openerAncestors };
    modalLayers.push(layer);
    syncModalLayers();

    return () => {
      const at = modalLayers.lastIndexOf(layer);
      if (at === -1) return;
      const wasTopmost = at === modalLayers.length - 1;

      // A nested stack leaving in one commit can clean up parent before child:
      // pass this opener to any child whose opener is in the detached surface.
      for (let i = at + 1; i < modalLayers.length; i++) {
        const above = modalLayers[i];
        if (above.opener && surface.contains(above.opener)) {
          above.opener = opener;
          above.openerAncestors = openerAncestors;
        }
      }

      modalLayers.splice(at, 1);
      syncModalLayers();
      if (wasTopmost) {
        const fallback = modalLayers[modalLayers.length - 1]?.surface;
        if (!focusTarget(layer.opener) && !focusTarget(fallback)) {
          focusFromOpenerRegion(layer);
        }
      }
    };
  }, []);

  useLayoutEffect(() => {
    const surface = surfaceRef.current;
    const onKey = (e: KeyboardEvent) => {
      // Every mounted Modal hears this document listener.
      if (!isTopmost()) return;
      if (e.key === "Escape") {
        requestClose();
        return;
      }
      if (e.key !== "Tab" || !surface) return;
      const items = Array.from(surface.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
        (el) => el.offsetParent !== null,
      );
      if (items.length === 0) {
        e.preventDefault();
        surface.focus();
        return;
      }
      const first = items[0];
      const last = items[items.length - 1];
      const active = document.activeElement;
      if (e.shiftKey && (active === first || active === surface)) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && active === last) {
        e.preventDefault();
        first.focus();
      }
    };

    // Lock before the first dialog frame so no frame paints unlocked.
    const unlockScroll = lockPageScroll();
    document.addEventListener("keydown", onKey);
    return () => {
      unlockScroll();
      document.removeEventListener("keydown", onKey);
    };
  }, [requestClose, isTopmost]);

  return createPortal(
    <div
      className={`modal-veil${closing ? " modal-veil--closing" : ""}`}
      onMouseDown={onVeilMouseDown}
      onMouseUp={onVeilMouseUp}
    >
      <div className="modal-backdrop" aria-hidden="true" />
      <div
        ref={surfaceRef}
        role="dialog"
        aria-label={label}
        aria-modal="true"
        tabIndex={-1}
        className={`modal${capped ? " modal--capped" : ""}${className ? ` ${className}` : ""}${closing ? " modal--closing" : ""}`}
        onMouseDown={(e) => e.stopPropagation()}
      >
        {children(requestClose)}
      </div>
    </div>,
    document.body,
  );
}
