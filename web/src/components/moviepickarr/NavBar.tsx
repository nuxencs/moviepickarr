import { useQuery } from "@tanstack/react-query";
import { Link, useRouterState } from "@tanstack/react-router";
import { ChartNoAxesColumnIcon, FilmIcon, MoonIcon, ShieldIcon, SunIcon, UsersIcon } from "lucide-react";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

import { MeQueryOptions } from "@/api/queries";

import { VolumeControl } from "@/components/moviepickarr/VolumeControl";
import { useTheme } from "@/components/theme-context";

export type Tab = "movies" | "users" | "stats";

const TABS: { id: Tab; label: string; icon: typeof FilmIcon; path: "/" | "/users" | "/stats" }[] = [
  { id: "movies", label: "Movies", icon: FilmIcon, path: "/" },
  { id: "users", label: "Members", icon: UsersIcon, path: "/users" },
  { id: "stats", label: "Stats", icon: ChartNoAxesColumnIcon, path: "/stats" },
];

/** The router is the source of truth for the active tab now. Map the current
 *  pathname back to a tab id ('/' → movies) to drive the active styling. */
function tabFromPath(pathname: string): Tab {
  if (pathname.startsWith("/stats")) return "stats";
  if (pathname.startsWith("/users")) return "users";
  return "movies";
}

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

export function NavBar() {
  const { theme, setTheme } = useTheme();
  const isDark = resolveDark(theme);
  const active = useRouterState({ select: (s) => tabFromPath(s.location.pathname) });
  // The admin roster entry only appears for admins. A 401 (not logged in) leaves
  // role undefined, so the link is hidden, never a dead entry a member can't use.
  const { data: me } = useQuery(MeQueryOptions());
  const onAdmin = useRouterState({ select: (s) => s.location.pathname.startsWith("/admin") });

  // A single shared underline that slides between tabs, rather than one per tab
  // that unmounts/remounts on switch. We measure the active link and drive the
  // indicator's left/width; CSS transitions the move (and the reduced-motion
  // guard in index.css collapses it to an instant jump).
  const btnRefs = useRef<Record<Tab, HTMLAnchorElement | null>>({
    movies: null,
    users: null,
    stats: null,
  });
  const [ink, setInk] = useState<{ left: number; width: number } | null>(null);

  const measure = useCallback(() => {
    const btn = btnRefs.current[active];
    // Skip when the top-bar tabs are hidden (mobile bottom-bar layout): a
    // display:none link reports offsetWidth 0, which would park the slider
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
          <h1 className="wordmark">
            <Link
              to="/"
              className="wordmark__home"
              aria-current={active === "movies" ? "page" : undefined}
              title="Go to Movies"
            >
              <span className="mark">
                <FilmIcon />
              </span>
              moviepickarr
            </Link>
          </h1>

          {/* Top-bar tabs with the sliding underline (desktop / tablet). Hidden on
              phones, where navigation moves to the fixed bottom bar below. */}
          <div className="nav__tabs">
            {TABS.map(({ id, label, icon: Icon, path }) => (
              <Link
                key={id}
                to={path}
                ref={(el) => {
                  btnRefs.current[id] = el;
                }}
                className="tab"
                data-active={active === id}
                aria-current={active === id ? "page" : undefined}
              >
                <Icon />
                {label}
              </Link>
            ))}
            {ink && (
              <span className="tab__ink" style={{ left: ink.left, width: ink.width }} />
            )}
          </div>

          <div className="nav__actions">
            {me?.role === "admin" && (
              <Link
                to="/admin"
                className="iconbtn"
                data-active={onAdmin}
                aria-current={onAdmin ? "page" : undefined}
                aria-label="Roster"
                title="Roster (admin)"
              >
                <ShieldIcon />
              </Link>
            )}

            <VolumeControl />

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
        </div>
      </nav>

      {/* Fixed bottom tab bar (phones only). Thumb-reach navigation; the active
          tab is gold-tinted instead of carrying the desktop underline slider.
          Hidden at the same breakpoint where the top-bar tabs reappear. */}
      <nav className="navbar-bottom" aria-label="Primary">
        {TABS.map(({ id, label, icon: Icon, path }) => (
          <Link
            key={id}
            to={path}
            className="navbar-bottom__tab"
            data-active={active === id}
            aria-current={active === id ? "page" : undefined}
          >
            <Icon />
            <span>{label}</span>
          </Link>
        ))}
      </nav>
    </>
  );
}
