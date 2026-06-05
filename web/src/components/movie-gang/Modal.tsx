import { ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

interface ModalProps {
  onClose: () => void;
  /** Extra class on the `.modal` surface (e.g. `modal--movie` for a narrower width). */
  className?: string;
  /** Render-prop receiving a `close` that plays the exit animation before unmounting. */
  children: (close: () => void) => ReactNode;
}

/**
 * Portalled modal shell with matching enter AND exit animations. Closing (Esc,
 * veil click, or a child calling `close`) flips to a closing state that plays
 * the out animation, then unmounts via `onClose` once it finishes.
 */
export function Modal({ onClose, className, children }: ModalProps) {
  const [closing, setClosing] = useState(false);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  const requestClose = useCallback(() => setClosing(true), []);

  useEffect(() => {
    if (!closing) return;
    const t = window.setTimeout(() => onCloseRef.current(), 200);
    return () => window.clearTimeout(t);
  }, [closing]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") requestClose();
    };
    document.body.style.overflow = "hidden";
    document.addEventListener("keydown", onKey);
    return () => {
      document.body.style.overflow = "";
      document.removeEventListener("keydown", onKey);
    };
  }, [requestClose]);

  return createPortal(
    <div className={`modal-veil${closing ? " modal-veil--closing" : ""}`} onMouseDown={requestClose}>
      <div
        className={`modal${className ? ` ${className}` : ""}${closing ? " modal--closing" : ""}`}
        onMouseDown={(e) => e.stopPropagation()}
      >
        {children(requestClose)}
      </div>
    </div>,
    document.body,
  );
}
