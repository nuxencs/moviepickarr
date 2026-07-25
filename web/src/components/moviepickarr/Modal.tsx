import { ReactNode, useCallback, useEffect, useRef } from "react";
import { createPortal } from "react-dom";

import { useDismissible } from "@/hooks/useDismissible";

interface ModalProps {
  onClose: () => void;
  /**
   * The parent's intent. Flipping it to false starts the exit motion, with
   * `onClose` following once the motion ends. Only a modal whose open-ness
   * lives outside React needs it (the movie modal, which reads a history
   * entry); a dialog the parent mounts and unmounts leaves it alone.
   */
  open?: boolean;
  /**
   * Where Esc / veil-click / the render-prop `close` go instead of dismissing
   * the surface directly. Passed alongside `open` by a modal that can't close
   * itself, so all four gestures (those three plus browser Back) take one
   * path. Fires at most once per mount: each request pops a history entry, and
   * a second would pop the entry behind the modal.
   */
  onRequestClose?: () => void;
  /** Extra class on the `.modal` surface (e.g. `modal--movie` for a narrower width). */
  className?: string;
  /**
   * When false, Esc / veil-click / the render-prop `close` are inert. Used to pin
   * a dialog open mid-save so a dismiss can't race the success-close (which would
   * otherwise toggle the dialog back open). Defaults to true.
   */
  dismissible?: boolean;
  /**
   * Cap the surface at the window height and scroll inside it instead of letting the
   * veil scroll. The surface stays centered at any content length and chrome outside
   * the scrolling part stays put. Children lay out as a flex column: put the region
   * that should scroll in a `.modal__scroll` and leave a close X or head beside it.
   * Off by default, since short dialogs should keep sizing to their content.
   */
  capped?: boolean;
  /** Render-prop receiving a `close` that plays the exit animation before unmounting. */
  children: (close: () => void) => ReactNode;
}

const FOCUSABLE =
  'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';

/**
 * Portalled modal shell with matching enter AND exit animations, Esc / veil-click
 * dismissal, body-scroll lock, and a focus trap (focus moves in on open, cycles
 * inside, and returns to the opener on close) so it behaves like a real dialog.
 */
export function Modal({
  onClose,
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
  const surfaceRef = useRef<HTMLDivElement>(null);

  // Mounting is parent-controlled, so only the closing phase is used: the
  // exit motion plays, then onClosed tells the parent to unmount. Focus
  // returns to the opener in the unmount cleanup below, not via the machine.
  const { closing, dismiss } = useDismissible({ onClosed: () => onCloseRef.current() });

  const requestClose = useCallback(() => {
    if (!dismissibleRef.current) return;
    // Hand the close to the parent when it owns one, and let the exit come
    // back as `open: false`. Otherwise dismiss here, the way it always was.
    if (onRequestCloseRef.current) {
      if (requestedRef.current) return;
      requestedRef.current = true;
      onRequestCloseRef.current();
      return;
    }
    dismiss();
  }, [dismiss]);

  // The parent withdrawing `open` is the other way in, and the only one a
  // browser Back can take: by the time the popstate lands the state is
  // already gone, so the motion has to run off its removal, not before it.
  useEffect(() => {
    if (!open) dismiss();
  }, [open, dismiss]);

  useEffect(() => {
    const surface = surfaceRef.current;
    const opener = document.activeElement as HTMLElement | null;

    // Move focus into the dialog: prefer the first form field (so a form dialog
    // lands on its input, not the close X); otherwise the surface itself.
    (surface?.querySelector<HTMLElement>("input,textarea,select") ?? surface)?.focus();

    const onKey = (e: KeyboardEvent) => {
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

    // Lock body scroll, compensating for the removed scrollbar so the page
    // doesn't jump right. (No-op where scrollbars are overlay — width is 0.)
    const scrollbarWidth = window.innerWidth - document.documentElement.clientWidth;
    const prevOverflow = document.body.style.overflow;
    const prevPaddingRight = document.body.style.paddingRight;
    document.body.style.overflow = "hidden";
    if (scrollbarWidth > 0) {
      document.body.style.paddingRight = `${scrollbarWidth}px`;
    }
    document.addEventListener("keydown", onKey);
    return () => {
      document.body.style.overflow = prevOverflow;
      document.body.style.paddingRight = prevPaddingRight;
      document.removeEventListener("keydown", onKey);
      // Return focus to whatever opened the modal.
      opener?.focus?.();
    };
  }, [requestClose]);

  return createPortal(
    <div className={`modal-veil${closing ? " modal-veil--closing" : ""}`} onMouseDown={requestClose}>
      <div
        ref={surfaceRef}
        role="dialog"
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
