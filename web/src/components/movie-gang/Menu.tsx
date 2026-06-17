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

import { exitDelayMs } from "@/components/movie-gang/exitDelay";
import { effectiveZoom } from "@/components/movie-gang/zoom";

export interface MenuAction {
  label: string;
  onSelect: () => void;
  icon?: ReactNode;
  danger?: boolean;
  disabled?: boolean;
}

interface MenuProps {
  /** Items shown top to bottom. */
  actions: MenuAction[];
  /** Accessible label for the trigger and the menu. */
  label: string;
  /** Trigger glyph; defaults to an ellipsis. */
  icon?: ReactNode;
  /** Which trigger edge the menu's near corner aligns to. */
  align?: "start" | "end";
  /** Extra class on the trigger button. */
  className?: string;
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
 * Bespoke "more actions" menu — a portalled floating surface on the shared
 * mg-scaleIn/Out motion (see Modal), replacing the former Radix dropdown so the
 * app owns its focus behaviour. Every reason but an outside-click returns focus
 * to the trigger; on select that happens *before* the action's Modal mounts, so
 * the Modal captures the trigger as its opener and moves focus inside itself —
 * the trigger never sits focused behind a dialog where Enter would reopen the menu.
 */
export function Menu({ actions, label, icon, align = "end", className }: MenuProps) {
  const [open, setOpen] = useState(false);
  const [closing, setClosing] = useState(false);
  const [placement, setPlacement] = useState<Placement | null>(null);

  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const closingRef = useRef(false);
  const timerRef = useRef<number | null>(null);
  const menuId = useId();

  const clearTimer = () => {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  };

  const openMenu = useCallback(() => {
    clearTimer();
    closingRef.current = false;
    setClosing(false);
    setPlacement(null);
    setOpen(true);
  }, []);

  const requestClose = useCallback((reason: CloseReason) => {
    if (closingRef.current) return;
    closingRef.current = true;
    setClosing(true);
    // Return focus to the trigger for every dismissal except an outside click.
    // On select this runs synchronously before the action's Modal mounts, so the
    // Modal adopts the trigger as opener and immediately traps focus inside.
    if (reason !== "outside") triggerRef.current?.focus();
    clearTimer();
    timerRef.current = window.setTimeout(() => {
      closingRef.current = false;
      setClosing(false);
      setOpen(false);
      setPlacement(null);
    }, exitDelayMs());
  }, []);

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

    // Flip/clamp run in the zoomed viewport space (GBCR + innerWidth/Height all
    // agree there); divide only the written coords so the fixed portal child
    // doesn't get scaled by the inherited :root zoom a second time.
    const zoom = effectiveZoom(menu);
    setPlacement({
      top: top / zoom,
      left: left / zoom,
      origin: `${above ? "bottom" : "top"} ${align === "end" ? "right" : "left"}`,
    });
  }, [align]);

  // Position the menu and focus its first item once mounted, before paint.
  useLayoutEffect(() => {
    if (!open || closing) return;
    place();
    menuRef.current
      ?.querySelector<HTMLButtonElement>('[role="menuitem"]:not([disabled])')
      ?.focus();
  }, [open, closing, place]);

  // Keep it anchored while scrolling/resizing; dismiss on Esc or outside click.
  useEffect(() => {
    if (!open || closing) return;

    const reposition = () => place();
    const onPointerDown = (e: PointerEvent) => {
      const node = e.target as Node;
      if (menuRef.current?.contains(node) || triggerRef.current?.contains(node)) return;
      requestClose("outside");
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        requestClose("escape");
      }
    };

    window.addEventListener("scroll", reposition, true);
    window.addEventListener("resize", reposition);
    document.addEventListener("pointerdown", onPointerDown, true);
    document.addEventListener("keydown", onKeyDown, true);
    return () => {
      window.removeEventListener("scroll", reposition, true);
      window.removeEventListener("resize", reposition);
      document.removeEventListener("pointerdown", onPointerDown, true);
      document.removeEventListener("keydown", onKeyDown, true);
    };
  }, [open, closing, place, requestClose]);

  useEffect(() => () => clearTimer(), []);

  const onMenuKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (!["ArrowDown", "ArrowUp", "Home", "End", "Tab"].includes(e.key)) return;
    const items = Array.from(
      menuRef.current?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not([disabled])') ?? [],
    );
    if (items.length === 0) return;
    if (e.key === "Tab") {
      e.preventDefault();
      requestClose("tab");
      return;
    }
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
        onClick={() => (open && !closingRef.current ? requestClose("trigger") : openMenu())}
        onKeyDown={(e) => {
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
