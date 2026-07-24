import { RosterPage } from "@/components/moviepickarr/admin/RosterPage";
import { Shell } from "@/components/moviepickarr/AppShell";

/** Route component for /admin. Lazy-loaded (see StatsPage for the why). */
export function AdminPage() {
  return (
    <Shell>
      <RosterPage />
    </Shell>
  );
}
