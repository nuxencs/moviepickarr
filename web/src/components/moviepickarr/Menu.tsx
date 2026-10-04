import { EllipsisIcon } from "lucide-react";
import {
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";

import { useDismissible } from "@/hooks/useDismissible";

export interface MenuAction {
  label: string;
  onSelect: () => void;
  icon?: ReactNode;
  danger?: boolean;
  disabled?: boolean;
}

interface MenuProps {
  actions: MenuAction[];
  label: string;
  icon?: ReactNode;
  /** Which trigger edge the menu's near corner aligns to. */
  align?: "start" | "end";
  className?: string;
  /** Prevent opening while the owning surface has an in-flight operation. */
  disabled?: boolean;
}

type CloseReason = "select" | "escape" | "tab" | "outside" | "trigger";

interface Placement {
  top: number;
  left: number;
  origin: string;
}

const GAP = 6;
const MARGIN = 8;

/**
 * Bespoke "more actions" menu, a portalled floating surface. Inside a dialog it
 * is the topmost surface, so Esc closes only the menu (#220).
 */
export function Menu({ actions, label, icon, align = "end", className, disabled = false }: MenuProps) {
  const [placement, setPlacement] = useState<Placement | null>(null);

  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const menuId = useId();
  const actionSignature = actions
    .map((action) => `${action.label}:${action.disabled ? "disabled" : "enabled"}`)
    .join("\u0000");
  const triggerDisabled = disabled || actions.every((action) => action.disabled);

  const { open, closing, show, dismiss, isTopmost } = useDismissible({
    restoreFocusTo: triggerRef,
    onClosed: () => setPlacement(null),
  });

  const openMenu = useCallback(() => {
    setPlacement(null);
    show();
  }, [show]);

  // On select this runs before the action's Modal mounts, so the Modal adopts
  // the trigger as opener; else Enter on the focused trigger reopens the menu.
  const requestClose = useCallback(
    (reason: CloseReason) => dismiss({ restoreFocus: reason !== "outside" }),
    [dismiss],
  );

  useEffect(() => {
    if (triggerDisabled && open && !closing) dismiss();
  }, [closing, dismiss, open, triggerDisabled]);

  const place = useCallback(() => {
    const trigger = triggerRef.current;
    const menu = menuRef.current;
    if (!trigger || !menu) return;
    const t = trigger.getBoundingClientRect();
    const m = menu.getBoundingClientRect();

    let top = t.bottom + GAP;
    let above = false;
    if (top + m.height > window.innerHeight - MARGIN && t.top - GAP - m.height >= MARGIN) {
      top = t.top - GAP - m.height;
      above = true;
    }

    let left = align === "end" ? t.right - m.width : t.left;
    left = Math.max(MARGIN, Math.min(left, window.innerWidth - m.width - MARGIN));

    setPlacement({
      top,
      left,
      origin: `${above ? "bottom" : "top"} ${align === "end" ? "right" : "left"}`,
    });
  }, [align]);

  // Keep focus inside when a live update disables the focused item (an invite
  // expiring while the menu is open).
  useLayoutEffect(() => {
    if (!open || closing) return;
    place();
    const menu = menuRef.current;
    const focused = document.activeElement;
    if (
      !menu?.contains(focused) ||
      (focused instanceof HTMLButtonElement && focused.disabled)
    ) {
      menu
        ?.querySelector<HTMLButtonElement>('[role="menuitem"]:not([disabled])')
        ?.focus();
    }
  }, [open, closing, place, actionSignature]);

  useEffect(() => {
    if (!open || closing) return;

    // One place() per frame: it reads rects, and scroll fires many times a frame.
    let frame: number | null = null;
    const reposition = () => {
      if (frame !== null) return;
      frame = requestAnimationFrame(() => {
        frame = null;
        place();
      });
    };
    // Capture-phase document listeners: defer to a surface on top (#220).
    const onPointerDown = (e: PointerEvent) => {
      if (!isTopmost()) return;
      const node = e.target as Node;
      if (menuRef.current?.contains(node) || triggerRef.current?.contains(node)) return;
      requestClose("outside");
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && isTopmost()) {
        e.stopPropagation();
        requestClose("escape");
      }
    };

    window.addEventListener("scroll", reposition, true);
    window.addEventListener("resize", reposition);
    document.addEventListener("pointerdown", onPointerDown, true);
    document.addEventListener("keydown", onKeyDown, true);
    return () => {
      if (frame !== null) cancelAnimationFrame(frame);
      window.removeEventListener("scroll", reposition, true);
      window.removeEventListener("resize", reposition);
      document.removeEventListener("pointerdown", onPointerDown, true);
      document.removeEventListener("keydown", onKeyDown, true);
    };
  }, [open, closing, place, requestClose, isTopmost]);

  const onMenuKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (!["ArrowDown", "ArrowUp", "Home", "End", "Tab"].includes(e.key)) return;
    if (e.key === "Tab") {
      e.preventDefault();
      requestClose("tab");
      return;
    }
    const items = Array.from(
      menuRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not([disabled])') ?? [],
    );
    if (items.length === 0) return;
    e.preventDefault();
    const current = items.indexOf(document.activeElement as HTMLButtonElement);
    const next =
      e.key === "Home"
        ? 0
        : e.key === "End"
          ? items.length - 1
          : e.key === "ArrowDown"
            ? (current + 1) % items.length
            : (current - 1 + items.length) % items.length;
    items[next]?.focus();
  };

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className={`iconbtn${className ? ` ${className}` : ""}`}
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        aria-disabled={triggerDisabled || undefined}
        onClick={() => {
          if (triggerDisabled) return;
          if (open && !closing) {
            requestClose("trigger");
          } else {
            openMenu();
          }
        }}
        onKeyDown={(e) => {
          if (triggerDisabled) {
            if (e.key === "ArrowDown" || e.key === "ArrowUp") e.preventDefault();
            return;
          }
          if (!open && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
            e.preventDefault();
            openMenu();
          }
        }}
      >
        {icon ?? <EllipsisIcon />}
      </button>

      {open &&
        createPortal(
          <div
            ref={menuRef}
            id={menuId}
            role="menu"
            aria-label={label}
            className={`mg-menu${closing ? " mg-menu--closing" : ""}`}
            style={{
              position: "fixed",
              top: placement?.top ?? 0,
              left: placement?.left ?? 0,
              transformOrigin: placement?.origin ?? "top right",
              visibility: placement ? "visible" : "hidden",
            }}
            onKeyDown={onMenuKeyDown}
          >
            {actions.map((action) => (
              <button
                key={action.label}
                type="button"
                role="menuitem"
                className={`mg-menu__item${action.danger ? " mg-menu__item--danger" : ""}`}
                disabled={action.disabled}
                onClick={() => {
                  action.onSelect();
                  requestClose("select");
                }}
              >
                {action.icon}
                {action.label}
              </button>
            ))}
          </div>,
          document.body,
        )}
    </>
  );
}
