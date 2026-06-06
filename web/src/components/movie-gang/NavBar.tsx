import { ChartNoAxesColumnIcon, FilmIcon, MoonIcon, SunIcon, UsersIcon } from "lucide-react";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

import { useTheme } from "@/components/ThemeProvider";

export type Tab = "movies" | "users" | "stats";

const TABS: { id: Tab; label: string; icon: typeof FilmIcon }[] = [
  { id: "movies", label: "Movies", icon: FilmIcon },
  { id: "users", label: "Users", icon: UsersIcon },
  { id: "stats", label: "Stats", icon: ChartNoAxesColumnIcon },
];

/**
 * Resolve dark/light from the `theme` value directly (not the DOM class):
 * reading the class here is unreliable because this child's effects run before
 * the parent ThemeProvider applies the class, so the read would be stale.
 */
function resolveDark(theme: string): boolean {
  if (theme === "system") {
    return window.matchMedia("(prefers-color-scheme: dark)").matches;
  }
  return theme === "dark";
}

/** Horizontal inset (px) the underline keeps from each edge of the active tab. */
const INK_INSET = 12;

export function NavBar({ active, onChange }: { active: Tab; onChange: (tab: Tab) => void }) {
  const { theme, setTheme } = useTheme();
  const isDark = resolveDark(theme);

  // A single shared underline that slides between tabs, rather than one per tab
  // that unmounts/remounts on switch. We measure the active button and drive the
  // indicator's left/width; CSS transitions the move (and the reduced-motion
  // guard in index.css collapses it to an instant jump).
  const btnRefs = useRef<Record<Tab, HTMLButtonElement | null>>({
    movies: null,
    users: null,
    stats: null,
  });
  const [ink, setInk] = useState<{ left: number; width: number } | null>(null);

  const measure = useCallback(() => {
    const btn = btnRefs.current[active];
    // Skip when the top-bar tabs are hidden (mobile bottom-bar layout): a
    // display:none button reports offsetWidth 0, which would park the slider
    // at a bogus negative width. It re-measures on resize back to desktop.
    if (!btn || btn.offsetParent === null) return;
    setInk({ left: btn.offsetLeft + INK_INSET, width: btn.offsetWidth - INK_INSET * 2 });
  }, [active]);

  // Measure before paint so the indicator never flashes at a stale position.
  useLayoutEffect(() => measure(), [measure]);

  // Re-measure on resize and once web fonts settle (font swap changes label width).
  useEffect(() => {
    window.addEventListener("resize", measure);
    document.fonts?.ready.then(measure);
    return () => window.removeEventListener("resize", measure);
  }, [measure]);

  const toggleTheme = () => setTheme(isDark ? "light" : "dark");

  return (
    <>
      <nav className="nav">
        <div className="nav__inner">
          <div className="wordmark">
            <span className="mark">
              <FilmIcon />
            </span>
            <h1>Movie Gang</h1>
          </div>

          {/* Top-bar tabs with the sliding underline (desktop / tablet). Hidden on
              phones, where navigation moves to the fixed bottom bar below. */}
          <div className="nav__tabs">
            {TABS.map(({ id, label, icon: Icon }) => (
              <button
                key={id}
                ref={(el) => {
                  btnRefs.current[id] = el;
                }}
                type="button"
                className="tab"
                data-active={active === id}
                aria-current={active === id ? "page" : undefined}
                onClick={() => onChange(id)}
              >
                <Icon />
                {label}
              </button>
            ))}
            {ink && (
              <span className="tab__ink" style={{ left: ink.left, width: ink.width }} />
            )}
          </div>

          <button
            type="button"
            className="iconbtn"
            onClick={toggleTheme}
            aria-label="Toggle theme"
            title={isDark ? "Switch to light" : "Switch to dark"}
          >
            {isDark ? <SunIcon /> : <MoonIcon />}
          </button>
        </div>
      </nav>

      {/* Fixed bottom tab bar (phones only). Thumb-reach navigation; the active
          tab is gold-tinted instead of carrying the desktop underline slider.
          Hidden at the same breakpoint where the top-bar tabs reappear. */}
      <nav className="navbar-bottom" aria-label="Primary">
        {TABS.map(({ id, label, icon: Icon }) => (
          <button
            key={id}
            type="button"
            className="navbar-bottom__tab"
            data-active={active === id}
            aria-current={active === id ? "page" : undefined}
            onClick={() => onChange(id)}
          >
            <Icon />
            <span>{label}</span>
          </button>
        ))}
      </nav>
    </>
  );
}
