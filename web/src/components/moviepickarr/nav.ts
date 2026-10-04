// Pure navbar decisions, split out from NavBar.tsx so node tests need no render.

export type Tab = "movies" | "users" | "stats" | "admin";

export interface TabDescriptor {
  id: Tab;
  label: string;
  path: "/" | "/users" | "/stats" | "/admin";
}

const ALL_TABS: TabDescriptor[] = [
  { id: "movies", label: "Movies", path: "/" },
  { id: "users", label: "Members", path: "/users" },
  { id: "stats", label: "Stats", path: "/stats" },
  { id: "admin", label: "Admin", path: "/admin" },
];

/** The tabs an actor sees. An undefined role (logged out) gets no Admin tab. */
export function tabsForRole(role: string | undefined): TabDescriptor[] {
  return role === "admin" ? ALL_TABS : ALL_TABS.filter((t) => t.id !== "admin");
}

/**
 * The active tab for a pathname. Non-tab pages (/settings) and unknown paths
 * return null, so Movies does not light up falsely.
 */
export function tabFromPath(pathname: string): Tab | null {
  if (pathname.startsWith("/admin")) return "admin";
  if (pathname.startsWith("/stats")) return "stats";
  if (pathname.startsWith("/users")) return "users";
  if (pathname === "/") return "movies";
  return null;
}
