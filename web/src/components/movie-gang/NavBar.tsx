import { ChartNoAxesColumnIcon, FilmIcon, MoonIcon, SunIcon, UsersIcon } from "lucide-react";

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

export function NavBar({ active, onChange }: { active: Tab; onChange: (tab: Tab) => void }) {
  const { theme, setTheme } = useTheme();
  const isDark = resolveDark(theme);

  return (
    <nav className="nav">
      <div className="nav__inner">
        <div className="wordmark">
          <span className="mark">
            <FilmIcon />
          </span>
          <h1>Movie Gang</h1>
        </div>

        <div className="nav__tabs">
          {TABS.map(({ id, label, icon: Icon }) => (
            <button
              key={id}
              type="button"
              className="tab"
              data-active={active === id}
              onClick={() => onChange(id)}
            >
              <Icon />
              {label}
              {active === id && <span className="tab__ink" />}
            </button>
          ))}
        </div>

        <button
          type="button"
          className="iconbtn"
          onClick={() => setTheme(isDark ? "light" : "dark")}
          aria-label="Toggle theme"
          title={isDark ? "Switch to light" : "Switch to dark"}
        >
          {isDark ? <SunIcon /> : <MoonIcon />}
        </button>
      </div>
    </nav>
  );
}
