import { RosterSection } from "@/components/moviepickarr/admin/RosterSection";
import { Shell } from "@/components/moviepickarr/AppShell";

/**
 * Route component for /admin. Lazy-loaded (see StatsPage for the why).
 *
 * Invite state stays in each member's Login cell instead of repeating members
 * in a second page-level section.
 */
export function AdminPage() {
  return (
    <Shell>
      <RosterSection />
    </Shell>
  );
}
