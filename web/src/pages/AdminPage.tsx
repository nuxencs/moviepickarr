import { RosterSection } from "@/components/moviepickarr/admin/RosterSection";
import { Shell } from "@/components/moviepickarr/AppShell";

/**
 * Route component for /admin. Lazy-loaded (see StatsPage for the why).
 *
 * The admin surface is a page of sections, the way Movies is (In the Pool,
 * Watched). Today it holds one: the roster. Pool locks and integrations become
 * siblings here, each its own `.sec-head` section.
 */
export function AdminPage() {
  return (
    <Shell>
      <RosterSection />
    </Shell>
  );
}
