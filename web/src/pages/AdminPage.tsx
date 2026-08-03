import { InvitesSection } from "@/components/moviepickarr/admin/InvitesSection";
import { RosterSection } from "@/components/moviepickarr/admin/RosterSection";
import { Shell } from "@/components/moviepickarr/AppShell";

/**
 * Route component for /admin. Lazy-loaded (see StatsPage for the why).
 *
 * The admin surface is a page of sections, the way Movies is (In the Pool,
 * Watched). Pool locks and integrations become siblings here, each its own
 * `.sec-head` section.
 *
 * Invites sits above the roster and renders nothing at all when nobody is
 * waiting to set up a login, which on a settled instance is always. It leads
 * because it is the section with an expiry on it: a link nobody acted on stops
 * working, and the roster is not going anywhere.
 */
export function AdminPage() {
  return (
    <Shell>
      <InvitesSection />
      <RosterSection />
    </Shell>
  );
}
