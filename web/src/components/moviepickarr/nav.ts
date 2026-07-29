// Pure navbar decisions, split out from NavBar.tsx so the tab set and the
// pathname → active-tab mapping are unit-tested in node without rendering.
// Icons stay in the component; this module only knows ids, labels, and paths.

export type Tab = "movies" | "users" | "stats" | "admin";

export interface TabDescriptor {
  id: Tab;
  label: string;
  path: "/" | "/users" | "/stats" | "/admin";
}

// Center-tab order, admin superset. Admin sits last, after Stats. The tab is
// named for the surface, not for the one collection it shows today: the roster
// is a set of members (CONTEXT.md), and pool locks and integrations land on the
// same page beside it.
const ALL_TABS: TabDescriptor[] = [
  { id: "movies", label: "Movies", path: "/" },
  { id: "users", label: "Members", path: "/users" },
  { id: "stats", label: "Stats", path: "/stats" },
  { id: "admin", label: "Admin", path: "/admin" },
];

/**
 * The tabs a given actor sees. Admin is admin-only; a member (or a logged-out
 * actor whose role is still undefined) never gets it, so the link is hidden
 * rather than a dead entry they can't use.
 */
export function tabsForRole(role: string | undefined): TabDescriptor[] {
  return role === "admin" ? ALL_TABS : ALL_TABS.filter((t) => t.id !== "admin");
}

/**
 * The router is the source of truth for the active tab. Map the current
 * pathname back to a tab id ('/' → movies, '/admin' → admin) to drive the
 * active styling. Pages that live in the navbar chrome but aren't tabs (account
 * settings at /settings) and unknown paths return null, so no tab is
 * highlighted rather than falsely lighting up Movies.
 */
export function tabFromPath(pathname: string): Tab | null {
  if (pathname.startsWith("/admin")) return "admin";
  if (pathname.startsWith("/stats")) return "stats";
  if (pathname.startsWith("/users")) return "users";
  if (pathname === "/") return "movies";
  return null;
}
