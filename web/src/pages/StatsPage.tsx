import { Shell } from "@/components/moviepickarr/AppShell";
import { StatsTab } from "@/components/moviepickarr/StatsTab";

/**
 * Route component for /stats. Outside router.tsx so lazyRouteComponent keeps
 * StatsTab out of the entry bundle.
 */
export function StatsPage() {
  return (
    <Shell>
      <StatsTab />
    </Shell>
  );
}
