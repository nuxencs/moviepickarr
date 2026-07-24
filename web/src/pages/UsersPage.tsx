import { Shell } from "@/components/moviepickarr/AppShell";
import { UsersTab } from "@/components/moviepickarr/UsersTab";

/** Route component for /users. Lazy-loaded (see StatsPage for the why). */
export function UsersPage() {
  return (
    <Shell>
      <UsersTab />
    </Shell>
  );
}
