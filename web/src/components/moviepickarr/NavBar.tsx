import { useQuery } from "@tanstack/react-query";
import { Link, useRouterState } from "@tanstack/react-router";
import { ChartNoAxesColumnIcon, FilmIcon, ShieldIcon, UsersIcon } from "lucide-react";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

import { MeQueryOptions } from "@/api/queries";

import { type Tab, tabFromPath, tabsForRole } from "@/components/moviepickarr/nav";
import { ProfilePanel } from "@/components/moviepickarr/ProfilePanel";
import { VolumeControl } from "@/components/moviepickarr/VolumeControl";

/** Icon per tab id; the pure nav module carries ids/labels/paths, not JSX. */
const TAB_ICONS: Record<Tab, typeof FilmIcon> = {
  movies: FilmIcon,
  users: UsersIcon,
  stats: ChartNoAxesColumnIcon,
  roster: ShieldIcon,
};

/** Horizontal inset (px) the underline keeps from each edge of the active tab. */
const INK_INSET = 12;

export function NavBar() {
  const active = useRouterState({ select: (s) => tabFromPath(s.location.pathname) });
  // The Roster tab only appears for admins. A 401 (not logged in) leaves role
  // undefined, so it's hidden, never a dead entry a member can't use.
  const { data: me } = useQuery(MeQueryOptions());
  const tabs = tabsForRole(me?.role);

  // A single shared underline that slides between tabs, rather than one per tab
  // that unmounts/remounts on switch. We measure the active link and drive the
  // indicator's left/width; CSS transitions the move (and the reduced-motion
  // guard in index.css collapses it to an instant jump).
  const btnRefs = useRef<Record<Tab, HTMLAnchorElement | null>>({
    movies: null,
    users: null,
    stats: null,
    roster: null,
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
  // tabs.length is a dep so the pass re-runs when the Roster tab appears once
  // `me` loads as admin (its ref is null on the first, roster-less render).
  useLayoutEffect(() => measure(), [measure, tabs.length]);

  // Re-measure on resize and once web fonts settle (font swap changes label width).
  useEffect(() => {
    window.addEventListener("resize", measure);
    document.fonts?.ready.then(measure);
    return () => window.removeEventListener("resize", measure);
  }, [measure]);

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
            {tabs.map(({ id, label, path }) => {
              const Icon = TAB_ICONS[id];
              return (
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
              );
            })}
            {ink && (
              <span className="tab__ink" style={{ left: ink.left, width: ink.width }} />
            )}
          </div>

          <div className="nav__actions">
            <VolumeControl />
            {me && <ProfilePanel me={me} />}
          </div>
        </div>
      </nav>

      {/* Fixed bottom tab bar (phones only). Thumb-reach navigation; the active
          tab is gold-tinted instead of carrying the desktop underline slider.
          Hidden at the same breakpoint where the top-bar tabs reappear. */}
      <nav className="navbar-bottom" aria-label="Primary">
        {tabs.map(({ id, label, path }) => {
          const Icon = TAB_ICONS[id];
          return (
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
          );
        })}
      </nav>
    </>
  );
}
