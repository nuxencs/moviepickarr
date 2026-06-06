import { ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

interface ModalProps {
  onClose: () => void;
  /** Extra class on the `.modal` surface (e.g. `modal--movie` for a narrower width). */
  className?: string;
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
/**
 * How long to keep a closing dialog mounted so its exit animation can finish,
 * read from the shared `--dur-fast` token so CSS and JS never desync. Reduced-
 * motion users skip the wait entirely (focus returns to the opener immediately).
 */
function exitDelayMs(): number {
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return 0;
  const raw = getComputedStyle(document.documentElement).getPropertyValue("--dur-fast");
  const secs = parseFloat(raw) || 0.14;
  return Math.round(secs * 1000) + 20;
}

export function Modal({ onClose, className, children }: ModalProps) {
  const [closing, setClosing] = useState(false);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;
  const surfaceRef = useRef<HTMLDivElement>(null);

  const requestClose = useCallback(() => setClosing(true), []);

  useEffect(() => {
    if (!closing) return;
    const t = window.setTimeout(() => onCloseRef.current(), exitDelayMs());
    return () => window.clearTimeout(t);
  }, [closing]);

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

    document.body.style.overflow = "hidden";
    document.addEventListener("keydown", onKey);
    return () => {
      document.body.style.overflow = "";
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
        className={`modal${className ? ` ${className}` : ""}${closing ? " modal--closing" : ""}`}
        onMouseDown={(e) => e.stopPropagation()}
      >
        {children(requestClose)}
      </div>
    </div>,
    document.body,
  );
}
