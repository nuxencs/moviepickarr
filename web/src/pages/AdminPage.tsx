import { RosterSection } from "@/components/moviepickarr/admin/RosterSection";

/**
 * Route component for /admin/roster. Lazy-loaded (see StatsPage for the why).
 *
 * Invite state stays in each member's Login cell instead of repeating members
 * in a second page-level section.
 */
export function AdminPage() {
  return <RosterSection />;
}
