import { Shell } from "@/components/moviepickarr/AppShell";
import { StatsTab } from "@/components/moviepickarr/StatsTab";

/**
 * Route component for /stats. Lives outside router.tsx so the route can load it
 * with lazyRouteComponent: everything StatsTab drags in (charts, filter menus)
 * ships in this chunk instead of the entry bundle.
 */
export function StatsPage() {
  return (
    <Shell>
      <StatsTab />
    </Shell>
  );
}
