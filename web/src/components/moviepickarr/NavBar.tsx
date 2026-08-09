import { useQuery } from "@tanstack/react-query";
import { Link, useRouterState } from "@tanstack/react-router";
import { ChartNoAxesColumnIcon, FilmIcon as MovieIcon, ShieldIcon, UsersIcon } from "lucide-react";

import { MeQueryOptions } from "@/api/queries";

import { RadarrAttentionBadge } from "@/components/moviepickarr/admin/RadarrAttentionBadge";
import { type Tab, tabFromPath, tabsForRole } from "@/components/moviepickarr/nav";
import { ProfilePanel } from "@/components/moviepickarr/ProfilePanel";

import { useSlidingTabIndicator } from "@/hooks/useSlidingTabIndicator";

/** Icon per tab id; the pure nav module carries ids/labels/paths, not JSX. */
const TAB_ICONS: Record<Tab, typeof MovieIcon> = {
  movies: MovieIcon,
  users: UsersIcon,
  stats: ChartNoAxesColumnIcon,
  admin: ShieldIcon,
};

export function NavBar() {
  const active = useRouterState({ select: (s) => tabFromPath(s.location.pathname) });
  // The Admin tab only appears for admins. A 401 (not logged in) leaves role
  // undefined, so it's hidden, never a dead entry a member can't use.
  const { data: me } = useQuery(MeQueryOptions());
  const tabs = tabsForRole(me?.role);

  const { position: ink, setItemRef } = useSlidingTabIndicator(active, tabs.length);

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
                <MovieIcon />
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
                    setItemRef(id, el);
                  }}
                  className="tab"
                  data-active={active === id}
                  aria-current={active === id ? "page" : undefined}
                >
                  <Icon />
                  <span className="tab__label">
                    {label}
                    {id === "admin" ? <RadarrAttentionBadge /> : null}
                  </span>
                </Link>
              );
            })}
            {ink && (
              <span className="tab__ink" style={{ left: ink.left, width: ink.width }} />
            )}
          </div>

          <div className="nav__actions">
            {me && <ProfilePanel me={me} />}
          </div>
        </div>
      </nav>

      {/* Fixed bottom tab bar, shown below 900px. Thumb-reach navigation; the active
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
              <span className="navbar-bottom__label">
                {label}
                {id === "admin" ? <RadarrAttentionBadge /> : null}
              </span>
            </Link>
          );
        })}
      </nav>
    </>
  );
}
