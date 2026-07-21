// Pure navbar decisions, split out from NavBar.tsx so the tab set and the
// pathname → active-tab mapping are unit-tested in node without rendering.
// Icons stay in the component; this module only knows ids, labels, and paths.

export type Tab = "movies" | "users" | "stats" | "roster";

export interface TabDescriptor {
  id: Tab;
  label: string;
  path: "/" | "/users" | "/stats" | "/admin";
}

// Center-tab order, admin superset. Roster sits last, after Stats.
const ALL_TABS: TabDescriptor[] = [
  { id: "movies", label: "Movies", path: "/" },
  { id: "users", label: "Members", path: "/users" },
  { id: "stats", label: "Stats", path: "/stats" },
  { id: "roster", label: "Roster", path: "/admin" },
];

/**
 * The tabs a given actor sees. Roster is admin-only; a member (or a logged-out
 * actor whose role is still undefined) never gets it, so the link is hidden
 * rather than a dead entry they can't use.
 */
export function tabsForRole(role: string | undefined): TabDescriptor[] {
  return role === "admin" ? ALL_TABS : ALL_TABS.filter((t) => t.id !== "roster");
}

/**
 * The router is the source of truth for the active tab. Map the current
 * pathname back to a tab id ('/' → movies, '/admin' → roster) to drive the
 * active styling. Pages that live in the navbar chrome but aren't tabs (account
 * settings at /settings) and unknown paths return null, so no tab is
 * highlighted rather than falsely lighting up Movies.
 */
export function tabFromPath(pathname: string): Tab | null {
  if (pathname.startsWith("/admin")) return "roster";
  if (pathname.startsWith("/stats")) return "stats";
  if (pathname.startsWith("/users")) return "users";
  if (pathname === "/") return "movies";
  return null;
}
